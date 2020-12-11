package e2e

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	operator_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	clientcmdapiv1 "k8s.io/client-go/tools/clientcmd/api/v1"

	"github.com/ghodss/yaml"
)

// lazyBoundStorageClass examines the requested storage class and returns true if
// the persistent volumes are bound when attached to a pod.
func lazyBoundStorageClass(t *testing.T, cluster *types.Cluster) bool {
	f := framework.Global

	// When explcitly stated, lookup the storage class.
	if f.StorageClassName != nil {
		sc, err := cluster.KubeClient.StorageV1().StorageClasses().Get(context.Background(), *f.StorageClassName, metav1.GetOptions{})
		if err != nil {
			e2eutil.Die(t, err)
		}

		return *sc.VolumeBindingMode == storagev1.VolumeBindingWaitForFirstConsumer
	}

	// When implicit (default), then lookup the default storage class that will
	// be used by Kubernetes.
	scs, err := cluster.KubeClient.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, sc := range scs.Items {
		value, ok := sc.Annotations["storageclass.kubernetes.io/is-default-class"]
		if !ok {
			continue
		}

		if value == "true" {
			return *sc.VolumeBindingMode == storagev1.VolumeBindingWaitForFirstConsumer
		}
	}

	return false
}

// supportsMultipleVolumeClaims returns true if multiple PVCs can be supported by a test.
// We can run the test if there is just a single node (minikube/minishift), all nodes are
// in the same zone (and thus all PVs will be scheduled in that zone), or lazy binding is
// enabled (the PVCs will be scheduled in the same zone as a pod).  Additionally for abnormal
// clusters we allow them to be used if explicitly stated in the cluster definition.
func supportsMultipleVolumeClaims(t *testing.T, cluster *types.Cluster) bool {
	return cluster.SupportsMultipleVolumeClaims ||
		e2eutil.MustNumNodes(t, cluster) == 1 ||
		mustNumAvailabilityZones(t, cluster) == 1 ||
		lazyBoundStorageClass(t, cluster)
}

// compoundError is a group of errors.
type compoundError struct {
	errs []error
}

func NewCompoundError(errs []error) error {
	return &compoundError{errs: errs}
}

func (e *compoundError) Error() string {
	errs := []string{}

	for _, err := range e.errs {
		errs = append(errs, err.Error())
	}

	return strings.Join(errs, "\n")
}

// isIgnroableResource determines whether the presence or absence of a file
// in the logs is acceptable.  This should only be global resources.
// When running in parallel some globally scoped resources will register in
// log collection tests that didn't create them, and lead to all manner of
// race conditions.  Rather than run these all in series (slow) we just ignore
// any differences with these types.
func isIgnroableResource(path string) bool {
	ignored := []string{
		"persistentvolume",
	}

	for _, i := range ignored {
		if strings.Contains(path, i) {
			return true
		}
	}

	return false
}

// mustVerifyArchiveContents examines a TGZ archive and errors if expected files
// are not present, or unexpected files are present.
func mustVerifyArchiveContents(t *testing.T, archive string, expected []string) {
	// Open the archive file.
	file, err := os.OpenFile(archive, os.O_RDONLY, 0444)
	if err != nil {
		e2eutil.Die(t, err)
	}

	defer func() { _ = file.Close() }()

	// Read each entry in the archive and store the file names.
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		e2eutil.Die(t, err)
	}

	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)

	actual := []string{}

	for {
		hdr, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			e2eutil.Die(t, err)
		}

		actual = append(actual, hdr.Name)
	}

	// Do an exhaustive search of both lists - O(N^2), I'm lazy - and accumulate
	// any errors
	errs := []error{}

	for _, e := range expected {
		found := false

		for _, a := range actual {
			if a == e {
				found = true
				break
			}
		}

		if !found && !isIgnroableResource(e) {
			errs = append(errs, fmt.Errorf("expected file %s not found in archive", e))
		}
	}

	for _, a := range actual {
		// Hack: Check for and add if there are events/logs associated with the involved resource.
		if strings.HasSuffix(a, "events.yaml") {
			continue
		}

		if strings.HasSuffix(a, ".log") {
			continue
		}

		found := false

		for _, e := range expected {
			if e == a {
				found = true
				break
			}
		}

		if !found && !isIgnroableResource(a) {
			errs = append(errs, fmt.Errorf("unexpected file %s found in archive", a))
		}
	}

	if len(errs) != 0 {
		e2eutil.Die(t, NewCompoundError(errs))
	}
}

// extractTimestamp extract the timestamp portion of the input file name e.g. cbopinfo-20180919T092607+0100.tar.gz
// will return 20180919T092607+0100.
func extractTimestamp(filename string) string {
	re := regexp.MustCompile(`\d{8}T\d{6}(\+|-)\d{4}`)
	return re.FindString(filename)
}

// archiveName returns the expected archive name for the specified parameters.
func archiveName(namespace, name, timestamp string, redacted bool) string {
	archive := "cbinfo-" + namespace + "-" + name + "-" + timestamp

	if redacted {
		archive += "-redacted"
	}

	return archive
}

// getLabelSelector returns an appropriate label selector for resources that
// are scoped to a specific couchbase cluster.
func getLabelSelector(all bool, clusters []string) (string, error) {
	selector := labels.Everything()

	if !all {
		requirements := []labels.Requirement{}

		req, err := labels.NewRequirement(operator_constants.LabelApp, selection.Equals, []string{operator_constants.App})
		if err != nil {
			return "", err
		}

		requirements = append(requirements, *req)

		if len(clusters) != 0 {
			req, err := labels.NewRequirement(operator_constants.LabelCluster, selection.In, clusters)
			if err != nil {
				return "", err
			}

			requirements = append(requirements, *req)
		}

		selector = labels.NewSelector()
		selector = selector.Add(requirements...)
	}

	return selector.String(), nil
}

