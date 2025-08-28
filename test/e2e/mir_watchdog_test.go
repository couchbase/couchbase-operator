package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	v1 "k8s.io/api/core/v1"
)

// TestMirWatchdogDisabledAnnotation tests that the manual intervention watchdog can be disabled using the annotation.
func TestMirWatchdogDisabledAnnotation(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 1

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Annotations = map[string]string{
		"cao.couchbase.com/enableMirWatchdog": "false",
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Fetch the auth secret so we can change the password back later.
	authSecret := e2eutil.MustGetSecret(t, kubernetes, kubernetes.DefaultSecret.Name)
	password := string(authSecret.Data["password"])
	updatedPassword := fmt.Sprintf("%s!", password)

	// Check the current value of the intervention metric is 0.
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 0, 1*time.Minute)

	// Change the password to something invalid so we know a intervention would be required.
	e2eutil.MustChangeClusterPassword(t, kubernetes, cluster, "", updatedPassword, 1*time.Minute)

	// Sleep for longer than the mirWatchdog interval so the watchdog (if it was running) would have looped at least once.
	time.Sleep(30 * time.Second)

	// Make sure the condition does not exist and metric is not incremented.
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 2*time.Minute, couchbasev2.ClusterConditionManualInterventionRequired)
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 0, 1*time.Minute)

	// Validate we don't see any mir events.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMirWatchdogOnInvalidClusterCredentials tests the manual intervention watchdog checks for unauthorised cluster credentials and
// triggers the mir cluster condition, metric and events accordingly.
func TestMirWatchdogOnInvalidClusterCredentials(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 1

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Fetch the auth secret so we can change the password back later.
	authSecret := e2eutil.MustGetSecret(t, kubernetes, kubernetes.DefaultSecret.Name)
	originalPassword := string(authSecret.Data["password"])
	updatedPassword := fmt.Sprintf("%s!", originalPassword)

	// Check the current value of the intervention metric is 0.
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 0, 1*time.Minute)

	// Change the cluster password directly using the API.
	e2eutil.MustChangeClusterPassword(t, kubernetes, cluster, "", updatedPassword, 1*time.Minute)

	// Check the watchdog adds the cluster condition and updates the intervention metric.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionManualInterventionRequired, v1.ConditionTrue, cluster, 2*time.Minute)
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 1, 1*time.Minute)

	// Change the cluster password back to the original password.
	e2eutil.MustChangeClusterPassword(t, kubernetes, cluster, updatedPassword, originalPassword, 1*time.Minute)

	// Check the cluster condition is now false and the intervention metric is 0.
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 2*time.Minute, couchbasev2.ClusterConditionManualInterventionRequired)
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 0, 1*time.Minute)

	// Check the cluster is healthy.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect. Depending on where the reconciliation loop is when we add the condition, we may see a reconcile failed event before, during or after the manual intervention events are applied.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
		eventschema.Event{Reason: k8sutil.EventReasonManualInterventionRequired},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
		eventschema.Event{Reason: k8sutil.EventReasonManualInterventionResolved},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMirWatchdogOnConsecutiveRebalanceFailures(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 2

	cluster := clusterOptions().WithPersistentTopology(clusterSize).MustCreate(t, kubernetes)

	// Create and populate a bucket (200 MB) to slow down the rebalance.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	e2eutil.MustPopulateWithDataSize(t, kubernetes, cluster, bucket.GetName(), f.CouchbaseServerImage, 200<<20, time.Minute)

	// Check the current value of the intervention metric is 0.
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 0, 1*time.Minute)

	// The MirWatchdog should alert when the rebalance retries are exhausted 3 times in a row. We can scale
	// the cluster up to trigger a rebalance, then fail it 3 times in a row. This loops 4 times
	// to ensure we hit the MIR state given the MirWatchdog and reconciliation loops are asynchronous.
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+1, kubernetes, cluster)
	for i := 0; i < 4; i++ {
		e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 2*time.Minute)
		e2eutil.MustFailRebalance(t, kubernetes, cluster, 2*time.Minute)
	}

	// Check that we enter the manual intervention required state.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionManualInterventionRequired, v1.ConditionTrue, cluster, 2*time.Minute)
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 1, 1*time.Minute)

	// Manually rebalance the cluster to resolve the MIR condition.
	e2eutil.MustRebalanceCluster(t, kubernetes, cluster, 2*time.Minute)

	// Once the cluster is healthy, we should remove the manual intervention condition and reset the metric.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 2*time.Minute, couchbasev2.ClusterConditionManualInterventionRequired)
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 0, 1*time.Minute)

	// Check the events match what we expect.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Repeat{
			Times: 3,
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
					eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed},
				},
			},
		},
		// Because the MirWatchdog and reconciliation loops are asynchronous, we will see another failed rebalance
		// which, depending on the timing of the MirWatchdog, will start before or after the manual intervention required event.
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted}},
		eventschema.Event{Reason: k8sutil.EventReasonManualInterventionRequired},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed},
		eventschema.Event{Reason: k8sutil.EventReasonManualInterventionResolved},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMirWatchdogOnManualActionDownNodes(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 2
	victimIndex := 0

	clusterOptions := clusterOptions().WithEphemeralTopology(clusterSize)
	clusterOptions.Options.AutoFailoverTimeout = e2espec.NewDurationS(5)
	cluster := clusterOptions.MustCreate(t, kubernetes)

	// Check the current value of the intervention metric is 0.
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 0, 1*time.Minute)

	// By killing a node in a 2 node cluster, cb server will be unable to auto failover.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)

	// Check that we enter the manual intervention required state.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionManualInterventionRequired, v1.ConditionTrue, cluster, 2*time.Minute)
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 1, 1*time.Minute)

	// Manually failover the node we killed to resolve the MIR condition.
	e2eutil.MustFailoverNode(t, kubernetes, cluster, victimIndex, true, 5*time.Minute)

	// Once the node has manually been removed, we should see the MIR condition removed and the metric reset.
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 2*time.Minute, couchbasev2.ClusterConditionManualInterventionRequired)
	e2eutil.MustCheckOperatorGaugeMetric(t, kubernetes, nil, "cluster_manual_intervention", 0, 1*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check we get the expected series of events
	// 1. Cluster Created
	// 2. Node Down (and unrecoverable)
	// 3. MIR Required
	// 4. MIR Resolved (Manually failed over the node)
	// 5. Cluster scaled back up by the operator
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
		eventschema.Event{Reason: k8sutil.EventReasonManualInterventionRequired},
		eventschema.Event{Reason: k8sutil.EventReasonManualInterventionResolved},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
