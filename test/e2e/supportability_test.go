package e2e

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

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

// Function to check collecinfo / log redaction file collection related prints from cmd output
func checkCollectInfoLogs(execOut []byte, kubeClient kubernetes.Interface, namespace, cbClusterName, cbopinfoLogDir string, errMsgList *failureList) error {
	pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbClusterName})
	if err != nil {
		return errors.New("Failed to list pods: " + err.Error())
	}
	execOutStr := string(execOut)
	commonLogStr := "kubectl cp " + namespace + "/"
	logFileTimeStampStr := strings.Split(cbopinfoLogDir, ".")[0]
	logFileTimeStampStr = strings.Join(strings.Split(logFileTimeStampStr, "-")[1:], "-")
	for _, pod := range pods.Items {
		cbopinfoStr := commonLogStr + pod.Name + ":/tmp/cbinfo-" + namespace + "-" + pod.Name + "-" + logFileTimeStampStr
		expectedCbInfoLogStr := cbopinfoStr + ".zip ."
		expectedCbInfoRedactionStr := cbopinfoStr + "-redacted.zip ."
		if !strings.Contains(execOutStr, expectedCbInfoLogStr) {
			errMsgList.AppendFailure("For pod "+pod.Name, errors.New("CbCollectInfo log file print missing"))
		}
		if !strings.Contains(execOutStr, expectedCbInfoRedactionStr) {
			errMsgList.AppendFailure("For pod "+pod.Name, errors.New("CbCollectInfo redaction file print missing"))
		}
	}
	return nil
}

