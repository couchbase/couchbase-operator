package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
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

	// Check the cluster is healthy.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

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

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

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
