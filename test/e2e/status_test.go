package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestStatusRecovery tests that the status is restored correctly when it is
// deleted by a user or an outside influence (Heptio Velero being the main offender).
func TestStatusRecovery(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, clusterSize)

	// Runtime configuration.
	uuid := e2eutil.MustGetUUID(t, kubernetes, cluster, time.Minute)

	// When ready pause the cluster and wait until reported as such, it is possible that a
	// reconciliation is in flight and will just reset the status to what it was.  Next delete
	// the status and enusre it has gone.  Finally restart the operator and ensure all status
	// fields are correctly populated.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/status"), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/status", couchbasev2.ClusterStatus{ControlPaused: true}), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)

	version, err := k8sutil.CouchbaseVersion(f.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}

	tests := jsonpatch.NewPatchSet().
		Test("/status/clusterId", uuid).
		Test("/status/size", clusterSize).
		Test("/status/currentVersion", version)

	e2eutil.MustPatchCluster(t, kubernetes, cluster, tests, time.Minute)
}