// mustVerifyServerLogs looks for pods that exist and should have associated server logs.
func mustVerifyServerLogs(t *testing.T, k8s *types.Cluster, archive string, redacted bool, clusters ...string) {
	// Grab the required pods.
	selector, err := getLabelSelector(false, clusters)
	if err != nil {
		e2eutil.Die(t, err)
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	dir := filepath.Dir(archive)

	// List all files.
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Grab the timestamp from the archive, all files share this across a cbopinfo run.
	timestamp := extractTimestamp(archive)

	// For each pod, ensure the associated server log exists.
	errs := []error{}
NextPod:
	for _, pod := range pods.Items {
		expected := archiveName(k8s.Namespace, pod.Name, timestamp, redacted) + ".zip"
		for _, file := range files {
			if file.Name() == expected {
				if redacted {
					if err := verifyLogRedaction(filepath.Join(dir, expected)); err != nil {
						errs = append(errs, err)
					}
				}

				continue NextPod
			}
		}

		errs = append(errs, fmt.Errorf("expected file %s not found", expected))
	}

	if len(errs) != 0 {
		e2eutil.Die(t, NewCompoundError(errs))
	}
}

// verifyLogRedaction verifies logs collected for pods are redacted.
func verifyLogRedaction(archive string) error {
	zipReader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}

	defer zipReader.Close()

	for _, file := range zipReader.File {
		if strings.HasSuffix(file.Name, "/users.dets") {
			return nil
		}
	}

	return fmt.Errorf("file %s not redacted", archive)
}

func verifyLogCollectListJSON(k8s *types.Cluster, cbClusterName, collectInfoListJSON string, errMsgList *failureList) error {
	pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbClusterName})
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		if !strings.Contains(collectInfoListJSON, pod.Name) {
			errMsgList.AppendFailure("Pod missing from JSON output: "+pod.Name, fmt.Errorf("pod missing in JSON output"))
		}
	}

	return nil
}

// mustGetFileList lists all resources that should be collected by cbopinfo.
func mustGetFileList(t *testing.T, k8s *types.Cluster, namespace, archive string, all, pprof, metrics bool, clusters ...string) []string {
	// The base file path will have a top level directory named the same as the archive.
	base := strings.TrimSuffix(filepath.Base(archive), ".tar.gz")

	// Initialize any required clients.
	apiExtensionsClient, err := clientset.NewForConfig(k8s.Config)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// These are files that will always exist
	files := []string{
		fmt.Sprintf("%s/cmdline", base),
	}

	// Gather cluster scoped resources.
	clusterRoles, err := k8s.KubeClient.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	clusterRoleBindings, err := k8s.KubeClient.RbacV1().ClusterRoleBindings().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	crds, err := apiExtensionsClient.ApiextensionsV1().CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	persistentVolumes, err := k8s.KubeClient.CoreV1().PersistentVolumes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	files = append(files, fmt.Sprintf("%s/namespace/%s/%s.yaml", base, namespace, namespace))

	for _, clusterRole := range clusterRoles.Items {
		files = append(files, fmt.Sprintf("%s/clusterrole/%s/%s.yaml", base, clusterRole.Name, clusterRole.Name))
	}

	for _, clusterRoleBinding := range clusterRoleBindings.Items {
		files = append(files, fmt.Sprintf("%s/clusterrolebinding/%s/%s.yaml", base, clusterRoleBinding.Name, clusterRoleBinding.Name))
	}

	for _, crd := range crds.Items {
		if strings.HasSuffix(crd.Name, ".couchbase.com") {
			files = append(files, fmt.Sprintf("%s/customresourcedefinition/%s/%s.yaml", base, crd.Name, crd.Name))
		}
	}

	for _, node := range nodes.Items {
		files = append(files, fmt.Sprintf("%s/node/%s/%s.yaml", base, node.Name, node.Name))
	}

	for _, persistentVolume := range persistentVolumes.Items {
		files = append(files, fmt.Sprintf("%s/persistentvolume/%s/%s.yaml", base, persistentVolume.Name, persistentVolume.Name))
	}

	// Gather namespace scoped resources.
	buckets, err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	ephemeralBuckets, err := k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	memcachedBuckets, err := k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	replications, err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	roles, err := k8s.KubeClient.RbacV1().Roles(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	rolebindings, err := k8s.KubeClient.RbacV1().RoleBindings(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	secrets, err := k8s.KubeClient.CoreV1().Secrets(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	serviceaccounts, err := k8s.KubeClient.CoreV1().ServiceAccounts(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, bucket := range buckets.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/couchbasebucket/%s/%s.yaml", base, namespace, bucket.Name, bucket.Name))
	}

	for _, ephemeralBucket := range ephemeralBuckets.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/couchbaseephemeralbucket/%s/%s.yaml", base, namespace, ephemeralBucket.Name, ephemeralBucket.Name))
	}

	for _, memcachedBucket := range memcachedBuckets.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/couchbasememcachedbucket/%s/%s.yaml", base, namespace, memcachedBucket.Name, memcachedBucket.Name))
	}

	for _, replication := range replications.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/couchbasereplication/%s/%s.yaml", base, namespace, replication.Name, replication.Name))
	}

	for _, role := range roles.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/role/%s/%s.yaml", base, namespace, role.Name, role.Name))
	}

	for _, rolebinding := range rolebindings.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/rolebinding/%s/%s.yaml", base, namespace, rolebinding.Name, rolebinding.Name))
	}

	for _, secret := range secrets.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/secret/%s/%s.yaml", base, namespace, secret.Name, secret.Name))
	}

	for _, serviceaccount := range serviceaccounts.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/serviceaccount/%s/%s.yaml", base, namespace, serviceaccount.Name, serviceaccount.Name))
	}

	// Deployments are special, we only collect them if the image matches the operator image.
	// It also has a collector that will pull out pprof and metrics if they are reachable.
	deployments, err := k8s.KubeClient.AppsV1().Deployments(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, deployment := range deployments.Items {
		operator := deployment.Spec.Template.Spec.Containers[0].Image == framework.Global.OpImage
		if !operator && !all {
			continue
		}

		files = append(files, fmt.Sprintf("%s/namespace/%s/deployment/%s/%s.yaml", base, namespace, deployment.Name, deployment.Name))

		if operator {
			if pprof {
				pprofFiles := []string{
					"pprof.block",
					"pprof.goroutine",
					"pprof.heap",
					"pprof.mutex",
					"pprof.threadcreate",
				}
				for _, pprofFile := range pprofFiles {
					files = append(files, fmt.Sprintf("%s/namespace/%s/deployment/%s/%s", base, namespace, deployment.Name, pprofFile))
				}
			}

			if metrics {
				metricsFiles := []string{
					"stats.cluster",
				}
				for _, metricsFile := range metricsFiles {
					files = append(files, fmt.Sprintf("%s/namespace/%s/deployment/%s/%s", base, namespace, deployment.Name, metricsFile))
				}
			}
		}
	}

	// CouchbaseClusters are filtered based on selection.
	couchbaseClusters, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, cluster := range couchbaseClusters.Items {
		if len(clusters) != 0 {
			found := false

			for _, c := range clusters {
				if c == cluster.Name {
					found = true
					break
				}
			}

			if !found {
				continue
			}
		}

		files = append(files, fmt.Sprintf("%s/namespace/%s/couchbasecluster/%s/%s.yaml", base, namespace, cluster.Name, cluster.Name))
	}

	// Gather namespace and cluster scoped resources.
	selector, err := getLabelSelector(all, clusters)
	if err != nil {
		e2eutil.Die(t, err)
	}

	endpoints, err := k8s.KubeClient.CoreV1().Endpoints(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	pvcs, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	pdbs, err := k8s.KubeClient.PolicyV1beta1().PodDisruptionBudgets(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	services, err := k8s.KubeClient.CoreV1().Services(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	configMaps, err := k8s.KubeClient.CoreV1().ConfigMaps(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, endpoint := range endpoints.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/endpoints/%s/%s.yaml", base, namespace, endpoint.Name, endpoint.Name))
	}

	for _, pvc := range pvcs.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/persistentvolumeclaim/%s/%s.yaml", base, namespace, pvc.Name, pvc.Name))
	}

	for _, pod := range pods.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/pod/%s/%s.yaml", base, namespace, pod.Name, pod.Name))
	}

	for _, pdb := range pdbs.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/poddisruptionbudget/%s/%s.yaml", base, namespace, pdb.Name, pdb.Name))
	}

	for _, service := range services.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/service/%s/%s.yaml", base, namespace, service.Name, service.Name))
	}

	for _, configMap := range configMaps.Items {
		files = append(files, fmt.Sprintf("%s/namespace/%s/configmap/%s/%s.yaml", base, namespace, configMap.Name, configMap.Name))
	}

	return files
}

