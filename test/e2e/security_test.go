package e2e

import (
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestRotateAdminPassword is a basic sanity test to ensure the operator rotates
// the password on demand and that the test suite is able to observe it happening.
func TestRotateAdminPassword(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, clusterSize)

	// Rotate the password.
	e2eutil.MustRotateClusterPassword(t, kubernetes)

	// Check the events match what we expect:
	// * Cluster created
	// * Password rotated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonAdminPasswordChanged},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestRotateAdminPasswordAndRestart tests that we operator continue to work across
// a restart.  Ensures that the operator wasn't relying on any cached state and the
// password has indeed been rotatated.
// Note: we could also validate the cluster persistence secret has the right value
// for absolute certainty.
func TestRotateAdminPasswordAndRestart(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, clusterSize)

	// Rotate the password and restart the operator.  We perform a simple
	// scaling operation so we can observe the operator actually being able
	// to talk to server.
	e2eutil.MustRotateClusterPassword(t, kubernetes)
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, framework.CreateDeploymentObject(kubernetes, f.OpImage, 0, f.PodCreateTimeout), time.Minute)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes, framework.CreateDeploymentObject(kubernetes, f.OpImage, 0, f.PodCreateTimeout))
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Password rotated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonAdminPasswordChanged},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestRotateAdminPasswordDuringRestart tests that the operator uses the persistence
// sercet as its source of truth on restart so thus using the current admin password
// rather than the one in the user specified secret that has been updated.
func TestRotateAdminPasswordDuringRestart(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, clusterSize)

	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, framework.CreateDeploymentObject(kubernetes, f.OpImage, 0, f.PodCreateTimeout), time.Minute)
	e2eutil.MustRotateClusterPassword(t, kubernetes)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes, framework.CreateDeploymentObject(kubernetes, f.OpImage, 0, f.PodCreateTimeout))
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Password rotated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonAdminPasswordChanged},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
