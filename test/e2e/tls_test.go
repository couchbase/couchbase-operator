package e2e

import (
	"os"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create couchbase cluster over TLS certificates
// Check TLS handshake is successful with all nodes
func TestTlsCreateCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, &e2eutil.TlsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	testCouchbase, err := e2eutil.NewTLSClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)
	if err != nil {
		t.Fatal("Create cluster failed:", err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, constants.Size3, constants.Retries10)
	if err != nil {
		t.Fatal("Cluster failed to become healthy:", err.Error())
	}

	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		t.Fatal("Unable to get couchbase pods:", err)
	}

	// TLS handshake with pods
	for _, pod := range pods.Items {
		if err := e2eutil.TlsCheckForPod(t, f.Namespace, pod.GetName(), targetKube.Config, ctx.CA); err != nil {
			t.Fatal("TLS verification failed:", err)
		}
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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

	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, &e2eutil.TlsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	podToKillMemberId := 1

	// create 1 node cluster
	testCouchbase, err := e2eutil.NewTLSClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminHidden, ctx)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// async scale up to 3 node cluster
	if err := e2eutil.ResizeClusterNoWait(t, 0, constants.Size3, targetKube.CRClient, testCouchbase); err != nil {
		t.Fatal("Failed to trigger cluster resize")
	}

	// wait for add member event
	event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
	if err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}

	for nodeIndex := 1; nodeIndex < constants.Size3; nodeIndex++ {
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
	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, constants.Size3, constants.Retries20)
	if err != nil {
		t.Fatal(err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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

	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, &e2eutil.TlsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewTLSClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size1, constants.WithoutBucket, constants.AdminHidden, ctx)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	clusterSizes := []int{2, 3, 2, 1}

	prevClusterSize := constants.Size1

	for _, clusterSize := range clusterSizes {
		service := 0
		err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, constants.Retries10)
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

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, constants.Size1, constants.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Remove Operator certificate after cluster deployment
// Delete the operator secret and kill a node from cluster.  The cluster should
// raise an invalid TLS event.
// Add the operator certificate back and check new node addition is successful
func TestTlsRemoveOperatorCertificateAndAddBack(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, &e2eutil.TlsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	podToKillMemberId := 1

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewTLSClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithBucket, constants.AdminHidden, ctx)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// Get current secret to re-create later
	operatorSecret, err := e2eutil.GetSecret(targetKube.KubeClient, f.Namespace, ctx.OperatorSecretName)
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, ctx.OperatorSecretName, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase operator certificate deleted")

	// kill pod 1
	err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podToKillMemberId)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	// Recreating the operator certificate with old data
	operatorSecretData := e2eutil.CreateOperatorSecretData(f.Namespace, ctx.OperatorSecretName, operatorSecret.Data["ca.crt"], operatorSecret.Data["couchbase-operator.crt"], operatorSecret.Data["couchbase-operator.key"])
	_, err = e2eutil.CreateSecret(targetKube.KubeClient, f.Namespace, operatorSecretData)
	if err != nil {
		t.Fatal(err)
	}

	event := e2eutil.NewMemberDownEvent(testCouchbase, podToKillMemberId)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberDownEvent(testCouchbase, podToKillMemberId)

	event = e2eutil.NewMemberFailedOverEvent(testCouchbase, podToKillMemberId)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 40); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podToKillMemberId)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 3)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, constants.Size3, constants.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy cluster using valid TLS certificates
