/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package certification

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/certification/util"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
	"github.com/mholt/archiver/v4"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
	// rbacName is the name of RBAC resources.
	rbacName = "couchbase-operator-certification"

	// certificationName is the name used by certification resources.
	certificationName = "certification"

	// artifactsName is the name used by resources used to extract artifacts.
	artifactsName = "artifacts"

	// serviceAccount is the default service account to use.
	serviceAccount = "couchbase-operator-certification"

	// resourceExistsMessage is used to provide a safe way of detecting existing runs
	// and not killing them.
	resourceExistsMessage = "requested resource already exists, ensure no one else is running the suite and rerun with --clean"

	// pullSecretLabel is the label applied to secrets created by the certification image for pulling private images.
	pullSecretLabel = "certification.couchbase.com/pull-secret"

	// pullSecretValue is the value given to the pullSecretLabel.
	pullSecretValue = "true"

	// namespaceLabel is the label to select certification namespaces.
	namespaceLabel = "couchbase-test"
)

var (
	// ErrNonZeroExit is returned when a "subprocess" exits reporting an error.
	ErrNonZeroExit = errors.New("terminated with non-zero exit code")

	// ErrInvalidRegistryString is returned when a registry string is provided that cannot be parsed.
	ErrInvalidRegistryString = errors.New("invalid cluster config value, expected SERVER,USERNAME,PASSWORD")

	// serviceAccountTemplate is the service account the certification pod runs under.
	serviceAccountTemplate = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: rbacName,
		},
	}

	// clusterRoleTemplate grants the certification pod all the things (for now...)
	clusterRoleTemplate = &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: rbacName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"*",
				},
				Resources: []string{
					"*",
				},
				Verbs: []string{
					"*",
				},
			},
		},
	}

	// clusterRoleBindingTemplate binds the cluster role to the certification service account.
	// TODO: the assumption is that everything runs on "default", however the Kubernetes CLI
	// library does allow the specification of --namespace, so we should fix this.
	clusterRoleBindingTemplate = &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: rbacName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     rbacName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind: "ServiceAccount",
				Name: rbacName,
			},
		},
	}

	// pvcTemplate is used to instantiate a PVC used to communicate artifacts between
	// the test pod and the one used to extract the artifacts.
	pvcTemplate = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: artifactsName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}

	// podTemplate is a genrica template for all tasks in the pipeline, it mounts the
	// PVC in the /artifacts directory.  We use Guaranteed QoS to prevent this pod
	// from getting evicted, essentially ensure all containers have CPU and memory
	// limits.
	podTemplate = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: certificationName,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name: certificationName,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      artifactsName,
							MountPath: "/artifacts",
						},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							// Fairly low as this is event driven.
							corev1.ResourceCPU: resource.MustParse("2"),
							// Current profiling shows this at ~1Gi with 8 way testing.
							corev1.ResourceMemory: resource.MustParse("3Gi"),
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: artifactsName,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: artifactsName,
						},
					},
				},
			},
		},
	}
)

// RegistryConfig defines a container image registry.  Registry configurations will
// automatically be added to all Operator/DAC deployments as image pull secrets.  They
// will be added to all Couchbase clusters also.  This allows testing of all assets
// from any private repository.
type RegistryConfig struct {
	// Server is the registry server to use e.g. "https://index.docker.io/v1/".
	Server string `json:"server"`

	// Username is the user/organization to authenticate as.
	Username string `json:"username"`

	// Password is the authentication password for the organization.
	Password string `json:"password"`
}

func (v *RegistryConfig) String() string {
	return fmt.Sprintf("%s,%s,%s", v.Server, v.Username, v.Password)
}

// RegistryConfigValue allows multiple container image registries to be passed on the command
// line e.g:
// --registry https://index.docker.io/v1/,organization,password.
type RegistryConfigValue struct {
	values []RegistryConfig
}

func (v *RegistryConfigValue) Set(value string) error {
	fields := strings.Split(value, ",")
	if len(fields) != 3 {
		return fmt.Errorf("%w", ErrInvalidRegistryString)
	}

	config := RegistryConfig{
		Server:   fields[0],
		Username: fields[1],
		Password: fields[2],
	}

	v.values = append(v.values, config)

	return nil
}

func (v *RegistryConfigValue) String() string {
	return ""
}

func (v *RegistryConfigValue) Type() string {
	return "string"
}

// archiveNameConfigValue is a bit of a hack, in order to have deterministic
// documentation, then it needs to be fixed, however, the ask was to have timestamps
// which quite frankly isn't... sigh.
type archiveNameConfigValue struct {
	name     string
	explicit bool
	ts       time.Time
}

func newArchiveNameConfigValue(name string, ts time.Time) archiveNameConfigValue {
	return archiveNameConfigValue{
		name: name,
		ts:   ts,
	}
}

func (v *archiveNameConfigValue) Set(value string) error {
	v.name = value
	v.explicit = true

	return nil
}

func (v *archiveNameConfigValue) String() string {
	return v.name
}

func (v *archiveNameConfigValue) Type() string {
	return "string"
}

