package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Create couchbase cluster over TLS certificates
// Check TLS handshake is successful with all nodes.
func TestTlsCreateCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, targetKube, ctx)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests scenario where a third node is being added to a cluster, and a separate
// node goes down immediately after the add & before the rebalance.
// Expects: autofailover of down node occurs and a replacement node is added
// Check TLS handshake is successful with all nodes.
func TestTlsKillClusterNode(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1
	scaledClusterSize := constants.Size3
	victimIndex := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(testCouchbase.Name, victimIndex)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, scaledClusterSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, scaledClusterSize-1), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: scaledClusterSize - clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create Couchbase cluster using certificates.
// Resize cluster to different sizes in loop.
// Check TLS handshake is successful with all cluster nodes.
func TestTlsResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1
	serviceID := 0

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	// Create the cluster.
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	// When the cluster is ready scale up to 3 nodes then down to 1 again.
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size3, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size1, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scales up and down
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Remove Operator certificate after cluster deployment.
// Delete the operator secret and kill a node from cluster.  The cluster should
// raise an invalid TLS event.
// Add the operator certificate back and check new node addition is successful.
func TestTlsRemoveOperatorCertificateAndAddBack(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, targetKube, ctx)
	secret := e2eutil.MustGetSecret(t, targetKube, ctx.OperatorSecretName)
	e2eutil.MustDeleteSecret(t, targetKube, ctx.OperatorSecretName)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30*time.Second)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustRecreateSecret(t, targetKube, secret)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Invalid TLS event
	// * Cluster scaled up
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		e2eutil.PodDownFailoverRecoverySequence(),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, targetKube, ctx)
	secret := e2eutil.MustGetSecret(t, targetKube, ctx.OperatorSecretName)
	e2eutil.MustDeleteSecret(t, targetKube, ctx.OperatorSecretName)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30*time.Second)
	e2eutil.MustRecreateSecret(t, targetKube, secret)
	testCouchbase = e2eutil.MustResizeCluster(t, 0, scaledClusterSize, targetKube, testCouchbase, 5*time.Minute)

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

// Deploy cluster using valid TLS certificates.
// Remove the cluster certificate from the cluster and kill one of the cluster pod.
// The cluster should raise a TLS invalid event, then reconcile once the valid cluster certificate is available.
func TestTlsRemoveClusterCertificateAndAddBack(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, targetKube, ctx)
	secret := e2eutil.MustGetSecret(t, targetKube, ctx.ClusterSecretName)
	e2eutil.MustDeleteSecret(t, targetKube, ctx.ClusterSecretName)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), 30*time.Second)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustRecreateSecret(t, targetKube, secret)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Invalid TLS event
	// * Cluster scaled up
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		e2eutil.PodDownFailoverRecoverySequence(),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy cluster using valid TLS certificates.
// Remove the cluster certificate from the cluster and scale up the cluster.
func TestTlsRemoveClusterCertificateAndResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	scaledClusterSize := constants.Size5

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, targetKube, ctx)
	secret := e2eutil.MustGetSecret(t, targetKube, ctx.ClusterSecretName)
	e2eutil.MustDeleteSecret(t, targetKube, ctx.ClusterSecretName)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), time.Minute)
	e2eutil.MustRecreateSecret(t, targetKube, secret)
	testCouchbase = e2eutil.MustResizeCluster(t, 0, scaledClusterSize, targetKube, testCouchbase, 5*time.Minute)
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

// Deploy cluster using invalid DNS name value in the certificate.
// Cluster creation should fail due to the invalid DNS value.
func TestTlsNegRSACertificateDnsName(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	opts := &e2eutil.TLSOpts{
		AltNames: []string{
			"*.test-couchbase-invalid-name." + targetKube.Namespace + ".svc",
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, opts)

	// Actual Test case function
	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, constants.Size3, ctx)
}

