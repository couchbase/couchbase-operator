package e2e

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	pkgconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create couchbase cluster over TLS certificates
// Check TLS handshake is successful with all nodes.
func TestTLSCreateCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestPKCS12CreateCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	pkcs8 := e2eutil.KeyEncodingPKCS8

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{PKCS12: true, KeyEncoding: &pkcs8, PKCS12Passphrase: "password", Source: e2eutil.TLSSourceKubernetesSecret})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSCreateClusterWithShadowing tests deploying a cluster with standard names
// in the TLS secret (standard as in ingresses, cert-manager, everything not Couchbase etc.)
func TestTLSCreateClusterWithShadowing(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.  Use the same names as are standard on Kubernetes.
	keyEncoding := e2eutil.KeyEncodingPKCS8
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests scenario where a third node is being added to a cluster, and a separate
// node goes down immediately after the add & before the rebalance.
// Expects: autofailover of down node occurs and a replacement node is added
// Check TLS handshake is successful with all nodes.
func TestTLSKillClusterNode(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	scaledClusterSize := constants.Size3
	victimIndex := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, scaledClusterSize, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, scaledClusterSize-1), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create Couchbase cluster using certificates.
// Resize cluster to different sizes in loop.
// Check TLS handshake is successful with all cluster nodes.
func TestTLSResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	serviceID := 0

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is ready scale up to 3 nodes then down to 1 again.
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size3, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Remove Operator certificate after cluster deployment.
// Delete the operator secret and kill a node from cluster.  The cluster should
// raise an invalid TLS event.
// Add the operator certificate back and check new node addition is successful.
func TestTLSRemoveOperatorCertificateAndAddBack(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
	secret := e2eutil.MustGetSecret(t, kubernetes, ctx.OperatorSecretName)
	e2eutil.MustDeleteSecret(t, kubernetes, ctx.OperatorSecretName)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSInvalidEvent(cluster), 30*time.Second)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustRecreateSecret(t, kubernetes, secret)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRemoveOperatorCertificateAndResizeCluster removes the CA certificate
// expects the operator to raise an invalid TLS error and be able to provision
// new pods once the CA cert is restorred.
func TestTLSRemoveOperatorCertificateAndResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3
	scaledClusterSize := constants.Size5

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
	secret := e2eutil.MustGetSecret(t, kubernetes, ctx.OperatorSecretName)
	e2eutil.MustDeleteSecret(t, kubernetes, ctx.OperatorSecretName)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSInvalidEvent(cluster), 30*time.Second)
	e2eutil.MustRecreateSecret(t, kubernetes, secret)
	cluster = e2eutil.MustResizeCluster(t, 0, scaledClusterSize, kubernetes, cluster, 5*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Deploy cluster using valid TLS certificates.
// Remove the cluster certificate from the cluster and kill one of the cluster pod.
// The cluster should raise a TLS invalid event, then reconcile once the valid cluster certificate is available.
func TestTLSRemoveClusterCertificateAndAddBack(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
	secret := e2eutil.MustGetSecret(t, kubernetes, ctx.ClusterSecretName)
	e2eutil.MustDeleteSecret(t, kubernetes, ctx.ClusterSecretName)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSInvalidEvent(cluster), 30*time.Second)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustRecreateSecret(t, kubernetes, secret)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Deploy cluster using valid TLS certificates.
// Remove the cluster certificate from the cluster and scale up the cluster.
func TestTLSRemoveClusterCertificateAndResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3
	scaledClusterSize := constants.Size5

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is healthy, remove the TLS certificate, expect the operator to
	// raise an event to the effect that the TLS is invalid then restore the secret.
	// Scale the cluster to ensure the TLS is still working.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
	secret := e2eutil.MustGetSecret(t, kubernetes, ctx.ClusterSecretName)
	e2eutil.MustDeleteSecret(t, kubernetes, ctx.ClusterSecretName)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.TLSInvalidEvent(cluster), time.Minute)
	e2eutil.MustRecreateSecret(t, kubernetes, secret)
	cluster = e2eutil.MustResizeCluster(t, 0, scaledClusterSize, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Deploy cluster using invalid DNS name value in the certificate.
// Cluster creation should fail due to the invalid DNS value.
func TestTLSNegRSACertificateDnsName(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	opts := &e2eutil.TLSOpts{
		AltNames: []string{
			"*.test-couchbase-invalid-name." + kubernetes.Namespace + ".svc",
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)

	// Actual Test case function
	clusterOptions().WithEphemeralTopology(1).WithTLS(ctx).MustNotCreate(t, kubernetes)
}

// Deploy a couchbase cluster using a expired TLS certificate.
// Cluster creation should fail.
func TestTLSNegCertificateExpiredBeforeDeployment(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Set the cert creation date 10 years in the past
	validTo := time.Now().In(time.UTC)
	validFrom := validTo.AddDate(-10, 0, 0)
	opts := &e2eutil.TLSOpts{
		ValidFrom: &validFrom,
		ValidTo:   &validTo,
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)

	// Actual Test case function
	clusterOptions().WithEphemeralTopology(1).WithTLS(ctx).MustNotCreate(t, kubernetes)
}

// Deploy the cluster using the certificate which is not yet valid.
// Cluster creation should not happen until the validity time crosses the current time.
func TestTLSCertificateDeployedBeforeValidity(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	validFrom := time.Now().In(time.UTC).Add(30 * time.Second)
	opts := &e2eutil.TLSOpts{
		ValidFrom: &validFrom,
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)

	clusterOptions().WithEphemeralTopology(1).WithTLS(ctx).MustNotCreate(t, kubernetes)
}

// Create a couchbase cluster using the wrong CA certificate type.
// Cluster deployment should fail.
func TestTLSGenerateWrongCACertType(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	caCertType := e2eutil.CertTypeServer
	opts := &e2eutil.TLSOpts{
		CaCertType: &caCertType,
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)

	// Create cluster
	clusterOptions().WithEphemeralTopology(1).WithTLS(ctx).MustNotCreate(t, kubernetes)
}

// Create a couchbase cluster using the wrong certificate type.
// Cluster deployment should fail.
func TestTLSGenerateWrongCertType(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterCertType := e2eutil.CertTypeClient
	opts := &e2eutil.TLSOpts{
		ClusterCertType: &clusterCertType,
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)

	clusterOptions().WithEphemeralTopology(1).WithTLS(ctx).MustNotCreate(t, kubernetes)
}

// TestTLSRotate tests a certificate can be reissued by a CA.
// * Ensures new certifcate is loaded from the inbox on update.
func testTLSRotate(t *testing.T, opts *e2eutil.TLSOpts) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is ready, swap out the old certificate for a new one and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificate(t, ctx, opts)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestTLSRotate(t *testing.T) {
	testTLSRotate(t, &e2eutil.TLSOpts{})
}

func TestTLSRotateWithShadowing(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8

	testTLSRotate(t, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})
}

// TestTLSRotateChain tests a certificate can be reissued by a CA with a new sub-CA.
// * Ensures new certifcate chain is loaded from the inbox on update.
func TestTLSRotateChain(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is ready, swap out the old certificate for a new chain and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateChain(t, ctx)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateCA tests a certificate and CA can be reissued.
// * Ensures a new PKI is loaded from the inbox and the cluster CA updated.
func testTLSRotateCA(t *testing.T, opts *e2eutil.TLSOpts) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is ready, swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

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
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestTLSRotateCA(t *testing.T) {
	testTLSRotateCA(t, &e2eutil.TLSOpts{})
}

func TestTLSRotateCAWithShadowing(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8

	testTLSRotateCA(t, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})
}

// TestTLSRotateCAAndScale tests the operator can talk to a cluster after
// replacing the CA.
func TestTLSRotateCAAndScale(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3
	clusterScaleUpSize := 1

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is ready, swap out the all certificates for a new ones and verify,
	// then make sure the operator can scale the cluster (e.g. talk to it with the new CA)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
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
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
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

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// When the cluster is ready, restart the operator and swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateCAKillPodAndKillOperator tests a certificate and CA can be reissued while
// the operator is being restarted with a stateful pod down.
func TestTLSRotateCAKillPodAndKillOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimIndex := 0

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithMixedTopology(mdsGroupSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, kill a stateful pod,  restart the operator and swap out the all certificates for new ones and verify
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberDownEvent(cluster, victimIndex), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Down member is recovered
	// * Cluster recovered
	// * Prior to 6.5.0 the cluster rebalanced
	// * TLS update event occurred for all nodes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered, FuzzyMessage: victimName},
		eventschema.Optional{
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
				},
			},
		},
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestTLSRotateInvalid tests the operator raises a TLSInvalid event when a certificate
// doesn't validate with the supplied CA.
func TestTLSRotateInvalid(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

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

// testMutualTLSCreateCluster ensures a cluster can be created with mTLS enabled.
func testMutualTLSCreateCluster(t *testing.T, policy couchbasev2.ClientCertificatePolicy, opts *e2eutil.TLSOpts) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, &policy).MustCreate(t, kubernetes)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSCreateCluster(t *testing.T) {
	testMutualTLSCreateCluster(t, couchbasev2.ClientCertificatePolicyEnable, &e2eutil.TLSOpts{})
}

func TestMandatoryMutualTLSCreateCluster(t *testing.T) {
	testMutualTLSCreateCluster(t, couchbasev2.ClientCertificatePolicyMandatory, &e2eutil.TLSOpts{})
}

// TestMutualTLSCreateClusterWithShadowing tests deploying a cluster with standard names
// in the TLS secret, using mutual TLS.
func TestMutualTLSCreateClusterWithShadowing(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8

	testMutualTLSCreateCluster(t, couchbasev2.ClientCertificatePolicyEnable, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})
}