func (v *archiveNameConfigValue) archiveName() string {
	if !v.explicit {
		return fmt.Sprintf("%s-%s.tar.bz2", v.name, v.ts.Format("20060102T150405-0700"))
	}

	return fmt.Sprintf("%s.tar.bz2", v.name)
}

type profileDirConfigValue struct {
	name     string
	explicit bool
	ts       time.Time
}

func newProfileDirConfigValue(name string, ts time.Time) profileDirConfigValue {
	return profileDirConfigValue{
		name: name,
		ts:   ts,
	}
}

func (v *profileDirConfigValue) Set(value string) error {
	v.name = value
	v.explicit = true

	return nil
}

func (v *profileDirConfigValue) String() string {
	return v.name
}

func (v *profileDirConfigValue) Type() string {
	return "string"
}

func (v *profileDirConfigValue) profileDir() string {
	if !v.explicit {
		return fmt.Sprintf("%s-%s", v.name, v.ts.Format("20060102T150405-0700"))
	}

	return v.name
}

// getCertifyCommand returns a new Cobra certification command.
func getCertifyCommand(flags *genericclioptions.ConfigFlags) *cobra.Command {
	ts := time.Now()

	o := certifyOptions{
		archiveName: newArchiveNameConfigValue("couchbase-operator-certification", ts),
		profileDir:  newProfileDirConfigValue("couchbase-operator-certification-profile", ts),
	}

	cmd := &cobra.Command{
		Use:   "certify",
		Short: "Runs the platform certification suite",
		Long: normalize(`
			Runs the platform certification suite

			It's impossible to officially test every combination of Kubernetes
			platform, CNI and CSI plugin in order to give confidence that your
			specific combination will work as intended with the Operator.  To
			this end, the certify command will run a platform certification
			subset of the official Operator tests to give confidence that your
			plaform will work in a safe and supportable manner with managed
			Couchbase Server.

			The certification process is relatively invasive, so we recommend
			that this command be executed on a dedicated test Kubernetes cluster
			and not a production one.

			The certification process requires that it be allowed to create
			and delete namespaces in order to facilitate testing concurrently.
			It also requires permission to create roles and rolebindings in
			order to deploy the operator and dynamic admission controller.
			As such it will not be able to run without cluster wide roles that
			allow such functionality.

			Resource access is scoped so that only couchbase.com CRDs are managed
			and namespace with the name 'test-*'.

			When running on a platform with Istio network service mesh, the
			dynamic admission controller will be installed into the default
			namespace, and MUST NOT have Istio injection enabled.  The
			certification image MUST be installed in a non-default namespace
			with Istio injecton enabled.
		`),
		Example: normalize(`
			# Run platform certification with defaults
			cao certify

			# Run platform certification with a custom storage class
			cao certify -storage-class my-class

			# Run platform certification with private image repository
			cao certify --registry=https://index.docker.io/v1/,username,password

			# Run certification on an Istio enabled platform.
			cao certify --namespace istio-enabled-namespace -- -istio
		`),
		Run: func(cmd *cobra.Command, args []string) {
			if err := o.certify(flags, args); err != nil {
				if !errors.Is(err, ErrNonZeroExit) {
					fmt.Println("Certification error:", err)
				}
			}
		},
	}

	cmd.Flags().StringVar(&o.image, "image", imageDefault, "Certification image to use")
	cmd.Flags().StringVar(&o.timeout, "timeout", "12h", "Maximum runtime to allow.  4h is enough for all tests on most platforms with 8 way concurrency.  It may take over a day running with 1 way concurrency")
	cmd.Flags().Var(&o.archiveName, "archive-name", "Set the default test archive name")
	cmd.Flags().IntVar(&o.parallel, "parallel", 1, "Controls how many tests are executed concurrently.  This value should be based on the size of your kubernetes cluster.  See our documention at https://docs.couchbase.com/operator/current/concept-platform-certification.html#platform-requirements for help on understanding what parallelism to utilize.")
	cmd.Flags().BoolVar(&o.clean, "clean", false, "Force a cleanup of existing resources on start up.  These may have been left over from an earlier aborted run")
	cmd.Flags().BoolVar(&o.useFSGroup, "use-fsgroup", useFSGroup, "Use a file system group for persistent volumes.")
	cmd.Flags().IntVar(&o.fsGroup, "fsgroup", 1000, "Set the file system group for persistent volumes.")
	cmd.Flags().Var(&o.registries, "registry", "Allows container image registry configuration e.g. SERVER,USERNAME,PASSWORD.  This will be added as an image pull secret.  Can be specified multiple times.")
	cmd.Flags().StringVar(&o.imagePullPolicy, "image-pull-policy", imagePullPolicyDefault, "Pull Policy to use when downloading the Certification container")
	cmd.Flags().IntVar(&o.CollectedLogLevel, "collected-log-level", 0, "Log level to be collected by cbopinfo")

	// Setup shared flags
	o.BindSharedFlags(cmd.Flags())

	// Secret hidden things for internal use.
	cmd.Flags().BoolVar(&o.profile, "profile", false, "Collect pprof profiling data")
	_ = cmd.Flags().MarkHidden("profile")

	cmd.Flags().Var(&o.profileDir, "profile-dir", "Directory to store profling result in")
	_ = cmd.Flags().MarkHidden("profile-dir")

	cmd.Flags().BoolVar(&o.debug, "debug", false, "Collect verbose tool information")
	_ = cmd.Flags().MarkHidden("debug")

	cmd.Flags().StringVar(&o.verifyFile, "verify", "", "Verify a certification output's checksum")
	_ = cmd.Flags().MarkHidden(("verify"))

	return cmd
}

