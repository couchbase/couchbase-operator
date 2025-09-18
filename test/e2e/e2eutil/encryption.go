package e2eutil

import (
	"context"
	"fmt"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MustAssertEncryptionKeyExists(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, keyName string) {
	client, err := CreateAdminConsoleClient(kubernetes, cluster)
	if err != nil {
		Die(t, err)
	}

	keys := couchbaseutil.EncryptionKeyList{}
	if err := couchbaseutil.ListEncryptionKeys(&keys).On(client.client, client.host); err != nil {
		Die(t, err)
	}

	if len(keys) == 0 {
		Die(t, fmt.Errorf("no encryption keys found"))
	}

	for _, key := range keys {
		if key.Name == keyName {
			return
		}
	}

	Die(t, fmt.Errorf("encryption key %s not found", keyName))
}

// MustAssertEncryptionKeyFinalizerExists asserts that the encryption key has the cluster finalizer.
func MustAssertEncryptionKeyFinalizerExists(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, keyName string) {
	key, err := kubernetes.CRClient.CouchbaseV2().CouchbaseEncryptionKeys(kubernetes.Namespace).Get(context.Background(), keyName, metav1.GetOptions{})
	if err != nil {
		Die(t, err)
	}

	if !key.HasClusterFinalizer(cluster) {
		Die(t, fmt.Errorf("encryption key %s does not have finalizer", keyName))
	}
}

// CheckIfEncryptionKeyIsDeleted checks if the encryption key is completely deleted.
func CheckIfEncryptionKeyIsDeleted(t *testing.T, kubernetes *types.Cluster, key *couchbasev2.CouchbaseEncryptionKey) bool {
	key, err := kubernetes.CRClient.CouchbaseV2().CouchbaseEncryptionKeys(key.Namespace).Get(context.Background(), key.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true
		}

		Die(t, err)
	}

	return key == nil
}
