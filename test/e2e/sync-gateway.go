package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
)

// testSyncGatewayCreate is a generic creation and connectivity test.
func testSyncGatewayCreate(t *testing.T, kubernetes1, kubernetes2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster in the target cluster.
	e2eutil.MustNewBucket(t, kubernetes2, kubernetes2.Namespace, e2espec.DefaultBucket)
	cluster := e2eutil.MustNewXDCRCluster(t, kubernetes2, clusterSize, nil, tls, policy)
	e2eutil.MustWaitUntilBucketsExists(t, kubernetes2, cluster, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Create the sync gateway in the source cluster and insert a document.
	e2eutil.MustCreateSyncGateway(t, kubernetes1, cluster, framework.Global.SyncGatewayImage, e2espec.DefaultBucket.Name, dns, tls, time.Minute)

	// Ensure meta-data documents appear in the Couchbase cluster.
	e2eutil.MustVerifyDocCountInBucketNonZero(t, kubernetes2, cluster, e2espec.DefaultBucket.Name, time.Minute)
}

// TestSyncGatewayCreateLocal tests connectivity within the same Kubernetes cluster.
func TestSyncGatewayCreateLocal(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)

	testSyncGatewayCreate(t, k8s1, k8s1, nil, nil, nil)
}

// TestSyncGatewayCreateLocalTLS tests TLS connectivity within the same Kubernetes cluster.
func TestSyncGatewayCreateLocalTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s1, k8s1.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	testSyncGatewayCreate(t, k8s1, k8s1, nil, tls, nil)
}

// TestSyncGatewayCreateLocalMutualTLS tests mTLS connectivity within the same Kubernetes cluster.
func TestSyncGatewayCreateLocalMutualTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s1, k8s1.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyEnable
	testSyncGatewayCreate(t, k8s1, k8s1, nil, tls, &policy)
}

// TestSyncGatewayCreateLocalMandatoryMutualTLS tests mandatory mTLS connectivity within the same Kubernetes cluster.
func TestSyncGatewayCreateLocalMandatoryMutualTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s1, k8s1.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testSyncGatewayCreate(t, k8s1, k8s1, nil, tls, &policy)
}

// TestSyncGatewayCreateRemote tests connectivity to a remote Kubernetes cluster.
func TestSyncGatewayCreateRemote(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)
	k8s2 := framework.Global.GetCluster(1)

	dns, cleanup := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)
	defer cleanup()

	testSyncGatewayCreate(t, k8s1, k8s2, dns, nil, nil)
}

// TestSyncGatewayCreateRemoteTLS tests TLS connectivity to a remote Kubernetes cluster.
func TestSyncGatewayCreateRemoteTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)
	k8s2 := framework.Global.GetCluster(1)

	dns, cleanup := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)
	defer cleanup()

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s2, k8s2.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	testSyncGatewayCreate(t, k8s1, k8s2, dns, tls, nil)
}

// TestSyncGatewayCreateRemoteMutualTLS tests mTLS connectivity to a remote Kubernetes cluster.
func TestSyncGatewayCreateRemoteMutualTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)
	k8s2 := framework.Global.GetCluster(1)

	dns, cleanup := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)
	defer cleanup()

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s2, k8s2.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyEnable
	testSyncGatewayCreate(t, k8s1, k8s2, dns, tls, &policy)
}

// TestSyncGatewayCreateRemoteMandatoryMutualTLS tests mandatory mTLS connectivity to a remote Kubernetes cluster.
func TestSyncGatewayCreateRemoteMandatoryMutualTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)
	k8s2 := framework.Global.GetCluster(1)

	dns, cleanup := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)
	defer cleanup()

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s2, k8s2.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testSyncGatewayCreate(t, k8s1, k8s2, dns, tls, &policy)
}
