package e2e

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapiv1 "k8s.io/client-go/tools/clientcmd/api/v1"

	"github.com/ghodss/yaml"
)

// lazyBoundStorageClass examines the requested storage class and returns true if
// the persistent volumes are bound when attached to a pod.
func lazyBoundStorageClass(t *testing.T, cluster *types.Cluster) bool {
	f := framework.Global
	sc, err := cluster.KubeClient.StorageV1().StorageClasses().Get(f.StorageClassName, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}
	return *sc.VolumeBindingMode == storagev1.VolumeBindingWaitForFirstConsumer
}

// supportsMultipleVolumeClaims returns true if multiple PVCs can be supported by a test.
// We can run the test if there is just a single node (minikube/minishift), all nodes are
// in the same zone (and thus all PVs will be scheduled in that zone), or lazy binding is
// enabled (the PVCs will be scheduled in the same zone as a pod).  Additionally for abnormal
// clusters we allow them to be used if explicitly stated in the cluster definition.
func supportsMultipleVolumeClaims(t *testing.T, cluster *types.Cluster) bool {
	return cluster.SupportsMultipleVolumeClaims ||
		e2eutil.MustNumNodes(t, cluster) == 1 ||
		MustNumAvailabilityZones(t, cluster) == 1 ||
		lazyBoundStorageClass(t, cluster)
}

// Removes first dir name present in the file path
func parentDirStrRemover(fileList []string) {
	for index, fileName := range fileList {
		fileList[index] = strings.Join(strings.Split(fileName, "/")[1:], "/")
	}
}

// Function to cross check log dir contents against populated file list
func checkLogDirContents(reqFileList []string, logDirName string, errMsgList *failureList) {
	for _, fileName := range reqFileList {
		if _, err := os.Stat(fileName); err != nil {
			errMsgList.AppendFailure("File "+fileName, errors.New("File not found!"))
		}
	}
}

// Function to cross check log dir contents are not present against the populated file list
func checkLogDirContentsForExcludedFiles(excludedFileList []string, logFileDir string, errMsgList *failureList) {
	for _, fileName := range excludedFileList {
		if _, err := os.Stat(fileName); err == nil {
			errMsgList.AppendFailure("File "+fileName, errors.New("File Exists!"))
		}
	}
}

// extractTimestamp extract the timestamp portion of the input file name e.g. cbopinfo-20180919T092607+0100.tar.gz
// will return 20180919T092607+0100.
func extractTimestamp(filename string) (string, error) {
	re, err := regexp.Compile(`\d{8}T\d{6}(\+|-)\d{4}`)
	if err != nil {
		return "", err
	}
	return re.FindString(filename), nil
}

// archiveName returns the expected archive name for the specified parameters.
func archiveName(namespace, name, timestamp string, redacted bool) string {
	archive := "cbinfo-" + namespace + "-" + name + "-" + timestamp
	if redacted {
		archive += "-redacted"
	}
	return archive
}

// checkCollectInfoLogs checks all expected Couchbase server logs are present.
func checkCollectInfoLogs(kubeClient kubernetes.Interface, namespace, cbClusterName, cbopinfoLogDir string, redacted bool, errMsgList *failureList) error {
	fileInfos, err := ioutil.ReadDir(".")
	if err != nil {
		errMsgList.AppendFailure("For directory "+" . ", fmt.Errorf("Failed to read directory: %v", err))
	}

	timestamp, err := extractTimestamp(cbopinfoLogDir)
	if err != nil {
		errMsgList.AppendFailure("For log directory "+cbopinfoLogDir, fmt.Errorf("Failed to extract timestamp: %v", err))
	}

	var pods *corev1.PodList
	err = retryutil.Retry(e2eutil.Context, 5*time.Second, 5, func() (bool, error) {
		pods, err = kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbClusterName})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
	if err != nil {
		return err
	}

PodForLoop:
	for _, pod := range pods.Items {
		archive := archiveName(namespace, pod.Name, timestamp, redacted) + ".zip"
		for _, fileInfo := range fileInfos {
			if fileInfo.Name() == archive {
				continue PodForLoop
			}
		}
		errMsgList.AppendFailure("For pod "+pod.Name, fmt.Errorf("server log file %s missing", archive))
	}
	return nil
}

// verifyLogRedaction verifies logs collected for pods are redacted.
func verifyLogRedaction(kubeClient kubernetes.Interface, namespace, cbClusterName, cbopinfoLogDir string, errMsgList *failureList) error {
	timestamp, err := extractTimestamp(cbopinfoLogDir)
	if err != nil {
		fmt.Printf("Failed to extract timestamp: %v", err)
	}

	var pods *corev1.PodList
	err = retryutil.Retry(e2eutil.Context, 5*time.Second, 5, func() (bool, error) {
		pods, err = kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbClusterName})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		// Derive unzipped log directory and archive names.
		directory := archiveName(namespace, pod.Name, timestamp, true)
		archive := directory + ".zip"

		// Clean up after ourselves
		defer os.RemoveAll(directory)
		defer os.Remove(archive)

		// Extract the archive
		if err := unzipFile(archive); err != nil {
			return err
		}

		// Extract the directory contents
		fileInfos, err := ioutil.ReadDir(directory)
		if err != nil {
			return err
		}

		// Redacted logs should not contain users.dets
		for _, fileInfo := range fileInfos {
			if fileInfo.Name() == "users.dets" {
				errMsgList.AppendFailure("Non-redacted file: users.dets", fmt.Errorf("Users file found in redacted file list"))
				break
			}
		}
	}
	return nil
}

func verifyLogCollectListJson(kubeClient kubernetes.Interface, namespace, cbClusterName, collectInfoListJson string, errMsgList *failureList) error {
	pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbClusterName})
	if err != nil {
		return errors.New("Failed to list pods: " + err.Error())
	}

	podPresent := true

	for _, pod := range pods.Items {
		if strings.Contains(collectInfoListJson, pod.Name) != podPresent {
			errMsgList.AppendFailure("Pod missing from JSON output: "+pod.Name, errors.New("Pod missing in JSON output!"))
		}
	}
	return nil
}

