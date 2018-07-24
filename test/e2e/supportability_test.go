package e2e

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
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
func checkLogDirContents(reqFileList []string, logDirName string, errMsgList *failureList) error {
	for _, reqFile := range reqFileList {
		if _, err := os.Stat(reqFile); err != nil {
			errMsgList.AppendFailure("File "+reqFile, errors.New("not found"))
		}
	}
	return nil
}

// Function to check collecinfo related prints from cmd output
func checkCollectInfoLogs(execOut []byte, kubeClient kubernetes.Interface, namespace, cbClusterName, cbopinfoLogDir string, errMsgList *failureList) error {
	pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase,couchbase_cluster=" + cbClusterName})
	if err != nil {
		return errors.New("Failed to list pods: " + err.Error())
	}
	execOutStr := string(execOut)
	commonLogStr := "Server logs accessible via: kubectl cp " + namespace + "/"
	logFileTimeStampStr := strings.Split(cbopinfoLogDir, ".")[0]
	logFileTimeStampStr = strings.Split(logFileTimeStampStr, "-")[1]
	for _, pod := range pods.Items {
		expectedStr := commonLogStr + pod.Name + ":/tmp/cbinfo-" + namespace + "-" + pod.Name + "-" + logFileTimeStampStr + ".zip ."
		if !strings.Contains(execOutStr, expectedStr) {
			errMsgList.AppendFailure("Collectinfo for pod "+pod.Name, errors.New("collectinfo log print missing"))
		}
	}
	return nil
}