// Function to populate deployment file list
func getDeployementFileList(kubeClient kubernetes.Interface, namespace, deploymentDir string, fileList *[]string) error {
	deployments, err := kubeClient.ExtensionsV1beta1().Deployments(namespace).List(metav1.ListOptions{})
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
	if err := getDeployementFileList(kubeClient, namespace, deploymentDir, reqFileList); err != nil {
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

	// persistentvolumeclaims dir contents
	persistentVolClaims, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list persistent volume claims: " + err.Error())
	}
	for _, pvc := range persistentVolClaims.Items {
		*reqFileList = append(*reqFileList, pvcDir+"/"+pvc.Name+"/"+pvc.Name+".yaml")
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
	cmdName := "../../build/bin/cbopinfo"
	return exec.Command(cmdName, cmdArgs...).CombinedOutput()
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

func createClusterRoles(kubeClient kubernetes.Interface, roleName string) error {
	clusterRoleList, err := kubeClient.RbacV1beta1().ClusterRoles().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, clusterRole := range clusterRoleList.Items {
		if clusterRole.GetName() == roleName {
			kubeClient.RbacV1beta1().ClusterRoles().Delete(roleName, &metav1.DeleteOptions{})
			err = framework.WaitForClusterRoleDeleted(kubeClient, roleName, 30)
			if err != nil {
				return err
			}
			break
		}
	}

	policyRule2 := rbacv1.PolicyRule{
		APIGroups: []string{"storage.k8s.io"},
		Resources: []string{"storageclasses"},
		Verbs:     []string{"get"},
	}

	policyRule3 := rbacv1.PolicyRule{
		APIGroups: []string{"apiextensions.k8s.io"},
		Resources: []string{"customresourcedefinitions"},
		Verbs:     []string{"*"},
	}

	policyRule4 := rbacv1.PolicyRule{
		APIGroups: []string{""},
		//Resources: []string{"pods", "services", "endpoints", "persistentvolumeclaims", "persistentvolumes", "events", "secrets"},
		Resources: []string{"services", "endpoints", "persistentvolumeclaims", "persistentvolumes"},
		Verbs:     []string{"*"},
	}

	clusterRoleSpec := &rbacv1.ClusterRole{
		TypeMeta:   metav1.TypeMeta{Kind: "ClusterRole", APIVersion: "rbac.authorization.k8s.io/v1beta1"},
		ObjectMeta: metav1.ObjectMeta{Name: roleName},
		Rules:      []rbacv1.PolicyRule{policyRule2, policyRule3, policyRule4},
	}
	_, err = kubeClient.RbacV1beta1().ClusterRoles().Create(clusterRoleSpec)
	return err
}

// Run cbopinfo command with all valid arguments
// and validate the exit status of the commands
func TestLogCollectValidateArguments(t *testing.T) {
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	kubeConfPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeName)
	errMsgList := failureList{}
	operatorRestPort := strconv.Itoa(int(constants.OperatorRestPort))

	// Validate args which won't produce output file
	for _, arg := range []string{"-help", "-version"} {
		if _, err := runCbopinfoCmd([]string{arg}); err != nil {
			errMsgList.AppendFailure("Failed while providing arg "+arg, err)
		}
	}

	// Validate all other arguments
	validArgumentList := []cbopinfoArg{
		{
			Name:     "Validating '-all' argument",
			Arg:      "-all",
			ArgValue: "",
		},
		{
			Name:     "Validating '-collectinfo' argument",
			Arg:      "-collectinfo",
			ArgValue: "",
		},
		{
			Name:        "Validating '-kubeconfig' argument",
			Arg:         "-kubeconfig",
			ArgValue:    kubeConfPath,
			ExpectedErr: "flag needs an argument: -kubeconfig",
		},
		{
			Name:        "Validating '-namespace' argument",
			Arg:         "-namespace",
			ArgValue:    f.Namespace,
			ExpectedErr: "flag needs an argument: -namespace",
		},
		{
			Name:     "Validating '-system' argument",
			Arg:      "-system",
			ArgValue: "",
		},
		{
			Name:        "Validating '-operator-image' argument",
			Arg:         "-operator-image",
			ArgValue:    f.OpImage,
			ExpectedErr: "flag needs an argument: -operator-image",
		},
		{
			Name:        "Validating '-operator-rest-port' argument",
			Arg:         "-operator-rest-port",
			ArgValue:    operatorRestPort,
			ExpectedErr: "flag needs an argument: -operator-rest-port",
		},
	}

	for _, arg := range validArgumentList {
		t.Log(arg.Name)
		cmdArgs := []string{}

		// If arg is 'namespace' or 'kubeconfig', verify with namespace arg only
		if arg.Arg == "-namespace" {
			cmdArgs = []string{"-kubeconfig", kubeConfPath, arg.Arg}
		} else {
			cmdArgs = []string{"-namespace", f.Namespace, "-kubeconfig", kubeConfPath, arg.Arg}
		}
		if arg.ArgValue != "" {
			cmdArgs = append(cmdArgs, arg.ArgValue)
		}

		execOut, err := runCbopinfoCmd(cmdArgs)
		execOutStr := strings.TrimSpace(string(execOut))
		errMsgForNoCbCluster := "no CouchbaseCluster resources discovered in name space " + f.Namespace
		t.Logf("Returned: %s\n", execOutStr)
		if err != nil {
			errMsgList.AppendFailure("cbopinfo "+arg.Arg, errors.New("Command failed without cb cluster"))
		} else {
			if !strings.Contains(execOutStr, errMsgForNoCbCluster) {
				errMsgList.AppendFailure("cbopinfo "+arg.Arg, errors.New("Invalid error message"))
			}
		}
		if logFileName := getLogFileNameFromExecOutput(execOutStr); logFileName == "" {
			errMsgList.AppendFailure("cbopinfo "+arg.Arg, errors.New("No logs generated without cb cluster"))
			defer os.Remove(logFileName)
		}
	}

	// Deploy cb server for cbopinfo validation
	if _, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size1, constants.WithoutBucket, constants.AdminHidden); err != nil {
		t.Fatal(err)
	}

	for _, arg := range validArgumentList {
		t.Log(arg.Name)
		cmdArgs := []string{}

		// If arg is '-namespace', verify with namespace arg only
		if arg.Arg == "-namespace" {
			cmdArgs = []string{"-kubeconfig", kubeConfPath, arg.Arg}
		} else {
			cmdArgs = []string{"-namespace", f.Namespace, "-kubeconfig", kubeConfPath, arg.Arg}
		}
		if arg.ArgValue != "" {
			cmdArgs = append(cmdArgs, arg.ArgValue)
		}

		execOut, err := runCbopinfoCmd(cmdArgs)
		execOutStr := strings.TrimSpace(string(execOut))
		t.Logf("Returned: %s\n", execOutStr)
		if err != nil {
			errMsgList.AppendFailure("Failed while providing arg "+arg.Arg, err)
		}
		logFileName := getLogFileNameFromExecOutput(execOutStr)
		defer os.Remove(logFileName)

		logFileDir := strings.Split(logFileName, ".")[0]
		defer os.RemoveAll(logFileDir)
		if err := untarGzFile(logFileName); err != nil {
			errMsgList.AppendFailure("Failed to untar file ", err)
		}

		// Check command fails with missing argument value
		if arg.ArgValue != "" {
			cmdArgs := []string{arg.Arg}
			execOut, err := runCbopinfoCmd(cmdArgs)
			execOutStr := strings.TrimSpace(string(execOut))
			t.Logf("Returned: %s\n", execOutStr)
			if err == nil {
				errMsgList.AppendFailure("Command executed successfully without providing value for "+arg.Arg, nil)
			}

			// Verify valid error message
			if !strings.Contains(execOutStr, arg.ExpectedErr) {
				errMsgList.AppendFailure("Invalid error for missing arg value "+arg.Arg+", \nExpected: "+arg.ExpectedErr+"\nReceived: "+execOutStr, nil)
			}

			// Check no output file is generated
			if logFileName := getLogFileNameFromExecOutput(execOutStr); logFileName != "" {
				errMsgList.AppendFailure("File created with missing argument for "+arg.Arg, nil)
				os.Remove(logFileName)
			}
		}
	}
	errMsgList.CheckFailures(t)
}