// testMutualTLSEnable tests mTLS can be enabled on a TLS cluster.
func testMutualTLSEnable(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster with a valid 1 deep certificate chain.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// Enable mTLS and ensure the cluster still appears to work.
	patchset := jsonpatch.NewPatchSet().
		Add("/spec/networking/tls/clientCertificatePolicy", policy).
		Add("/spec/networking/tls/clientCertificatePaths", []couchbasev2.ClientCertificatePath{
			{
				Path: "subject.cn",
			},
		})

	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSUpdatedEvent(cluster, k8sutil.ClientTLSUpdateReasonCreateClientAuth), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Settings updated
	// * Cluster upgrades (due to readiness becoming non-tls port)
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonCreateClientAuth)},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
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
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, &policy).MustCreate(t, kubernetes)

	// Disable mTLS and ensure the cluster still works.
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/networking/tls/clientCertificatePolicy"), time.Minute)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSUpdatedEvent(cluster, k8sutil.ClientTLSUpdateReasonDeleteClientAuth), 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Settings updated
	// * Cluster upgrades (due to readiness becoming non-tls port)
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonDeleteClientAuth)},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
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
func testMutualTLSRotateClient(t *testing.T, policy couchbasev2.ClientCertificatePolicy, opts *e2eutil.TLSOpts) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, &policy).MustCreate(t, kubernetes)

	// Rotate the certificate and ensure the cluster still works.
	e2eutil.MustRotateClientCertificate(t, ctx)
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSUpdatedEvent(cluster, k8sutil.ClientTLSUpdateReasonUpdateClientAuth), time.Minute)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Client TLS updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateClientAuth)},
		e2eutil.ClusterScaleUpSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSRotateClient(t *testing.T) {
	testMutualTLSRotateClient(t, couchbasev2.ClientCertificatePolicyEnable, &e2eutil.TLSOpts{})
}

