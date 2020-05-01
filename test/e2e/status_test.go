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
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, kubernetes.Namespace, clusterSize)

	// Runtime configuration.
	uuid := e2eutil.MustGetUUID(t, kubernetes, cluster, time.Minute)

	// When ready pause the cluster and wait until reported as such, it is possible that a
	// reconciliation is in flight and will just reset the status to what it was.  Next delete
	// the status and enusre it has gone.  Finally restart the operator and ensure all status
	// fields are correctly populated.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/Status/ControlPaused", true), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/Status"), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/Status", couchbasev2.ClusterStatus{ControlPaused: true}), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)

	version, err := k8sutil.CouchbaseVersion(f.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}

	tests := jsonpatch.NewPatchSet().
		Test("/Status/ClusterID", uuid).
		Test("/Status/Size", clusterSize).
		Test("/Status/CurrentVersion", version)

	e2eutil.MustPatchCluster(t, kubernetes, cluster, tests, time.Minute)
}
