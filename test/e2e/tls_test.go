package e2e

import (
	"os"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Decorator struct to store all the required parameters for
// setting up TLS secrets on cluster
type TlsDecorator struct {
	certValidFrom      time.Time
	certValidTo        time.Time
	keyType            e2eutil.KeyType
	certType           e2eutil.CertType
	ca                 *e2eutil.CertificateAuthority
	test               framework.TestFunc
	dnsNames           []string
	randomNameSuffix   string
	clusterName        string
	caCommonName       string
	operatorCommonName string
	clusterCommonName  string
	operatorSecretName string
	clusterSecretName  string
}

// Initializes the TlsDecorator struct will default working values
func (obj *TlsDecorator) Init(randSuffix, namespace string, keyType e2eutil.KeyType) {
	obj.randomNameSuffix = randSuffix
	obj.certValidFrom = time.Now().In(time.UTC)
	obj.certValidTo = (obj.certValidFrom).AddDate(10, 0, 0)
	obj.caCommonName = "Couchbase CA"
	obj.keyType = keyType
	obj.certType = e2eutil.CertTypeCA
	obj.operatorCommonName = "Couchbase Operator"
	obj.operatorSecretName = "operator-secret-tls-" + randSuffix
	obj.clusterCommonName = "Couchbase Cluster"
	obj.clusterSecretName = "cluster-secret-tls-" + randSuffix
	obj.clusterName = randomClusterName(randSuffix)
	obj.dnsNames = []string{
		"*." + obj.clusterName + "." + namespace + ".svc",
	}
}

func (obj *TlsDecorator) CreateCaRootCert(t *testing.T) {
	var err error
	// Create the root CA (self signed)
	obj.ca, err = e2eutil.NewCertificateAuthority(obj.keyType, obj.caCommonName, obj.certValidFrom, obj.certValidTo, obj.certType)
	if err != nil {
		t.Fatal("Unable to generate CA:", err)
	}
}

func (obj *TlsDecorator) CreateOperatorSecret(t *testing.T, f *framework.Framework, kubeName string) *corev1.Secret {
	reqKube := f.ClusterSpec[kubeName]
	// Generate CB operator certificate key data
	certReq := e2eutil.CreateOperatorCertReq(obj.operatorCommonName)
	req := e2eutil.CreateKeyPairReqData(obj.keyType, certReq)
	operatorKeyPEM, operatorCertPEM, err := req.Generate(obj.ca, obj.certValidFrom, obj.certValidTo)
	if err != nil {
		t.Fatal("unable to generate operator key pair", err)
	}

	// Create the operator secret
	operatorSecretData := e2eutil.CreateOperatorSecretData(f.Namespace, obj.operatorSecretName, obj.ca.Certificate, operatorCertPEM, operatorKeyPEM)
	operatorSecret, err := e2eutil.CreateSecret(reqKube.KubeClient, f.Namespace, operatorSecretData)
	if err != nil {
		t.Fatal("unable to create operator secret:", err)
	}
	return operatorSecret
}

func (obj *TlsDecorator) CreateClusterSecret(t *testing.T, f *framework.Framework, kubeName string) *corev1.Secret {
	reqKube := f.ClusterSpec[kubeName]
	// Generate CB cluster certificate key data
	certReq := e2eutil.CreateClusterCertReq(obj.clusterCommonName, obj.dnsNames)
	req := e2eutil.CreateKeyPairReqData(obj.keyType, certReq)
	clusterKeyPEM, clusterCertPEM, err := req.Generate(obj.ca, obj.certValidFrom, obj.certValidTo)
	if err != nil {
		t.Fatal("unable to generate cluster key pair", err)
	}

	// Create the cluster secret
	clusterSecretData := e2eutil.CreateClusterSecretData(f.Namespace, obj.clusterSecretName, clusterCertPEM, clusterKeyPEM)
	clusterSecret, err := e2eutil.CreateSecret(reqKube.KubeClient, f.Namespace, clusterSecretData)
	if err != nil {
		t.Fatal("unable to create cluster secret:", err)
	}
	return clusterSecret
}

func (obj *TlsDecorator) SetTlsForTesting(operatorSecret, clusterSecret *corev1.Secret) {
	tls := &api.TLSPolicy{
		Static: &api.StaticTLS{
			Member: &api.MemberSecret{
				ServerSecret: clusterSecret.Name,
			},
			OperatorSecret: operatorSecret.Name,
		},
	}
	e2espec.SetTLS(tls)
}

// Returns a wrapper function after embedding the passed function within it
func CreateWrapperFunc(kubeName string, decoratorObj *TlsDecorator, keyType e2eutil.KeyType) framework.TestFunc {
	wrapperFunc := func(t *testing.T) {
		f := framework.Global
		targetKube := f.ClusterSpec[kubeName]

		decoratorObj.CreateCaRootCert(t)
		operatorSecret := decoratorObj.CreateOperatorSecret(t, f, kubeName)
		defer e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, operatorSecret.Name, &metav1.DeleteOptions{})

		clusterSecret := decoratorObj.CreateClusterSecret(t, f, kubeName)
		defer e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, clusterSecret.Name, &metav1.DeleteOptions{})

		// Update cluster parameters
		e2espec.SetClusterName(decoratorObj.clusterName)
		defer e2espec.ResetClusterName()

		decoratorObj.SetTlsForTesting(operatorSecret, clusterSecret)
		defer e2espec.ResetTLS()

		if decoratorObj.test != nil {
			// Run the test!
			decoratorObj.test(t)
		}
	}
	return wrapperFunc
}

