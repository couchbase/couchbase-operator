package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

func TestMigrateCluster(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3

	// Create the source cluster.
	srcCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Pause the source cluster.
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	srcCluster = e2eutil.MustPatchCluster(t, kubernetes, srcCluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	dstCluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	dstCluster.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{
		UnmanagedClusterHost: fmt.Sprintf("%s.%s.svc.cluster.local", srcCluster.Name, srcCluster.Namespace),
	}

	e2eutil.MustNewClusterFromSpec(t, kubernetes, dstCluster)

	// Check that all nodes from the initial cluster have been ejected
	e2eutil.MustBeUnitializedCluster(t, kubernetes, srcCluster)
}