// Function to populate deployment file list
func getDeployementFileList(kubeClient kubernetes.Interface, namespace, deploymentDir string, fileList *[]string, allFlag bool) error {
	var err error
	var deployments *appsv1.DeploymentList
	if allFlag {
		deployments, err = kubeClient.AppsV1().Deployments(namespace).List(metav1.ListOptions{})
	} else {
		deployments, err = kubeClient.AppsV1().Deployments(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	}
	if err != nil {
		return errors.New("Failed to list deployments: " + err.Error())
	}
	for _, deployment := range deployments.Items {
		*fileList = append(*fileList, deploymentDir+"/"+deployment.Name+"/"+deployment.Name+".yaml")
		if namespace != "kube-system" {
			*fileList = append(*fileList, deploymentDir+"/"+deployment.Name+"/"+deployment.Name+".log")
		}
	}
	return nil
}

// Function to get kube-system specific log file names
func getNonCouchbaseLogFileList(kubeClient kubernetes.Interface, crClient versioned.Interface, config *rest.Config, namespace, cbopinfoLogDir string, allFlag bool, reqFileList *[]string) error {
	namespaceDir := cbopinfoLogDir + "/" + namespace

	clusterroleDir := namespaceDir + "/clusterrole"
	clusterroleBindingDir := namespaceDir + "/clusterrolebinding"
	crdDir := namespaceDir + "/customresourcedefinition"
	deploymentDir := namespaceDir + "/deployment"
	endpointsDir := namespaceDir + "/endpoints"
	podDir := namespaceDir + "/pod"
	secretDir := namespaceDir + "/secret"
	serviceDir := namespaceDir + "/service"
	pvDir := cbopinfoLogDir + "/persistentvolume"
	pvcDir := namespaceDir + "/persistentvolumeclaim"

	// clusterrole dir contents
	clusterRoles, err := kubeClient.RbacV1beta1().ClusterRoles().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list cluster roles: " + err.Error())
	}
	for _, clusterRole := range clusterRoles.Items {
		*reqFileList = append(*reqFileList, clusterroleDir+"/"+clusterRole.Name+"/"+clusterRole.Name+".yaml")
	}

	// clusterrolebinding dir contents
	clusterRoleBindings, err := kubeClient.RbacV1beta1().ClusterRoleBindings().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list cluster role bindings: " + err.Error())
	}
	for _, clusterRoleBinding := range clusterRoleBindings.Items {
		*reqFileList = append(*reqFileList, clusterroleBindingDir+"/"+clusterRoleBinding.Name+"/"+clusterRoleBinding.Name+".yaml")
	}

	// customresourcedefinition dir contents
	clientset, err := clientset.NewForConfig(config)
	if err != nil {
		return errors.New("Failed to create clientset object: " + err.Error())
	}
	crds, _ := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list crds: " + err.Error())
	}
	for _, crd := range crds.Items {
		if strings.Contains(crd.Name, "couchbase.com") {
			*reqFileList = append(*reqFileList, crdDir+"/"+crd.Name+"/"+crd.Name+".yaml")
		}
	}

	// deployment dir contents
	if err := getDeployementFileList(kubeClient, namespace, deploymentDir, reqFileList, allFlag); err != nil {
		return err
	}

	if allFlag {
		// endpoints dir content
		endpoints, err := kubeClient.CoreV1().Endpoints(namespace).List(metav1.ListOptions{LabelSelector: "app!=couchbase"})
		if err != nil {
			return errors.New("Failed to list endpoints: " + err.Error())
		}
		for _, endpoint := range endpoints.Items {
			*reqFileList = append(*reqFileList, endpointsDir+"/"+endpoint.Name+"/"+endpoint.Name+".yaml")
		}

		// service dir contents
		services, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: "app!=couchbase"})
		if err != nil {
			return errors.New("Failed to list services: " + err.Error())
		}
		for _, service := range services.Items {
			*reqFileList = append(*reqFileList, serviceDir+"/"+service.Name+"/"+service.Name+".yaml")
		}

		// pod dir contents
		pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: "app!=couchbase"})
		if err != nil {
			return errors.New("Failed to list pods: " + err.Error())
		}
		for _, pod := range pods.Items {
			if strings.Contains(pod.Name, "couchbase-operator-") {
				continue
			}
			*reqFileList = append(*reqFileList, podDir+"/"+pod.Name+"/"+pod.Name+".yaml")
		}

		// persistentvolumeclaims dir contents
		persistentVolClaims, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: "app!=couchbase"})
		if err != nil {
			return errors.New("Failed to list persistent volume claims: " + err.Error())
		}
		for _, pvc := range persistentVolClaims.Items {
			*reqFileList = append(*reqFileList, pvcDir+"/"+pvc.Name+"/"+pvc.Name+".yaml")
		}
	}

	// secret dir contents
	secrets, err := kubeClient.CoreV1().Secrets(namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list secrets: " + err.Error())
	}
	for _, secret := range secrets.Items {
		*reqFileList = append(*reqFileList, secretDir+"/"+secret.Name+"/"+secret.Name+".yaml")
	}

	// persistentvolumes dir contents
	persistentVols, err := kubeClient.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list persistent volumes: " + err.Error())
	}
	for _, pv := range persistentVols.Items {
		*reqFileList = append(*reqFileList, pvDir+"/"+pv.Name+"/"+pv.Name+".yaml")
	}
	return nil
}

// Function to get autonomous-operator extended debug file list
func getOperatorExtendedDebugFileList(namespace, deploymentName, cbopinfoLogDir string, reqFileList *[]string) {
	namespaceDir := cbopinfoLogDir + "/" + namespace
	deploymentDir := namespaceDir + "/deployment/" + deploymentName
	debugFileList := []string{"pprof.block", "pprof.goroutine", "pprof.heap", "pprof.mutex", "pprof.threadcreate", "stats.cluster"}
	for _, fileName := range debugFileList {
		*reqFileList = append(*reqFileList, deploymentDir+"/"+fileName)
	}
}

// Function to get couchbase cluster specific log file names
func getCouchbaseFileList(kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, cbopinfoLogDir, cbClusterName string, reqFileList *[]string) error {
	namespaceDir := cbopinfoLogDir + "/" + namespace

	cbClusterDir := namespaceDir + "/couchbasecluster"
	podDir := namespaceDir + "/pod"
	endpointsDir := namespaceDir + "/endpoints"
	serviceDir := namespaceDir + "/service"
	pvcDir := namespaceDir + "/persistentvolumeclaim"

	// Cluster dependent file - couchbasecluster, endpoints, pods, secrets
	clusters, err := crClient.CouchbaseV1().CouchbaseClusters(namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list clusters: " + err.Error())
	}
	for _, cbCluster := range clusters.Items {
		if cbCluster.Name != cbClusterName {
			continue
		}
		// couchbasecluster dir contents
		*reqFileList = append(*reqFileList, cbClusterDir+"/"+cbCluster.Name+"/"+cbCluster.Name+".yaml")

		// pod dir contents
		pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbCluster.Name})
		if err != nil {
			return errors.New("Failed to list pods: " + err.Error())
		}
		for _, pod := range pods.Items {
			*reqFileList = append(*reqFileList, podDir+"/"+pod.Name+"/couchbase-server.log")
			*reqFileList = append(*reqFileList, podDir+"/"+pod.Name+"/"+pod.Name+".yaml")
		}

		endpoints, err := kubeClient.CoreV1().Endpoints(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbCluster.Name})
		if err != nil {
			return errors.New("Failed to list endpoints: " + err.Error())
		}
		for _, endpoint := range endpoints.Items {
			*reqFileList = append(*reqFileList, endpointsDir+"/"+endpoint.Name+"/"+endpoint.Name+".yaml")
		}

		// service dir contents
		services, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbCluster.Name})
		if err != nil {
			return errors.New("Failed to list services: " + err.Error())
		}
		for _, service := range services.Items {
			*reqFileList = append(*reqFileList, serviceDir+"/"+service.Name+"/"+service.Name+".yaml")
		}

		// persistentvolumeclaims dir contents
		persistentVolClaims, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbCluster.Name})
		if err != nil {
			return errors.New("Failed to list persistent volume claims: " + err.Error())
		}
		for _, pvc := range persistentVolClaims.Items {
			*reqFileList = append(*reqFileList, pvcDir+"/"+pvc.Name+"/"+pvc.Name+".yaml")
		}
	}
	return nil
}

// Function to unzip the zip file
func unzipFile(zipFileName string) error {
	destFileName := strings.Replace(zipFileName, ".zip", "", -1)
	zipReader, err := zip.OpenReader(zipFileName)
	if err != nil {
		return err
	}
	defer zipReader.Close()

	os.MkdirAll(destFileName, 0777)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(file *zip.File) error {
		rc, err := file.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		filePath := filepath.Join(destFileName, file.Name)

		if file.FileInfo().IsDir() {
			os.MkdirAll(filePath, 0777)
		} else {
			os.MkdirAll(filepath.Dir(filePath), 0777)
			fPtr, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
			if err != nil {
				return err
			}
			defer fPtr.Close()

			if _, err := io.Copy(fPtr, rc); err != nil {
				return err
			}
		}
		return nil
	}

	for _, file := range zipReader.File {
		if err := extractAndWriteFile(file); err != nil {
			return err
		}
	}
	return nil
}

