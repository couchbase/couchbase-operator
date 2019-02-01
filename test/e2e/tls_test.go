package e2e

import (
	"os"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

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
	targetKube := f.GetCluster(0)

	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()

	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, targetKube, f.Namespace, ctx)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	targetKube := f.GetCluster(0)

	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()

	podToKillMemberId := 1

	// create 1 node cluster
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminHidden, ctx)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// async scale up to 3 node cluster
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, constants.Size3, targetKube, testCouchbase)

	// wait for add member event
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 2), 5*time.Minute)

	for nodeIndex := 1; nodeIndex < constants.Size3; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}

	// kill pod 1
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podToKillMemberId, true)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// cluster should also be balanced
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create Couchbase cluster using certificates
// Resize cluster to different sizes in loop
// Check TLS handshake is successful with all cluster nodes
func TestTlsResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithoutBucket, constants.AdminHidden, ctx)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	clusterSizes := []int{2, 3, 2, 1}

	prevClusterSize := constants.Size1

	for _, clusterSize := range clusterSizes {
		service := 0
		testCouchbase = e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, testCouchbase, constants.Retries30)

		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	targetKube := f.GetCluster(0)

	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()

	podToKillMemberId := 1

	// create 3 node cluster
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithBucket, constants.AdminHidden, ctx)

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
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podToKillMemberId, true)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30*time.Second)
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	// Recreating the operator certificate with old data
	operatorSecretData := e2eutil.CreateOperatorSecretData(f.Namespace, ctx.OperatorSecretName, operatorSecret.Data["ca.crt"], operatorSecret.Data["couchbase-operator.crt"], operatorSecret.Data["couchbase-operator.key"])
	_, err = e2eutil.CreateSecret(targetKube.KubeClient, f.Namespace, operatorSecretData)
	if err != nil {
		t.Fatal(err)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, podToKillMemberId), 30*time.Second)
	expectedEvents.AddMemberDownEvent(testCouchbase, podToKillMemberId)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberFailedOverEvent(testCouchbase, podToKillMemberId), 40*time.Second)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podToKillMemberId)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 3), 2*time.Minute)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// TestTlsRemoveOperatorCertificateAndResizeCluster removes the CA certificate
// expects the operator to raise an invalid TLS error and be able to provision
// new pods once the CA cert is restorred.
func TestTlsRemoveOperatorCertificateAndResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	scaledClusterSize := constants.Size5

	// Create the cluster.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden, ctx)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, targetKube, f.Namespace, ctx)
	secret := e2eutil.MustGetSecret(t, targetKube, f.Namespace, ctx.OperatorSecretName)
	e2eutil.MustDeleteSecret(t, targetKube, f.Namespace, ctx.OperatorSecretName)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30*time.Second)
	e2eutil.MustRecreateSecret(t, targetKube, f.Namespace, secret)
	testCouchbase = e2eutil.MustResizeCluster(t, 0, scaledClusterSize, targetKube, testCouchbase, 300)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Invalid TLS event
	// * Cluster scaled up
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		e2eutil.ClusterScaleUpSequence(scaledClusterSize - clusterSize),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)

}

// Deploy cluster using valid TLS certificates
// Remove the cluster certificate from the cluster and kill one of the cluster pod
// The cluster should raise a TLS invalid event, then reconcile once the valid cluster certificate is available
func TestTlsRemoveClusterCertificateAndAddBack(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()

	podToKillMemberId := 1

	// create 3 node cluster
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithBucket, constants.AdminHidden, ctx)

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
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podToKillMemberId, true)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30*time.Second)
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	// Recreate the cluster certificate with old data
	clusterSecretData := e2eutil.CreateClusterSecretData(f.Namespace, ctx.ClusterSecretName, clusterSecret.Data["chain.pem"], clusterSecret.Data["pkey.key"])
	_, err = e2eutil.CreateSecret(targetKube.KubeClient, f.Namespace, clusterSecretData)
	if err != nil {
		t.Fatal(err)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 3), 6*time.Minute)

	expectedEvents.AddMemberDownEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podToKillMemberId)

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy cluster using valid TLS certificates
// Remove the cluster certificate from the cluster and scale up the cluster
func TestTlsRemoveClusterCertificateAndResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()

	// create 3 node cluster
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithBucket, constants.AdminHidden, ctx)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	if err := e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, ctx.ClusterSecretName, &metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Log("Couchbase Cluster certificate deleted")

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30*time.Second)
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy cluster using invalid DNS name value in the certificate
// Cluster creation should fail due to the invalid DNS value
func TestTlsNegRSACertificateDnsName(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	opts := &e2eutil.TlsOpts{
		AltNames: []string{
			"*.test-couchbase-invalid-name." + f.Namespace + ".svc",
		},
	}
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, opts)
	defer teardown()

	// Actual Test case function
	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)
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
	targetKube := f.GetCluster(0)

	validTo := time.Now().In(time.UTC).Add(time.Second * 240)
	opts := &e2eutil.TlsOpts{
		ValidTo: &validTo,
	}
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, opts)
	defer teardown()

	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)

	expectedEvents := e2eutil.EventList{}
	for memberId := 0; memberId < constants.Size3; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustCheckClusterTLS(t, targetKube, f.Namespace, ctx)

	t.Log("Waiting for certificate to expire")
	for {
		currTime := time.Now().In(time.UTC)
		if currTime.After(validTo) {
			break
		}
		time.Sleep(time.Second)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30*time.Second)
	expectedEvents.AddTLSInvalidEvent(testCouchbase)

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy a couchbase cluster using a expired TLS certificate
// Cluster creation should fail
func TestTlsNegCertificateExpiredBeforeDeployment(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Set the cert creation date 10 years in the past
	validTo := time.Now().In(time.UTC)
	validFrom := validTo.AddDate(-10, 0, 0)
	opts := &e2eutil.TlsOpts{
		ValidFrom: &validFrom,
		ValidTo:   &validTo,
	}
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, opts)
	defer teardown()

	// Actual Test case function
	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)
}