// initializeRuntime creates any runtime objects we need in the certification steps.
func (o *certifyOptions) initializeRuntime(flags *genericclioptions.ConfigFlags) error {
	util.PrintLine(util.PrettyHeading("Initializing ..."))

	configLoader := flags.ToRawKubeConfigLoader()

	config, err := configLoader.ClientConfig()
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	mc, err := metricsclient.NewForConfig(config)
	if err != nil {
		return err
	}

	o.config = config
	o.client = client
	o.metricsclient = mc
	o.namespace, _, _ = configLoader.Namespace()

	return nil
}

func (o *certifyOptions) deleteServiceAccount() {
	if _, err := o.client.CoreV1().ServiceAccounts(o.namespace).Get(context.TODO(), serviceAccountTemplate.Name, metav1.GetOptions{}); err != nil {
		return
	}

	util.PrintLine("Deleting service account ...")

	if err := o.client.CoreV1().ServiceAccounts(o.namespace).Delete(context.TODO(), serviceAccountTemplate.Name, metav1.DeleteOptions{}); err != nil {
		util.PrintError("Error deleting service account ...", err)
	}
}

func (o *certifyOptions) createServiceAccount() (func(), error) {
	util.PrintLine("Creating service account ...")

	if _, err := o.client.CoreV1().ServiceAccounts(o.namespace).Create(context.TODO(), serviceAccountTemplate, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("%s: %w", resourceExistsMessage, err)
	}

	cleanup := func() {
		o.deleteServiceAccount()
	}

	return cleanup, nil
}

func (o *certifyOptions) deleteClusterRole() {
	if _, err := o.client.RbacV1().ClusterRoles().Get(context.TODO(), clusterRoleTemplate.Name, metav1.GetOptions{}); err != nil {
		return
	}

	util.PrintLine("Deleting cluster role ...")

	if err := o.client.RbacV1().ClusterRoles().Delete(context.TODO(), clusterRoleTemplate.Name, metav1.DeleteOptions{}); err != nil {
		util.PrintError("Error deleting cluster role ...", err)
	}
}

func (o *certifyOptions) createClusterRole() (func(), error) {
	util.PrintLine("Creating cluster role ...")

	if _, err := o.client.RbacV1().ClusterRoles().Create(context.TODO(), clusterRoleTemplate, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("%s: %w", resourceExistsMessage, err)
	}

	cleanup := func() {
		o.deleteClusterRole()
	}

	return cleanup, nil
}

func (o *certifyOptions) deleteClusterRoleBinding() {
	if _, err := o.client.RbacV1().ClusterRoleBindings().Get(context.TODO(), clusterRoleBindingTemplate.Name, metav1.GetOptions{}); err != nil {
		return
	}

	util.PrintLine("Deleting cluster role binding ...")

	if err := o.client.RbacV1().ClusterRoleBindings().Delete(context.TODO(), clusterRoleBindingTemplate.Name, metav1.DeleteOptions{}); err != nil {
		util.PrintError("Error deleting cluster role binding ...", err)
	}
}

func (o *certifyOptions) createClusterRoleBinding() (func(), error) {
	util.PrintLine("Creating cluster role binding ...")

	clusterRoleBinding := clusterRoleBindingTemplate.DeepCopy()
	clusterRoleBinding.Subjects[0].Namespace = o.namespace

	if _, err := o.client.RbacV1().ClusterRoleBindings().Create(context.TODO(), clusterRoleBinding, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("%s: %w", resourceExistsMessage, err)
	}

	cleanup := func() {
		o.deleteClusterRoleBinding()
	}

	return cleanup, nil
}

func (o *certifyOptions) deleteVolume() {
	if _, err := o.client.CoreV1().PersistentVolumeClaims(o.namespace).Get(context.TODO(), pvcTemplate.Name, metav1.GetOptions{}); err != nil {
		return
	}

	util.PrintLine("Deleting artifacts volume ...")

	if err := o.client.CoreV1().PersistentVolumeClaims(o.namespace).Delete(context.TODO(), pvcTemplate.Name, metav1.DeleteOptions{}); err != nil {
		util.PrintError("Error deleting artifacts volume ...", err)
	}

	callback := func() error {
		return util.VolumeDeleted(o.client, o.namespace, pvcTemplate.Name)
	}

	if err := util.WaitFor(callback, 5*time.Minute); err != nil {
		util.PrintError("Timeout waiting for artifact volume to be deleted ...", err)
	}
}

