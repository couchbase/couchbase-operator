package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestLightsOutEphemeral tests turning the power off and the operator recovering
// an ephemeral cluster.
func TestLightsOutEphemeral(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, clusterSize)

	// Once the cluster is up and running, stop the operator and terminate all the
	// pods (e.g. turn the datacenter off).  Restart the operator and expect it to
	// bring the cluster back to life!
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, config.OperatorResourceName, time.Minute)
	e2eutil.MustTerminateAllPods(t, kubernetes, cluster)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes, framework.CreateDeploymentObject(kubernetes, f.OpImage, 0, f.PodCreateTimeout))
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster created again
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestLightsOutPersistent tests turning the power off and the operator recovering
// a persistent cluster.
func TestLightsOutPersistent(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	mdsGroupSize := 3
	clusterSize := mdsGroupSize * 2
	recoveryPolicy := couchbasev2.PrioritzeUptime

	// Create a basic supportable cluster with 2 stateful and 2 stateless nodes
	// Set an aggressive recovery policy so that the two failed query nodes get
	// kicked out.
	cluster := e2espec.NewSupportableCluster(mdsGroupSize)
	cluster.Spec.RecoveryPolicy = &recoveryPolicy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Once the cluster is up and running, stop the operator and terminate all the
	// pods (e.g. turn the datacenter off).  Restart the operator and expect it to
	// bring the cluster back to life!
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, config.OperatorResourceName, time.Minute)
	e2eutil.MustTerminateAllPods(t, kubernetes, cluster)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes, framework.CreateDeploymentObject(kubernetes, f.OpImage, 0, f.PodCreateTimeout))
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster recovered
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.PodDownWithPVCRecoverySequenceWithEphemeral(clusterSize, mdsGroupSize, mdsGroupSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