func TestMandatoryMutualTLSRotateClient(t *testing.T) {
	testMutualTLSRotateClient(t, couchbasev2.ClientCertificatePolicyMandatory, &e2eutil.TLSOpts{})
}

func TestMutualTLSRotateClientWithShadowing(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8

	testMutualTLSRotateClient(t, couchbasev2.ClientCertificatePolicyEnable, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})
}

// testMutualTLSRotateClientChain ensure we can rotate operator client certificate and
// support certificate chains.
func testMutualTLSRotateClientChain(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, &policy).MustCreate(t, kubernetes)

	// Rotate the certificate and ensure the cluster still works.
	e2eutil.MustRotateClientCertificateChain(t, ctx)
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSUpdatedEvent(cluster, k8sutil.ClientTLSUpdateReasonUpdateClientAuth), time.Minute)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Client TLS updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateClientAuth)},
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
func testMutualTLSRotateCA(t *testing.T, policy couchbasev2.ClientCertificatePolicy, opts *e2eutil.TLSOpts) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, &policy).MustCreate(t, kubernetes)

	// Rotate the certificate and ensure the cluster still works.
	e2eutil.MustRotateServerCertificateClientCertificateAndCA(t, ctx)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSUpdatedEvent(cluster, k8sutil.ClientTLSUpdateReasonUpdateCA), 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

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
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateClientAuth)},
		e2eutil.ClusterScaleUpSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMutualTLSRotateCA(t *testing.T) {
	testMutualTLSRotateCA(t, couchbasev2.ClientCertificatePolicyEnable, &e2eutil.TLSOpts{})
}

func TestMandatoryMutualTLSRotateCA(t *testing.T) {
	testMutualTLSRotateCA(t, couchbasev2.ClientCertificatePolicyMandatory, &e2eutil.TLSOpts{})
}

func TestMutualTLSRotateCAWithShadowing(t *testing.T) {
	keyEncoding := e2eutil.KeyEncodingPKCS8

	testMutualTLSRotateCA(t, couchbasev2.ClientCertificatePolicyEnable, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret, KeyEncoding: &keyEncoding})
}

// testMutualTLSRotateClientChain ensure we can rotate operator client certificate and
// support certificate chains.
func testMutualTLSRotateInvalid(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, &policy).MustCreate(t, kubernetes)

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