// Negative test scenarios with command argument
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

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
		cluster1, err = e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, cluster1Size, constants.WithoutBucket, constants.AdminHidden)
		cluster1Err <- err
	}()

	go func() {
		var err error
		cluster2, err = e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, cluster2Size, constants.WithoutBucket, constants.AdminHidden)
		cluster2Err <- err
	}()

	go func() {
		var err error
		cluster3, err = e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, cluster3Size, constants.WithoutBucket, constants.AdminHidden)
		cluster3Err <- err
	}()

	for _, errChan := range []chan error{cluster1Err, cluster2Err, cluster3Err} {
		if err := <-errChan; err != nil {
			t.Fatal(err)
		}
	}

	kubeConfPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeName)
	isAllFlagSet := false

	/////////////////////////////////////////////////////
	// Log collection using '-namespace' & cluster arg //
	/////////////////////////////////////////////////////
	t.Log("Collecting logs from single cluster")
	reqFileList := []string{}
	errMsgList := failureList{}
	commonArgs := []string{"-operator-image", f.OpImage, "-kubeconfig", kubeConfPath, "-namespace", f.Namespace}
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
	cmdArgs = append(commonArgs, "-collectinfo", cluster1.Name)
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

	if err := checkCollectInfoLogs(execOut, targetKube.KubeClient, f.Namespace, cluster1.Name, logFileDir, &errMsgList); err != nil {
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
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	svcAccName := "rbac-test"
	kubeConfPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeName)

	cluster1, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size1, constants.WithoutBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	// Code to backup current config and replace after test execution
	configData, err := ioutil.ReadFile(kubeConfPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(kubeConfPath+"_bk", configData, 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(kubeConfPath+"_bk", kubeConfPath)

	// Code to create ClusterRole, ServiceAccount, ClusterRoleBinding for reduced RBAC permissions
	if err := createClusterRoles(targetKube.KubeClient, svcAccName); err != nil {
		t.Fatal(err)
	}
	defer framework.RemoveClusterRole(targetKube.KubeClient, svcAccName)

	if err := framework.RecreateServiceAccount(targetKube.KubeClient, f.Namespace, svcAccName); err != nil {
		t.Fatal(err)
	}
	defer framework.RemoveServiceAccount(targetKube.KubeClient, f.Namespace, svcAccName)

	if err := framework.RecreateClusterRoleBindings(targetKube.KubeClient, f.Namespace, svcAccName); err != nil {
		t.Fatal(err)
	}
	defer framework.RemoveClusterRoleBinding(targetKube.KubeClient, f.Namespace, svcAccName+"-"+f.Namespace)

	// Update current config with updated rbac user permission context
	cmdName := "resources/createKubeContextFromServiceAcc.sh"
	cmdArgs := []string{f.Namespace, svcAccName, kubeConfPath}
	execOut, err := exec.Command(cmdName, cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(execOut))

	// Collect logs
	cmdArgs = []string{"-operator-image", f.OpImage, "-kubeconfig", kubeConfPath, "-namespace", f.Namespace, cluster1.Name}
	execOut, err = runCbopinfoCmd(cmdArgs)
	execOutStr := strings.TrimSpace(string(execOut))
	t.Log(execOutStr)
	expectedErrMsg := "unable to poll CouchbaseCluster resources: couchbaseclusters.couchbase.com is forbidden: User \"system:serviceaccount:" + f.Namespace + ":rbac-test\" cannot list couchbaseclusters.couchbase.com in the namespace \"" + f.Namespace + "\""
	if err == nil {
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
		if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster1.Name, &reqFileList); err != nil {
			t.Fatal(err)
		}
		checkLogDirContents(reqFileList, logFileDir, &errMsgList)
		errMsgList.PrintFailures(t)

		t.Fatal("Able to read resource without valid rbac permissions")
	}
	if execOutStr != expectedErrMsg {
		t.Fatal("Invalid error message")
	}
}