// cbopinfo runs the command with the specified arguments returning the archive name
// created.
func cbopinfo(t *testing.T, args e2eutil.ArgumentList) (string, func()) {
	// Collect into per-cbopinfo directories in the interests of
	// parallelization.  Also make them relative to the workspace so
	// they get cleaned up by Jenkins.
	pwd, err := os.Getwd()
	if err != nil {
		e2eutil.Die(t, err)
	}

	temp, err := ioutil.TempDir(pwd, "test-logs-")
	if err != nil {
		e2eutil.Die(t, err)
	}

	if err := os.MkdirAll(temp, 0755); err != nil {
		e2eutil.Die(t, err)
	}

	args.Add("--directory", temp)

	stdout, err := e2eutil.Cbopinfo(framework.Global.CbopinfoPath, args.Slice())
	if err != nil {
		e2eutil.Die(t, fmt.Errorf("cbopinfo command failed: %w: %s", err, string(stdout)))
	}

	re := regexp.MustCompile(`Wrote cluster information to (\S+)`)

	matches := re.FindStringSubmatch(string(stdout))
	if len(matches) != 2 {
		e2eutil.Die(t, fmt.Errorf("failed to extract archive"))
	}

	cleanup := func() {
		_ = os.RemoveAll(temp)
	}

	return matches[1], cleanup
}

func getLogFileNameFromExecOutput(outputStr string) string {
	startIndex := strings.LastIndex(outputStr, "Wrote cluster information to ")
	if startIndex == -1 {
		return ""
	}

	outputStrArr := strings.Split(outputStr[startIndex:], " ")

	return outputStrArr[len(outputStrArr)-1]
}

// Struct to define cbopinfo command args.
type cbopinfoArg struct {
	Name        string
	Arg         string
	ArgValue    string
	WillFail    bool
	ExpectedErr string
}

// Run cbopinfo command with all valid arguments
// and validate the exit status of the commands.
func TestLogCollectValidateArguments(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	kubeConfPath := targetKube.KubeConfPath
	context := targetKube.Context
	operatorRestPort := strconv.Itoa(constants.OperatorRestPort)

	t.Logf("KubeConfPath: %+v", kubeConfPath)
	t.Logf("Context: %v", context)

	// Validate args which won't produce output file
	for _, arg := range []string{"--help", "--version"} {
		if _, err := e2eutil.Cbopinfo(f.CbopinfoPath, []string{arg}); err != nil {
			e2eutil.Die(t, fmt.Errorf("Failed while providing arg %s: %w", arg, err))
		}
	}

	// Validate all other arguments
	validArgumentList := []cbopinfoArg{
		{
			Name:     "TestValidateCbopinfoAll",
			Arg:      "--all",
			ArgValue: "",
		},
		{
			Name:        "TestValidateCbopinfoKubeconfig",
			Arg:         "--kubeconfig",
			ArgValue:    kubeConfPath,
			ExpectedErr: "flag needs an argument: --kubeconfig",
		},
		{
			Name:        "TestValidateCbopintargetKube.Namespace",
			Arg:         "--namespace",
			ArgValue:    targetKube.Namespace,
			ExpectedErr: "flag needs an argument: --namespace",
		},
		{
			Name:     "TestValidateCbopinfoSystem",
			Arg:      "--system",
			ArgValue: "",
		},
		{
			Name:        "TestValidateCbopinfoOperatorImage",
			Arg:         "--operator-image",
			ArgValue:    f.OpImage,
			ExpectedErr: "flag needs an argument: --operator-image",
		},
		{
			Name:        "TestValidateCbopinfoOperatorRestPort",
			Arg:         "--operator-rest-port",
			ArgValue:    operatorRestPort,
			ExpectedErr: "flag needs an argument: --operator-rest-port",
		},
	}

	// Deploy cb server for cbopinfo validation
	e2eutil.MustNewClusterBasic(t, targetKube, constants.Size1)

	for i := range validArgumentList {
		arg := validArgumentList[i]

		t.Run(arg.Name, func(t *testing.T) {
			cleanup := f.SetupSubTest(t)
			defer cleanup()

			args := e2eutil.ArgumentList{}
			args.AddClusterDefaults(targetKube)
			args.Add(arg.Arg, arg.ArgValue)

			execOut, err := e2eutil.Cbopinfo(f.CbopinfoPath, args.Slice())
			execOutStr := strings.TrimSpace(string(execOut))

			t.Logf("Returned: %s\n", execOutStr)

			if err != nil {
				e2eutil.Die(t, fmt.Errorf("Failed while providing arg %s: %w", arg.Arg, err))
			}

			logFileName := getLogFileNameFromExecOutput(execOutStr)

			defer os.Remove(logFileName)

			// Check command fails with missing argument value
			if arg.ArgValue != "" {
				args := e2eutil.ArgumentList{}
				args.Add(arg.Arg, "")
				execOut, err := e2eutil.Cbopinfo(f.CbopinfoPath, args.Slice())
				execOutStr := strings.TrimSpace(string(execOut))

				t.Logf("Returned: %s\n", execOutStr)

				if err == nil {
					e2eutil.Die(t, fmt.Errorf("Command executed successfully without providing value for %s: %w", arg.Arg, err))
				}

				// Verify valid error message
				if !strings.Contains(execOutStr, arg.ExpectedErr) {
					e2eutil.Die(t, fmt.Errorf("Invalid error for missing arg value %s\nExpected: %v\nReceived: %v", arg.Arg, arg.ExpectedErr, execOutStr))
				}

				// Check no output file is generated
				if logFileName := getLogFileNameFromExecOutput(execOutStr); logFileName != "" {
					e2eutil.Die(t, fmt.Errorf("File created with missing argument for %s", arg.Arg))
				}
			}
		})
	}
}