// testCreateClusterWithTLSAndNodeToNode creates a cluster with N2N initially enabled.
func testCreateClusterWithTLSAndNodeToNode(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = ctx.ClusterName
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check the state is as we expect.
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNode(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSAndFullNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNode(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterWithTLSAndNodeToNodeAndRotateCA creates a cluster with N2N initially enabled,
// the CA is rotated, which is a pain as N2N is required to be off before the CA can be reloaded.
func testCreateClusterWithTLSAndNodeToNodeAndRotateCA(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = ctx.ClusterName
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check the state is as we expect.
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, ctx)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Node to node dactivated
	// * TLS rotated
	// * Node to node reactivated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified},
		},
		eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModified},
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
		eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModified},
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNodeAndRotateCA(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeAndRotateCA(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSAndFullNodeToNodeAndRotateCA(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeAndRotateCA(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterWithTLSAndNodeToNodeThenScale creates a cluster with N2N initially enabled
// and ensures scaling works.
func testCreateClusterWithTLSAndNodeToNodeThenScale(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := 3
	scaleUp := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = ctx.ClusterName
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check the state is as we expect, then scale, and repeat the check.
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+scaleUp, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scaled
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		e2eutil.ClusterScaleUpSequence(scaleUp),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 1

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = ctx.ClusterName
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check the state is as we expect, kill a pod, let it recover and recheck N2N.
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Pod down, failed and recovered
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		e2eutil.PodDownFailoverRecoverySequence(),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// Enable N2N encryption and check the state is as we expect.
	patchset := jsonpatch.NewPatchSet().Add("/spec/networking/tls/nodeToNodeEncryption", encryptionType)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCreateClusterWithTLSThenEnableControlPlaneNodeToNode(t *testing.T) {
	testCreateClusterWithTLSThenEnableNodeToNode(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSThenEnableFullNodeToNode(t *testing.T) {
	testCreateClusterWithTLSThenEnableNodeToNode(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterThenEnableNodeToNode tests enabling N2N at the same time as TLS which
// causes all kinds of ordering misery.
func testCreateClusterThenEnableNodeToNode(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster without TLS.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When ready create the required TLS secrets and patch them into the running
	// cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{ClusterName: cluster.Name})
	tls := &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}

	// Enable N2N encryption and check the state is as we expect.
	patchset := jsonpatch.NewPatchSet().Add("/spec/networking/tls", tls)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Client updated
	// * TLS enabled (cluster upgraded)
	// * N2N enabled
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModified},
	}

	// Control plane only is the default, anything else will trigger a mode change.
	if encryptionType != couchbasev2.NodeToNodeControlPlaneOnly {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified})
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCreateClusterThenEnableControlPlaneNodeToNode(t *testing.T) {
	testCreateClusterThenEnableNodeToNode(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterThenEnableFullNodeToNode(t *testing.T) {
	testCreateClusterThenEnableNodeToNode(t, couchbasev2.NodeToNodeAll)
}

// testCreateClusterWithTLSAndNodeToNodeThenDisableNodeToNode tests disabling node to node
// encryption, unlikely though it is to be required.
func testCreateClusterWithTLSAndNodeToNodeThenDisableNodeToNode(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = ctx.ClusterName
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Disable N2N then check state is as we expect.
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)

	patchset := jsonpatch.NewPatchSet().Remove("/spec/networking/tls/nodeToNodeEncryption")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)

	e2eutil.MustCheckN2NDisabled(t, kubernetes, cluster, encryptionType, time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = ctx.ClusterName
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check the state is as we expect.
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)

	patchset := jsonpatch.NewPatchSet().Replace("/spec/networking/tls/nodeToNodeEncryption", &newEncryptionType)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdatedN2NEncryptionModeModified},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenChangeToFullNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenChangeNodeToNodeMode(t, couchbasev2.NodeToNodeControlPlaneOnly, couchbasev2.NodeToNodeAll)
}

func TestCreateClusterWithTLSAndFullNodeToNodeThenChangeToControlPlaneNodeToNode(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenChangeNodeToNodeMode(t, couchbasev2.NodeToNodeAll, couchbasev2.NodeToNodeControlPlaneOnly)
}

func testCreateClusterWithTLSAndNodeToNodeThenRotateServerCertificate(t *testing.T, encryptionType couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipN2NCheck(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = ctx.ClusterName
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: &encryptionType,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, encryptionType, time.Minute)
	e2eutil.MustRotateServerCertificate(t, ctx, &e2eutil.TLSOpts{})
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * N2N disabled
	// * TLS update event occurred
	// * N2N enabled
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithN2N(clusterSize, encryptionType),
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenRotateServerCertificate(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenRotateServerCertificate(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

func TestCreateClusterWithTLSAndFullNodeToNodeThenRotateServerCertificate(t *testing.T) {
	testCreateClusterWithTLSAndNodeToNodeThenRotateServerCertificate(t, couchbasev2.NodeToNodeControlPlaneOnly)
}

// TestTLSEditSettings checks that doing so works.
func TestTLSEditSettings(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	op1 := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, k8sutil.SecuritySettingsUpdatedEvent(cluster, k8sutil.SecuritySettingUpdated), time.Minute)
	defer op1.Cancel()

	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	tlsUpdateVersion := couchbasev2.TLS10

	if tls10Deprecated, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if tls10Deprecated {
		tlsUpdateVersion = couchbasev2.TLS13
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/tls/tlsMinimumVersion", tlsUpdateVersion), time.Minute)
	e2eutil.MustReceiveErrorValue(t, op1)

	op2 := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, k8sutil.SecuritySettingsUpdatedEvent(cluster, k8sutil.SecuritySettingUpdated), time.Minute)
	defer op2.Cancel()

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/disableUIOverHTTP", true), time.Minute)
	e2eutil.MustReceiveErrorValue(t, op2)

	op3 := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, k8sutil.SecuritySettingsUpdatedEvent(cluster, k8sutil.SecuritySettingUpdated), time.Minute)
	defer op3.Cancel()

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/disableUIOverHTTPS", true), time.Minute)
	e2eutil.MustReceiveErrorValue(t, op3)

	op4 := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, k8sutil.SecuritySettingsUpdatedEvent(cluster, k8sutil.SecuritySettingUpdated), time.Minute)
	defer op4.Cancel()

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/security/uiSessionTimeout", 2), time.Minute)
	e2eutil.MustReceiveErrorValue(t, op4)

	op5 := e2eutil.WaitForPendingClusterEvent(kubernetes, cluster, k8sutil.SecuritySettingsUpdatedEvent(cluster, k8sutil.SecuritySettingUpdated), time.Minute)
	defer op5.Cancel()

	cipherSuites := []string{
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
	}
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/tls/cipherSuites", cipherSuites), time.Minute)
	e2eutil.MustReceiveErrorValue(t, op5)

	// Check the events match what we expect:
	// * Cluster created
	// * Settings edited
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times:     5,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated, FuzzyMessage: k8sutil.SecuritySettingUpdated},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// testMandatoryMutualTLSWithMultipleCAs tests that a cluster using server secrets from
// various different sources, works with client certs from a different PKI.
func testMandatoryMutualTLSWithMultipleCAs(t *testing.T, serverTLSSourceType e2eutil.TLSSource, multipleCAs bool) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size1
	policy := couchbasev2.ClientCertificatePolicyMandatory

	serverTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: serverTLSSourceType, MultipleCAs: multipleCAs})
	clientTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(serverTLS, &policy).WithClientTLS(clientTLS).MustCreate(t, kubernetes)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMandatoryMutualTLSWithMultipleCAsAndKubernetesSecrets tests that a cluster using Kubernetes
// server secrets (CA supplied separately), works with client certs from a different PKI.
func TestMandatoryMutualTLSWithMultipleCAsAndKubernetesSecrets(t *testing.T) {
	testMandatoryMutualTLSWithMultipleCAs(t, e2eutil.TLSSourceKubernetesSecret, false)
}

// TestMandatoryMutualTLSWithSingleSecretMultipleCAsAndKubernetesSecrets tests that a cluster using Kubernetes
// server secrets (CA supplied separately), works with client certs from a different PKI.
func TestMandatoryMutualTLSWithSingleSecretMultipleCAsAndKubernetesSecrets(t *testing.T) {
	testMandatoryMutualTLSWithMultipleCAs(t, e2eutil.TLSSourceKubernetesSecret, true)
}

// TestMandatoryMutualTLSWithMultipleCAsAndCertManagerSecrets tests that a cluster using cert-manager
// server secrets (CA integrated), works with client certs from a different PKI.
func TestMandatoryMutualTLSWithMultipleCAsAndCertManagerSecrets(t *testing.T) {
	testMandatoryMutualTLSWithMultipleCAs(t, e2eutil.TLSSourceCertManagerSecret, false)
}

// TestMandatoryMutualTLSWithSingleSecretMultipleCAsAndCertManagerSecrets tests that a cluster using cert-manager
// server secrets (CA integrated), works with client certs from a different PKI.
func TestMandatoryMutualTLSWithSingleSecretMultipleCAsAndCertManagerSecrets(t *testing.T) {
	testMandatoryMutualTLSWithMultipleCAs(t, e2eutil.TLSSourceCertManagerSecret, true)
}

func TestTLSWithMultipleServerCerts(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Generate an initial TLS context
	opts := e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret}
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &opts)

	// Retrieve the server secret created by the context
	secret := e2eutil.MustGetSecret(t, kubernetes, ctx.ClusterSecretName)

	// Generate a random CA that was not used to sign the server sert
	caCN := "amityville inc"
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	extraCA, err := e2eutil.NewCertificateAuthority(e2eutil.KeyTypeRSA, caCN, validFrom, validTo, e2eutil.CertTypeCA)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Combine the random CA to valid CA as the first PEM
	secret.Data[pkgconstants.CertManagerCAKey] = append(extraCA.Certificate, ctx.CA.Certificate...)

	// Patch the Secret with updated CA
	e2eutil.MustUpdateSecret(t, kubernetes, secret)

	// Create Cluster
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMultipleCAsAddAndRemove tests that addition and removal of CAs works, and is
// independent of other operations.
func TestMultipleCAsAddAndRemove(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size1

	// Create a cluster.
	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceCertManagerSecret})
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(tls).MustCreate(t, kubernetes)

	// Check only the expected use cert is present.
	e2eutil.MustValidateCAPool(t, kubernetes, cluster, time.Minute, tls)

	// Add a new CA to the cluster and expect it to appear,
	// take it away and expect it to disappear
	tls2 := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})

	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/tls/rootCAs", []string{tls2.CASecretName}), time.Minute)
	e2eutil.MustValidateCAPool(t, kubernetes, cluster, 2*time.Minute, tls, tls2)

	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/networking/tls/rootCAs"), time.Minute)
	e2eutil.MustValidateCAPool(t, kubernetes, cluster, 2*time.Minute, tls)
}