// Function to untar the log file
func untarGzFile(tarGzFilePath string) error {
	file, err := os.OpenFile(tarGzFilePath, os.O_RDONLY, 0444)
	defer file.Close()
	if err != nil {
		return err
	}

	gr, err := gzip.NewReader(file)
	defer gr.Close()
	if err != nil {
		return err
	}

	tarReader := tar.NewReader(gr)
	for {
		hdr, err := tarReader.Next()
		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return err
		case err == nil:
			filePath := hdr.Name
			filePathAsList := strings.Split(filePath, "/")
			filePathAsList = filePathAsList[0 : len(filePathAsList)-1]
			filePathToCreate := strings.Join(filePathAsList, "/")
			os.MkdirAll(filePathToCreate, 0766)

			filePtr, err := os.OpenFile(filePath, os.O_RDWR|os.O_TRUNC, 0777)
			defer filePtr.Close()
			if err != nil {
				filePtr, err = os.Create(filePath)
				if err != nil {
					return err
				}
			}

			if _, err := io.Copy(filePtr, tarReader); err != nil {
				return err
			}
		}
	}
}

// Generic function to run cbopinfo command
func runCbopinfoCmd(cmdArgs []string) ([]byte, error) {
	return exec.Command(framework.Global.CbopinfoPath, cmdArgs...).CombinedOutput()
}

func getLogFileNameFromExecOutput(outputStr string) string {
	startIndex := strings.LastIndex(outputStr, "Wrote cluster information to ")
	if startIndex == -1 {
		return ""
	}
	outputStrArr := strings.Split(outputStr[startIndex:], " ")
	return outputStrArr[len(outputStrArr)-1]
}

// Struct to define cbopinfo command args
type cbopinfoArg struct {
	Name        string
	Arg         string
	ArgValue    string
	WillFail    bool
	ExpectedErr string
}

// argumentList represents parameters to cbopinfo.  They are modelled as a
// map to support keys and values (an empty value is ignored) and to allow
// simple overriding (uniqueness).
type argumentList map[string]string

// slice returns the flattened argumentList with empty values removed.
func (a argumentList) slice() []string {
	args := []string{}
	for k, v := range a {
		args = append(args, k)
		if v != "" {
			args = append(args, v)
		}
	}
	return args
}

// add adds a new key and value to the argument list.
func (a argumentList) add(k, v string) {
	a[k] = v
}

// remove removes a kay from the argument list.
func (a argumentList) remove(k string) {
	delete(a, k)
}

// addClusterDefaults adds in configuration specific default arguments that must
// be used for a successful run.
func (a argumentList) addClusterDefaults(k8s *types.Cluster) {
	a.add("--kubeconfig", k8s.KubeConfPath)
	a.add("--namespace", framework.Global.Namespace)
	if k8s.Context != "" {
		a.add("--context", k8s.Context)
	}
}

// addEnvironmentDefaults adds in configuration specific default arguments for deployments
// that should be used for a successful run.
func (a argumentList) addEnvironmentDefaults() {
	a.add("--operator-image", framework.Global.OpImage)
}

// Run cbopinfo command with all valid arguments
// and validate the exit status of the commands
func TestLogCollectValidateArguments(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	kubeConfPath := targetKube.KubeConfPath
	context := targetKube.Context
	t.Logf("KubeConfPath: %+v", kubeConfPath)
	t.Logf("Context: %v", context)
	operatorRestPort := strconv.Itoa(constants.OperatorRestPort)

	// Validate args which won't produce output file
	for _, arg := range []string{"-help", "-version"} {
		if _, err := runCbopinfoCmd([]string{arg}); err != nil {
			t.Fatalf("Failed while providing arg %s: %v", arg, err)
		}
	}

	// Validate all other arguments
	validArgumentList := []cbopinfoArg{
		{
			Name:     "TestValidateCbopinfoAll",
			Arg:      "-all",
			ArgValue: "",
		},
		{
			Name:        "TestValidateCbopinfoKubeconfig",
			Arg:         "-kubeconfig",
			ArgValue:    kubeConfPath,
			ExpectedErr: "flag needs an argument: -kubeconfig",
		},
		{
			Name:        "TestValidateCbopinfoNamespace",
			Arg:         "-namespace",
			ArgValue:    f.Namespace,
			ExpectedErr: "flag needs an argument: -namespace",
		},
		{
			Name:     "TestValidateCbopinfoSystem",
			Arg:      "-system",
			ArgValue: "",
		},
		{
			Name:        "TestValidateCbopinfoOperatorImage",
			Arg:         "-operator-image",
			ArgValue:    f.OpImage,
			ExpectedErr: "flag needs an argument: -operator-image",
		},
		{
			Name:        "TestValidateCbopinfoOperatorRestPort",
			Arg:         "-operator-rest-port",
			ArgValue:    operatorRestPort,
			ExpectedErr: "flag needs an argument: -operator-rest-port",
		},
	}

	// Deploy cb server for cbopinfo validation
	e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithoutBucket, constants.AdminHidden)

	for _, arg := range validArgumentList {
		t.Run(arg.Name, func(t *testing.T) {
			args := argumentList{}
			args.addClusterDefaults(targetKube)
			args.add(arg.Arg, arg.ArgValue)

			execOut, err := runCbopinfoCmd(args.slice())
			execOutStr := strings.TrimSpace(string(execOut))
			t.Logf("Returned: %s\n", execOutStr)
			if err != nil {
				t.Fatalf("Failed while providing arg %s: %v", arg.Arg, err)
			}
			logFileName := getLogFileNameFromExecOutput(execOutStr)
			defer os.Remove(logFileName)

			logFileDir := strings.Split(logFileName, ".")[0]
			defer os.RemoveAll(logFileDir)
			if err := untarGzFile(logFileName); err != nil {
				t.Fatalf("Failed to untar file %s: %v", logFileName, err)
			}

			// Check command fails with missing argument value
			if arg.ArgValue != "" {
				args := argumentList{}
				args.add(arg.Arg, "")
				execOut, err := runCbopinfoCmd(args.slice())
				execOutStr := strings.TrimSpace(string(execOut))
				t.Logf("Returned: %s\n", execOutStr)
				if err == nil {
					t.Fatalf("Command executed successfully without providing value for %s: %v", arg.Arg, err)
				}

				// Verify valid error message
				if !strings.Contains(execOutStr, arg.ExpectedErr) {
					t.Fatalf("Invalid error for missing arg value %s\nExpected: %v\nReceived: %v", arg.Arg, arg.ExpectedErr, execOutStr)
				}

				// Check no output file is generated
				if logFileName := getLogFileNameFromExecOutput(execOutStr); logFileName != "" {
					t.Fatalf("File created with missing argument for %s", arg.Arg)
				}
			}
		})
	}
}

// Negative test scenarios with command argument
// TODO: Development cannot run/fix this
func TestNegLogCollectValidateArgs(t *testing.T) {
	invalidKubeConfPath := e2eutil.GetKubeConfigToUse(framework.Global.KubeType, "k8s_reclustered")
	unreachableKubeConfPath := e2eutil.GetKubeConfigToUse(framework.Global.KubeType, "k8s_unreachable")
	errMsgList := failureList{}

	validArgumentList := []cbopinfoArg{
		{
			Name:        "Validating invalid '-kubeconfig' file",
			Arg:         "-kubeconfig",
			ArgValue:    invalidKubeConfPath,
			WillFail:    true,
			ExpectedErr: "unable to discover cluster resources",
		},
		{
			Name:        "Unreachable '-kubeconfig' file",
			Arg:         "-kubeconfig",
			ArgValue:    unreachableKubeConfPath,
			WillFail:    true,
			ExpectedErr: "unable to discover cluster resources",
		},
		{
			Name:        "Validating invalid '-kubeconfig' file missing",
			Arg:         "-kubeconfig",
			ArgValue:    "/tmp/fileNotFound",
			WillFail:    true,
			ExpectedErr: "unable to initialize context: stat /tmp/fileNotFound: no such file or directory",
		},
	}

	for _, arg := range validArgumentList {
		t.Log(arg.Name)
		cmdArgs := []string{arg.Arg}
		cmdArgs = append(cmdArgs, arg.ArgValue)

		execOut, err := runCbopinfoCmd(cmdArgs)
		execOutStr := strings.TrimSpace(string(execOut))
		t.Logf("Returned: %s\n", execOutStr)
		if err == nil {
			errMsgList.AppendFailure(arg.Name, errors.New("Command executed successfully"))
		}

		// Verify valid error message
		if !strings.Contains(execOutStr, arg.ExpectedErr) {
			errMsgList.AppendFailure(arg.Name, errors.New("Invalid error message: "+execOutStr))
		}

		// Check no output file is generated
		if logFileName := getLogFileNameFromExecOutput(execOutStr); logFileName != "" {
			errMsgList.AppendFailure(arg.Name, errors.New("Log file created"))
			os.Remove(logFileName)
		}
	}
	errMsgList.CheckFailures(t)
}

