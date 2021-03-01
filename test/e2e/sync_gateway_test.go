package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// skipRBAC inhibits tests for versions below 6.5.0.
func skipRBAC(t *testing.T) {
	tag, err := k8sutil.CouchbaseVersion(framework.Global.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}

	version, err := couchbaseutil.NewVersion(tag)
	if err != nil {
		e2eutil.Die(t, err)
	}

	threshold, _ := couchbaseutil.NewVersion("6.5.0")
	if version.Less(threshold) {
		t.Skip("Unsupported couchbase version: RBAC requires >6.5.0")
	}
}

// testSyncGatewayCreate is a generic creation and connectivity test.
func testSyncGatewayCreate(t *testing.T, kubernetes1, kubernetes2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster in the target cluster.
	bucket := e2eutil.MustGetBucket(t, framework.Global.BucketType, framework.Global.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes2, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(tls, policy).MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, cluster, bucket, time.Minute)

	// Create the sync gateway in the source cluster and insert a document.
	e2eutil.MustCreateSyncGateway(t, kubernetes1, cluster, framework.Global.SyncGatewayImage, bucket.GetName(), nil, dns, tls, time.Minute)

	// Ensure meta-data documents appear in the Couchbase cluster.
	e2eutil.MustVerifyDocCountInBucketNonZero(t, kubernetes2, cluster, bucket.GetName(), 5*time.Minute)
}

// TestSyncGatewayCreateLocal tests connectivity within the same Kubernetes cluster.
func TestSyncGatewayCreateLocal(t *testing.T) {
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	testSyncGatewayCreate(t, k8s1, k8s1, nil, nil, nil)
}

// TestSyncGatewayCreateLocalTLS tests TLS connectivity within the same Kubernetes cluster.
func TestSyncGatewayCreateLocalTLS(t *testing.T) {
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, k8s1, &e2eutil.TLSOpts{})

	testSyncGatewayCreate(t, k8s1, k8s1, nil, tls, nil)
}

// TestSyncGatewayCreateLocalMutualTLS tests mTLS connectivity within the same Kubernetes cluster.
func TestSyncGatewayCreateLocalMutualTLS(t *testing.T) {
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, k8s1, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testSyncGatewayCreate(t, k8s1, k8s1, nil, tls, &policy)
}

// TestSyncGatewayCreateLocalMandatoryMutualTLS tests mandatory mTLS connectivity within the same Kubernetes cluster.
func TestSyncGatewayCreateLocalMandatoryMutualTLS(t *testing.T) {
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, k8s1, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testSyncGatewayCreate(t, k8s1, k8s1, nil, tls, &policy)
}

// TestSyncGatewayCreateRemote tests connectivity to a remote Kubernetes cluster.
func TestSyncGatewayCreateRemote(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	testSyncGatewayCreate(t, k8s1, k8s2, dns, nil, nil)
}

// TestSyncGatewayCreateRemoteTLS tests TLS connectivity to a remote Kubernetes cluster.
func TestSyncGatewayCreateRemoteTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})

	testSyncGatewayCreate(t, k8s1, k8s2, dns, tls, nil)
}

// TestSyncGatewayCreateRemoteMutualTLS tests mTLS connectivity to a remote Kubernetes cluster.
func TestSyncGatewayCreateRemoteMutualTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testSyncGatewayCreate(t, k8s1, k8s2, dns, tls, &policy)
}

// TestSyncGatewayCreateRemoteMandatoryMutualTLS tests mandatory mTLS connectivity to a remote Kubernetes cluster.
func TestSyncGatewayCreateRemoteMandatoryMutualTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testSyncGatewayCreate(t, k8s1, k8s2, dns, tls, &policy)
}

// TestSyncGatewayRBAC tests SGW works end-to-end with the bucket_full_access role.
func TestSyncGatewayRBAC(t *testing.T) {
	// Platform configuration.
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	skipRBAC(t)

	// Static configuration.
	// NOTE: the secret handling is a hack, by default the sync-gateway configuration will
	// use the cluster's admin account secret, so while RBAC requires "password" we also put
	// "username" in there too so that the client configuration works correctly.
	clusterSize := 3
	resourceName := "sync-gateway"
	password := "4Sparta!!!!"
	secretName := resourceName + "-" + e2eutil.RandomSuffix()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		Data: map[string][]byte{
			constants.AuthSecretUsernameKey: []byte(resourceName),
			constants.AuthSecretPasswordKey: []byte(password),
		},
	}

	user := &couchbasev2.CouchbaseUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Spec: couchbasev2.CouchbaseUserSpec{
			AuthDomain: couchbasev2.InternalAuthDomain,
			AuthSecret: secretName,
		},
	}
	group := &couchbasev2.CouchbaseGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Spec: couchbasev2.CouchbaseGroupSpec{
			Roles: []couchbasev2.Role{
				{
					Name:   couchbasev2.RoleApplicationAccess,
					Bucket: e2espec.DefaultBucket().Name,
				},
			},
		},
	}
	binding := &couchbasev2.CouchbaseRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Spec: couchbasev2.CouchbaseRoleBindingSpec{
			Subjects: []couchbasev2.CouchbaseRoleBindingSubject{
				{
					Kind: couchbasev2.RoleBindingSubjectTypeUser,
					Name: resourceName,
				},
			},
			RoleRef: couchbasev2.CouchbaseRoleBindingRef{
				Kind: couchbasev2.RoleBindingReferenceTypeGroup,
				Name: resourceName,
			},
		},
	}
	// Create the RBAC primitives and Couchbase cluster.
	e2eutil.MustCreateSecret(t, k8s1, secret)

	e2eutil.MustNewUser(t, k8s1, user)
	e2eutil.MustNewGroup(t, k8s1, group)
	e2eutil.MustNewRoleBinding(t, k8s1, binding)

	bucket := e2eutil.MustGetBucket(t, framework.Global.BucketType, framework.Global.CompressionMode)
	e2eutil.MustNewBucket(t, k8s1, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, k8s1)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, cluster, bucket, time.Minute)

	// Create the sync gateway in the source cluster and insert a document.
	e2eutil.MustCreateSyncGateway(t, k8s1, cluster, framework.Global.SyncGatewayImage, bucket.GetName(), secret, nil, nil, time.Minute)

	// Ensure meta-data documents appear in the Couchbase cluster.
	e2eutil.MustVerifyDocCountInBucketNonZero(t, k8s1, cluster, bucket.GetName(), 5*time.Minute)
}