func ReDeployOperator(t *testing.T, kubeClient kubernetes.Interface, imageName string, port int32) error {
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
	if _, err := kubeClient.ExtensionsV1beta1().Deployments(f.Namespace).Create(deployment); err != nil {
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
func CollectExtendedDebugLogGeneric(t *testing.T, kubeName, opImageName string, testPort, defPort int32, cmdArgs []string) {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]
	clusterSize := 3

	defer ReDeployOperator(t, targetKube.KubeClient, f.OpImage, defPort)
	if err := ReDeployOperator(t, targetKube.KubeClient, opImageName, testPort); err != nil {
		t.Fatal(err)
	}

	// Create Couchbase cluster
	cbCluster, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, clusterSize, constants.WithoutBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir, kubeName, t.Name())

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

	if err := checkCollectInfoLogs(execOut, targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, &errMsgList); err != nil {
		t.Fatal(err)
	}

	if failureExists {
		t.Fatal("Test failed")
	}
}

// Collect cbopinfo using '--operator-image' and '--operator-rest-port'
// with default values and validate the logs collected
func TestExtendedDebugWithDefaultValues(t *testing.T) {
	f := framework.Global
	kubeName := "BasicCluster"
	kubeConfPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeName)
	defPort := constants.OperatorRestPort
	cmdArgs := []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, "-collectinfo", "-all"}
	CollectExtendedDebugLogGeneric(t, kubeName, constants.DefOperatorImgTag, defPort, defPort, cmdArgs)
}

// Collect cbopinfo using '--operator-image' and '--operator-rest-port'
// with custom values and validate the logs collected
func TestExtendedDebugWithNonDefaultValues(t *testing.T) {
	f := framework.Global
	kubeName := "BasicCluster"
	kubeConfPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeName)
	var defPort int32
	var testPort int32
	testPort = 32123
	containerPorts := f.Deployment.Spec.Template.Spec.Containers[0].Ports
	for _, temPort := range containerPorts {
		if temPort.Name == "readiness-port" {
			defPort = temPort.ContainerPort
		}
	}
	cmdArgs := []string{"-operator-image", f.OpImage, "-operator-rest-port", strconv.Itoa(int(testPort)), "-kubeconfig", kubeConfPath, "-namespace", f.Namespace, "-collectinfo", "-all"}
	CollectExtendedDebugLogGeneric(t, kubeName, f.OpImage, testPort, defPort, cmdArgs)
}