// deleteNamespaces cleans up namespaces created by certification testing.
func (o *certifyOptions) deleteNamespaces() {
	selector, _ := labels.NewRequirement("app", selection.Equals, []string{namespaceLabel})

	namespaces, err := o.client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		util.PrintError("Error retrieving namespaces ...", err)
	}

	for _, namespace := range namespaces.Items {
		if err := o.client.CoreV1().Namespaces().Delete(context.Background(), namespace.Name, *metav1.NewDeleteOptions(0)); err != nil {
			util.PrintError("Error deleting namespaces ...", err)
		}
	}
}

// createVolume creates a workspace volume to communicate results from the certification
// container to the artifacts one.
func (o *certifyOptions) createVolume(storageClassName string) (func(), error) {
	util.PrintLine("Creating artifacts volume ...")

	if storageClassName != "" {
		pvcTemplate.Spec.StorageClassName = &storageClassName
	}

	if _, err := o.client.CoreV1().PersistentVolumeClaims(o.namespace).Create(context.TODO(), pvcTemplate, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("%s: %w", resourceExistsMessage, err)
	}

	cleanup := func() {
		o.deleteVolume()
	}

	return cleanup, nil
}

// RecreateDockerAuthSecret deletes existing secrets and creates a new one if specified.
// This secret, if defined, will be added to the operator and admission controllers in
// order to pull from a private repository.
func (o *certifyOptions) createPullSecrets() ([]string, func(), error) {
	util.PrintLine("Creating pull secrets ...")

	cleanup := func() {
		o.deletePullSecrets()
	}

	pullSecrets := make([]string, len(o.registries.values))

	// If specified create the authentication secrets
	for i, registry := range o.registries.values {
		// auth string is simply "username:password" base64 encoded
		auth := registry.Username + ":" + registry.Password
		auth = base64.StdEncoding.EncodeToString([]byte(auth))

		// authentication data is encoded as per "~/.docker/config.json", and created by "docker login"
		data := `{"auths":{"` + registry.Server + `":{"auth":"` + auth + `"}}}`

		// create the new secret
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-docker-pull-secret-",
				Labels: map[string]string{
					pullSecretLabel: pullSecretValue,
				},
			},
			Type: corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				".dockerconfigjson": []byte(data),
			},
		}

		newSecret, err := o.client.CoreV1().Secrets(o.namespace).Create(context.Background(), secret, metav1.CreateOptions{})
		if err != nil {
			return nil, cleanup, err
		}

		// Register that we have a pull secret, this will be used for all couchbase
		// clusters and deployments.
		pullSecrets[i] = newSecret.Name
	}

	return pullSecrets, cleanup, nil
}

func (o *certifyOptions) deletePullSecrets() {
	util.PrintLine("Deleting pull secrets ...")

	requirement, err := labels.NewRequirement(pullSecretLabel, selection.Equals, []string{pullSecretValue})
	if err != nil {
		util.PrintError("Error retrieving pull secrets ...", err)
	}

	selector := labels.NewSelector().Add(*requirement).String()

	if err := o.client.CoreV1().Secrets(o.namespace).DeleteCollection(context.TODO(), *metav1.NewDeleteOptions(0), metav1.ListOptions{LabelSelector: selector}); err != nil {
		util.PrintError("Error deleting pull secrets ...", err)
	}
}

// createCertificationPod is responsible for creating the certification pod and acting
// as an interface between this command's parameters and the actual container's.
func (o *certifyOptions) createCertificationPod(args []string, secrets []string) (func(), error) {
	util.PrintLine("Creating certification pod ...")

	certificationArgs := []string{
		"-test.v",
		"-test.run",
		"TestOperator",
		"-test.timeout",
		o.timeout,
		"-test.parallel",
		strconv.Itoa(o.parallel),
		"-parallelism",
		strconv.Itoa(o.parallel),
		"-color",
		"-collect-logs",
		"-collect-server-logs",
		"-collected-log-level",
		strconv.Itoa(o.CollectedLogLevel),
	}

	certificationArgs = append(certificationArgs, args...)

	certificationPod := podTemplate.DeepCopy()

	for _, registry := range o.registries.values {
		certificationArgs = append(certificationArgs, "-registry", registry.String())
	}

	extraArgs := getOverrides(o.SharedTestFlags, args)
	certificationArgs = append(certificationArgs, extraArgs...)

	certificationPod.Spec.ServiceAccountName = serviceAccount
	certificationPod.Spec.Containers[0].Image = o.image
	certificationPod.Spec.Containers[0].ImagePullPolicy = corev1.PullPolicy(o.imagePullPolicy)
	certificationPod.Spec.Containers[0].Args = certificationArgs

	for _, secret := range secrets {
		reference := corev1.LocalObjectReference{
			Name: secret,
		}

		certificationPod.Spec.ImagePullSecrets = append(certificationPod.Spec.ImagePullSecrets, reference)
	}

	// All our images should be run as non root as we have control.
	runAsNonRoot := true
	certificationPod.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRoot,
	}

	if o.useFSGroup {
		fsGroup := int64(o.fsGroup)
		certificationPod.Spec.SecurityContext.FSGroup = &fsGroup
	}

	if _, err := o.client.CoreV1().Pods(o.namespace).Create(context.TODO(), certificationPod, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("%s: %w", resourceExistsMessage, err)
	}

	cleanup := func() {
		util.PrintLine("Deleting certification pod ...")

		if err := o.client.CoreV1().Pods(o.namespace).Delete(context.TODO(), certificationPod.Name, metav1.DeleteOptions{}); err != nil {
			util.PrintError("Error deleting certification pod ...", err)
		}
	}

	return cleanup, nil
}