// Negative test scenarios with command argument.
func TestNegLogCollectValidateArgs(t *testing.T) {
	_, cleanup := framework.Global.SetupTest(t, framework.NoOperator)
	defer cleanup()

	// Invent a kubernetes configuration, using an address from TEST-NET-1 will
	// ensure it's always going to be unreachable.
	config := &clientcmdapiv1.Config{
		Clusters: []clientcmdapiv1.NamedCluster{
			{
				Name: "default",
				Cluster: clientcmdapiv1.Cluster{
					Server: "http://192.0.2.1",
				},
			},
		},
		AuthInfos: []clientcmdapiv1.NamedAuthInfo{
			{
				Name: "default",
				AuthInfo: clientcmdapiv1.AuthInfo{
					Token: "mickey mouse",
				},
			},
		},
		Contexts: []clientcmdapiv1.NamedContext{
			{
				Name: "default",
				Context: clientcmdapiv1.Context{
					Cluster:   "default",
					AuthInfo:  "default",
					Namespace: "default",
				},
			},
		},
		CurrentContext: "default",
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		e2eutil.Die(t, err)
	}

	kubeconfig, err := ioutil.TempFile("/tmp", "*")
	if err != nil {
		e2eutil.Die(t, err)
	}

	defer kubeconfig.Close()

	defer os.Remove(kubeconfig.Name())

	if _, err := kubeconfig.Write(data); err != nil {
		e2eutil.Die(t, err)
	}

	if err := kubeconfig.Sync(); err != nil {
		e2eutil.Die(t, err)
	}

	errMsgList := failureList{}

	validArgumentList := []cbopinfoArg{
		{
			Name:        "Unreachable '-kubeconfig' file",
			Arg:         "--kubeconfig",
			ArgValue:    kubeconfig.Name(),
			WillFail:    true,
			ExpectedErr: "unable to initialize context",
		},
		{
			Name:        "Validating invalid '-kubeconfig' file missing",
			Arg:         "--kubeconfig",
			ArgValue:    "/tmp/fileNotFound",
			WillFail:    true,
			ExpectedErr: "unable to initialize context",
		},
	}

	for _, arg := range validArgumentList {
		t.Log(arg.Name)
		cmdArgs := []string{arg.Arg}
		cmdArgs = append(cmdArgs, arg.ArgValue)

		execOut, err := e2eutil.Cbopinfo(framework.Global.CbopinfoPath, cmdArgs)
		execOutStr := strings.TrimSpace(string(execOut))

		t.Logf("Returned: %s\n", execOutStr)

		if err == nil {
			errMsgList.AppendFailure(arg.Name, fmt.Errorf("command executed successfully"))
		}

		// Verify valid error message
		if !strings.Contains(execOutStr, arg.ExpectedErr) {
			errMsgList.AppendFailure(arg.Name, fmt.Errorf("invalid error message: %s", execOutStr))
		}

		// Check no output file is generated
		if logFileName := getLogFileNameFromExecOutput(execOutStr); logFileName != "" {
			errMsgList.AppendFailure(arg.Name, fmt.Errorf("log file created unexpectedly"))
			os.Remove(logFileName)
		}
	}

	errMsgList.CheckFailures(t)
}

// Create a couchbase cluster.
// Get the logs from the desired clustername and namespace and verify.
// Get logs from multiple / all clusters and verify the files.
func TestLogCollect(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	cluster1Size := constants.Size1
	cluster2Size := constants.Size1
	cluster3Size := constants.Size1

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	cluster1 := e2eutil.MustNewClusterBasic(t, targetKube, cluster1Size)
	cluster2 := e2eutil.MustNewClusterBasic(t, targetKube, cluster2Size)
	cluster3 := e2eutil.MustNewClusterBasic(t, targetKube, cluster3Size)

	commonArgs := e2eutil.ArgumentList{}
	commonArgs.AddClusterDefaults(targetKube)
	commonArgs.AddEnvironmentDefaults(f.OpImage)

	t.Run("TestLogCollectSingle", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := commonArgs.Clone()
		args.Add(cluster1.Name, "")

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, true, true, cluster1.Name)
		mustVerifyArchiveContents(t, archive, files)
	})

	t.Run("TestLogCollectMultiple", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := commonArgs.Clone()
		args.Add(cluster1.Name, "")
		args.Add(cluster3.Name, "")

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, true, true, cluster1.Name, cluster3.Name)
		mustVerifyArchiveContents(t, archive, files)
	})

	t.Run("TestLogCollect", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		archive, cleanCbopinfo := cbopinfo(t, commonArgs)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, true, true)
		mustVerifyArchiveContents(t, archive, files)
	})

	t.Run("TestLogCollectSingleSystem", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := commonArgs.Clone()
		args.Add("--system", "")
		args.Add(cluster2.Name, "")

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, true, true, cluster2.Name)
		files = append(files, mustGetFileList(t, targetKube, "kube-system", archive, true, true, true)...)
		mustVerifyArchiveContents(t, archive, files)
	})

	t.Run("TestLogCollectMultipleSystem", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := commonArgs.Clone()
		args.Add("--system", "")
		args.Add(cluster1.Name, "")
		args.Add(cluster3.Name, "")

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, true, true, cluster1.Name, cluster3.Name)
		files = append(files, mustGetFileList(t, targetKube, "kube-system", archive, true, true, true)...)
		mustVerifyArchiveContents(t, archive, files)
	})

	t.Run("TestLogCollectSystem", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := commonArgs.Clone()
		args.Add("--system", "")

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, true, true)
		files = append(files, mustGetFileList(t, targetKube, "kube-system", archive, true, true, true)...)
		mustVerifyArchiveContents(t, archive, files)
	})

	t.Run("TestLogCollectSingleCollectInfo", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := commonArgs.Clone()
		args.Add("--collectinfo", "")
		args.Add("--collectinfo-collect", "all")
		args.Add(cluster1.Name, "")

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, true, true, cluster1.Name)
		mustVerifyArchiveContents(t, archive, files)
		mustVerifyServerLogs(t, targetKube, archive, false, cluster1.Name)
	})

	t.Run("TestLogCollectAll", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := commonArgs.Clone()
		args.Add("--all", "")

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, true, true, true)
		mustVerifyArchiveContents(t, archive, files)
	})
}