// Create a couchbase cluster
// Get the logs from the desired clustername and namespace and verify
// Get logs from multiple / all clusters and verify the files
func TestLogCollectUsingClusterNameAndNamespace(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	failureExists := false
	cluster1Size := constants.Size3
	cluster2Size := constants.Size3
	cluster3Size := constants.Size1
	var cluster1, cluster2, cluster3 *api.CouchbaseCluster
	cluster1Err := make(chan error)
	cluster2Err := make(chan error)
	cluster3Err := make(chan error)

	go func() {
		var err error
		cluster1, err = e2eutil.NewClusterBasic(t, targetKube, f.Namespace, cluster1Size, constants.WithoutBucket, constants.AdminHidden)
		cluster1Err <- err
	}()

	go func() {
		var err error
		cluster2, err = e2eutil.NewClusterBasic(t, targetKube, f.Namespace, cluster2Size, constants.WithoutBucket, constants.AdminHidden)
		cluster2Err <- err
	}()

	go func() {
		var err error
		cluster3, err = e2eutil.NewClusterBasic(t, targetKube, f.Namespace, cluster3Size, constants.WithoutBucket, constants.AdminHidden)
		cluster3Err <- err
	}()

	for _, errChan := range []chan error{cluster1Err, cluster2Err, cluster3Err} {
		if err := <-errChan; err != nil {
			t.Fatal(err)
		}
	}

	isAllFlagSet := false

	/////////////////////////////////////////////////////
	// Log collection using '-namespace' & cluster arg //
	/////////////////////////////////////////////////////
	t.Log("Collecting logs from single cluster")
	reqFileList := []string{}
	errMsgList := failureList{}
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	commonArgs := args.slice()
	cmdArgs := append(commonArgs, cluster1.Name)
	execOut, err := runCbopinfoCmd(cmdArgs)
	execOutStr := strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster1.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists = errMsgList.PrintFailures(t) || failureExists

	// collect logs from multi clusters by specifying cluster names in command line
	t.Log("Collecting logs from cluster1 and cluster3")
	reqFileList = []string{}
	errMsgList = failureList{}
	cmdArgs = append(commonArgs, cluster1.Name, cluster3.Name)
	execOut, err = runCbopinfoCmd(cmdArgs)
	execOutStr = strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster3.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}

	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists = errMsgList.PrintFailures(t) || failureExists

	// collect logs from all clusters in the given namespace
	t.Log("Collecting logs from all available cluster")
	reqFileList = []string{}
	errMsgList = failureList{}
	execOut, err = runCbopinfoCmd(commonArgs)
	execOutStr = strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster2.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists = errMsgList.PrintFailures(t) || failureExists

	///////////////////////////////////////////////
	/////// Log collection using '-system' ////////
	///////////////////////////////////////////////
	t.Log("Log Verification for kube-system and single cb cluster")
	reqFileList = []string{}
	errMsgList = failureList{}
	cmdArgs = append(commonArgs, "-system", cluster2.Name)
	execOut, err = runCbopinfoCmd(cmdArgs)
	execOutStr = strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	for _, namespace := range []string{f.Namespace, "kube-system"} {
		if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster2.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists = errMsgList.PrintFailures(t) || failureExists

	// Verify kube-system logs with multiple couchbase cluster logs
	t.Log("Collecting logs from specific cb clusters")
	reqFileList = []string{}
	errMsgList = failureList{}
	cmdArgs = append(commonArgs, "-system", cluster1.Name, cluster3.Name)
	execOut, err = runCbopinfoCmd(cmdArgs)
	execOutStr = strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	for _, namespace := range []string{f.Namespace, "kube-system"} {
		if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	for _, clusterName := range []string{cluster1.Name, cluster3.Name} {
		if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, clusterName, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists = errMsgList.PrintFailures(t) || failureExists

	// Verify kube-system logs with all other cb cluster logs
	t.Log("Collecting logs from all available cb clusters")
	reqFileList = []string{}
	errMsgList = failureList{}
	cmdArgs = append(commonArgs, "-system")
	execOut, err = runCbopinfoCmd(cmdArgs)
	execOutStr = strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	for _, namespace := range []string{f.Namespace, "kube-system"} {
		if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	for _, clusterName := range []string{cluster1.Name, cluster2.Name, cluster3.Name} {
		if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, clusterName, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists = errMsgList.PrintFailures(t) || failureExists

	///////////////////////////////////////////////////
	/////// Log collection using '-collectinfo' ///////
	///////////////////////////////////////////////////
	t.Log("Collecting logs with -collectinfo flag on single cb cluster")
	reqFileList = []string{}
	errMsgList = failureList{}
	cmdArgs = append(commonArgs, "-collectinfo", "-collectinfo-collect", "all", cluster1.Name)
	execOut, err = runCbopinfoCmd(cmdArgs)
	execOutStr = strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster1.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists = errMsgList.PrintFailures(t) || failureExists

	///////////////////////////////////////////////////
	/////////// Log collection using '-all' ///////////
	///////////////////////////////////////////////////
	t.Log("Collecting logs with -all flag on all cb clusters")
	reqFileList = []string{}
	errMsgList = failureList{}
	cmdArgs = append(commonArgs, "-all")
	execOut, err = runCbopinfoCmd(cmdArgs)
	execOutStr = strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	isAllFlagSet = true
	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
		t.Fatal(err)
	}
	for _, clusterName := range []string{cluster1.Name, cluster2.Name, cluster3.Name} {
		if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, clusterName, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists = errMsgList.PrintFailures(t) || failureExists

	if err := checkCollectInfoLogs(targetKube.KubeClient, f.Namespace, cluster1.Name, logFileDir, false, &errMsgList); err != nil {
		t.Fatal(err)
	}

	if failureExists {
		t.Fatal("Test failed")
	}
}

// Create couchbase cluster
// Create Rbac user with reduced k8s cluster access
// Verify collected log file list with reduced cluster access
func TestLogCollectRbacPermission(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Create the cluster.
	cluster := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithoutBucket, constants.AdminHidden)

	// Create a service account with no permissions.
	if err := framework.RecreateServiceAccount(targetKube.KubeClient, f.Namespace, cluster.Name); err != nil {
		e2eutil.Die(t, err)
	}
	defer framework.RemoveServiceAccount(targetKube.KubeClient, f.Namespace, cluster.Name)

	// Create a kubernetes configuration file.
	sa, err := targetKube.KubeClient.CoreV1().ServiceAccounts(f.Namespace).Get(cluster.Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	secret, err := targetKube.KubeClient.CoreV1().Secrets(f.Namespace).Get(sa.Secrets[0].Name, metav1.GetOptions{})
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
					Namespace: f.Namespace,
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
	if _, err := kubeconfig.Write(data); err != nil {
		e2eutil.Die(t, err)
	}
	if err := kubeconfig.Sync(); err != nil {
		e2eutil.Die(t, err)
	}

	// Collect logs
	args := argumentList{}
	args.add("--kubeconfig", kubeconfig.Name())
	execOut, err := runCbopinfoCmd(args.slice())
	execOutStr := strings.TrimSpace(string(execOut))
	t.Log(execOutStr)
	expectedErrMsg := "unable to poll CouchbaseCluster resources: couchbaseclusters.couchbase.com is forbidden: User \"system:serviceaccount:" + f.Namespace + ":" + cluster.Name + "\" cannot list couchbaseclusters.couchbase.com in the namespace \"" + f.Namespace + "\""
	if err == nil {
		e2eutil.Die(t, fmt.Errorf("Able to read resource without valid rbac permissions"))
	}
	if execOutStr != expectedErrMsg {
		e2eutil.Die(t, fmt.Errorf("Invalid error message"))
	}
}

func ReDeployOperator(t *testing.T, kubeClient kubernetes.Interface, imageName string, port int) error {
	f := framework.Global

	// Delete existing Deployment
	if err := framework.DeleteOperatorCompletely(kubeClient, f.Deployment.Name, f.Namespace); err != nil {
		return err
	}

	// Create new deployment object to deploy
	deployment, err := framework.CreateDeploymentObject(imageName, port)
	if err != nil {
		return err
	}

	t.Logf("Deploying operator using image '%s' and port %d", imageName, port)
	if _, err := kubeClient.AppsV1().Deployments(f.Namespace).Create(deployment); err != nil {
		return err
	}
	if err := e2eutil.WaitUntilOperatorReady(kubeClient, f.Namespace, constants.CouchbaseOperatorLabel); err != nil {
		return err
	}
	return nil
}

/***********************************
   Operator extended debug cases
***********************************/

// Generic function to re-deploy the operator with given image name and rest-port
// Collect logs with appropiate cbopinfo arguments and verify the collected info
func CollectExtendedDebugLogGeneric(t *testing.T, k8s *types.Cluster, operatorImage string, operatorPort int, cmdArgs []string) {
	f := framework.Global
	targetKube := k8s
	clusterSize := 3

	defer ReDeployOperator(t, targetKube.KubeClient, f.OpImage, 0)
	if err := ReDeployOperator(t, targetKube.KubeClient, operatorImage, operatorPort); err != nil {
		t.Fatal(err)
	}

	// Create Couchbase cluster
	cbCluster := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)
	defer e2eutil.CleanUpCluster(t, targetKube, f.Namespace, f.LogDir, f.TestClusters[0], t.Name())

	// Collect logs
	execOut, err := runCbopinfoCmd(append(cmdArgs, cbCluster.Name))
	execOutStr := strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	// Verify file list
	errMsgList := failureList{}
	reqFileList := []string{}

	getOperatorExtendedDebugFileList(f.Namespace, f.Deployment.Name, logFileDir, &reqFileList)

	isAllFlagSet := true
	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cbCluster.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}

	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists := errMsgList.PrintFailures(t)

	if err := checkCollectInfoLogs(targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, false, &errMsgList); err != nil {
		t.Fatal(err)
	}

	if failureExists {
		t.Fatal("Test failed")
	}
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
	targetKube := f.GetCluster(0)
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	args.add("--collectinfo", "")
	args.add("--collectinfo-collect", "all")
	args.add("--all", "")
	CollectExtendedDebugLogGeneric(t, targetKube, f.OpImage, constants.OperatorRestPort, args.slice())
}

// Collect cbopinfo using '--operator-image' and '--operator-rest-port'
// with custom values and validate the logs collected
func TestExtendedDebugWithNonDefaultValues(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	testPort := 32123
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	args.add("--operator-rest-port", strconv.Itoa(testPort))
	args.add("--collectinfo", "")
	args.add("--collectinfo-collect", "all")
	args.add("--all", "")
	CollectExtendedDebugLogGeneric(t, targetKube, f.OpImage, testPort, args.slice())
}

// Collect cbopinfo with '--operator-image' & '-operator-rest-port'
// with invalid values and validate the log collection
func TestExtendedDebugWithInvalidValues(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	invalidImgName := "couchbase/couchbase-operator:invalidversion"
	invalidPortVal := "32080"
	clusterSize := constants.Size1
	cbopinfoAllFlag := false

	// Create Couchbase cluster
	cbCluster := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// Collect logs with invalid operator-image-name
	t.Log("Collecting logs using invalid -operator-image arg value")
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.add("--operator-image", invalidImgName)
	args.add("--operator-rest-port", strconv.Itoa(constants.OperatorRestPort))

	execOut, err := runCbopinfoCmd(append(args.slice(), cbCluster.Name))
	execOutStr := strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	// Untar log file
	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Error(err)
	} else {
		// Verify file list if untar is successful
		errMsgList := failureList{}
		excludedFileList := []string{}
		deploymentDir := logFileDir + "/" + f.Namespace + "/deployment"

		if err := getDeployementFileList(targetKube.KubeClient, f.Namespace, deploymentDir, &excludedFileList, cbopinfoAllFlag); err != nil {
			t.Error(err)
		}
		getOperatorExtendedDebugFileList(f.Namespace, f.Deployment.Name, logFileDir, &excludedFileList)

		checkLogDirContentsForExcludedFiles(excludedFileList, logFileDir, &errMsgList)
		if failureExists := errMsgList.PrintFailures(t); failureExists {
			t.Error("Log file verification failed")
		}
	}

	// Collect logs with invalid operator-rest-port
	t.Log("Collecting logs using invalid -operator-rest-port arg value")
	args = argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	args.add("--operator-rest-port", invalidPortVal)

	execOut, err = runCbopinfoCmd(append(args.slice(), cbCluster.Name))
	execOutStr = strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	// Untar log file
	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Error(err)
	} else {
		// Verify file list if untar is successful
		errMsgList := failureList{}
		reqFileList := []string{}
		excludedFileList := []string{}
		deploymentDir := logFileDir + "/" + f.Namespace + "/deployment"

		// In invalid rest-port case, deployment yaml, logs will exists. Only rest-port files will be missing
		if err := getDeployementFileList(targetKube.KubeClient, f.Namespace, deploymentDir, &reqFileList, cbopinfoAllFlag); err != nil {
			t.Error(err)
		}
		getOperatorExtendedDebugFileList(f.Namespace, f.Deployment.Name, logFileDir, &excludedFileList)

		checkLogDirContents(reqFileList, logFileDir, &errMsgList)
		checkLogDirContentsForExcludedFiles(excludedFileList, logFileDir, &errMsgList)
		if failureExists := errMsgList.PrintFailures(t); failureExists {
			t.Error("Log file verification failed")
		}
	}
}