// waitCertificationPodReady waits for the certification pod to report as ready,
// thus running, and thus ready for log streaming.
func (o *certifyOptions) waitCertificationPodReady() error {
	util.PrintLine("Waiting for certification pod to become ready ...")

	callback := func() error {
		return util.PodReady(o.client, o.namespace, certificationName)
	}

	return util.WaitFor(callback, 5*time.Minute)
}

// streamLogs provides "live" streaming of logs from the certification container.
// This is useful for everyone as it shows it's doing something, and you can abort
// early if you've gubbed everything.
func (o *certifyOptions) streamLogs(since *metav1.Time) error {
	options := &corev1.PodLogOptions{
		Follow:    true,
		SinceTime: since,
		Container: certificationName,
	}

	request := o.client.CoreV1().Pods(o.namespace).GetLogs(certificationName, options)

	readCloser, err := request.Stream(context.TODO())
	if err != nil {
		return err
	}

	defer readCloser.Close()

	r := bufio.NewReader(readCloser)

	for {
		bytes, err := r.ReadBytes('\n')
		util.WriteToStdout(bytes)

		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}

			break
		}
	}

	return nil
}

// retryStreamLogs will attempt to stream logs as long as the pod is still running.
// There may be a disturbance in the Force that cuts comms prematurely and we don't
// want to abort the test run and waste all that time.
func (o *certifyOptions) retryStreamLogs() {
	var since *metav1.Time

	for {
		if err := util.ContainerCompleted(o.client, o.namespace, certificationName, o.debug); err == nil {
			break
		}

		util.PrintLine("Certification pod running, streaming logs ...")

		if err := o.streamLogs(since); err != nil {
			util.PrintError("Log stream terminated with error:", err)
		}

		since = &metav1.Time{
			Time: time.Now(),
		}
	}
}

// waitCertificationPodCompletion is triggered when the log streaming is terminated,
// implying the container has also finished.  As an extra safety measure this ensures
// it actually has completed before continuing.
func (o *certifyOptions) waitCertificationPodCompletion() (int32, error) {
	util.PrintLine("Waiting for certification pod to complete ...")

	var exitCode int32

	callback := func() error {
		return util.ContainerCompleted(o.client, o.namespace, certificationName, o.debug)
	}

	if err := util.WaitFor(callback, 5*time.Minute); err != nil {
		if pod, err := o.client.CoreV1().Pods(o.namespace).Get(context.TODO(), certificationName, metav1.GetOptions{}); err == nil {
			util.PrintLine(fmt.Sprintf("%+v", pod))
		}

		return -1, err
	}

	util.PrintLine(fmt.Sprintf("Certification exit code: %d", exitCode))

	return exitCode, nil
}

// deleteCertificationPod removes the certification pod.  Some platforms or CSI plugins
// behave differently, so kill off the pods entirely so the PVC is available to be mounted
// in the artifacts extraction container.
func (o *certifyOptions) deleteCertificationPod() error {
	util.PrintLine("Deleting certification pod ...")

	return o.client.CoreV1().Pods(o.namespace).Delete(context.TODO(), certificationName, metav1.DeleteOptions{})
}

func (o *certifyOptions) deleteArtifactsPod() {
	if _, err := o.client.CoreV1().Pods(o.namespace).Get(context.TODO(), artifactsName, metav1.GetOptions{}); err != nil {
		return
	}

	util.PrintLine("Deleting artifact pod ...")

	if err := o.client.CoreV1().Pods(o.namespace).Delete(context.TODO(), artifactsName, metav1.DeleteOptions{}); err != nil {
		util.PrintError("Error deleting artifact pod ...", err)
	}
}