// Collect cbopinfo with '--operator-image' & '-operator-rest-port'
// with invalid values and validate the log collection
func TestExtendedDebugWithInvalidValues(t *testing.T) {
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	invalidImgName := "couchbase/couchbase-operator:invalidversion"
	invalidPortVal := "32080"
	clusterSize := constants.Size1
	kubeConfPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeName)

	// Create Couchbase cluster
	cbCluster, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, clusterSize, constants.WithoutBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	// Collect logs with invalid operator-image-name
	t.Log("Collecting logs using invalid -operator-image arg value")
	cmdArgs := []string{"-operator-image", invalidImgName, "-operator-rest-port", strconv.Itoa(int(constants.OperatorRestPort)), "-kubeconfig", kubeConfPath, "-namespace", f.Namespace}

	execOut, err := runCbopinfoCmd(append(cmdArgs, cbCluster.Name))
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

		if err := getDeployementFileList(targetKube.KubeClient, f.Namespace, deploymentDir, &excludedFileList); err != nil {
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
	cmdArgs = []string{"-operator-image", f.OpImage, "-operator-rest-port", invalidPortVal, "-kubeconfig", kubeConfPath, "-namespace", f.Namespace}

	execOut, err = runCbopinfoCmd(append(cmdArgs, cbCluster.Name))
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
		if err := getDeployementFileList(targetKube.KubeClient, f.Namespace, deploymentDir, &reqFileList); err != nil {
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	clusterSize := constants.Size1
	execOut := []byte{}
	kubeConfPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeName)

	// Create Couchbase cluster
	cbCluster, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, clusterSize, constants.WithoutBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Collecting logs using invalid operator-image value")
	cmdArgs := []string{"-operator-image", f.OpImage, "-operator-rest-port", strconv.Itoa(int(constants.OperatorRestPort)), "-kubeconfig", kubeConfPath, "-namespace", f.Namespace, "-collectinfo", "-all"}

	logFileNameChan := make(chan string)
	go func() {
		// Collect logs when operator pod goes down in parallel
		t.Log("Starting log collection")
		execOut, err = runCbopinfoCmd(append(cmdArgs, cbCluster.Name))
		execOutStr := strings.TrimSpace(string(execOut))
		t.Logf("Returned: %s\n", execOutStr)
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

	if err := checkCollectInfoLogs(execOut, targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, &errMsgList); err != nil {
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
func EphemeralLogCollectUsingLogPVGeneric(t *testing.T, kubeName, podDownMethod string, isOperatorKilledWithServerPod bool) {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]

	clusterSize := 5
	newMemberIndex := clusterSize
	podMembersToKill := []int{2, 3, 4}
	bucketName := "PVBucket"
	pvcName := "couchbase-log-pv"
	operatorKilledErrChan := make(chan error)
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size2, "test_config_1", []string{"data"})
	serviceConfig1["defaultVolMnt"] = pvcName

	serviceConfig2 := e2eutil.GetServiceConfigMap(constants.Size3, "test_config_2", []string{"search", "query", "eventing"})
	serviceConfig2["logVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
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

	killOperatorFunc := func() {
		operatorKilledErrChan <- e2eutil.KillOperatorAndWaitForRecovery(t, targetKube.KubeClient, f.Namespace)
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(constants.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster, err := e2eutil.CreateClusterFromSpec(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, constants.AdminHidden, clusterSpec)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, cbCluster.Name, f.Namespace, clusterSize, constants.Retries30); err != nil {
		t.Fatal(err)
	}

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
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}

	// Kill PV log enabled pods and verify the logs are persisted after pod deletion
	for _, memberToKill := range podMembersToKill {
		podNameToKill := couchbaseutil.CreateMemberName(cbCluster.Name, memberToKill)

		// Kills operator pod in async way
		if isOperatorKilledWithServerPod {
			go killOperatorFunc()
		}

		// Bring down couchbase server pod
		switch podDownMethod {
		case "deletePod":
			if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podNameToKill, metav1.NewDeleteOptions(0)); err != nil {
				t.Fatal(err)
			}
		case "killServerProcess":
			if _, err := f.ExecShellInPod(kubeName, podNameToKill, "pkill beam.smp"); err != nil {
				t.Fatal(err)
			}
		}

		// If operator was killed, will waits for operator recovery to happen
		if isOperatorKilledWithServerPod {
			if err := <-operatorKilledErrChan; err != nil {
				t.Fatal(err)
			}
		}

		event := e2eutil.NewMemberFailedOverEvent(cbCluster, memberToKill)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 60); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberDown", memberToKill)
		expectedEvents.AddClusterPodEvent(cbCluster, "FailedOver", memberToKill)

		event = e2eutil.NewMemberAddEvent(cbCluster, newMemberIndex)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 180); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", newMemberIndex)

		// To validate the new PVC created for new pod
		newMemberName := couchbaseutil.CreateMemberName(cbCluster.Name, newMemberIndex)
		expectedPvcMap[newMemberName] = 1

		event = e2eutil.NewMemberRemoveEvent(cbCluster, memberToKill)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 300); err != nil {
			t.Fatal(err)
		}

		event = e2eutil.RebalanceCompletedEvent(cbCluster)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 60); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberToKill)
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")
		newMemberIndex++
	}

	// Verifying the persistence of log PVs are preserved by operator
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, cbCluster.Name, expectedEvents)
}

// Define log mount for ephemeral pods and validate the logs are preserved
// even after abnormal pod removal
func TestCollectLogFromEphemeralPodsUsingLogPV(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	isOperatorKilledWithServerPod := false

	// Pods brought down using DeletePod method
	EphemeralLogCollectUsingLogPVGeneric(t, targetKubeName, "deletePod", isOperatorKilledWithServerPod)

	// Pods brought down by killing cb-server process
	if f.KubeType == "kubernetes" {
		EphemeralLogCollectUsingLogPVGeneric(t, targetKubeName, "killServerProcess", isOperatorKilledWithServerPod)
	}
}