// Create couchbase cluster.
// Create Rbac user with reduced k8s cluster access.
// Verify collected log file list with reduced cluster access.
func TestLogCollectRbacPermission(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create the cluster.
	cluster := e2eutil.MustNewClusterBasic(t, targetKube, constants.Size1)

	// Create a service account with no permissions.
	if err := framework.RecreateServiceAccount(targetKube, cluster.Name); err != nil {
		e2eutil.Die(t, err)
	}

	// Create a kubernetes configuration file.
	sa, err := targetKube.KubeClient.CoreV1().ServiceAccounts(targetKube.Namespace).Get(context.Background(), cluster.Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if len(sa.Secrets) == 0 {
		t.Skip("Cluster does not permit service account tokens")
	}

	secret, err := targetKube.KubeClient.CoreV1().Secrets(targetKube.Namespace).Get(context.Background(), sa.Secrets[0].Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	config := &clientcmdapiv1.Config{
		Clusters: []clientcmdapiv1.NamedCluster{
			{
				Name: "default",
				Cluster: clientcmdapiv1.Cluster{
					Server:                   targetKube.Config.Host,
					CertificateAuthorityData: secret.Data["ca.crt"],
				},
			},
		},
		AuthInfos: []clientcmdapiv1.NamedAuthInfo{
			{
				Name: "default",
				AuthInfo: clientcmdapiv1.AuthInfo{
					Token: string(secret.Data["token"]),
				},
			},
		},
		Contexts: []clientcmdapiv1.NamedContext{
			{
				Name: "default",
				Context: clientcmdapiv1.Context{
					Cluster:   "default",
					AuthInfo:  "default",
					Namespace: targetKube.Namespace,
				},
			},
		},
		CurrentContext: "default",
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		e2eutil.Die(t, err)
	}

	kubeconfig, err := ioutil.TempFile("/tmp", "*")
	if err != nil {
		e2eutil.Die(t, err)
	}

	defer kubeconfig.Close()

	defer os.Remove(kubeconfig.Name())

	if _, err := kubeconfig.Write(data); err != nil {
		e2eutil.Die(t, err)
	}

	if err := kubeconfig.Sync(); err != nil {
		e2eutil.Die(t, err)
	}

	// Collect logs
	args := e2eutil.ArgumentList{}
	args.Add("--kubeconfig", kubeconfig.Name())
	execOut, err := e2eutil.Cbopinfo(f.CbopinfoPath, args.Slice())
	execOutStr := strings.TrimSpace(string(execOut))

	t.Log(execOutStr)

	if err == nil {
		e2eutil.Die(t, fmt.Errorf("Able to read resource without valid rbac permissions"))
	}

	if !strings.Contains(execOutStr, "is forbidden") {
		e2eutil.Die(t, fmt.Errorf("Invalid error message: %v", execOutStr))
	}
}

func ReDeployOperator(t *testing.T, k8s *types.Cluster, imageName string, port int) error {
	// Delete existing Deployment
	e2eutil.MustDeleteOperatorDeployment(t, k8s, time.Minute)

	// Create new deployment object to deploy
	deployment := k8s.OperatorDeployment.DeepCopy()
	deployment.Spec.Template.Spec.Containers[0].Image = imageName

	if port != 0 {
		deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("--listen-addr=0.0.0.0:%d", port))

		// Direct readiness probe to listening address
		for i, containerPort := range deployment.Spec.Template.Spec.Containers[0].Ports {
			if containerPort.Name != "http" {
				continue
			}

			deployment.Spec.Template.Spec.Containers[0].Ports[i].ContainerPort = int32(port)
		}
	}

	t.Logf("Deploying operator using image '%s' and port %d", imageName, port)

	if _, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Create(context.Background(), deployment, metav1.CreateOptions{}); err != nil {
		return err
	}

	if err := e2eutil.WaitUntilOperatorReady(k8s, 5*time.Minute); err != nil {
		return err
	}

	return nil
}

/***********************************
   Operator extended debug cases
***********************************/

// Generic function to re-deploy the operator with given image name and rest-port.
// Collect logs with appropriate cbopinfo arguments and verify the collected info.
func CollectExtendedDebugLogGeneric(t *testing.T, k8s *types.Cluster, operatorImage string, operatorPort int, args e2eutil.ArgumentList) {
	targetKube := k8s
	clusterSize := 3

	if err := ReDeployOperator(t, targetKube, operatorImage, operatorPort); err != nil {
		e2eutil.Die(t, err)
	}

	// Create Couchbase cluster
	cbCluster := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Collect logs
	args.Add(cbCluster.Name, "")

	archive, cleanCbopinfo := cbopinfo(t, args)
	defer cleanCbopinfo()

	files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, true, true, true)
	mustVerifyArchiveContents(t, archive, files)
	mustVerifyServerLogs(t, targetKube, archive, false)
}