// createArtifactsPod creates a basic pod that can consume artifacts generated by the
// main certification pod.
func (o *certifyOptions) createArtifactsPod(secrets []string) (func(), error) {
	util.PrintLine("Creating artifact pod ...")

	artifactPod := podTemplate.DeepCopy()
	artifactPod.Name = artifactsName
	artifactPod.Spec.Containers[0].Image = o.image
	artifactPod.Spec.Containers[0].Command = []string{
		"/bin/sleep",
	}
	artifactPod.Spec.Containers[0].Args = []string{
		"3600",
	}

	for _, secret := range secrets {
		reference := corev1.LocalObjectReference{
			Name: secret,
		}

		artifactPod.Spec.ImagePullSecrets = append(artifactPod.Spec.ImagePullSecrets, reference)
	}

	// I'm having issues finding a suitable non root image with tar in it, so as a
	// compromise, we'll just force the container into a specific non-root user.
	// All our images should be run as non root as we have control.
	runAsNonRoot := true
	artifactPod.Spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRoot,
	}

	if o.useFSGroup {
		fsGroup := int64(o.fsGroup)
		artifactPod.Spec.SecurityContext.FSGroup = &fsGroup
	}

	if _, err := o.client.CoreV1().Pods(o.namespace).Create(context.TODO(), artifactPod, metav1.CreateOptions{}); err != nil {
		return nil, err
	}

	cleanup := func() {
		o.deleteArtifactsPod()
	}

	return cleanup, nil
}

// waitArtifactsPodReady waits for the artifacts pod to be running before we attempt to
// shell into it to extract the artifacts archive.
func (o *certifyOptions) waitArtifactsPodReady() error {
	util.PrintLine("Waiting for artifact pod to become ready ...")

	callback := func() error {
		return util.PodReady(o.client, o.namespace, artifactsName)
	}

	if err := util.WaitFor(callback, 5*time.Minute); err != nil {
		if pod, err := o.client.CoreV1().Pods(o.namespace).Get(context.TODO(), artifactsName, metav1.GetOptions{}); err == nil {
			util.PrintLine(fmt.Sprintf("%+v", pod))
		}

		return err
	}

	return nil
}

// downloadArtifacts basically does what `kubectl cp` does, executes tar in the container
// and streams the contents out via stdout.  We buffer the archive stream and write it
// out locally.
func (o *certifyOptions) downloadArtifacts() error {
	util.PrintLine("Copying artifacts from artifact pod ...")

	execRequest := o.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(artifactsName).
		Namespace(o.namespace).
		SubResource("exec").
		Param("container", certificationName)

	execRequest.VersionedParams(&corev1.PodExecOptions{
		Container: certificationName,
		Command: []string{
			"tar",
			"-cj",
			"/artifacts",
		},
		Stdout: true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(o.config, "POST", execRequest.URL())
	if err != nil {
		return err
	}

	var stdout bytes.Buffer

	if err := exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{Stdout: &stdout}); err != nil {
		return err
	}

	return os.WriteFile(o.archiveName.archiveName(), stdout.Bytes(), 0644)
}

// startProfiling begins periodic profiling of the certification container
// using the magic of pprof.
func (o *certifyOptions) startProfiling() {
	if !o.profile {
		return
	}

	o.profilerStopChan = make(chan interface{})
	o.profilerStoppedChan = make(chan interface{})

	go func() {
		tick := time.NewTicker(time.Minute)

		defer tick.Stop()

		for {
			select {
			case <-o.profilerStopChan:
				close(o.profilerStoppedChan)

				return

			case <-tick.C:
				o.profileCertification()
			}
		}
	}()
}

// profileCertification collects process and memory statistics from
// the certification process so we capture any routine or memory leaks,
// or just generally capture resource utilization.
func (o *certifyOptions) profileCertification() {
	restorer := portforward.Silent()
	defer restorer()

	pf := portforward.PortForwarder{
		Config:    o.config,
		Client:    o.client,
		Namespace: o.namespace,
		Pod:       certificationName,
		Port:      "6060",
	}

	if err := pf.ForwardPorts(); err != nil {
		return
	}

	defer pf.Close()

	endpoints := []string{
		"goroutine",
		"heap",
		"allocs",
		"block",
		"mutex",
		"threads",
	}

	dir := filepath.Join(o.profileDir.profileDir(), time.Now().Format("20060102T150405-0700"))

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}

	for _, endpoint := range endpoints {
		data, err := gatherProfileData(endpoint)
		if err != nil {
			continue
		}

		file := filepath.Join(dir, endpoint)

		if err := os.WriteFile(file, data, 0640); err != nil {
			continue
		}
	}

	metrics, err := o.gatherPodMetrics()
	if err == nil {
		file := filepath.Join(dir, "metrics")

		_ = os.WriteFile(file, metrics, 0640)
	}
}

// gatherProfileData grabs raw pprof data from a specified endpoint.
func gatherProfileData(endpoint string) ([]byte, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:6060/debug/pprof/%s", endpoint))
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return data, err
}

// gatherPodMetrics grabs raw pod metrics (CPU/memory as seen by Kubernetes) from
// the metrics service, if it happens to be running on your cluster.
func (o *certifyOptions) gatherPodMetrics() ([]byte, error) {
	podMetrics, err := o.metricsclient.MetricsV1beta1().PodMetricses(o.namespace).Get(context.Background(), certificationName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(podMetrics)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// endProfiling stops the profiling of the certification container.
func (o *certifyOptions) endProfiling() {
	if !o.profile {
		return
	}

	close(o.profilerStopChan)

	<-o.profilerStoppedChan
}

func (o *certifyOptions) isParallelDefaulted() bool {
	defaulted := true

	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--parallel") {
			defaulted = false
		}
	}

	return defaulted
}