// Deploy the cluster using the certificate which is not yet valid
// Cluster creation should not happen until the validity time crosses the current time
func TestTlsCertificateDeployedBeforeValidity(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	validFrom := time.Now().In(time.UTC).Add(30 * time.Second)
	opts := &e2eutil.TlsOpts{
		ValidFrom: &validFrom,
	}
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, opts)
	defer teardown()

	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)
}

// Create a couchbase cluster using the wrong CA certificate type
// Cluster deployment should fail
func TestTlsGenerateWrongCACertType(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	caCertType := e2eutil.CertTypeServer
	opts := &e2eutil.TlsOpts{
		CaCertType: &caCertType,
	}
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, opts)
	defer teardown()

	// Create cluster
	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)
}

// Create a couchbase cluster using the wrong certificate type
// Cluster deployment should fail
func TestTlsGenerateWrongCertType(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterCertType := e2eutil.CertTypeClient
	opts := &e2eutil.TlsOpts{
		ClusterCertType: &clusterCertType,
	}
	ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, opts)
	defer teardown()

	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden, ctx)
}

// TestTLSRotate tests a certificate can be reissued by a CA.
// * Ensures new certifcate is loaded from the inbox on update
func TestTLSRotate(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, kubernetes, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden, ctx)

	// When the cluster is ready, swap out the old certificate for a new one and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificate(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster.Namespace, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateChain tests a certificate can be reissued by a CA with a new sub-CA.
// * Ensures new certifcate chain is loaded from the inbox on update
func TestTLSRotateChain(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, kubernetes, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden, ctx)

	// When the cluster is ready, swap out the old certificate for a new chain and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateChain(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster.Namespace, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateCA tests a certificate and CA can be reissued.
// * Ensures a new PKI is loaded from the inbox and the cluster CA updated.
func TestTLSRotateCA(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, kubernetes, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden, ctx)

	// When the cluster is ready, swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster.Namespace, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateCAAndScale tests the operator can talk to a cluster after
// replacing the CA.
func TestTLSRotateCAAndScale(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	clusterScaleUpSize := 1

	// Create the cluster with a valid 1 deep certificate chain.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, kubernetes, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden, ctx)

	// When the cluster is ready, swap out the all certificates for a new ones and verify,
	// then make sure the operator can scale the cluster (e.g. talk to it with the new CA)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster.Namespace, ctx)
	e2eutil.MustResizeCluster(t, 0, clusterSize+clusterScaleUpSize, kubernetes, cluster, constants.Retries30)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	// * Cluster successfully connects to, initializes and balances in new nodes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		e2eutil.ClusterScaleUpSequence(clusterScaleUpSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateCAAndKillOperator tests a certificate and CA can be reissued while
// the operator is being restarted.
// * Ensures a new PKI is loaded from the inbox and the cluster CA updated.
func TestTLSRotateCAAndKillOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, kubernetes, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden, ctx)

	// When the cluster is ready, restart the operator and swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustDeleteCouchbaseOperator(t, kubernetes, cluster.Namespace)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster.Namespace, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateCAKillPodAndKillOperator tests a certificate and CA can be reissued while
// the operator is being restarted with a stateful pod down.
func TestTLSRotateCAKillPodAndKillOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimIndex := 0

	// Create the cluster with a valid 1 deep certificate chain.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, kubernetes, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()
	cluster := e2eutil.MustNewSupportableTLSCluster(t, kubernetes, f.Namespace, mdsGroupSize, ctx)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, kill a stateful pod,  restart the operator and swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustDeleteCouchbaseOperator(t, kubernetes, cluster.Namespace)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster.Namespace, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred for live nodes
	// * Down member is recovered
	// * TLS update exent occurred for recovered node
	// * Cluster recovered
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateInvalid tests the operator raises a TLSInvalid event when a certificate
// doesn't validate with the supplied CA.
func TestTLSRotateInvalid(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx, teardown := e2eutil.MustInitClusterTLS(t, kubernetes, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown()
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden, ctx)

	// When the cluster is ready, swap out the server certificate for a new one from a new CA.
	// Expect the operator to raise an event to alert that TLS has been misconfigured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateWrongCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSInvalidEvent(cluster), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS failed event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