// randomClusterName returns a randomized cluster name
func randomClusterName(randomSuffix string) string {
	return "test-couchbase-" + randomSuffix
}

// tlsDecorator accepts a test function and key type and returns a decorated
// version of the function which creates cluster specific TLS certificates and
// installs them into K8S before running the test(s)
func tlsDecorator(test framework.TestFunc, keyType e2eutil.KeyType) framework.TestFunc {
	f := framework.Global
	kubeName := "BasicCluster"
	RandomNameSuffix = e2eutil.RandomSuffix()
	decoratorObj := &TlsDecorator{}
	decoratorObj.Init(RandomNameSuffix, f.Namespace, keyType)
	decoratorObj.test = test
	return CreateWrapperFunc(kubeName, decoratorObj, keyType)
}

// rsaDecorator runs a test with static TLS and RSA based keys
func rsaDecorator(test framework.TestFunc) framework.TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeRSA)
}

// ellipticP224Decorator runs a test with static TLS and EC P224 based keys
func ellipticP224Decorator(test framework.TestFunc) framework.TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeEllipticP224)
}

// ellipticP256Decorator runs a test with static TLS and EC P256 based keys
func ellipticP256Decorator(test framework.TestFunc) framework.TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeEllipticP256)
}

// ellipticP384Decorator runs a test with static TLS and EC P384 based keys
func ellipticP384Decorator(test framework.TestFunc) framework.TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeEllipticP384)
}

// ellipticP521Decorator runs a test with static TLS and EC P521 based keys
func ellipticP521Decorator(test framework.TestFunc) framework.TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeEllipticP521)
}