func (o *certifyOptions) prepareOutput() {
	util.PrintLine("Appending stdout to archive ...")

	// get stdout lines and get ready

	lines := ""

	for _, line := range util.GetStdout() {
		if line == "" {
			continue
		}

		if strings.HasSuffix(line, "\n") {
			lines += line
		} else {
			lines += line + "\n"
		}
	}

	// create temp file with stdout name
	tempFile, err := os.CreateTemp("", "stdout.txt")
	if err != nil {
		fmt.Println("Unable to create temporary file")
	}

	defer tempFile.Close()

	if _, err := tempFile.Write([]byte(lines)); err != nil {
		fmt.Println(err)
	}

	// Append file to archive
	tempArchive, err := os.CreateTemp("", "results.tar.bz")
	if err != nil {
		fmt.Println(err)
	}

	defer tempArchive.Close()

	input, err := os.ReadFile(o.archiveName.archiveName())
	if err != nil {
		fmt.Println(err)
	}

	err = os.WriteFile(tempArchive.Name(), input, 0644)
	if err != nil {
		fmt.Println(err)
	}

	// create archive
	format := archiver.CompressedArchive{
		Compression: archiver.Bz2{},
		Archival:    archiver.Tar{},
	}

	files, err := archiver.FilesFromDisk(nil, map[string]string{
		tempFile.Name():    "stdout.txt",
		tempArchive.Name(): "results.tar.bz",
	})
	if err != nil {
		fmt.Println(err)
	}

	output, err := os.OpenFile(o.archiveName.archiveName(), os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		fmt.Println(err)
	}

	err = output.Truncate(0)
	if err != nil {
		fmt.Println(err)
	}

	_, err = output.Seek(0, 0)
	if err != nil {
		fmt.Println(err)
	}

	err = format.Archive(context.Background(), output, files)
	if err != nil {
		fmt.Println(err)
	}

	// let's add checksums
	hash := sha256.New()
	stdoutHash := sha256.New()
	resultsHash := sha256.New()

	fsys, err := archiver.FileSystem(context.TODO(), o.archiveName.archiveName())
	if err != nil {
		fmt.Println(err)
	}

	stdout, err := fsys.Open("stdout.txt")
	if err != nil {
		fmt.Println(err)
	}

	archive, err := fsys.Open("results.tar.bz")
	if err != nil {
		fmt.Println(err)
	}

	_, err = io.Copy(stdoutHash, stdout)
	if err != nil {
		fmt.Println(err)
	}

	_, err = io.Copy(resultsHash, archive)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Fprintf(hash, "%x %s \n", stdoutHash.Sum(nil), "stdout.txt")
	fmt.Fprintf(hash, "%x %s \n", resultsHash.Sum(nil), "results.tar.bz")
	// creating hash file to add to final archive
	tempHashFile, err := os.CreateTemp("", "hash-*")
	if err != nil {
		fmt.Println("Unable to create temporary file")
	}

	defer tempHashFile.Close()

	if _, err := tempHashFile.Write([]byte(base64.StdEncoding.EncodeToString(hash.Sum(nil)))); err != nil {
		fmt.Println(err)
	}

	fmt.Println("Results Checksum: " + base64.StdEncoding.EncodeToString(hash.Sum(nil)))

	files, err = archiver.FilesFromDisk(nil, map[string]string{
		tempFile.Name():     "stdout.txt",
		tempArchive.Name():  "results.tar.bz",
		tempHashFile.Name(): "checksum.txt",
	})
	if err != nil {
		fmt.Println(err)
	}

	output, err = os.OpenFile(o.archiveName.archiveName(), os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		fmt.Println(err)
	}

	err = output.Truncate(0)
	if err != nil {
		fmt.Println(err)
	}

	_, err = output.Seek(0, 0)
	if err != nil {
		fmt.Println(err)
	}

	err = format.Archive(context.Background(), output, files)
	if err != nil {
		fmt.Println(err)
	}
}

var (
	ErrInvalidOutputArchive = fmt.Errorf("file to be verified is not a valid certification archive")
	ErrMissingFile          = fmt.Errorf("error reading certification archive - files missing")
	ErrNotValid             = fmt.Errorf("archive checksum was not able to be verified")
)