// Collect cbopinfo using '--operator-image' and '--operator-rest-port'
// with default values and validate the logs collected
//
// SM: Given the default is the build we are working on, you need to
// either push to the public repo with an unreleased build, or do some
// raw docker nastiness to install the "default" image.  As neither are
// sane just sanitise this code with the tested operator image.
func TestExtendedDebugWithDefaultValues(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	args := e2eutil.ArgumentList{}
	args.AddClusterDefaults(targetKube)
	args.AddEnvironmentDefaults(f.OpImage)
	args.Add("--collectinfo", "")
	args.Add("--collectinfo-collect", "all")
	args.Add("--all", "")
	CollectExtendedDebugLogGeneric(t, targetKube, f.OpImage, constants.OperatorRestPort, args)
}

// Collect cbopinfo using '--operator-image' and '--operator-rest-port'
// with custom values and validate the logs collected.
func TestExtendedDebugWithNonDefaultValues(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testPort := 32123
	args := e2eutil.ArgumentList{}
	args.AddClusterDefaults(targetKube)
	args.AddEnvironmentDefaults(f.OpImage)
	args.Add("--operator-rest-port", strconv.Itoa(testPort))
	args.Add("--collectinfo", "")
	args.Add("--collectinfo-collect", "all")
	args.Add("--all", "")
	CollectExtendedDebugLogGeneric(t, targetKube, f.OpImage, testPort, args)
}

// Collect cbopinfo with '--operator-image' & '-operator-rest-port'
// with invalid values and validate the log collection.
func TestLogCollectInvalid(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	invalidImgName := "couchbase/couchbase-operator:invalidversion"
	invalidPortVal := "32080"
	clusterSize := constants.Size1

	// Create Couchbase cluster
	e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Collect logs with invalid operator-image-name
	t.Run("TestLogCollectInvalidOperatorImage", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := e2eutil.ArgumentList{}
		args.AddClusterDefaults(targetKube)
		args.Add("--operator-image", invalidImgName)
		args.Add("--operator-rest-port", strconv.Itoa(constants.OperatorRestPort))

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		filtered := []string{}

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, true, true)
		for _, file := range files {
			if !strings.Contains(file, "/deployment/") {
				filtered = append(filtered, file)
			}
		}

		mustVerifyArchiveContents(t, archive, filtered)
	})

	// Collect logs with invalid operator-rest-port
	t.Run("TestLogCollectInvalidRestPort", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		args := e2eutil.ArgumentList{}
		args.AddClusterDefaults(targetKube)
		args.AddEnvironmentDefaults(f.OpImage)
		args.Add("--operator-rest-port", invalidPortVal)

		archive, cleanCbopinfo := cbopinfo(t, args)
		defer cleanCbopinfo()

		files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, false, false, true)
		mustVerifyArchiveContents(t, archive, files)
	})
}

// Collect cbopinfo with '-operator-image' & '-operator-rest-port'
// and kill the operator pod during log collection in parallel.
func TestExtendedDebugKillOperatorDuringLogCollection(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := constants.Size1

	// Create Couchbase cluster
	e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	args := e2eutil.ArgumentList{}
	args.AddClusterDefaults(targetKube)
	args.AddEnvironmentDefaults(f.OpImage)
	args.Add("--operator-rest-port", strconv.Itoa(constants.OperatorRestPort))
	args.Add("--collectinfo", "")
	args.Add("--collectinfo-collect", "all")
	args.Add("--all", "")

	e2eutil.MustDeleteCouchbaseOperator(t, targetKube)

	// Collect logs when operator pod goes down in parallel
	archive, cleanCbopinfo := cbopinfo(t, args)
	defer cleanCbopinfo()

	// Verify file list
	files := mustGetFileList(t, targetKube, targetKube.Namespace, archive, true, true, true)
	mustVerifyArchiveContents(t, archive, files)
	mustVerifyServerLogs(t, targetKube, archive, false)
}

/**************************************
  Ephemeral pod log collection cases
***************************************/

// Generic function to kill couchbase server pod and operator with log PVs defined for server pods
// 'podDownMethod' argument with either one of ['deletePod', 'killServerProcess'].
func EphemeralLogCollectUsingLogPVGeneric(t *testing.T, k8s *types.Cluster, podDownMethod string, isOperatorKilledWithServerPod bool) {
	targetKube := k8s

	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2
	victims := []int{2, 3}

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	cbCluster := e2eutil.MustNewSupportableCluster(t, targetKube, mdsGroupSize)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, cbCluster, e2espec.DefaultBucketTwoReplicas(), time.Minute)

	// To cross check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{}

	for i := 0; i < clusterSize; i++ {
		expectedPvcMap[couchbaseutil.CreateMemberName(cbCluster.Name, i)] = 1
	}

	// Verifying the persistence of log PVs are preserved by operator
	mustVerifyPvcMappingForPods(t, targetKube, expectedPvcMap)

	// Kill PV log enabled pods and verify the logs are persisted after pod deletion
	for i, victim := range victims {
		// Kills operator pod in async way
		if isOperatorKilledWithServerPod {
			e2eutil.MustDeleteCouchbaseOperator(t, targetKube)
		}

		switch podDownMethod {
		case "deletePod":
			e2eutil.MustKillPodForMember(t, targetKube, cbCluster, victim, false)

			expectedPvcMap[couchbaseutil.CreateMemberName(cbCluster.Name, clusterSize+i)] = 1
		case "killServerProcess":
			podNameToKill := couchbaseutil.CreateMemberName(cbCluster.Name, victim)
			e2eutil.MustExecShellInPod(t, targetKube, podNameToKill, "pkill beam.smp")
		}

		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 10*time.Minute)
	}

	// Verifying the persistence of log PVs are preserved by operator
	mustVerifyPvcMappingForPods(t, targetKube, expectedPvcMap)

	var validator eventschema.Validatable

	switch podDownMethod {
	case "deletePod":
		validator = e2eutil.PodDownFailoverRecoverySequence()
	case "killServerProcess":
		validator = e2eutil.ServerCrashRecoverySequence()
	default:
		e2eutil.Die(t, fmt.Errorf("invalid murder weapon: %s", podDownMethod))
	}

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: len(victims), Validator: validator},
	}

	ValidateEvents(t, targetKube, cbCluster, expectedEvents)
}