// Create couchbase cluster over TLS certificates
// Check TLS handshake is successful with all nodes
func TestTlsCreateCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal("Create cluster failed:", err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
	if err != nil {
		t.Fatal("Cluster failed to become healthy:", err.Error())
	}

	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	if err != nil {
		t.Fatal("Unable to get couchbase pods:", err)
	}

	// TLS handshake with pods
	for _, pod := range pods.Items {
		err = e2eutil.TlsCheckForPod(t, f.Namespace, pod.GetName(), targetKube.Config)
		if err != nil {
			t.Fatal("TLS verification failed:", err)
		}
	}

	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v\n", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Tests scenario where a third node is being added to a cluster, and a separate
// node goes down immediately after the add & before the rebalance.
// Expects: autofailover of down node occurs and a replacement node is added
// Check TLS handshake is successful with all nodes
func TestTlsKillClusterNode(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	podToKillMemberId := 1

	// create 1 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// async scale up to 3 node cluster
	if err := e2eutil.ResizeClusterNoWait(t, 0, e2eutil.Size3, targetKube.CRClient, testCouchbase); err != nil {
		t.Fatal("Failed to trigger cluster resize")
	}

	// wait for add member event
	event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
	if err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}

	for nodeIndex := 1; nodeIndex < e2eutil.Size3; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}

	// kill pod 1
	err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podToKillMemberId)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// cluster should also be balanced
	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries20)
	if err != nil {
		t.Fatal(err)
	}

	// Event checking
	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get couchbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Create Couchbase cluster using certificates
// Resize cluster to different sizes in loop
// Check TLS handshake is successful with all cluster nodes
func TestTlsResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	clusterSizes := []int{2, 3, 2, 1}

	prevClusterSize := e2eutil.Size1

	for _, clusterSize := range clusterSizes {
		service := 0
		err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
		if err != nil {
			t.Fatal("Cluster not healthy:", err)
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
		}
		prevClusterSize = clusterSize
	}

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get couchbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

}

// Remove Operator certificate after cluster deployment
// Kill a node from cluster and new node addition should fail
// Add the operator certificate back and check new node addition is successful
func TestTlsRemoveOperatorCertificateAndAddBack(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	podToKillMemberId := 1

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// Get current secret to re-create later
	operatorSecret, err := e2eutil.GetSecret(targetKube.KubeClient, f.Namespace, "operator-secret-tls-"+RandomNameSuffix)
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, "operator-secret-tls-"+RandomNameSuffix, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase operator certificate deleted")

	// kill pod 1
	err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podToKillMemberId)
	if err != nil {
		t.Fatal(err)
	}

	event := e2eutil.NewMemberCreationFailedEvent(testCouchbase, 3)
	err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddMemberDownEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, 3)

	// Recreating the operator certificate with old data
	operatorSecretData := e2eutil.CreateOperatorSecretData(f.Namespace, "operator-secret-tls-"+RandomNameSuffix, operatorSecret.Data["ca.crt"], operatorSecret.Data["couchbase-operator.crt"], operatorSecret.Data["couchbase-operator.key"])
	_, err = e2eutil.CreateSecret(targetKube.KubeClient, f.Namespace, operatorSecretData)
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// Event checking
	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get couchbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Deploy cluster using valid TLS certificates
// Remove the operator certificate from the cluster and try to scale up the cluster
// New pods will be removed the operator since it cannot communicate with the new pods
// Add back the operator certificate back to ensure scaling up succeeds
func TestTlsRemoveOperatorCertificateAndResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	if err != nil {
		t.Fatal(err)
	}

	// TLS handshake with pods
	for _, pod := range pods.Items {
		err = e2eutil.TlsCheckForPod(t, f.Namespace, pod.GetName(), targetKube.Config)
		if err != nil {
			t.Fatal("TLS verification failed:", err)
		}
	}

	resizeErr := make(chan error)
	operatorSecret, err := e2eutil.GetSecret(targetKube.KubeClient, f.Namespace, "operator-secret-tls-"+RandomNameSuffix)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, "operator-secret-tls-"+RandomNameSuffix, &metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase operator certificate deleted")

	go func() {
		service := 0
		resizeErr <- e2eutil.ResizeCluster(t, service, e2eutil.Size5, targetKube.CRClient, testCouchbase)
	}()

	event := e2eutil.NewMemberCreationFailedEvent(testCouchbase, 3)
	err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)

	// Recreating the operator certificate with old data
	operatorSecretData := e2eutil.CreateOperatorSecretData(f.Namespace, "operator-secret-tls-"+RandomNameSuffix, operatorSecret.Data["ca.crt"], operatorSecret.Data["couchbase-operator.crt"], operatorSecret.Data["couchbase-operator.key"])
	_, err = e2eutil.CreateSecret(targetKube.KubeClient, f.Namespace, operatorSecretData)
	if err != nil {
		t.Fatal(err)
	}

	err = <-resizeErr
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatal(err.Error())
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// Event checking
	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get couchbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Deploy cluster using valid TLS certificates