func (o *certifyOptions) verifyOutput() error {
	if !strings.HasSuffix(o.verifyFile, "tar.bz2") {
		return ErrInvalidOutputArchive
	}

	fsys, err := archiver.FileSystem(context.TODO(), o.verifyFile)
	if err != nil {
		return err
	}

	if entries, err := fs.Glob(fsys, "stdout.txt"); err != nil || entries == nil {
		return ErrMissingFile
	}

	if entries, err := fs.Glob(fsys, "results.tar.bz"); err != nil || entries == nil {
		return ErrMissingFile
	}

	if entries, err := fs.Glob(fsys, "checksum.txt"); err != nil || entries == nil {
		return ErrMissingFile
	}

	stdout, err := fsys.Open("stdout.txt")
	if err != nil {
		return err
	}

	archive, err := fsys.Open("results.tar.bz")
	if err != nil {
		return err
	}

	checksum, err := fsys.Open("checksum.txt")
	if err != nil {
		return err
	}

	f, err := checksum.Stat()
	if err != nil {
		return err
	}

	size := f.Size()
	checksumBytes := make([]byte, size)

	_, err = checksum.Read(checksumBytes)
	if err != nil {
		return err
	}

	checksumString := string(checksumBytes)
	stdoutHash := sha256.New()
	resultsHash := sha256.New()

	_, err = io.Copy(stdoutHash, stdout)
	if err != nil {
		return err
	}

	_, err = io.Copy(resultsHash, archive)
	if err != nil {
		return err
	}

	hash := sha256.New()

	fmt.Fprintf(hash, "%x %s \n", stdoutHash.Sum(nil), "stdout.txt")
	fmt.Fprintf(hash, "%x %s \n", resultsHash.Sum(nil), "results.tar.bz")

	hashString := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	fmt.Println("Expected: " + checksumString)
	fmt.Println("   Found: " + hashString)

	if checksumString != hashString {
		return ErrNotValid
	}

	fmt.Println("Archive checksum is valid.")

	return nil
}

// certify runs the certification suite on the provided options.
func (o *certifyOptions) certify(flags *genericclioptions.ConfigFlags, args []string) error {
	if o.verifyFile != "" {
		return o.verifyOutput()
	}

	util.PrintLine(util.PrettyHeading("Pre-Run Parameter Checks"))

	if o.isParallelDefaulted() {
		util.PrintLine(fmt.Sprintf("Parallel = %d %s  (see cao certify --help)", o.parallel, util.PrettyResult(util.ResultTypeFail)))
	} else {
		util.PrintLine(fmt.Sprintf("Parallel = %d %s", o.parallel, util.PrettyResult(util.ResultTypePass)))
	}

	defer o.prepareOutput()
	// Create any runtime objects we need in the certification steps.
	if err := o.initializeRuntime(flags); err != nil {
		return err
	}

	if o.clean {
		util.PrintLine(util.PrettyHeading("Cleaning Environment ..."))

		o.deleteServiceAccount()
		o.deleteClusterRole()
		o.deleteClusterRoleBinding()
		o.deletePullSecrets()

		// Delete the pods first, the PVC wont terminate until it's unused.
		_ = o.deleteCertificationPod()
		o.deleteArtifactsPod()
		o.deleteVolume()

		// Test resources may be left around, occupying the whole platform so we need
		// to delete namespaces to free space, otherwise the certification container
		// cannot be scheduled.
		o.deleteNamespaces()
	}

	util.PrintLine(util.PrettyHeading("Creating Resources ..."))

	cleanServiceAccount, err := o.createServiceAccount()
	if err != nil {
		return err
	}

	defer cleanServiceAccount()

	cleanClusterRole, err := o.createClusterRole()
	if err != nil {
		return err
	}

	defer cleanClusterRole()

	cleanClusterRoleBinding, err := o.createClusterRoleBinding()
	if err != nil {
		return err
	}

	defer cleanClusterRoleBinding()

	// Create the artifacts volume.
	cleanVolume, err := o.createVolume(o.StorageClassName)
	if err != nil {
		return err
	}

	defer cleanVolume()

	// Create the pull secrets.
	secrets, cleanPullSecrets, err := o.createPullSecrets()
	if err != nil {
		return err
	}

	defer cleanPullSecrets()

	// Create the certification pod.
	cleanCertificationPod, err := o.createCertificationPod(args, secrets)
	if err != nil {
		return err
	}

	defer cleanCertificationPod()

	// Wait for the certification pod to start.
	if err := o.waitCertificationPodReady(); err != nil {
		if errors.Is(err, util.ErrStatusTerminated) {
			// Certification pod has already terminated stream all logs
			if err := o.streamLogs(nil); err != nil {
				util.PrintError("Log stream terminated with error:", err)
			}
		}

		return err
	}

	o.startProfiling()

	// Stream off the certification pod logs.
	o.retryStreamLogs()

	o.endProfiling()

	// Wait for the certification pod to properly complete and get
	// the exit code that will be propagated to this command's exit code.
	exitCode, err := o.waitCertificationPodCompletion()
	if err != nil {
		return err
	}

	// Delete the certification pod to free up the artifacts volume.
	if err := o.deleteCertificationPod(); err != nil {
		return err
	}

	// Create the artifacts volume.
	cleanArtifactsPod, err := o.createArtifactsPod(secrets)
	if err != nil {
		return err
	}

	defer cleanArtifactsPod()

	// Wait for the artifacts volume to start.
	if err := o.waitArtifactsPodReady(); err != nil {
		return err
	}

	// Extract the artifacts from the artifacts volume.
	if err := o.downloadArtifacts(); err != nil {
		return err
	}

	if exitCode != 0 {
		return fmt.Errorf("%w: %v", ErrNonZeroExit, exitCode)
	}

	return nil
}