// Generic function to kill Cb server pod and update the server class in parallel
// and check how operator handles the log retention as expected.
func LogCollectWithClusterResizeAndServerPodKilledGeneric(t *testing.T, isOperatorKilledWithServerPod bool) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := 3
	clusterSize := mdsGroupSize * 2
	resizedService := 1
	victim := 3

	// Create the cluster (3 stateful nodes, 3 stateless nodes)
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	cbCluster := e2eutil.MustNewSupportableCluster(t, targetKube, mdsGroupSize)

	// When ready, ensure the persistent volumes are allocated as expected.
	mustVerifyPvcMappingForPods(t, targetKube, map[string]int{
		couchbaseutil.CreateMemberName(cbCluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 3): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 4): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 5): 1,
	})

	e2eutil.MustKillPodForMember(t, targetKube, cbCluster, victim, false)

	if isOperatorKilledWithServerPod {
		e2eutil.MustDeleteCouchbaseOperator(t, targetKube)
	}

	cbCluster = e2eutil.MustResizeCluster(t, resizedService, 1, targetKube, cbCluster, 5*time.Minute)

	mustVerifyPvcMappingForPods(t, targetKube, map[string]int{
		couchbaseutil.CreateMemberName(cbCluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 3): 1,
	})

	// Check the events match what we expect:
	// * Cluster created
	// * Pod goes down (optionally it may failover while the operator is down) and fails
	// * Scales from 3 -> 1
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
		},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
		e2eutil.ClusterScaleDownSequence(2),
	}

	ValidateEvents(t, targetKube, cbCluster, expectedEvents)
}

// Define log mount for ephemeral pods and validate the logs are preserved
// even after abnormal pod removal.
func TestCollectLogFromEphemeralPodsUsingLogPV(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	isOperatorKilledWithServerPod := false

	// Pods brought down using DeletePod method
	EphemeralLogCollectUsingLogPVGeneric(t, targetKube, "deletePod", isOperatorKilledWithServerPod)
}

func TestCollectLogFromEphemeralPodsUsingLogPVKillProcess(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	isOperatorKilledWithServerPod := false

	if f.KubeType != "kubernetes" {
		t.Skip("unsupported platform")
	}

	// Pods brought down by killing cb-server process
	EphemeralLogCollectUsingLogPVGeneric(t, targetKube, "killServerProcess", isOperatorKilledWithServerPod)
}

// Define log mount for ephemeral pods and validate the logs are preserved
// even after abnormal pod removal.
func TestCollectLogFromEphemeralPodsWithOperatorKilled(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	isOperatorKilledWithServerPod := true

	// Pods brought down using DeletePod method
	EphemeralLogCollectUsingLogPVGeneric(t, targetKube, "deletePod", isOperatorKilledWithServerPod)
}

func TestCollectLogFromEphemeralPodsWithOperatorKilledKillProcess(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	isOperatorKilledWithServerPod := true

	if f.KubeType != "kubernetes" {
		t.Skip("unsupported platform")
	}

	// Pods brought down by killing cb-server process
	EphemeralLogCollectUsingLogPVGeneric(t, targetKube, "killServerProcess", isOperatorKilledWithServerPod)
}

// Deploys Couchbase server with log PV defined for server pods
// Scale down the couchbase cluster and check log PVs cleanup has happened.
func TestEphemeralLogCollectResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := 3
	clusterSize := mdsGroupSize * 2
	scaledService := 1

	// Create the cluster (3 stateful and 3 stateless)
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	cbCluster := e2espec.NewSupportableCluster(mdsGroupSize)
	cbCluster.Spec.Logging.LogRetentionCount = 3
	cbCluster = e2eutil.MustNewClusterFromSpec(t, targetKube, cbCluster)

	// When ready, ensure the currect volumes are in place, then scale up and down.
	// Expect only volumes to exist for live pods on completion.
	mustVerifyPvcMappingForPods(t, targetKube, map[string]int{
		couchbaseutil.CreateMemberName(cbCluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 3): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 4): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 5): 1,
	})

	cbCluster = e2eutil.MustResizeCluster(t, scaledService, 2, targetKube, cbCluster, 5*time.Minute)
	cbCluster = e2eutil.MustResizeCluster(t, scaledService, 4, targetKube, cbCluster, 5*time.Minute)
	cbCluster = e2eutil.MustResizeCluster(t, scaledService, 1, targetKube, cbCluster, 5*time.Minute)

	mustVerifyPvcMappingForPods(t, targetKube, map[string]int{
		couchbaseutil.CreateMemberName(cbCluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 3): 1,
	})

	// Check the events match what we expect:
	// * Cluster created
	// * Scales from 3 -> 2
	// * Scales from 2 -> 4
	// * Scales from 4 -> 1
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleUpSequence(2),
		e2eutil.ClusterScaleDownSequence(3),
	}

	ValidateEvents(t, targetKube, cbCluster, expectedEvents)
}

// Kill Cb server pod and update the server class in parallel
// and check how operator handles the log retention.
func TestLogCollectWithClusterResizeAndServerPodKilled(t *testing.T) {
	isOperatorKilledWithServerPod := false
	LogCollectWithClusterResizeAndServerPodKilledGeneric(t, isOperatorKilledWithServerPod)
}

// Kill Operator, Cb server pod anb update the server class all in parallel
// and check how operator handles the log retention.
func TestLogCollectWithClusterResizeAndOperatorPodKilled(t *testing.T) {
	isOperatorKilledWithServerPod := true
	LogCollectWithClusterResizeAndServerPodKilledGeneric(t, isOperatorKilledWithServerPod)
}