// Define log mount for ephemeral pods and validate the logs are preserved
// even after abnormal pod removal
func TestCollectLogFromEphemeralPodsWithOperatorKilled(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	isOperatorKilledWithServerPod := true

	// Pods brought down using DeletePod method
	EphemeralLogCollectUsingLogPVGeneric(t, targetKubeName, "deletePod", isOperatorKilledWithServerPod)

	// Pods brought down by killing cb-server process
	if f.KubeType == "kubernetes" {
		EphemeralLogCollectUsingLogPVGeneric(t, targetKubeName, "killServerProcess", isOperatorKilledWithServerPod)
	}
}

// Deploys Couchbase server with log PV defined for server pods
// Scale down the couchbase cluster and check log PVs cleanup has happened
func TestEphemeralLogCollectResizeCluster(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

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

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
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

	pvcTemplate1 := createPersistentVolumeClaimSpec(constants.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster, err := e2eutil.CreateClusterFromSpec(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, constants.AdminHidden, clusterSpec)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, cbCluster.Name, f.Namespace, clusterSize, constants.Retries30); err != nil {
		t.Fatal(err)
	}

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
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}

	// Start resizing service config to 2 node service
	serviceSize := constants.Size2
	if err := e2eutil.ResizeClusterNoWait(t, serviceIndexToResize, serviceSize, targetKube.CRClient, cbCluster); err != nil {
		t.Fatal(err)
	}
	event := e2eutil.RebalanceCompletedEvent(cbCluster)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 300); err != nil {
		t.Fatal(err)
	}

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
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}

	// Start resizing service config to 4 node service
	serviceSize = constants.Size4
	if err := e2eutil.ResizeClusterNoWait(t, serviceIndexToResize, serviceSize, targetKube.CRClient, cbCluster); err != nil {
		t.Fatal(err)
	}
	event = e2eutil.RebalanceCompletedEvent(cbCluster)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 300); err != nil {
		t.Fatal(err)
	}

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
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}

	// Start resizing service config to 4 node service
	serviceSize = constants.Size1
	if err := e2eutil.ResizeClusterNoWait(t, serviceIndexToResize, serviceSize, targetKube.CRClient, cbCluster); err != nil {
		t.Fatal(err)
	}
	event = e2eutil.RebalanceCompletedEvent(cbCluster)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 300); err != nil {
		t.Fatal(err)
	}

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
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, cbCluster.Name, expectedEvents)
}

// Collect logs from ephemeral log PVs
// using default log retention time and size values
func TestLogCollectWithDefaultRetentionAndSize(t *testing.T) {
	f := framework.Global
	kubeName := "NewCluster1"
	targetKube := f.ClusterSpec[kubeName]

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

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(constants.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster, err := e2eutil.CreateClusterFromSpec(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, constants.AdminHidden, clusterSpec)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, cbCluster.Name, f.Namespace, clusterSize, constants.Retries30); err != nil {
		t.Fatal(err)
	}

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
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}

	for memberIdToKill := 2; memberIdToKill <= 7; memberIdToKill++ {
		memberNameToKill := couchbaseutil.CreateMemberName(cbCluster.Name, memberIdToKill)
		if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, memberNameToKill, metav1.NewDeleteOptions(0)); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberDown", memberIdToKill)

		// Wait for failover event
		event := e2eutil.NewMemberFailedOverEvent(cbCluster, memberIdToKill)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 60); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "FailedOver", memberIdToKill)

		// Wait for new pod add event
		event = e2eutil.NewMemberAddEvent(cbCluster, newPodMemberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 180); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", newPodMemberId)

		// Wait for rebalance complete event
		event = e2eutil.RebalanceCompletedEvent(cbCluster)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 300); err != nil {
			t.Fatal(err)
		}

		// Add expected events for cluster for verification
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIdToKill)
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

		// Updating expectedPvcMap for new cluster pod
		expectedPvcMap[couchbaseutil.CreateMemberName(cbCluster.Name, newPodMemberId)] = 1
		newPodMemberId++
	}

	// Verifying the persistence of log PVs are preserved by operator
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, cbCluster.Name, expectedEvents)
}