// Remove the cluster certificate from the cluster and kill one of the cluster pod
// New pod creation with fail unless the valid cluster certificate is available
func TestTlsRemoveClusterCertificateAndAddBack(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	podToKillMemberId := 1

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	clusterSecret, err := e2eutil.GetSecret(targetKube.KubeClient, f.Namespace, "cluster-secret-tls-"+RandomNameSuffix)
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, "cluster-secret-tls-"+RandomNameSuffix, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase Cluster certificate deleted")

	// kill pod 1
	err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podToKillMemberId)
	if err != nil {
		t.Fatal(err)
	}

	// This will take an absolute eternity as we need to allow a large leeway for a
	// node to pull container images down before timing out
	event := e2eutil.NewMemberCreationFailedEvent(testCouchbase, 3)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 360); err != nil {
		t.Fatal("Failed to observe member fail addition")
	}

	expectedEvents.AddMemberDownEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, 3)

	// Recreate the cluster certificate with old data
	clusterSecretData := e2eutil.CreateClusterSecretData(f.Namespace, "cluster-secret-tls-"+RandomNameSuffix, clusterSecret.Data["chain.pem"], clusterSecret.Data["pkey.key"])
	_, err = e2eutil.CreateSecret(targetKube.KubeClient, f.Namespace, clusterSecretData)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries30)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Event checking
	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get couchbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Deploy cluster using valid TLS certificates
// Remove the cluster certificate from the cluster and scale up the cluster
// New pod creation with fail due to the missing certificate data
func TestTlsRemoveClusterCertificateAndResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	err = e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, "cluster-secret-tls-"+RandomNameSuffix, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase Cluster certificate deleted")

	// Set the resize off
	service := 0
	if err := e2eutil.ResizeClusterNoWait(t, service, e2eutil.Size4, targetKube.CRClient, testCouchbase); err != nil {
		t.Fatal(err)
	}

	// This will take an absolute eternity as we need to allow a large leeway for a
	// node to pull container images down before timing out
	event := e2eutil.NewMemberCreationFailedEvent(testCouchbase, 3)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 360); err != nil {
		t.Fatal("Failed to observe member fail addition")
	}

	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, 3)

	// Event checking
	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get couchbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Deploy cluster using invalid DNS name value in the certificate
// Cluster creation should fail due to the invalid DNS value
func TestTlsNegRSACertificateDnsName(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// Actual Test case function
	TestToRun := func(t *testing.T) {
		cluster, err := e2eutil.NewClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
		if err != nil {
			t.Fatal("Cluster creation failed:", err)
		}
		defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

		// Expect the cluster to enter a failed state
		if err := e2eutil.WaitClusterPhaseFailed(t, targetKube.CRClient, cluster.Name, f.Namespace, 10); err != nil {
			t.Fatalf("cluster failed to enter failed state: %v\n", err)
		}
	}

	dnsNames := []string{
		"*.test-couchbase-invalid-name." + f.Namespace + ".svc",
	}

	// Setting up TLS certificates into K8S
	RandomNameSuffix = e2eutil.RandomSuffix()
	decoratorObj := &TlsDecorator{}
	decoratorObj.Init(RandomNameSuffix, f.Namespace, e2eutil.KeyTypeRSA)

	// Setting invalid DNS host value
	decoratorObj.dnsNames = dnsNames
	decoratorObj.test = TestToRun
	execFunc := CreateWrapperFunc(kubeName, decoratorObj, e2eutil.KeyTypeRSA)
	execFunc(t)
}