// Collect logs from ephemeral log PVs.
// using default log retention time and size values.
func TestLogCollectWithDefaultRetentionAndSize(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2
	victims := 6

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := e2eutil.MustNewSupportableCluster(t, kubernetes, mdsGroupSize)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Cross check number of persistent vol claims matches the defined spec.
	expectedPvcMap := map[string]int{}

	for i := 0; i < clusterSize; i++ {
		expectedPvcMap[couchbaseutil.CreateMemberName(cluster.Name, i)] = 1
	}

	mustVerifyPvcMappingForPods(t, kubernetes, expectedPvcMap)

	// Kill stateless pods repeatedly waiting for recovery each time.
	for victim := mdsGroupSize; victim < mdsGroupSize+victims; victim++ {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim, false)
		e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	}

	// Check that log volumes are persisted.
	for i := clusterSize; i < clusterSize+victims; i++ {
		expectedPvcMap[couchbaseutil.CreateMemberName(cluster.Name, i)] = 1
	}

	mustVerifyPvcMappingForPods(t, kubernetes, expectedPvcMap)

	// Check the events match what we expect:
	// * Cluster created
	// * Members go down and are failed over
	// * New members balanced in to replace the failed ones
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: victims, Validator: e2eutil.PodDownFailoverRecoverySequence()},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Collect logs from ephemeral log PVs.
// using custom log retention time and size values.
func TestLogCollectWithCustomRetentionAndSize(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victims := 6
	maxLogCount := 2

	// Create the cluster
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := e2espec.NewSupportableCluster(mdsGroupSize)
	cluster.Spec.Logging.LogRetentionTime = "15m"
	cluster.Spec.Logging.LogRetentionCount = maxLogCount
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Track pods we create and their expected number of persistent volumes.
	expectedPvcMap := map[string]int{}

	for i := 0; i < clusterSize; i++ {
		expectedPvcMap[couchbaseutil.CreateMemberName(cluster.Name, i)] = 1
	}

	// For each victim, kill the pod in turn and wait for the janitor to catch up.
	for victim := 0; victim < victims; victim++ {
		// Start killing from the start of the stateless pods.
		victimIndex := mdsGroupSize + victim

		// Kill the member and wait for the rebalance to complete.  We *must* wait for at least a minute
		// so the janitor marks PVCs detached in order.  If two PVCs are detached at the same time we
		// make *NO* guarantees about which one to retain.
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
		e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 5*time.Minute)
		time.Sleep(time.Minute)

		// Update the pod/pvc mapping with the new node
		expectedPvcMap[couchbaseutil.CreateMemberName(cluster.Name, clusterSize+victim)] = 1
	}

	// We only expect the last N stateless logs to be left behind.
	for victim := 0; victim < victims-maxLogCount; victim++ {
		expectedPvcMap[couchbaseutil.CreateMemberName(cluster.Name, mdsGroupSize+victim)] = 0
	}

	mustVerifyPvcMappingForPods(t, kubernetes, expectedPvcMap)

	// Check the events match what we expect:
	// * Cluster created
	// * Members go down and are failed over
	// * New members balanced in to replace the failed ones
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: victims, Validator: e2eutil.PodDownFailoverRecoverySequence()},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

/***********************************
   Log redaction verification
***********************************/

func TestLogRedactionVerify(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create Couchbase cluster
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	e2eutil.MustNewClusterBasic(t, targetKube, constants.Size3)

	// Collect logs
	args := e2eutil.ArgumentList{}
	args.AddClusterDefaults(targetKube)
	args.AddEnvironmentDefaults(f.OpImage)
	args.Add("--collectinfo", "")
	args.Add("--collectinfo-collect", "all")
	args.Add("--collectinfo-redact", "")
	args.Add("--all", "")

	archive, cleanCbopinfo := cbopinfo(t, args)
	defer cleanCbopinfo()

	mustVerifyServerLogs(t, targetKube, archive, true)
}

func TestLogRedactionWithPvVerify(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	version := strings.Split(f.CouchbaseServerImage, ":")

	if version[1] == "6.5.0" && f.EnableIstio {
		t.Skip("Analytics broken on 6.5.0 with Istio")
	}

	clusterSize := constants.Size3
	pvcName := "couchbase"

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())

	cbCluster := e2espec.NewBasicCluster(clusterSize)
	cbCluster.Spec.Servers[0].Services = append(cbCluster.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	cbCluster.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
		AnalyticsClaims: []string{
			pvcName,
			pvcName,
		},
	}
	cbCluster.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	e2eutil.MustNewClusterFromSpec(t, targetKube, cbCluster)

	// Collect logs
	args := e2eutil.ArgumentList{}
	args.AddClusterDefaults(targetKube)
	args.AddEnvironmentDefaults(f.OpImage)
	args.Add("--collectinfo", "")
	args.Add("--collectinfo-collect", "all")
	args.Add("--collectinfo-redact", "")
	args.Add("--all", "")

	archive, cleanCbopinfo := cbopinfo(t, args)
	defer cleanCbopinfo()

	mustVerifyServerLogs(t, targetKube, archive, true)
}

// TestLogRetentionMultiCluster ensures that one cluster's retention settings do not affect anothers
// running in the same namespace.
func TestLogRetentionMultiCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2

	// Create two clusters.
	cluster1 := e2eutil.MustNewSupportableCluster(t, kubernetes, mdsGroupSize)
	cluster2 := e2eutil.MustNewSupportableCluster(t, kubernetes, mdsGroupSize)

	// Ensure cluster 1 is healthy and update the retention period to be 1m.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster1, 2*time.Minute)
	_ = e2eutil.MustPatchCluster(t, kubernetes, cluster1, jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionTime", "1m"), time.Minute)

	// Ensure cluster2 is healthy then kill the first stateless pod in cluster 2.  Wait for the recovery to
	// start and complete.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster2, 2*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster2, mdsGroupSize, false)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster2, e2eutil.RebalanceStartedEvent(cluster2), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster2, 2*time.Minute)

	// We expect that after 3 minutes (1m to flag as orphaned and 1m retention period) the
	// persistent log volume should still be present.
	time.Sleep(3 * time.Minute)

	pvcMapping := map[string]int{}

	for i := 0; i < clusterSize+1; i++ {
		pvcMapping[couchbaseutil.CreateMemberName(cluster2.Name, i)] = 1
	}

	mustVerifyPvcMappingForPods(t, kubernetes, pvcMapping)
}

func TestLogCollectListJson(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create Couchbase cluster
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	cbCluster := e2eutil.MustNewClusterBasic(t, targetKube, constants.Size3)

	// Collect logs
	args := e2eutil.ArgumentList{}
	args.AddClusterDefaults(targetKube)
	args.AddEnvironmentDefaults(f.OpImage)
	args.Add("--collectinfo", "")
	args.Add("--collectinfo-list", "")
	execOut, err := e2eutil.Cbopinfo(f.CbopinfoPath, args.Slice())
	execOutStr := strings.TrimSpace(string(execOut))

	t.Logf("Returned: %s\n", execOutStr)

	if err != nil {
		e2eutil.Die(t, err)
	}

	errMsgList := failureList{}
	testHasErrors := false

	if err := verifyLogCollectListJSON(targetKube, cbCluster.Name, execOutStr, &errMsgList); err != nil {
		e2eutil.Die(t, err)
	}

	testHasErrors = errMsgList.PrintFailures(t) || testHasErrors

	if testHasErrors {
		e2eutil.Die(t, fmt.Errorf("test has errors"))
	}
}