// Collect logs from ephemeral log PVs
// using custom log retention time and size values
func TestLogCollectWithCustomRetentionAndSize(t *testing.T) {
	f := framework.Global
	kubeName := "NewCluster1"
	targetKube := f.ClusterSpec[kubeName]

	clusterSize := 5
	newPodMemberId := clusterSize
	logRetentionCount := 2
	bucketName := "PVBucket"
	pvcName := "couchbase-log-pv"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size2, "test_config_1", []string{"data"})
	serviceConfig1["defaultVolMnt"] = pvcName

	serviceConfig2 := e2eutil.GetServiceConfigMap(constants.Size3, "test_config_2", []string{"search", "query", "eventing"})
	serviceConfig2["logVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	otherConfig1 := map[string]string{
		"logRetentionTime":  "15m",
		"logRetentionCount": strconv.Itoa(logRetentionCount),
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
		"other1":   otherConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(constants.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster, err := e2eutil.CreateClusterFromSpec(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, constants.AdminHidden, clusterSpec)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, cbCluster.Name, f.Namespace, clusterSize, constants.Retries30); err != nil {
		t.Fatal(err)
	}

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
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}

	memberIdsToKill := []int{2, 3, 4, 5, 6, 7}
	for index, memberIdToKill := range memberIdsToKill {
		memberNameToKill := couchbaseutil.CreateMemberName(cbCluster.Name, memberIdToKill)
		if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, memberNameToKill, metav1.NewDeleteOptions(0)); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberDown", memberIdToKill)

		// Wait for failover event
		event := e2eutil.NewMemberFailedOverEvent(cbCluster, memberIdToKill)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 60); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "FailedOver", memberIdToKill)

		// Wait for new pod add event
		event = e2eutil.NewMemberAddEvent(cbCluster, newPodMemberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 180); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", newPodMemberId)

		// Wait for rebalance complete event
		event = e2eutil.RebalanceCompletedEvent(cbCluster)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 300); err != nil {
			t.Fatal(err)
		}

		// Add expected events for cluster for verification
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIdToKill)
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

		// Updating expectedPvcMap for new cluster pod
		temMemberName := couchbaseutil.CreateMemberName(cbCluster.Name, newPodMemberId)
		expectedPvcMap[temMemberName] = 1

		// Mark all other then logRetention count PV pods to ZERO for verification
		if index >= logRetentionCount {
			for temMemId := 2; temMemId < len(memberIdsToKill)-logRetentionCount; temMemId++ {
				temMemberName := couchbaseutil.CreateMemberName(cbCluster.Name, temMemId)
				expectedPvcMap[temMemberName] = 0
			}
		}

		// Verifying the persistence of log PVs are preserved by operator
		if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
			t.Error(err)
		}

		newPodMemberId++
	}

	// Sleep for log retention time feature to delete all old logs
	time.Sleep(time.Minute * 10)

	// Updating expecter PVC for final verification
	for memberIndex := newPodMemberId - 4; memberIndex > 1; memberIndex-- {
		temMemberName := couchbaseutil.CreateMemberName(cbCluster.Name, memberIndex)
		expectedPvcMap[temMemberName] = 0
	}

	// Verifying the persistence of log PVs are preserved by operator
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap); err != nil {
		t.Error(err)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, cbCluster.Name, expectedEvents)
}

/**************************************
  Persistent pods log collection cases
***************************************/

// Create couchbase cluster with persistent volume claim
// Collect log and check for persistent volume definition files
func TestLogCollectClusterWithPVC(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "NewCluster1"
	targetKube := f.ClusterSpec[kubeName]
	kubeConfPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeName)

	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(constants.Size2, "test_config_1", []string{"data", "query", "index", "analytics"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["indexVolMnt"] = pvcName
	serviceConfig1["analyticsVolMnt"] = pvcName + "," + pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap("default", "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(constants.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	cbCluster, err := e2eutil.CreateClusterFromSpec(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, constants.AdminHidden, clusterSpec)
	if err != nil {
		t.Fatal(err)
	}

	// Collect logs
	cmdArgs := []string{"-operator-image", f.OpImage, "-kubeconfig", kubeConfPath, "-namespace", f.Namespace, "-collectinfo", "-all", cbCluster.Name}
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
	if err := checkCollectInfoLogs(execOut, targetKube.KubeClient, f.Namespace, cbCluster.Name, logFileDir, &errMsgList); err != nil {
		t.Error(err)
	}
	errMsgList.CheckFailures(t)
}