// Function to get kube-system specific log file names
func getNonCouchbaseLogFileList(kubeClient kubernetes.Interface, crClient versioned.Interface, config *rest.Config, namespace, cbopinfoLogDir string, reqFileList *[]string) error {
	namespaceDir := cbopinfoLogDir + "/" + namespace

	clusterroleDir := namespaceDir + "/clusterrole"
	clusterroleBindingDir := namespaceDir + "/clusterrolebinding"
	crdDir := namespaceDir + "/customresourcedefinition"
	deploymentDir := namespaceDir + "/deployment"
	endpointsDir := namespaceDir + "/endpoints"
	podDir := namespaceDir + "/pod"
	secretDir := namespaceDir + "/secret"
	serviceDir := namespaceDir + "/service"
	pvDir := namespaceDir + "/persistentvolumes"
	pvcDir := namespaceDir + "/persistentvolumeclaims"

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
		*reqFileList = append(*reqFileList, crdDir+"/"+crd.Name+"/"+crd.Name+".yaml")
	}

	// deployment dir contents
	deployments, err := kubeClient.ExtensionsV1beta1().Deployments(namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list deployments: " + err.Error())
	}
	for _, deployment := range deployments.Items {
		*reqFileList = append(*reqFileList, deploymentDir+"/"+deployment.Name+"/"+deployment.Name+".yaml")
		if namespace != "kube-system" {
			*reqFileList = append(*reqFileList, deploymentDir+"/"+deployment.Name+"/events.yaml")
			*reqFileList = append(*reqFileList, deploymentDir+"/"+deployment.Name+"/"+deployment.Name+".log")
		}
	}

	// endpoints dir content
	endpoints, err := kubeClient.CoreV1().Endpoints(namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list endpoints: " + err.Error())
	}
	for _, endpoint := range endpoints.Items {
		if endpoint.Name == "couchbase-operator" {
			continue
		}
		*reqFileList = append(*reqFileList, endpointsDir+"/"+endpoint.Name+"/"+endpoint.Name+".yaml")
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
		if namespace != "kube-system" {
			*reqFileList = append(*reqFileList, podDir+"/"+pod.Name+"/events.yaml")
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

	// service dir contents
	services, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: "app!=couchbase"})
	if err != nil {
		return errors.New("Failed to list services: " + err.Error())
	}
	for _, service := range services.Items {
		*reqFileList = append(*reqFileList, serviceDir+"/"+service.Name+"/"+service.Name+".yaml")
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

// Function to get couchbase cluster specific log file names
func getCouchbaseFileList(kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, cbopinfoLogDir string, cbClusterName string, reqFileList *[]string) error {
	namespaceDir := cbopinfoLogDir + "/" + namespace

	cbClusterDir := namespaceDir + "/couchbasecluster"
	podDir := namespaceDir + "/pod"
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
		*reqFileList = append(*reqFileList, cbClusterDir+"/"+cbCluster.Name+"/events.yaml")
		*reqFileList = append(*reqFileList, cbClusterDir+"/"+cbCluster.Name+"/"+cbCluster.Name+".yaml")

		// pod dir contents
		pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase,couchbase_cluster=" + cbCluster.Name})
		if err != nil {
			return errors.New("Failed to list pods: " + err.Error())
		}
		for _, pod := range pods.Items {
			*reqFileList = append(*reqFileList, podDir+"/"+pod.Name+"/couchbase-server.log")
			*reqFileList = append(*reqFileList, podDir+"/"+pod.Name+"/events.yaml")
			*reqFileList = append(*reqFileList, podDir+"/"+pod.Name+"/"+pod.Name+".yaml")
		}

		// service dir contents
		services, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase,couchbase_cluster=" + cbCluster.Name})
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

func getLogFileNameFromExecOutput(output []byte) string {
	outputStr := strings.TrimSpace(string(output))
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
	kubeConfPath := os.Getenv("HOME") + "/.kube/config_" + kubeName
	errMsgList := failureList{}

	// If cluster specific file doesn't exists, point to default file
	if _, err := os.Stat(kubeConfPath); os.IsNotExist(err) {
		kubeConfPath = os.Getenv("HOME") + "/.kube/config"
	}

	// Validate args which won't produce output file
	for _, arg := range []string{"-help", "-version"} {
		_, err := runCbopinfoCmd([]string{arg})
		if err != nil {
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
	}

	for _, arg := range validArgumentList {
		t.Log(arg.Name)
		cmdArgs := []string{arg.Arg}
		if arg.ArgValue != "" {
			cmdArgs = append(cmdArgs, arg.ArgValue)
		}

		execOut, err := runCbopinfoCmd(cmdArgs)
		if err != nil {
			errMsgList.AppendFailure("Failed while providing arg "+arg.Arg, err)
		}
		logFileName := getLogFileNameFromExecOutput(execOut)
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
			if err == nil {
				errMsgList.AppendFailure("Command executed successfully without providing value for "+arg.Arg, nil)
			}

			// Verify valid error message
			execOutStr := string(execOut)
			if !strings.Contains(execOutStr, arg.ExpectedErr) {
				errMsgList.AppendFailure("Invalid error for missing arg value "+arg.Arg+", \nExpected: "+arg.ExpectedErr+"\nReceived: "+execOutStr, nil)
			}

			// Check no output file is generated
			if logFileName := getLogFileNameFromExecOutput(execOut); logFileName != "" {
				errMsgList.AppendFailure("File created with missing argument for "+arg.Arg, nil)
				os.Remove(logFileName)
			}
		}
	}
	errMsgList.CheckFailures(t)
}

// Negative test scenarios with command argument
func TestNegLogCollectValidateArgs(t *testing.T) {
	invalidKubeConfPath := os.Getenv("HOME") + "/.kube/config_k8s_reclustered"
	unreachableKubeConfPath := os.Getenv("HOME") + "/.kube/config_k8s_unreachable"
	errMsgList := failureList{}

	validArgumentList := []cbopinfoArg{
		{
			Name:        "Validating invalid '-kubeconfig' file",
			Arg:         "-kubeconfig",
			ArgValue:    invalidKubeConfPath,
			WillFail:    true,
			ExpectedErr: "unable to initialize context",
		},
		{
			Name:        "Unreachable '-kubeconfig' file",
			Arg:         "-kubeconfig",
			ArgValue:    unreachableKubeConfPath,
			WillFail:    true,
			ExpectedErr: "unable to initialize context",
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
		if err == nil {
			errMsgList.AppendFailure(arg.Name, errors.New("Command executed successfully"))
		}
		execOutStr := strings.TrimSpace(string(execOut))

		// Verify valid error message
		if !strings.Contains(execOutStr, arg.ExpectedErr) {
			errMsgList.AppendFailure(arg.Name, errors.New("Invalid error message: "+execOutStr))
		}

		// Check no output file is generated
		if logFileName := getLogFileNameFromExecOutput(execOut); logFileName != "" {
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

	clusterSize := e2eutil.Size3
	cluster1, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, clusterSize, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	cluster2, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, clusterSize, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	cluster3, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	kubeConfPath := os.Getenv("HOME") + "/.kube/config_" + kubeName

	/////////////////////////////////////////////////////
	// Log collection using '-namespace' & cluster arg //
	/////////////////////////////////////////////////////
	t.Log("Collecting logs from single cluster")
	cmdArgs := []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, cluster1.Name}
	execOut, err := runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOut)
	defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	reqFileList := []string{}
	errMsgList := failureList{}
	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster1.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)

	// collect logs from multi clusters by specifying cluster names in command line
	t.Log("Collecting logs from cluster1 and cluster3")
	cmdArgs = []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, cluster1.Name, cluster3.Name}
	execOut, err = runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOut)
	defer os.Remove(logFileName)

	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster3.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)

	// collect logs from all clusters in the given namespace
	t.Log("Collecting logs from all available cluster")
	cmdArgs = []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace}

	execOut, err = runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOut)
	defer os.Remove(logFileName)

	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster2.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	failureExists := errMsgList.PrintFailures(t)

	///////////////////////////////////////////////
	/////// Log collection using '-system' ////////
	///////////////////////////////////////////////
	t.Log("Log Verification for kube-system")
	errMsgList = failureList{}

	// Verify kube-system logs with single cb cluster logs
	reqFileList = []string{}
	cmdArgs = []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, "-system", cluster2.Name}
	execOut, err = runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOut)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	for _, namespace := range []string{f.Namespace, "kube-system"} {
		if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, namespace, logFileDir, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster2.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)

	// Verify kube-system logs with multiple couchbase cluster logs
	reqFileList = []string{}
	cmdArgs = []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, "-system", cluster1.Name, cluster3.Name}
	execOut, err = runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOut)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	for _, namespace := range []string{f.Namespace, "kube-system"} {
		if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, namespace, logFileDir, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	for _, clusterName := range []string{cluster1.Name, cluster3.Name} {
		if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, clusterName, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)

	// Verify kube-system logs with all other cb cluster logs
	reqFileList = []string{}
	cmdArgs = []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, "-system"}
	execOut, err = runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOut)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	for _, namespace := range []string{f.Namespace, "kube-system"} {
		if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, namespace, logFileDir, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	for _, clusterName := range []string{cluster1.Name, cluster2.Name, cluster3.Name} {
		if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, clusterName, &reqFileList); err != nil {
			t.Fatal(err)
		}
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)

	///////////////////////////////////////////////////
	/////// Log collection using '-collectinfo' ///////
	///////////////////////////////////////////////////
	cmdArgs = []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, "-collectinfo", cluster1.Name}
	execOut, err = runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName = getLogFileNameFromExecOutput(execOut)
	defer os.Remove(logFileName)

	logFileDir = strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	reqFileList = []string{}
	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster1.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)

	if err := checkCollectInfoLogs(execOut, targetKube.KubeClient, f.Namespace, cluster1.Name, logFileDir, &errMsgList); err != nil {
		t.Fatal(err)
	}

	failureExists = failureExists || errMsgList.PrintFailures(t)
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
	kubeConfPath := os.Getenv("HOME") + "/.kube/config_" + kubeName

	cluster1, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithoutBucket, e2eutil.AdminHidden)
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
	cmdArgs = []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, cluster1.Name}
	execOut, err = runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOut)
	//defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	// Verify file list
	errMsgList := failureList{}
	reqFileList := []string{}
	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cluster1.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)

	failureExists := errMsgList.PrintFailures(t)
	if !failureExists {
		t.Fatal("Log file has all required files despite of rbac constraint")
	}
}