// Deploy cluster using a TLS certificates which will expire after few minutes.
// Cluster creation will be successful.
// Wait for certificate to expire and try to scale up the cluster.
// Cluster scaling will fail due to new pod creation failure.
func TestTlsCertificateExpiry(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	exipiry := 5 * time.Minute

	// Create the cluster
	validTo := time.Now().In(time.UTC).Add(exipiry)
	opts := &e2eutil.TLSOpts{
		ValidTo: &validTo,
	}

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, opts)

	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	// When the cluster is ready, check that TLS is valid, after the expiry period
	// expect the TLS to become invalid.
	e2eutil.MustCheckClusterTLS(t, targetKube, ctx)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSInvalidEvent(testCouchbase), exipiry+30*time.Second)

	// Check the events match what we expect:
	// * Cluster created
	// * Invalid TLS event
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy a couchbase cluster using a expired TLS certificate.
// Cluster creation should fail.
func TestTlsNegCertificateExpiredBeforeDeployment(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Set the cert creation date 10 years in the past
	validTo := time.Now().In(time.UTC)
	validFrom := validTo.AddDate(-10, 0, 0)
	opts := &e2eutil.TLSOpts{
		ValidFrom: &validFrom,
		ValidTo:   &validTo,
	}

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, opts)

	// Actual Test case function
	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, constants.Size3, ctx)
}

// Deploy the cluster using the certificate which is not yet valid.
// Cluster creation should not happen until the validity time crosses the current time.
func TestTlsCertificateDeployedBeforeValidity(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	validFrom := time.Now().In(time.UTC).Add(30 * time.Second)
	opts := &e2eutil.TLSOpts{
		ValidFrom: &validFrom,
	}

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, opts)

	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, constants.Size3, ctx)
}

// Create a couchbase cluster using the wrong CA certificate type.
// Cluster deployment should fail.
func TestTlsGenerateWrongCACertType(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	caCertType := e2eutil.CertTypeServer
	opts := &e2eutil.TLSOpts{
		CaCertType: &caCertType,
	}

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, opts)

	// Create cluster
	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, constants.Size3, ctx)
}

// Create a couchbase cluster using the wrong certificate type.
// Cluster deployment should fail.
func TestTlsGenerateWrongCertType(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterCertType := e2eutil.CertTypeClient
	opts := &e2eutil.TLSOpts{
		ClusterCertType: &clusterCertType,
	}

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, opts)

	e2eutil.MustNotNewTLSClusterBasic(t, targetKube, constants.Size3, ctx)
}

// TestTLSRotate tests a certificate can be reissued by a CA.
// * Ensures new certifcate is loaded from the inbox on update.
func TestTLSRotate(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, clusterSize, ctx)

	// When the cluster is ready, swap out the old certificate for a new one and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificate(t, ctx, []string{})
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

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
// * Ensures new certifcate chain is loaded from the inbox on update.
func TestTLSRotateChain(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, clusterSize, ctx)

	// When the cluster is ready, swap out the old certificate for a new chain and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateChain(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

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
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, clusterSize, ctx)

	// When the cluster is ready, swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	// * Client TLS updated (new CA)
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		// Race condition updating secrets.
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		},
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
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
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, clusterSize, ctx)

	// When the cluster is ready, swap out the all certificates for a new ones and verify,
	// then make sure the operator can scale the cluster (e.g. talk to it with the new CA)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)
	e2eutil.MustResizeCluster(t, 0, clusterSize+clusterScaleUpSize, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	// * Client TLS updated (new CA)
	// * Cluster successfully connects to, initializes and balances in new nodes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		// Race condition updating secrets.
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		},
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
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
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, clusterSize, ctx)

	// When the cluster is ready, restart the operator and swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, config.OperatorResourceName, time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes, framework.CreateDeploymentObject(kubernetes, f.OpImage, 0, f.PodCreateTimeout))
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSUpdatedEvent(cluster), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

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
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	e2eutil.MustNewBucket(t, kubernetes, e2espec.DefaultBucket)
	cluster := e2eutil.MustNewSupportableTLSCluster(t, kubernetes, mdsGroupSize, ctx)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, kill a stateful pod,  restart the operator and swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, config.OperatorResourceName, time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes, framework.CreateDeploymentObject(kubernetes, f.OpImage, 0, f.PodCreateTimeout))
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberDownEvent(cluster, victimIndex), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred for live nodes
	// * Down member is recovered
	// * TLS update exent occurred for recovered node
	// * Cluster recovered
	// * Prior to 6.5.0 the cluster rebalanced
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		eventschema.Optional{
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
				},
			},
		},
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
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, clusterSize, ctx)

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