// Collect cbopinfo with '-operator-image' & '-operator-rest-port'
// and kill the operator pod during log collection in parallel
func TestExtendedDebugKillOperatorDuringLogCollection(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	clusterSize := constants.Size1
	execOut := []byte{}

	// Create Couchbase cluster
	cbCluster := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	t.Log("Collecting logs using invalid operator-image value")
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	args.add("--operator-rest-port", strconv.Itoa(constants.OperatorRestPort))
	args.add("--collectinfo", "")
	args.add("--collectinfo-collect", "all")
	args.add("--all", "")

	logFileNameChan := make(chan string)
	go func() {
		// Collect logs when operator pod goes down in parallel
		t.Log("Starting log collection")
		var err error
		execOut, err = runCbopinfoCmd(append(args.slice(), cbCluster.Name))
		execOutStr := strings.TrimSpace(string(execOut))
		t.Logf("Returned: %s\n", execOutStr)
		// TODO: This is broken, you cannot call Fatal() in a go routine :/
		if err != nil {
			t.Fatal(err)
		}
		logFileNameChan <- getLogFileNameFromExecOutput(execOutStr)
	}()

	if err := e2eutil.KillOperatorAndWaitForRecovery(t, targetKube.KubeClient, f.Namespace); err != nil {
		t.Fatal(err)
	}

	logFileName := <-logFileNameChan
	defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	// Verify file list
	errMsgList := failureList{}
	reqFileList := []string{}

	getOperatorExtendedDebugFileList(f.Namespace, f.Deployment.Name, logFileDir, &reqFileList)

	isAllFlagSet := true
	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, isAllFlagSet, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cbCluster.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}

	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists := errMsgList.PrintFailures(t)

	if err := checkCollectInfoLogs(targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, false, &errMsgList); err != nil {
		t.Fatal(err)
	}

	if failureExists {
		t.Fatal("Test failed")
	}
}