// Remove the operator certificate from the cluster and try to scale up the cluster
// The cluster should raise a TLS invalid event.
// Add back the operator certificate back to ensure scaling up succeeds
func TestTlsRemoveOperatorCertificateAndResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, &e2eutil.TlsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewTLSClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithBucket, constants.AdminHidden, ctx)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		t.Fatal(err)
	}

	// TLS handshake with pods
	for _, pod := range pods.Items {
		if err := e2eutil.TlsCheckForPod(t, f.Namespace, pod.GetName(), targetKube.Config, ctx.CA); err != nil {
			t.Fatal("TLS verification failed:", err)
		}
	}

	resizeErr := make(chan error)
	operatorSecret, err := e2eutil.GetSecret(targetKube.KubeClient, f.Namespace, ctx.OperatorSecretName)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, ctx.OperatorSecretName, &metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase operator certificate deleted")

	go func() {
		service := 0
		resizeErr <- e2eutil.ResizeCluster(t, service, constants.Size5, targetKube.CRClient, testCouchbase)
	}()

	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)

	// Recreating the operator certificate with old data
	operatorSecretData := e2eutil.CreateOperatorSecretData(f.Namespace, ctx.OperatorSecretName, operatorSecret.Data["ca.crt"], operatorSecret.Data["couchbase-operator.crt"], operatorSecret.Data["couchbase-operator.key"])
	_, err = e2eutil.CreateSecret(targetKube.KubeClient, f.Namespace, operatorSecretData)
	if err != nil {
		t.Fatal(err)
	}

	err = <-resizeErr
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, constants.Size5, constants.Retries20)
	if err != nil {
		t.Fatal(err.Error())
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy cluster using valid TLS certificates
// Remove the cluster certificate from the cluster and kill one of the cluster pod
// The cluster should raise a TLS invalid event, then reconcile once the valid cluster certificate is available
func TestTlsRemoveClusterCertificateAndAddBack(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, &e2eutil.TlsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	podToKillMemberId := 1

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewTLSClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithBucket, constants.AdminHidden, ctx)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	clusterSecret, err := e2eutil.GetSecret(targetKube.KubeClient, f.Namespace, ctx.ClusterSecretName)
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, ctx.ClusterSecretName, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase Cluster certificate deleted")

	// kill pod 1
	err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podToKillMemberId)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	// Recreate the cluster certificate with old data
	clusterSecretData := e2eutil.CreateClusterSecretData(f.Namespace, ctx.ClusterSecretName, clusterSecret.Data["chain.pem"], clusterSecret.Data["pkey.key"])
	_, err = e2eutil.CreateSecret(targetKube.KubeClient, f.Namespace, clusterSecretData)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 3), 360); err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddMemberDownEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podToKillMemberId)

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, constants.Size3, constants.Retries30)
	if err != nil {
		t.Fatal(err.Error())
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy cluster using valid TLS certificates
// Remove the cluster certificate from the cluster and scale up the cluster
func TestTlsRemoveClusterCertificateAndResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, &e2eutil.TlsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewTLSClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithBucket, constants.AdminHidden, ctx)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// Set the resize off
	service := 0
	if err := e2eutil.ResizeClusterNoWait(t, service, constants.Size4, targetKube.CRClient, testCouchbase); err != nil {
		t.Fatal(err)
	}

	err = e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, ctx.ClusterSecretName, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase Cluster certificate deleted")

	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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

	opts := &e2eutil.TlsOpts{
		AltNames: []string{
			"*.test-couchbase-invalid-name." + f.Namespace + ".svc",
		},
	}
	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	// Actual Test case function
	if _, err := e2eutil.NewTLSClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx); err == nil {
		t.Fatal("Cluster creation succeeded")
	}
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

	validTo := time.Now().In(time.UTC).Add(time.Second * 240)
	opts := &e2eutil.TlsOpts{
		ValidTo: &validTo,
	}
	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	testCouchbase, err := e2eutil.NewTLSClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	for memberId := 0; memberId < constants.Size3; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pod := range pods.Items {
		err = e2eutil.TlsCheckForPod(t, f.Namespace, pod.GetName(), targetKube.Config, ctx.CA)
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Log("Waiting for certificate to expire")
	for {
		currTime := time.Now().In(time.UTC)
		if currTime.After(validTo) {
			break
		}
		time.Sleep(time.Second)
	}

	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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

	// Set the cert creation date 10 years in the past
	validTo := time.Now().In(time.UTC)
	validFrom := validTo.AddDate(-10, 0, 0)
	opts := &e2eutil.TlsOpts{
		ValidFrom: &validFrom,
		ValidTo:   &validTo,
	}
	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	// Actual Test case function
	if _, err := e2eutil.NewTLSClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx); err == nil {
		t.Fatal("Cluster creation succeeded\n")
	}
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

	validFrom := time.Now().In(time.UTC).Add(30 * time.Second)
	opts := &e2eutil.TlsOpts{
		ValidFrom: &validFrom,
	}
	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	if _, err := e2eutil.NewTLSClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx); err == nil {
		t.Fatal("Cluster creation succeeded\n")
	}
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

	caCertType := e2eutil.CertTypeServer
	opts := &e2eutil.TlsOpts{
		CaCertType: &caCertType,
	}
	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	// Create cluster
	if _, err := e2eutil.NewTLSClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx); err == nil {
		t.Fatal("Cluster creation succeeded\n")
	}
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

	clusterCertType := e2eutil.CertTypeClient
	opts := &e2eutil.TlsOpts{
		ClusterCertType: &clusterCertType,
	}
	ctx, teardown, err := e2eutil.InitClusterTLS(targetKube.KubeClient, f.Namespace, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()

	if _, err := e2eutil.NewTLSClusterBasicNoWait(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx); err == nil {
		t.Fatal("Cluster creation succeeded\n")
	}
}
