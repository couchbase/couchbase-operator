package e2eutil

import (
	"fmt"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
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