/**************************************
  Ephemeral pod log collection cases
***************************************/

// Generic function to kill couchbase server pod and operator with log PVs defined for server pods
// 'podDownMethod' argument with either one of ['deletePod', 'killServerProcess']
func EphemeralLogCollectUsingLogPVGeneric(t *testing.T, k8s *types.Cluster, podDownMethod string, isOperatorKilledWithServerPod bool) {
	f := framework.Global
	targetKube := k8s

	clusterSize := 5
	newMemberIndex := clusterSize
	podMembersToKill := []int{2, 3, 4}
	bucketName := "PVBucket"
	pvcName := "couchbase-log-pv"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"
	clusterConfig["AutoFailoverTimeout"] = "5"
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size2, "test_config_1", []string{"data"})
	serviceConfig1["defaultVolMnt"] = pvcName

	serviceConfig2 := e2eutil.GetServiceConfigMap(constants.Size3, "test_config_2", []string{"search", "query", "eventing"})
	serviceConfig2["logVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 2, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	otherConfig1 := map[string]string{
		"logRetentionTime":  "2h",
		"logRetentionCount": "3",
	}

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
		"other1":   otherConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 5*time.Minute)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(cbCluster, "Create", bucketName)

	// To cross check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{
		couchbaseutil.CreateMemberName(cbCluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 3): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 4): 1,
	}

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)

	// Kill PV log enabled pods and verify the logs are persisted after pod deletion
	for _, memberToKill := range podMembersToKill {
		// Kills operator pod in async way
		if isOperatorKilledWithServerPod {
			e2eutil.MustDeleteCouchbaseOperator(t, targetKube, f.Namespace)
		}

		switch podDownMethod {
		case "deletePod":
			// Only in DeletePod, FailOver and NewMemberAdd is triggered
			e2eutil.MustKillPodForMember(t, targetKube, cbCluster, memberToKill, false)
			e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberFailedOverEvent(cbCluster, memberToKill), time.Minute)
			e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberAddEvent(cbCluster, newMemberIndex), 3*time.Minute)
			e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberRemoveEvent(cbCluster, memberToKill), 5*time.Minute)
			e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)

			// To validate the new PVC created for new pod
			newMemberName := couchbaseutil.CreateMemberName(cbCluster.Name, newMemberIndex)
			expectedPvcMap[newMemberName] = 1

			expectedEvents.AddClusterPodEvent(cbCluster, "MemberDown", memberToKill)
			expectedEvents.AddClusterPodEvent(cbCluster, "FailedOver", memberToKill)
			expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", newMemberIndex)
			expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
			expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberToKill)
			expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

		case "killServerProcess":
			// In KillServerProcess, cluster rebalance is triggered after cb service is restarted by operator
			podNameToKill := couchbaseutil.CreateMemberName(cbCluster.Name, memberToKill)
			e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, podNameToKill, "pkill beam.smp")
			e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)

			expectedEvents.AddOptionalClusterPodEvent(cbCluster, "MemberDown", memberToKill)
			expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
			expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")
		}

		newMemberIndex++
	}

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)
	ValidateEvents(t, targetKube, cbCluster, expectedEvents)
}

// Generic function to kill Cb server pod and update the server class in parallel
// and check how operator handles the log retention as expected
func LogCollectWithClusterResizeAndServerPodKilledGeneric(t *testing.T, isOperatorKilledWithServerPod bool) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	operatorKilledErrChan := make(chan error)
	clusterSize := 6
	serverIndexToResize := 1
	podMemberToKill := 3
	bucketName := "PVBucket"
	pvcName := "couchbase-log-pv"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size3, "test_config_1", []string{"data"})
	serviceConfig1["defaultVolMnt"] = pvcName

	serviceConfig2 := e2eutil.GetServiceConfigMap(constants.Size3, "test_config_2", []string{"search", "query"})
	serviceConfig2["logVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 2, constants.BucketFlushDisabled, constants.IndexReplicaDisabled)

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	// Create Cb cluster
	cbCluster := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 5*time.Minute)

	// Add expected kube events for verification
	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(cbCluster, "Create", bucketName)

	// To cross check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{
		couchbaseutil.CreateMemberName(cbCluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 3): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 4): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 5): 1,
	}

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)

	// Trigger async Cluster's service config resize
	cbCluster = e2eutil.MustResizeClusterNoWait(t, serverIndexToResize, constants.Size1, targetKube, cbCluster)

	// Kill operator if flag is enabled
	if isOperatorKilledWithServerPod {
		go func() {
			operatorKilledErrChan <- e2eutil.KillOperatorAndWaitForRecovery(t, targetKube.KubeClient, f.Namespace)
		}()
	}

	// Kill pod in parallel to resize
	podNameToKill := couchbaseutil.CreateMemberName(cbCluster.Name, podMemberToKill)
	if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podNameToKill, metav1.NewDeleteOptions(0)); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddClusterPodEvent(cbCluster, "MemberDown", podMemberToKill)

	// If operator was killed, will waits for operator recovery to happen
	if isOperatorKilledWithServerPod {
		if err := <-operatorKilledErrChan; err != nil {
			t.Fatal(err)
		}
	}

	// Wait for failover of killed server pod
	e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberFailedOverEvent(cbCluster, podMemberToKill), 2*time.Minute)
	expectedEvents.AddClusterPodEvent(cbCluster, "FailedOver", podMemberToKill)

	// Wait for rebalance complete event
	e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
	expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", podMemberToKill)
	expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", clusterSize-1)
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

	// Updating expectedPvcMap for resizing pods
	expectedPvcMap[couchbaseutil.CreateMemberName(cbCluster.Name, clusterSize-1)] = 0

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)
	ValidateEvents(t, targetKube, cbCluster, expectedEvents)
}

// Define log mount for ephemeral pods and validate the logs are preserved
// even after abnormal pod removal
func TestCollectLogFromEphemeralPodsUsingLogPV(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	isOperatorKilledWithServerPod := false

	// Pods brought down using DeletePod method
	EphemeralLogCollectUsingLogPVGeneric(t, targetKube, "deletePod", isOperatorKilledWithServerPod)
}

func TestCollectLogFromEphemeralPodsUsingLogPVKillProcess(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	isOperatorKilledWithServerPod := false

	if f.KubeType != "kubernetes" {
		t.Skip("unsupported platform")
	}

	// Pods brought down by killing cb-server process
	EphemeralLogCollectUsingLogPVGeneric(t, targetKube, "killServerProcess", isOperatorKilledWithServerPod)
}

// Define log mount for ephemeral pods and validate the logs are preserved
// even after abnormal pod removal
func TestCollectLogFromEphemeralPodsWithOperatorKilled(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	isOperatorKilledWithServerPod := true

	// Pods brought down using DeletePod method
	EphemeralLogCollectUsingLogPVGeneric(t, targetKube, "deletePod", isOperatorKilledWithServerPod)
}