// TestMandatoryMutualTLSWithMultipleCAsAndRotateServerPKI checks that the server PKI can be
// rotated independently from the client PKI.
func TestMandatoryMutualTLSWithMultipleCAsAndRotateServerPKI(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size1
	policy := couchbasev2.ClientCertificatePolicyMandatory

	// Create the cluster with multiple CAs for server and users.
	serverTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})
	clientTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(serverTLS, &policy).WithClientTLS(clientTLS).MustCreate(t, kubernetes)

	// Rotate the server PKI and validate all is as expected.
	e2eutil.MustRotateServerCertificateAndCA(t, serverTLS)
	e2eutil.MustValidateCAPool(t, kubernetes, cluster, 2*time.Minute, serverTLS, clientTLS)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, serverTLS, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Server certs and client CA updated.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		},
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMandatoryMutualTLSWithMultipleCAsAndRotateServerPKIWithOperatorDown checks that the server
// PKI can be rotated independently from the client PKI, when the operator isn't running for
// whatever reason.
func TestMandatoryMutualTLSWithMultipleCAsAndRotateServerPKIWithOperatorDown(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size1
	policy := couchbasev2.ClientCertificatePolicyMandatory

	// Create the cluster with multiple CAs for server and users.
	serverTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})
	clientTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(serverTLS, &policy).WithClientTLS(clientTLS).MustCreate(t, kubernetes)

	// Rotate the server PKI with the operator off and validate all is as expected.
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)
	e2eutil.MustRotateServerCertificateAndCA(t, serverTLS)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes)
	e2eutil.MustValidateCAPool(t, kubernetes, cluster, 2*time.Minute, serverTLS, clientTLS)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, serverTLS, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Server certs and client CA updated.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		},
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMandatoryMutualTLSWithMultipleCAsAndRotateClientPKI checks that the client PKI can be
// rotated independently from the server PKI.
func TestMandatoryMutualTLSWithMultipleCAsAndRotateClientPKI(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size1
	policy := couchbasev2.ClientCertificatePolicyMandatory

	// Create the cluster with multiple CAs for server and users.
	serverTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})
	clientTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(serverTLS, &policy).WithClientTLS(clientTLS).MustCreate(t, kubernetes)

	// Rotate the client PKI and validate all is as expected.
	e2eutil.MustRotateServerCertificateClientCertificateAndCA(t, clientTLS)
	e2eutil.MustValidateCAPool(t, kubernetes, cluster, 2*time.Minute, serverTLS, clientTLS)

	// Check the events match what we expect:
	// * Cluster created
	// * Client cert, key and CA updated.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateClientAuth)},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMandatoryMutualTLSWithMultipleCAsAndRotateClientPKIWithOperatorDown checks that the