// Deploy cluster using a TLS certificates which will expire after few minutes
// Cluster creation will be successful.
// Wait for certificate to expire and try to scale up the cluster
// Cluster scaling will fail due to new pod creation failure
func TestTlsCertificateExpiry(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// Setting up TLS certificates into K8S
	RandomNameSuffix = e2eutil.RandomSuffix()
	decoratorObj := &TlsDecorator{}
	decoratorObj.Init(RandomNameSuffix, f.Namespace, e2eutil.KeyTypeRSA)

	// Actual Test case function
	TestToRun := func(t *testing.T) {
		testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
		if err != nil {
			t.Fatal(err)
		}
		defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

		pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
		if err != nil {
			t.Fatal(err)
		}
		for _, pod := range pods.Items {
			err = e2eutil.TlsCheckForPod(t, f.Namespace, pod.GetName(), targetKube.Config)
			if err != nil {
				t.Fatal(err)
			}
		}

		t.Log("Waiting for certificate to expire")
		for {
			currTime := time.Now().In(time.UTC)
			if currTime.After(decoratorObj.certValidTo) {
				break
			}
			time.Sleep(time.Second)
		}

		service := 0
		err = e2eutil.ResizeCluster(t, service, e2eutil.Size4, targetKube.CRClient, testCouchbase)
		if err != nil {
			t.Fatalf("Resize cluster failed: %v\n", err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size4, e2eutil.Retries5)
		if err == nil {
			t.Fatal("Cluster scaled with expired certificates")
		}
	}

	// Setting Certificate expiry time of 2mins from current time
	decoratorObj.certValidTo = decoratorObj.certValidFrom.Add(time.Second * 120)
	decoratorObj.test = TestToRun
	execFunc := CreateWrapperFunc(kubeName, decoratorObj, e2eutil.KeyTypeRSA)
	execFunc(t)
}

// Deploy a couchbase cluster using a expired TLS certificate
// Cluster creation should fail
func TestTlsNegCertificateExpiredBeforeDeployment(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// Actual Test case function
	TestToRun := func(t *testing.T) {
		cluster, err := e2eutil.NewClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
		if err != nil {
			t.Fatalf("Cluster creation failed: %v\n", err)
		}
		defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

		// Expect the cluster to enter a failed state
		if err := e2eutil.WaitClusterPhaseFailed(t, targetKube.CRClient, cluster.Name, f.Namespace, 10); err != nil {
			t.Fatalf("cluster failed to enter failed state: %v\n", err)
		}
	}

	// Setting up TLS certificates into K8S
	RandomNameSuffix = e2eutil.RandomSuffix()
	decoratorObj := &TlsDecorator{}
	decoratorObj.Init(RandomNameSuffix, f.Namespace, e2eutil.KeyTypeRSA)

	// Setting Certificate expiry time before deployment time
	decoratorObj.certValidTo = decoratorObj.certValidFrom.Add(time.Second * -1)
	decoratorObj.test = TestToRun
	execFunc := CreateWrapperFunc(kubeName, decoratorObj, e2eutil.KeyTypeRSA)
	execFunc(t)
}

// Deploy the cluster using the certificate which is not yet valid
// Cluster creation should not happen until the validity time crosses the current time
func TestTlsCertificateDeployedBeforeValidity(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// Setting up TLS certificates into K8S
	RandomNameSuffix = e2eutil.RandomSuffix()
	decoratorObj := &TlsDecorator{}
	decoratorObj.Init(RandomNameSuffix, f.Namespace, e2eutil.KeyTypeRSA)

	// Actual Test case function
	TestToRun := func(t *testing.T) {
		var err error
		createClusterErrChan := make(chan error)
		go func() {
			_, err = e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
			createClusterErrChan <- err
		}()
		defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
		time.Sleep(time.Second)

		t.Log("Retrying to deploy operator on valid TLS time-slot")
		for {
			currTime := time.Now().In(time.UTC)
			if currTime.After(decoratorObj.certValidFrom) {
				break
			}

			pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
			if err != nil {
				t.Fatal(err)
			}
			if len(pods.Items) > 1 {
				t.Fatal("Cluster initialized before certificate activation period")
			}
			time.Sleep(time.Second)
		}

		err = <-createClusterErrChan
		if err != nil {
			t.Fatal("Cluster creation failed:", err)
		}
	}

	// Setting Certificate validaity after 3mins from current time
	decoratorObj.certValidFrom = decoratorObj.certValidFrom.Add(time.Second * 180)
	decoratorObj.certValidTo = decoratorObj.certValidFrom.AddDate(10, 0, 0)
	decoratorObj.test = TestToRun
	execFunc := CreateWrapperFunc(kubeName, decoratorObj, e2eutil.KeyTypeRSA)
	execFunc(t)
}

// Create a couchbase cluster using the wrong CA certificate type
// Cluster deployment should fail
func TestTlsGenerateWrongCACertType(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// Actual Test case function
	TestToRun := func(t *testing.T) {
		// Create cluster
		testCouchbase, err := e2eutil.NewClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
		if err != nil {
			t.Fatalf("Error while creating cluster: %v\n", err)
		}
		defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

		// Expect the cluster to enter a failed state
		if err := e2eutil.WaitClusterPhaseFailed(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, 10); err != nil {
			t.Fatalf("cluster failed to enter failed state: %v\n", err)
		}
		t.Log(err)
	}

	// Setting up TLS certificates into K8S
	RandomNameSuffix = e2eutil.RandomSuffix()
	decoratorObj := &TlsDecorator{}
	decoratorObj.Init(RandomNameSuffix, f.Namespace, e2eutil.KeyTypeRSA)

	// Setting wrong CA cert type
	decoratorObj.certType = e2eutil.CertTypeServer
	decoratorObj.test = TestToRun
	execFunc := CreateWrapperFunc(kubeName, decoratorObj, e2eutil.KeyTypeRSA)
	execFunc(t)
}

// Create a couchbase cluster using the wrong certificate type
// Cluster deployment should fail
func TestTlsGenerateWrongCertType(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	TestToRun := func(t *testing.T) {
		cluster, err := e2eutil.NewClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
		if err != nil {
			t.Fatalf("Cluster creation failed: %v\n", err)
		}
		defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

		// Expect the cluster to enter a failed state
		if err := e2eutil.WaitClusterPhaseFailed(t, targetKube.CRClient, cluster.Name, f.Namespace, 10); err != nil {
			t.Fatalf("cluster failed to enter failed state: %v\n", err)
		}
	}

	RandomNameSuffix = e2eutil.RandomSuffix()
	decoratorObj := &TlsDecorator{}
	decoratorObj.Init(RandomNameSuffix, f.Namespace, e2eutil.KeyTypeRSA)

	decoratorObj.CreateCaRootCert(t)
	operatorSecret := decoratorObj.CreateOperatorSecret(t, f, kubeName)
	defer e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, operatorSecret.Name, &metav1.DeleteOptions{})

	clusterSecret := decoratorObj.CreateClusterSecret(t, f, kubeName)
	defer e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, clusterSecret.Name, &metav1.DeleteOptions{})

	// Update cluster parameters
	e2espec.SetClusterName(decoratorObj.clusterName)
	defer e2espec.ResetClusterName()

	// Creating invalid TLS by swapping secrets
	decoratorObj.SetTlsForTesting(clusterSecret, operatorSecret)
	defer e2espec.ResetTLS()

	TestToRun(t)
}