// Create couchbase cluster with persistent volume claim
// Collect log and check for persistent volume definition files
func TestLogCollectClusterWithPVC(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	kubeConfPath := os.Getenv("HOME") + "/.kube/config_" + kubeName

	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(e2eutil.Size2, "test_config_1", []string{"data", "query", "index", "analytics"})
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

	pvcTemplate1 := createPersistentVolumeClaimSpec("standard", pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}

	cbCluster, err := e2eutil.CreateClusterFromSpec(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, e2eutil.AdminHidden, clusterSpec)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	// Collect logs
	cmdArgs := []string{"-kubeconfig", kubeConfPath, "-namespace", f.Namespace, cbCluster.Name}
	execOut, err := runCbopinfoCmd(cmdArgs)
	if err != nil {
		t.Fatal(err)
	}
	logFileName := getLogFileNameFromExecOutput(execOut)
	defer os.Remove(logFileName)

	logFileDir := strings.Split(logFileName, ".")[0]
	defer os.RemoveAll(logFileDir)
	if err := untarGzFile(logFileName); err != nil {
		t.Fatal(err)
	}

	// Verify file list
	errMsgList := failureList{}
	reqFileList := []string{}
	if err := getNonCouchbaseLogFileList(targetKube.KubeClient, targetKube.CRClient, targetKube.Config, f.Namespace, logFileDir, &reqFileList); err != nil {
		t.Fatal(err)
	}
	if err := getCouchbaseFileList(targetKube.KubeClient, targetKube.CRClient, f.Namespace, logFileDir, cbCluster.Name, &reqFileList); err != nil {
		t.Fatal(err)
	}
	checkLogDirContents(reqFileList, logFileDir, &errMsgList)
	errMsgList.CheckFailures(t)
}