// client PKI can be rotated independently from the server PKI, when the operator isn't running for
// whatever reason.
func TestMandatoryMutualTLSWithMultipleCAsAndRotateClientPKIWithOperatorDown(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size1
	policy := couchbasev2.ClientCertificatePolicyMandatory

	// Create the cluster with multiple CAs for server and users.
	serverTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})
	clientTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(serverTLS, &policy).WithClientTLS(clientTLS).MustCreate(t, kubernetes)

	// Rotate the client PKI and validate all is as expected.
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)
	e2eutil.MustRotateServerCertificateClientCertificateAndCA(t, clientTLS)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes)
	e2eutil.MustValidateCAPool(t, kubernetes, cluster, 2*time.Minute, serverTLS, clientTLS)

	// Check the events match what we expect:
	// * Cluster created
	// * Client cert, key and CA updated.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateClientAuth)},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create couchbase cluster over with encrypted TLS using
// the script method to register associated passphrase.
func TestTLSScriptPassphrase(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()
	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size1
	secretName := "tls-passphrase"
	passphrase := "password"

	// Create the passphrase secret
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		Data: map[string][]byte{
			pkgconstants.PassphraseSecretKey: []byte(passphrase),
		},
	}
	e2eutil.MustCreateSecret(t, kubernetes, secret)

	// Setting TLS Options to generate encrypted key
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := e2eutil.TLSOpts{
		KeyPassphrase: passphrase,
		KeyEncoding:   &keyEncoding,
		Source:        e2eutil.TLSSourceCertManagerSecret,
	}

	// Add Script config settings to cluster options
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).Generate(kubernetes)
	cluster.Spec.Networking.TLS.PassphraseConfig.Script = &couchbasev2.PassphraseScriptConfig{
		Secret: secretName,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Test rotation of encrypted server key using the script passphrase.
func TestTLSRotateScriptPassphrase(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()
	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size3
	secretName := "tls-passphrase"
	passphrase := "password"

	// Create the passphrase secret
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		Data: map[string][]byte{
			pkgconstants.PassphraseSecretKey: []byte(passphrase),
		},
	}
	e2eutil.MustCreateSecret(t, kubernetes, secret)

	// Setting TLS Options to generate encrypted key
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := e2eutil.TLSOpts{
		KeyPassphrase: passphrase,
		KeyEncoding:   &keyEncoding,
		Source:        e2eutil.TLSSourceCertManagerSecret,
	}

	// Add Script config settings to cluster options
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).Generate(kubernetes)
	cluster.Spec.Networking.TLS.PassphraseConfig.Script = &couchbasev2.PassphraseScriptConfig{
		Secret: secretName,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Rotate the server certificates.
	// Can take a while because Operator also rotates internal pub/priv key
	// used to encrypt the passphrase.
	e2eutil.MustRotateServerCertificate(t, ctx, &opts)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Test that server key can be rotated with a different passphrase.
func TestTLSRotateAndChangeScriptPassphrase(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()
	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := constants.Size3
	secretName := "tls-passphrase"
	passphrase := "password"
	passphraseNew := "passwordnew"

	// Create the passphrase secret
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		Data: map[string][]byte{
			pkgconstants.PassphraseSecretKey: []byte(passphrase),
		},
	}
	e2eutil.MustCreateSecret(t, kubernetes, secret)

	// Setting TLS Options to generate encrypted key
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := e2eutil.TLSOpts{
		KeyPassphrase: passphrase,
		KeyEncoding:   &keyEncoding,
		Source:        e2eutil.TLSSourceCertManagerSecret,
	}

	// Add Script config settings to cluster options
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).Generate(kubernetes)
	cluster.Spec.Networking.TLS.PassphraseConfig.Script = &couchbasev2.PassphraseScriptConfig{
		Secret: secretName,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Change the passphrase secret
	secret.Data[pkgconstants.PassphraseSecretKey] = []byte(passphraseNew)
	e2eutil.MustUpdateSecret(t, kubernetes, secret)

	// Rotate the server certificates with new passphrase
	opts.KeyPassphrase = passphraseNew
	e2eutil.MustRotateServerCertificate(t, ctx, &opts)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Start web server on requested address and send specified response.
// Address should include random port as web servers can run in parallel.
func startWebServ(address string, response string) *http.Server {
	// webRoot is a handler func that sends respons
	webRoot := func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, response)
	}

	// initialize web server with listening address
	m := http.NewServeMux()
	s := http.Server{Addr: address, Handler: m}

	// register webroot handler to "/" path
	m.HandleFunc("/", webRoot)

	// start webserver in background
	go func() {
		_ = s.ListenAndServe()
	}()

	return &s
}

