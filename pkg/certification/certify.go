package certification

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/certification/util"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	// rbacName is the name of RBAC resources.
	rbacName = "couchbase-platform-certification"

	// certificationName is the name used by certification resources.
	certificationName = "certification"

	// artifactsName is the name used by resources used to extract artifacts.
	artifactsName = "artifacts"

	// serviceAccount is the default service account to use.
	serviceAccount = "couchbase-platform-certification"

	// resourceExistsMessage is used to provide a safe way of detecting existing runs
	// and not killing them.
	resourceExistsMessage = "requested resource already exists, ensure no one else is running the suite and rerun with --clean"
)

var (
	// ErrNonZeroExit is returned when a "subprocess" exits reporting an error.
	ErrNonZeroExit = errors.New("terminated with non-zero exit code")

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
	// PVC in the /artifacts directory.
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
						Requests: corev1.ResourceList{
							// Fairly low as this is event driven.
							corev1.ResourceCPU: resource.MustParse("2"),
							// Has been seen as high as "2614504Ki".
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

// certifyOptions defines all options for a certification task.
type certifyOptions struct {
	// image is the certification image name.
	image string

	// parallel is the default parallelism (really concurrency) to use when running
	// test cases.
	parallel int

	// timeout is timeout for the certification process.
	timeout string

	// config is set at runtime and is the Kubernetes configuration.
	config *rest.Config

	// client is set at runtime and is a Kubernetes client.
	client kubernetes.Interface

	// namespace is set at runtime and is the namespace this is being run in.
	namespace string

	// clean assumes the previous run was aborted and there are some resources
	// left lying about.
	clean bool

	// archiveName allows us to set a unique archive name for each run, in case
	// there are any races.
	archiveName string
}

// getCertifyCommand returns a new Cobra certification command.
func getCertifyCommand(flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := certifyOptions{}

	archiveName := fmt.Sprintf("couchbase-platform-certification-%s", time.Now().Format("20060102T150405-0700"))

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
		`),
		Example: normalize(`
			# Run platform certification with defaults
			cao certify

			# Run platform certification with a custom storage class
			cao certify -- -storage-class my-class
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
	cmd.Flags().StringVar(&o.archiveName, "archive-name", archiveName, "Set the default test archive name")
	cmd.Flags().IntVar(&o.parallel, "parallel", 8, "Test concurrency")
	cmd.Flags().BoolVar(&o.clean, "clean", false, "Force a cleanup of existing resources on start up.  These may have been left over from an earlier aborted run")

	return cmd
}

// initializeRuntime creates any runtime objects we need in the certification steps.
func (o *certifyOptions) initializeRuntime(flags *genericclioptions.ConfigFlags) error {
	fmt.Println("Initializing ...")

	configLoader := flags.ToRawKubeConfigLoader()

	config, err := configLoader.ClientConfig()
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	o.config = config
	o.client = client
	o.namespace, _, _ = configLoader.Namespace()

	return nil
}

func (o *certifyOptions) deleteServiceAccount() {
	if _, err := o.client.CoreV1().ServiceAccounts(o.namespace).Get(context.TODO(), serviceAccountTemplate.Name, metav1.GetOptions{}); err != nil {
		return
	}

	fmt.Println("Deleting service account ...")

	if err := o.client.CoreV1().ServiceAccounts(o.namespace).Delete(context.TODO(), serviceAccountTemplate.Name, metav1.DeleteOptions{}); err != nil {
		fmt.Println(err)
	}
}

func (o *certifyOptions) createServiceAccount() (func(), error) {
	fmt.Println("Creating service account ...")

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

	fmt.Println("Deleting cluster role ...")

	if err := o.client.RbacV1().ClusterRoles().Delete(context.TODO(), clusterRoleTemplate.Name, metav1.DeleteOptions{}); err != nil {
		fmt.Println(err)
	}
}

func (o *certifyOptions) createClusterRole() (func(), error) {
	fmt.Println("Creating cluster role ...")

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

	fmt.Println("Deleting cluster role binding ...")

	if err := o.client.RbacV1().ClusterRoleBindings().Delete(context.TODO(), clusterRoleBindingTemplate.Name, metav1.DeleteOptions{}); err != nil {
		fmt.Println(err)
	}
}

func (o *certifyOptions) createClusterRoleBinding() (func(), error) {
	fmt.Println("Creating cluster role binding ...")

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

	fmt.Println("Deleting artifacts volume ...")

	if err := o.client.CoreV1().PersistentVolumeClaims(o.namespace).Delete(context.TODO(), pvcTemplate.Name, metav1.DeleteOptions{}); err != nil {
		fmt.Println(err)
	}

	callback := func() error {
		return util.VolumeDeleted(o.client, o.namespace, pvcTemplate.Name)
	}

	if err := util.WaitFor(callback, 5*time.Minute); err != nil {
		fmt.Println(err)
	}
}

// createVolume creates a workspace volume to communicate results from the certification
// container to the artifacts one.
func (o *certifyOptions) createVolume() (func(), error) {
	fmt.Println("Creating artifacts volume ...")

	if _, err := o.client.CoreV1().PersistentVolumeClaims(o.namespace).Create(context.TODO(), pvcTemplate, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("%s: %w", resourceExistsMessage, err)
	}

	cleanup := func() {
		o.deleteVolume()
	}

	return cleanup, nil
}

// createCertificationPod is responsible for creating the certification pod and acting
// as an interface between this command's parameters and the actual container's.
func (o *certifyOptions) createCertificationPod(args []string) (func(), error) {
	fmt.Println("Creating certification pod ...")

	certificationArgs := []string{
		"-test.v",
		"-test.run",
		"TestOperator",
		"-test.timeout",
		o.timeout,
		"-test.parallel",
		strconv.Itoa(o.parallel),
		"-color",
	}

	certificationArgs = append(certificationArgs, args...)

	certificationPod := podTemplate.DeepCopy()
	certificationPod.Spec.ServiceAccountName = serviceAccount
	certificationPod.Spec.Containers[0].Image = o.image
	certificationPod.Spec.Containers[0].Args = certificationArgs

	if _, err := o.client.CoreV1().Pods(o.namespace).Create(context.TODO(), certificationPod, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("%s: %w", resourceExistsMessage, err)
	}

	cleanup := func() {
		fmt.Println("Deleting certification pod ...")

		if err := o.client.CoreV1().Pods(o.namespace).Delete(context.TODO(), certificationPod.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Println(err)
		}
	}

	return cleanup, nil
}

// waitCertificationPodReady waits for the certification pod to report as ready,
// thus running, and thus ready for log streaming.
func (o *certifyOptions) waitCertificationPodReady() error {
	fmt.Println("Wating for certification pod to become ready ...")

	callback := func() error {
		return util.PodReady(o.client, o.namespace, certificationName)
	}

	if err := util.WaitFor(callback, 5*time.Minute); err != nil {
		return err
	}

	return nil
}

// streamLogs provides "live" streaming of logs from the certification container.
// This is useful for everyone as it shows it's doing something, and you can abort
// early if you've gubbed everything.
func (o *certifyOptions) streamLogs(since *metav1.Time) error {
	request := o.client.CoreV1().Pods(o.namespace).GetLogs(certificationName, &corev1.PodLogOptions{Follow: true, SinceTime: since})

	readCloser, err := request.Stream(context.TODO())
	if err != nil {
		return err
	}

	defer readCloser.Close()

	r := bufio.NewReader(readCloser)

	for {
		bytes, err := r.ReadBytes('\n')
		os.Stderr.Write(bytes)

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
		if err := util.PodCompleted(o.client, o.namespace, certificationName, nil); err == nil {
			break
		}

		fmt.Println("Certification pod running, streaming logs ...")

		if err := o.streamLogs(since); err != nil {
			fmt.Println("Log stream terminated with error:", err)
		}

		since = &metav1.Time{
			Time: time.Now(),
		}
	}
}

// waitCertificationPodCompletion is triggered when the log streaming is terminated,
// implying the container has also finished.  As an extra safetly measure this ensures
// it actually has completed before continuing.
func (o *certifyOptions) waitCertificationPodCompletion() (int32, error) {
	fmt.Println("Wating for certification pod to complete ...")

	var exitCode int32

	callback := func() error {
		return util.PodCompleted(o.client, o.namespace, certificationName, &exitCode)
	}

	if err := util.WaitFor(callback, 5*time.Minute); err != nil {
		if pod, err := o.client.CoreV1().Pods(o.namespace).Get(context.TODO(), certificationName, metav1.GetOptions{}); err == nil {
			fmt.Println(pod)
		}

		return -1, err
	}

	fmt.Println("Certification exit code", exitCode)

	return exitCode, nil
}

// deleteCertificationPod removes the certification pod.  Some platforms or CSI plugins
// behave differently, so kill off the pods entirely so the PVC is available to be mounted
// in the artifacts extraction container.
func (o *certifyOptions) deleteCertificationPod() error {
	fmt.Println("Deleting certification pod ...")

	return o.client.CoreV1().Pods(o.namespace).Delete(context.TODO(), certificationName, metav1.DeleteOptions{})
}

func (o *certifyOptions) deleteArtifactsPod() {
	if _, err := o.client.CoreV1().Pods(o.namespace).Get(context.TODO(), artifactsName, metav1.GetOptions{}); err != nil {
		return
	}

	fmt.Println("Deleting artifact pod ...")

	if err := o.client.CoreV1().Pods(o.namespace).Delete(context.TODO(), artifactsName, metav1.DeleteOptions{}); err != nil {
		fmt.Println(err)
	}
}

// createArtifactsPod creates a basic pod that can consume artifacts generated by the
// main certification pod.
func (o *certifyOptions) createArtifactsPod() (func(), error) {
	fmt.Println("Creating artifact pod ...")

	artifactPod := podTemplate.DeepCopy()
	artifactPod.Name = artifactsName
	artifactPod.Spec.Containers[0].Image = "busybox"
	artifactPod.Spec.Containers[0].Command = []string{
		"/bin/sleep",
	}
	artifactPod.Spec.Containers[0].Args = []string{
		"3600",
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
	fmt.Println("Wating for artifact pod to become ready ...")

	callback := func() error {
		return util.PodReady(o.client, o.namespace, artifactsName)
	}

	if err := util.WaitFor(callback, 5*time.Minute); err != nil {
		if pod, err := o.client.CoreV1().Pods(o.namespace).Get(context.TODO(), artifactsName, metav1.GetOptions{}); err == nil {
			fmt.Println(pod)
		}

		return err
	}

	return nil
}

// downloadArtifacts basically does what `kubectl cp` does, executes tar in the container
// and streams the contents out via stdout.  We buffer the archive stream and write it
// out locally.
func (o *certifyOptions) downloadArtifacts() error {
	fmt.Println("Copying artifacts from artifact pod ...")

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

	if err := exec.Stream(remotecommand.StreamOptions{Stdout: &stdout}); err != nil {
		return err
	}

	if err := ioutil.WriteFile(o.archiveName+".tar.bz2", stdout.Bytes(), 0644); err != nil {
		return err
	}

	return nil
}

// certify runs the certification suite on the provided options.
func (o *certifyOptions) certify(flags *genericclioptions.ConfigFlags, args []string) error {
	// Create any runtime objects we need in the certification steps.
	if err := o.initializeRuntime(flags); err != nil {
		return err
	}

	if o.clean {
		o.deleteServiceAccount()
		o.deleteClusterRole()
		o.deleteClusterRoleBinding()

		// Delete the pods first, the PVC wont terminate until it's unused.
		_ = o.deleteCertificationPod()
		o.deleteArtifactsPod()
		o.deleteVolume()
	}

	// TODO: Test resources may be left around, occupying the whole platform so we need
	//       to delete namespaces to free space, otherwise the certification container
	//       cannot be scheduled.  We should:
	//
	//       * Probably offer this functionality as a flag as above for parity.

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
	cleanVolume, err := o.createVolume()
	if err != nil {
		return err
	}

	defer cleanVolume()

	// Create the certification pod.
	cleanCertificationPod, err := o.createCertificationPod(args)
	if err != nil {
		return err
	}

	defer cleanCertificationPod()

	// Wait for the certification pod to start.
	if err := o.waitCertificationPodReady(); err != nil {
		return err
	}

	// Stream off the certification pod logs.
	o.retryStreamLogs()

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
	cleanArtifactsPod, err := o.createArtifactsPod()
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