func TestCollectLogFromEphemeralPodsWithOperatorKilledKillProcess(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)
	isOperatorKilledWithServerPod := true

	if f.KubeType != "kubernetes" {
		t.Skip("unsupported platform")
	}

	// Pods brought down by killing cb-server process
	EphemeralLogCollectUsingLogPVGeneric(t, targetKube, "killServerProcess", isOperatorKilledWithServerPod)
}

// Deploys Couchbase server with log PV defined for server pods
// Scale down the couchbase cluster and check log PVs cleanup has happened
func TestEphemeralLogCollectResizeCluster(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 7
	bucketName := "PVBucket"
	pvcName := "couchbase-log-pv"
	serviceIndexToResize := 1
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size2, "test_config_1", []string{"data"})
	serviceConfig1["defaultVolMnt"] = pvcName

	serviceConfig2 := e2eutil.GetServiceConfigMap(constants.Size5, "test_config_2", []string{"search", "query", "eventing"})
	serviceConfig2["logVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 2, constants.BucketFlushDisabled, constants.IndexReplicaDisabled)
	otherConfig1 := map[string]string{
		"logRetentionTime":  "2h",
		"logRetentionCount": "3",
	}

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
		"other1":   otherConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 5*time.Minute)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(cbCluster, "Create", bucketName)

	// To cross check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{
		couchbaseutil.CreateMemberName(cbCluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 3): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 4): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 5): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 6): 1,
	}

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)

	// Start resizing service config to 2 node service
	serviceSize := constants.Size2
	cbCluster = e2eutil.MustResizeClusterNoWait(t, serviceIndexToResize, serviceSize, targetKube, cbCluster)
	e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)

	// Add expected events
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
	for memberIndex := 4; memberIndex <= 6; memberIndex++ {
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIndex)
		memberName := couchbaseutil.CreateMemberName(cbCluster.Name, memberIndex)
		// Update expectedPvcMap for removed nodes
		expectedPvcMap[memberName] = 0
	}
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)

	// Start resizing service config to 4 node service
	serviceSize = constants.Size4
	cbCluster = e2eutil.MustResizeClusterNoWait(t, serviceIndexToResize, serviceSize, targetKube, cbCluster)
	e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)

	// Add expected events
	for memberIndex := 7; memberIndex <= 8; memberIndex++ {
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", memberIndex)
		memberName := couchbaseutil.CreateMemberName(cbCluster.Name, memberIndex)
		// Update expectedPvcMap for removed nodes
		expectedPvcMap[memberName] = 1
	}
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)

	// Start resizing service config to 4 node service
	serviceSize = constants.Size1
	cbCluster = e2eutil.MustResizeClusterNoWait(t, serviceIndexToResize, serviceSize, targetKube, cbCluster)
	e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)

	// Add expected events
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
	for _, memberIndex := range []int{3, 7, 8} {
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIndex)
		memberName := couchbaseutil.CreateMemberName(cbCluster.Name, memberIndex)
		// Update expectedPvcMap for removed nodes
		expectedPvcMap[memberName] = 0
	}
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)
	ValidateEvents(t, targetKube, cbCluster, expectedEvents)
}

// Kill Cb server pod and update the server class in parallel
// and check how operator handles the log retention
func TestLogCollectWithClusterResizeAndServerPodKilled(t *testing.T) {
	isOperatorKilledWithServerPod := false
	LogCollectWithClusterResizeAndServerPodKilledGeneric(t, isOperatorKilledWithServerPod)
}

// Kill Operator, Cb server pod anb update the server class all in parallel
// and check how operator handles the log retention
func TestLogCollectWithClusterResizeAndOperatorPodKilled(t *testing.T) {
	isOperatorKilledWithServerPod := true
	LogCollectWithClusterResizeAndServerPodKilledGeneric(t, isOperatorKilledWithServerPod)
}

// Collect logs from ephemeral log PVs
// using default log retention time and size values
func TestLogCollectWithDefaultRetentionAndSize(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 5
	newPodMemberId := clusterSize
	bucketName := "PVBucket"
	pvcName := "couchbase-log-pv"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size2, "test_config_1", []string{"data"})
	serviceConfig1["defaultVolMnt"] = pvcName

	serviceConfig2 := e2eutil.GetServiceConfigMap(constants.Size3, "test_config_2", []string{"search", "query", "eventing"})
	serviceConfig2["logVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 2, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 5*time.Minute)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
	expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(cbCluster, "Create", bucketName)

	// To cross check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{
		couchbaseutil.CreateMemberName(cbCluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 3): 1,
		couchbaseutil.CreateMemberName(cbCluster.Name, 4): 1,
	}

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)

	for memberIdToKill := 2; memberIdToKill <= 7; memberIdToKill++ {
		memberNameToKill := couchbaseutil.CreateMemberName(cbCluster.Name, memberIdToKill)
		if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, memberNameToKill, metav1.NewDeleteOptions(0)); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberDown", memberIdToKill)

		// Wait for failover event
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberFailedOverEvent(cbCluster, memberIdToKill), time.Minute)
		expectedEvents.AddClusterPodEvent(cbCluster, "FailedOver", memberIdToKill)

		// Wait for new pod add event
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberAddEvent(cbCluster, newPodMemberId), 3*time.Minute)
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", newPodMemberId)

		// Wait for rebalance complete event
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)

		// Add expected events for cluster for verification
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIdToKill)
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

		// Updating expectedPvcMap for new cluster pod
		expectedPvcMap[couchbaseutil.CreateMemberName(cbCluster.Name, newPodMemberId)] = 1
		newPodMemberId++
	}

	// Verifying the persistence of log PVs are preserved by operator
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap, f.PlatformType)
	ValidateEvents(t, targetKube, cbCluster, expectedEvents)
}

// Collect logs from ephemeral log PVs
// using custom log retention time and size values
func TestLogCollectWithCustomRetentionAndSize(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victims := 6
	maxLogCount := 2
	maxLogRetention := "15m"
	autoFailoverTimeout := uint64(30)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)

	// Track pods we create and their expected number of persistent volumes.
	expectedPvcMap := map[string]int{}
	for i := 0; i < clusterSize; i++ {
		expectedPvcMap[couchbaseutil.CreateMemberName(cluster.Name, i)] = 1
	}

	// When ready check that PVCs are correctly associated with the pod. Update the rentention settings.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	MustVerifyPvcMappingForPods(t, kubernetes, f.Namespace, expectedPvcMap, f.PlatformType)
	patches := jsonpatch.NewPatchSet()
	patches.Replace("/Spec/ClusterSettings/AutoFailoverTimeout", autoFailoverTimeout)
	patches.Replace("/Spec/LogRetentionTime", maxLogRetention)
	patches.Replace("/Spec/LogRetentionCount", maxLogCount)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patches, time.Minute)

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

	// Verify the persistence of log PVCs that are preserved by operator.
	MustVerifyPvcMappingForPods(t, kubernetes, f.Namespace, expectedPvcMap, f.PlatformType)

	// Check the events match what we expect:
	// * Cluster created
	// * Members go down and are failed over
	// * New members balanced in to replace the failed ones
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Repeat{Times: victims, Validator: e2eutil.PodDownFailoverRecoverySequence()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

/**************************************
  Persistent pods log collection cases
***************************************/

// Generic function to deploy Couchbase server with default persistent volumes mounts
// Argument 'serverMemberIdsToKill' will kill those pod members and wait for recovery before log collection
// Argument 'isOperatorKilledWithServerPod' tells whether operator needs to be killed along with server pods
func LogCollectionWithDefaultPvcMount(t *testing.T, k8s *types.Cluster, serverMemberIdToKill map[int]string, isOperatorKilledWithServerPod bool) {
	f := framework.Global
	targetKube := k8s

	clusterSize := constants.Size2
	operatorKilledErrChan := make(chan error)
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "analytics"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["indexVolMnt"] = pvcName
	serviceConfig1["analyticsVolMnt"] = pvcName + "," + pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap("default", "couchbase", "high", constants.Mem256Mb, 2, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 2*time.Minute)

	// Kills operator pod in async way
	if isOperatorKilledWithServerPod {
		go func() {
			operatorKilledErrChan <- e2eutil.KillOperatorAndWaitForRecovery(t, targetKube.KubeClient, f.Namespace)
		}()
	}

	// Delete pod with default persistent volumes defined for it
	for podMemberToKill, podDownMethod := range serverMemberIdToKill {
		podNameToKill := couchbaseutil.CreateMemberName(cbCluster.Name, podMemberToKill)
		switch podDownMethod {
		case "deletePod":
			if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podNameToKill, metav1.NewDeleteOptions(0)); err != nil {
				t.Fatal(err)
			}
		case "killServerProcess":
			e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, podNameToKill, "pkill beam.smp")
		}
	}

	// Wait for operator recovery to happen
	if isOperatorKilledWithServerPod {
		if err := <-operatorKilledErrChan; err != nil {
			t.Fatal(err)
		}
	}

	if len(serverMemberIdToKill) != 0 {
		// Wait for cluster to be rebalanced before log collection and verification
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceStartedEvent(cbCluster), 5*time.Minute)
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)
	}

	// Collect logs
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	args.add("--collectinfo", "")
	args.add("--collectinfo-collect", "all")
	args.add("--all", "")
	execOut, err := runCbopinfoCmd(args.slice())
	execOutStr := strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	// Verify file list
	errMsgList := failureList{}
	reqFileList := []string{}
	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, true, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cbCluster.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	if err := checkCollectInfoLogs(targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, false, &errMsgList); err != nil {
		t.Error(err)
	}
	errMsgList.CheckFailures(t)
}