// Test creation of server with encrypted key and passphrase registered as rest endpoint.
func TestTLSRestPassphrase(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()
	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	//  Single node cluster
	clusterSize := constants.Size1

	// Start web server with passphrase as response
	passphrase := "webpass"
	address := e2eutil.GetHostAddressWithPort(t, 1000, 6000)
	s := startWebServ(address, passphrase)

	defer func() {
		_ = s.Shutdown(context.Background())
	}()

	// Setting TLS Options to generate encrypted key
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := e2eutil.TLSOpts{
		KeyPassphrase: passphrase,
		KeyEncoding:   &keyEncoding,
		Source:        e2eutil.TLSSourceCertManagerSecret,
	}

	// Add Script config settings to cluster options
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).Generate(kubernetes)
	cluster.Spec.Networking.TLS.PassphraseConfig.Rest = &couchbasev2.PassphraseRestConfig{
		URL: "http://" + address,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Test creation of server with encrypted key and passphrase registered as
// rest endpoint  followed by cert rotation relying on script passphrase.
func TestTLSRotateRestToScriptPassphrase(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()
	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	//  3 node cluster with key encryption
	clusterSize := constants.Size3
	passphrase := "webpass"
	passphraseNew := "scriptpass"
	secretName := "tls-passphrase"

	// Start web server with passphrase as response
	address := e2eutil.GetHostAddressWithPort(t, 1000, 6000)
	s := startWebServ(address, passphrase)

	defer func() {
		_ = s.Shutdown(context.Background())
	}()

	// Setting TLS Options to generate encrypted key
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := e2eutil.TLSOpts{
		KeyPassphrase: passphrase,
		KeyEncoding:   &keyEncoding,
		Source:        e2eutil.TLSSourceCertManagerSecret,
	}

	// Add Script config settings to cluster options
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).Generate(kubernetes)
	cluster.Spec.Networking.TLS.PassphraseConfig.Rest = &couchbasev2.PassphraseRestConfig{
		URL: "http://" + address,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Create the passphrase secret
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		Data: map[string][]byte{
			pkgconstants.PassphraseSecretKey: []byte(passphraseNew),
		},
	}
	e2eutil.MustCreateSecret(t, kubernetes, secret)

	// Rotate the server certificates with new passphrase
	opts.KeyPassphrase = passphraseNew
	e2eutil.MustRotateServerCertificate(t, ctx, &opts)

	// Change the passphrase config from rest to script
	patchset := jsonpatch.NewPatchSet().
		Remove("/spec/networking/tls/passphrase/rest").
		Add("/spec/networking/tls/passphrase/script", &couchbasev2.PassphraseScriptConfig{
			Secret: secretName,
		})

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)

	// Config change should result in cluster upgrade
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests that user can recover connectivity to server after client certificates have expired.
func TestMandatoryMutualTLSRotateClientExpiring(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Set the client certs to expire some time in the future
	validFrom := time.Now().In(time.UTC)
	validTo := time.Now().Add(3 * time.Minute)
	opts := &e2eutil.TLSOpts{
		ClientValidFrom: &validFrom,
		ClientValidTo:   &validTo,
	}

	// Create tls cluster with mandatory client auth
	clusterSize := constants.Size3
	policy := couchbasev2.ClientCertificatePolicyMandatory
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, &policy).MustCreate(t, kubernetes)

	// waiting additional time to ensure that the clients have expired
	expiryTime := validTo.Add(15 * time.Second)
	time.Sleep(time.Until(expiryTime))

	// At this point the client is locked out but we should be able to rotate certs and proceed
	e2eutil.MustRotateClientCertificate(t, ctx)
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSUpdatedEvent(cluster, k8sutil.ClientTLSUpdateReasonUpdateClientAuth), 3*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Client TLS updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSInvalid, Message: string(k8sutil.EventReasonTLSInvalidMessage)},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateClientAuth)},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMandatoryMutualTLSRotateCAExpiring tests scenario where the root CA has expired
// and we have to rotate the full TLS stack.
func TestMandatoryMutualTLSRotateCAExpiring(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Set the client certs to expire some time in the future
	validFrom := time.Now().In(time.UTC)
	validTo := time.Now().Add(3 * time.Minute)
	opts := &e2eutil.TLSOpts{
		ValidFrom: &validFrom,
		ValidTo:   &validTo,
	}

	// Create tls cluster with mandatory client auth
	clusterSize := constants.Size3
	policy := couchbasev2.ClientCertificatePolicyMandatory
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(ctx, &policy).MustCreate(t, kubernetes)

	// patch to allow rotation of expired server certificates
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/tls/allowPlainTextCertReload", true), time.Minute)

	// waiting additional time to ensure that the clients have expired
	expiryTime := validTo.Add(15 * time.Second)
	time.Sleep(time.Until(expiryTime))

	// At this point the client is locked out but we should be able to rotate certs and proceed
	e2eutil.MustRotateServerCertificateClientCertificateAndCA(t, ctx)

	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.ClientTLSUpdatedEvent(cluster, k8sutil.ClientTLSUpdateReasonUpdateClientAuth), 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Invalid TLS is raised for both root CA and server cert
	// * Invalid Client TLS is raised for expired client cert
	// * Client TLS updated
	// * Cluster resized successfully
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid, Message: string(k8sutil.EventReasonTLSInvalidMessage)},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSInvalid, Message: string(k8sutil.EventReasonTLSInvalidMessage)},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateClientAuth)},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestTLSRestCreateRestRotate(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()
	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	//  Single node cluster
	clusterSize := constants.Size1

	// Start web server with passphrase as response
	passphrase := "webpass"
	address := e2eutil.GetHostAddressWithPort(t, 1000, 6000)
	s := startWebServ(address, passphrase)

	defer func() {
		_ = s.Shutdown(context.Background())
	}()

	// Setting TLS Options to generate encrypted key
	keyEncoding := e2eutil.KeyEncodingPKCS8
	opts := e2eutil.TLSOpts{
		KeyPassphrase: passphrase,
		KeyEncoding:   &keyEncoding,
		Source:        e2eutil.TLSSourceCertManagerSecret,
	}

	// Add Script config settings to cluster options
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).Generate(kubernetes)
	cluster.Spec.Networking.TLS.PassphraseConfig.Rest = &couchbasev2.PassphraseRestConfig{
		URL: "http://" + address,
	}
	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Rotate the server certificates.
	// Can take a while because Operator also rotates internal pub/priv key
	// used to encrypt the passphrase.
	e2eutil.MustRotateServerCertificate(t, ctx, &opts)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * TLS update event occurred
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times:     clusterSize,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestTLSScriptCreateRestRotate(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()
	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	//  Single node cluster
	clusterSize := constants.Size1

	// Start web server with passphrase as response
	passphrase := "webpass"
	secretName := "tls-passphrase"
	passphraseNew := "restpass"
	address := e2eutil.GetHostAddressWithPort(t, 1000, 6000)
	s := startWebServ(address, passphraseNew)

	defer func() {
		_ = s.Shutdown(context.Background())
	}()

	// Setting TLS Options to generate encrypted key
	keyEncoding := e2eutil.KeyEncodingPKCS8
	// Create the passphrase secret
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		Data: map[string][]byte{
			pkgconstants.PassphraseSecretKey: []byte(passphrase),
		},
	}
	e2eutil.MustCreateSecret(t, kubernetes, secret)

	opts := e2eutil.TLSOpts{
		KeyPassphrase: passphrase,
		KeyEncoding:   &keyEncoding,
		Source:        e2eutil.TLSSourceCertManagerSecret,
	}

	// Add Script config settings to cluster options
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &opts)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).Generate(kubernetes)
	cluster.Spec.Networking.TLS.PassphraseConfig.Script = &couchbasev2.PassphraseScriptConfig{
		Secret: secretName,
	}

	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Rotate the server certificates with new passphrase
	opts.KeyPassphrase = passphraseNew
	e2eutil.MustRotateServerCertificate(t, ctx, &opts)

	// Change the passphrase config from script to rest
	patchset := jsonpatch.NewPatchSet().
		Remove("/spec/networking/tls/passphrase/script").
		Add("/spec/networking/tls/passphrase/rest", &couchbasev2.PassphraseRestConfig{
			URL: "http://" + address,
		})

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
}