// skipMutualTLSCheck doesn't run these tests unless using 5.5.4+ or
// 6.0.2+ due to a bug in NS server when using administrator certificates.
func skipMutualTLSCheck(t *testing.T) {
	f := framework.Global

	rawVersion, err := k8sutil.CouchbaseVersion(f.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}

	version, err := couchbaseutil.NewVersion(rawVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}

	switch version.Major() {
	case 5:
		minVersion, err := couchbaseutil.NewVersion("5.5.4")
		if err != nil {
			e2eutil.Die(t, err)
		}

		if version.Less(minVersion) {
			t.Skip("Test requires Couchbase 5.5.4 or greater")
		}
	case 6:
		minVersion, err := couchbaseutil.NewVersion("6.0.2")
		if err != nil {
			e2eutil.Die(t, err)
		}

		if version.Less(minVersion) {
			t.Skip("Test requires Couchbase 6.0.2 or greater")
		}
	}
}

// testMutualTLSCreateCluster ensures a cluster can be created with mTLS enabled.
func testMutualTLSCreateCluster(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	skipMutualTLSCheck(t)

	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewMutualTLSClusterBasic(t, kubernetes, clusterSize, ctx, policy)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSCreateCluster(t *testing.T) {
	testMutualTLSCreateCluster(t, couchbasev2.ClientCertificatePolicyEnable)
}

func TestMandatoryMutualTLSCreateCluster(t *testing.T) {
	testMutualTLSCreateCluster(t, couchbasev2.ClientCertificatePolicyMandatory)
}

// testMutualTLSEnable tests mTLS can be enabled on a TLS cluster.
func testMutualTLSEnable(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	skipMutualTLSCheck(t)

	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewTLSClusterBasic(t, kubernetes, clusterSize, ctx)

	// Enable mTLS and ensure the cluster still appears to work.
	patchset := jsonpatch.NewPatchSet().
		Add("/Spec/Networking/TLS/ClientCertificatePolicy", &policy).
		Add("/Spec/Networking/TLS/ClientCertificatePaths", []couchbasev2.ClientCertificatePath{
			{
				Path: "subject.cn",
			},
		})
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * Settings updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		e2eutil.ClusterScaleUpSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSEnable(t *testing.T) {
	testMutualTLSEnable(t, couchbasev2.ClientCertificatePolicyEnable)
}

func TestMandatoryMutualTLSEnable(t *testing.T) {
	testMutualTLSEnable(t, couchbasev2.ClientCertificatePolicyMandatory)
}

// testMutualTLSDisable tests mTLS can be disabled on a TLS cluster.
func testMutualTLSDisable(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	skipMutualTLSCheck(t)

	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewMutualTLSClusterBasic(t, kubernetes, clusterSize, ctx, policy)

	// Disable mTLS and ensure the cluster still works.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/Spec/Networking/TLS/ClientCertificatePolicy"), time.Minute)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * Settings updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		e2eutil.ClusterScaleUpSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSDisable(t *testing.T) {
	testMutualTLSDisable(t, couchbasev2.ClientCertificatePolicyEnable)
}

func TestMandatoryMutualTLSDisable(t *testing.T) {
	testMutualTLSDisable(t, couchbasev2.ClientCertificatePolicyMandatory)
}

// testMutualTLSRotateClient ensures we can rotate the operator client certificate.
func testMutualTLSRotateClient(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	skipMutualTLSCheck(t)

	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewMutualTLSClusterBasic(t, kubernetes, clusterSize, ctx, policy)

	// Rotate the certificate and ensure the cluster still works.
	e2eutil.MustRotateClientCertificate(t, ctx)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * Client TLS updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		e2eutil.ClusterScaleUpSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSRotateClient(t *testing.T) {
	testMutualTLSRotateClient(t, couchbasev2.ClientCertificatePolicyEnable)
}

func TestMandatoryMutualTLSRotateClient(t *testing.T) {
	testMutualTLSRotateClient(t, couchbasev2.ClientCertificatePolicyMandatory)
}

// testMutualTLSRotateClientChain ensure we can rotate operator client certificate and
// support certificate chains.
func testMutualTLSRotateClientChain(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	skipMutualTLSCheck(t)

	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewMutualTLSClusterBasic(t, kubernetes, clusterSize, ctx, policy)

	// Rotate the certificate and ensure the cluster still works.
	e2eutil.MustRotateClientCertificateChain(t, ctx)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * Client TLS updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		e2eutil.ClusterScaleUpSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSRotateClientChain(t *testing.T) {
	testMutualTLSRotateClientChain(t, couchbasev2.ClientCertificatePolicyEnable)
}

func TestMandatoryMutualTLSRotateClientChain(t *testing.T) {
	testMutualTLSRotateClientChain(t, couchbasev2.ClientCertificatePolicyMandatory)
}

// testMutualTLSRotateCA ensures we can rotate eveything.
func testMutualTLSRotateCA(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	skipMutualTLSCheck(t)

	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewMutualTLSClusterBasic(t, kubernetes, clusterSize, ctx, policy)

	// Rotate the certificate and ensure the cluster still works.
	e2eutil.MustRotateServerCertificateClientCertificateAndCA(t, ctx)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * Server TLS updated
	// * Client TLS updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		// Race condition updating secrets.
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		},
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		e2eutil.ClusterScaleUpSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSRotateCA(t *testing.T) {
	testMutualTLSRotateCA(t, couchbasev2.ClientCertificatePolicyEnable)
}

func TestMandatoryMutualTLSRotateCA(t *testing.T) {
	testMutualTLSRotateCA(t, couchbasev2.ClientCertificatePolicyMandatory)
}

// testMutualTLSRotateClientChain ensure we can rotate operator client certificate and
// support certificate chains.
func testMutualTLSRotateInvalid(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	skipMutualTLSCheck(t)

	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := e2eutil.MustNewMutualTLSClusterBasic(t, kubernetes, clusterSize, ctx, policy)

	// Rotate the certificate and ensure the cluster still works.
	e2eutil.MustRotateClientCertificateWrongCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSInvalidEvent(cluster), 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS reported as invalid
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSInvalid},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSRotateInvalid(t *testing.T) {
	testMutualTLSRotateInvalid(t, couchbasev2.ClientCertificatePolicyEnable)
}

func TestMandatoryMutualTLSRotateInvalid(t *testing.T) {
	testMutualTLSRotateInvalid(t, couchbasev2.ClientCertificatePolicyMandatory)
}

// skipN2NCheck doesn't run these tests unless using 6.5.1+.
func skipN2NCheck(t *testing.T) {
	f := framework.Global

	rawVersion, err := k8sutil.CouchbaseVersion(f.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}

	version, err := couchbaseutil.NewVersion(rawVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}

	minVersion, err := couchbaseutil.NewVersion("6.5.1")
	if err != nil {
		e2eutil.Die(t, err)
	}

	if version.Less(minVersion) {
		t.Skip("Test requires Couchbase 6.5.1 or greater")
	}
}

func getEncryptionLevel(encryptionType couchbasev2.NodeToNodeEncryptionType) string {
	if encryptionType == "ControlPlaneOnly" {
		return "control"
	}

	return "all"
}

// testCreateClusterWithTLSAndNodeToNode creates a cluster with N2N initially enabled.
func testCreateClusterWithTLSAndNodeToNode(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	skipN2NCheck(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	//get the encyptionLevel
	encryptionLevel := getEncryptionLevel(encryptionType)

	// Check the state is as we expect.
	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNode(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSAndFullNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNode(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterWithTLSAndNodeToNodeThenScale creates a cluster with N2N initially enabled
// and ensures scaling works.
func testCreateClusterWithTLSAndNodeToNodeThenScale(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	skipN2NCheck(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3
	scaleUp := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	//get the encyptionLevel
	encryptionLevel := getEncryptionLevel(encryptionType)

	// Check the state is as we expect, then scale, and repeat the check.
	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, 0, clusterSize+scaleUp, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scaled
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		e2eutil.ClusterScaleUpSequence(scaleUp),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenScale(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenScale(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSAndFullNodeToNodeThenScale(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenScale(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterWithTLSAndNodeToNodeThenKillPod creates a cluster with N2N initially enabled
// and kills a pod.
func testCreateClusterWithTLSAndNodeToNodeThenKillPod(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	skipN2NCheck(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	//get the encyptionLevel
	encryptionLevel := getEncryptionLevel(encryptionType)

	// Check the state is as we expect, kill a pod, let it recover and recheck N2N.
	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Pod down, failed and recovered
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		e2eutil.PodDownFailoverRecoverySequence(),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenKillPod(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenKillPod(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSAndFullNodeToNodeThenKillPod(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenKillPod(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterWithTLSThenEnableNodeToNode creates a cluster and enables N2N after
// provisioning.
func testCreateClusterWithTLSThenEnableNodeToNode(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	skipN2NCheck(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testCouchbase := e2eutil.MustNewTLSClusterBasic(t, targetKube, clusterSize, ctx)

	//get the encyptionLevel
	encryptionLevel := getEncryptionLevel(encryptionType)

	// Enable N2N encryption and check the state is as we expect.
	patchset := jsonpatch.NewPatchSet().Add("/Spec/Networking/TLS/NodeToNodeEncryption", &encryptionType)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, patchset, time.Minute)
	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * N2N enabled
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModified},
	}

	// Control plane only is the default, anything else will trigger a mode change.
	if encryptionType != couchbasev2.NodeToNodeControlPlaneOnly {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified})
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestCreateClusterWithTLSThenEnableControlPlaneNodeToNode(t *testing.T) {
	testCreateClusterWithTLSThenEnableNodeToNode(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSThenEnableFullNodeToNode(t *testing.T) {
	testCreateClusterWithTLSThenEnableNodeToNode(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterWithTLSAndNodeToNodeThenDisableNodeToNode tests disabling node to node
// encryption, unlikely though it is to be required.
func testCreateClusterWithTLSAndNodeToNodeThenDisableNodeToNode(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	skipN2NCheck(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	//get the encyptionLevel
	encryptionLevel := getEncryptionLevel(encryptionType)

	// Disable N2N then check state is as we expect.
	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)

	patchset := jsonpatch.NewPatchSet().Remove("/Spec/Networking/TLS/NodeToNodeEncryption")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, patchset, time.Minute)

	e2eutil.MustCheckN2NDisabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
	}

	// If the mode is "All" it needs changing before disabling
	if encryptionType != couchbasev2.NodeToNodeControlPlaneOnly {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified})
	}

	expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModified})

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestCreateClusterWithTLSAnControlPlanedNodeToNodeThenDisableNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenDisableNodeToNode(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSAndFullNodeToNodeThenDisableNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenDisableNodeToNode(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterWithTLSAndNodeToNodeThenChangeNodeToNodeMode tests modifying N2N mode
// settings.
func testCreateClusterWithTLSAndNodeToNodeThenChangeNodeToNodeMode(t *testing.T, encryptionType, newEncryptionType couchbasev2.NodeToNodeEncryptionType) {
	skipN2NCheck(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	//get the encyptionLevel
	encryptionLevel := getEncryptionLevel(encryptionType)

	// Check the state is as we expect.
	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)

	patchset := jsonpatch.NewPatchSet().Replace("/Spec/Networking/TLS/NodeToNodeEncryption", &newEncryptionType)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, patchset, time.Minute)

	// TODO: race, check cluster state.
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.SecuritySettingsUpdatedEvent(testCouchbase, k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenChangeToFullNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenChangeNodeToNodeMode(t, couchbasev2.NodeToNodeControlPlaneOnly, couchbasev2.NodeToNodeAll)
}

func TestCreateClusterWithTLSAndFullNodeToNodeThenChangeToControlPlaneNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenChangeNodeToNodeMode(t, couchbasev2.NodeToNodeAll, couchbasev2.NodeToNodeControlPlaneOnly)
}

func testCreateClusterWithTLSAndNodeToNodeThenRotateServerCertificate(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	skipN2NCheck(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	//get the encyptionLevel
	encryptionLevel := getEncryptionLevel(encryptionType)

	e2eutil.MustCheckN2NEnabled(t, targetKube, testCouchbase, encryptionLevel, time.Minute)
	e2eutil.MustRotateServerCertificate(t, ctx, []string{})
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.TLSUpdatedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, targetKube, ctx)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenRotateServerCertificate(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenRotateServerCertificate(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSAndFullNodeToNodeThenRotateServerCertificate(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenRotateServerCertificate(t, couchbasev2.NodeToNodeControlPlaneOnly)
}