// Create couchbase cluster with persistent volume claim
// Collect log and check for persistent volume definition files
func TestLogCollectClusterWithPVC(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	serverPodsToKill := map[int]string{}
	isOperatorKilledWithServerPod := false
	LogCollectionWithDefaultPvcMount(t, targetKube, serverPodsToKill, isOperatorKilledWithServerPod)
}

// Create couchbase cluster with persistent volume claim
// Bring down a pod and collect log and check for persistent volume definition files
func TestCollectLogFromPvPodRecovered(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	isOperatorKilledWithServerPod := false

	// Pods brought down by DeletePod API
	serverPodsToKill := map[int]string{
		1: "deletePod",
	}
	LogCollectionWithDefaultPvcMount(t, targetKube, serverPodsToKill, isOperatorKilledWithServerPod)

	// Pods brought down by killing cb-server process
	if f.KubeType == "kubernetes" {
		serverPodsToKill := map[int]string{
			1: "killServerProcess",
		}
		LogCollectionWithDefaultPvcMount(t, targetKube, serverPodsToKill, isOperatorKilledWithServerPod)
	}
}

// Create couchbase cluster with persistent volume claim
// Bring down a pod with operator and wait for recovery
// Collect log and check for persistent volume definition files
func TestCollectLogFromPvPodAndOperatorRecovered(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	isOperatorKilledWithServerPod := true

	// Pods brought down by DeletePod API
	serverPodsToKill := map[int]string{
		1: "deletePod",
	}
	LogCollectionWithDefaultPvcMount(t, targetKube, serverPodsToKill, isOperatorKilledWithServerPod)

	// Pods brought down by killing cb-server process
	if f.KubeType == "kubernetes" {
		serverPodsToKill := map[int]string{
			1: "killServerProcess",
		}
		LogCollectionWithDefaultPvcMount(t, targetKube, serverPodsToKill, isOperatorKilledWithServerPod)
	}
}

/***********************************
   Log redaction verification
***********************************/

func TestLogRedactionVerify(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	bucketName := "default"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size1, "test_config_1", []string{"data"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(constants.Size2, "test_config_2", []string{"query", "index", "analytics"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 2, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
	}

	// Create Couchbase cluster
	cbCluster := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 2*time.Minute)

	// Collect logs
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	args.add("--collectinfo", "")
	args.add("--collectinfo-collect", "all")
	args.add("--collectinfo-redact", "")
	args.add("--all", "")
	execOut, err := runCbopinfoCmd(args.slice())
	execOutStr := strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	// Variable to denote failure of test
	testHasErrors := false

	// Verify required log files are generated
	errMsgList := failureList{}
	if err := checkCollectInfoLogs(targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, true, &errMsgList); err != nil {
		t.Error(err)
	}
	testHasErrors = errMsgList.PrintFailures(t) || testHasErrors

	// Verify log redaction part in collected files
	errMsgList = failureList{}
	if err := verifyLogRedaction(targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, &errMsgList); err != nil {
		t.Error(err)
	}
	testHasErrors = errMsgList.PrintFailures(t) || testHasErrors

	if testHasErrors {
		t.Fail()
	}
}

func TestLogRedactionWithPvVerify(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	clusterSize := constants.Size3
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "analytics"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["indexVolMnt"] = pvcName
	serviceConfig1["analyticsVolMnt"] = pvcName + "," + pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap("default", "couchbase", "high", constants.Mem256Mb, 2, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	// Create Couchbase cluster
	cbCluster := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 2*time.Minute)

	// Collect logs
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	args.add("--collectinfo", "")
	args.add("--collectinfo-collect", "all")
	args.add("--collectinfo-redact", "")
	args.add("--all", "")
	execOut, err := runCbopinfoCmd(args.slice())
	execOutStr := strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOutStr)
	defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	// Variable to denote failure of test
	testHasErrors := false

	// Verify required log files are generated
	errMsgList := failureList{}
	if err := checkCollectInfoLogs(targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, true, &errMsgList); err != nil {
		t.Error(err)
	}
	testHasErrors = errMsgList.PrintFailures(t) || testHasErrors

	// Verify log redaction part in collected files
	errMsgList = failureList{}
	if err := verifyLogRedaction(targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, &errMsgList); err != nil {
		t.Error(err)
	}
	testHasErrors = errMsgList.PrintFailures(t) || testHasErrors

	if testHasErrors {
		t.Fail()
	}
}

// TestLogRetentionMultiCluster ensures that one cluster's retention settings do not affect anothers
// running in the same namespace.
func TestLogRetentionMultiCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2

	// Create two clusters.
	cluster1 := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)
	cluster2 := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)

	// Ensure cluster 1 is healthy and update the retention period to be 1m.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster1, 2*time.Minute)
	cluster1 = e2eutil.MustPatchCluster(t, kubernetes, cluster1, jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionTime", "1m"), time.Minute)

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
	MustVerifyPvcMappingForPods(t, kubernetes, f.Namespace, pvcMapping, f.PlatformType)
}

func TestLogCollectListJson(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	bucketName := "default"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size1, "test_config_1", []string{"data"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(constants.Size2, "test_config_2", []string{"query", "index", "analytics"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 2, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
	}

	// Create Couchbase cluster
	cbCluster := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 2*time.Minute)

	// Collect logs
	args := argumentList{}
	args.addClusterDefaults(targetKube)
	args.addEnvironmentDefaults()
	args.add("--collectinfo", "")
	args.add("--collectinfo-list", "")
	execOut, err := runCbopinfoCmd(args.slice())
	execOutStr := strings.TrimSpace(string(execOut))
	t.Logf("Returned: %s\n", execOutStr)
	if err != nil {
		t.Fatal(err)
	}

	errMsgList := failureList{}
	testHasErrors := false
	if err := verifyLogCollectListJson(targetKube.KubeClient, f.Namespace, cbCluster.Name, execOutStr, &errMsgList); err != nil {
		t.Error(err)
	}

	testHasErrors = errMsgList.PrintFailures(t) || testHasErrors

	if testHasErrors {
		t.Fail()
	}
}
