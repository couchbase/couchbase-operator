package e2e

import (
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

const (
	// Name of config with only query ndoes.
	queryConfigName = "test_config_query"

	// Metric incrementing from 0-100 by 10
	testMetricIncrement = "test-metric-increment"

	// TODO: additional metric targets to test against
	//       uncomment as tests are implemented
	// Metric decrementing from 100-0 by 10
	testMetricDecrement = "test-metric-decrement"
	/*
		// Metric with eventual average value of 200
		testMetricAverage = "test-metric-average"
		// Metric with random values between 0-1000
		testMetricRandom = "test-metric-randome"
	*/
)

// TestAutoscaleEnabled tests autoscaling resource creation.
// 1. Create cluster with Autoscaling enabled.
// 2. Wait for autoscaler CR's creation.
func TestAutoscaleEnabled(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterSize, []string{})

	// Check autoscaler was created
	autoscalerName := testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * AutoscalerCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestAutoscaleDisabled tests behavior of disabling autoscaling.
// 1. Create cluster with Autoscaling enabled.
// 2. Wait for autoscaler CR's creation.
// 3. Disable cluster autoscaling.
// 4. Wait for autoscaler CR's deletion.
func TestAutoscaleDisabled(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterSize, []string{})

	// Check autoscaler was created
	autoscalerName := testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Disable autoscaling
	e2eutil.MustDisableCouchbaseAutoscaling(t, targetKube, testCouchbase)
	e2eutil.MustWaitForCouchbaseAutoscalerDeletion(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscalerDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestAutoscalerDeleted tests behavior of user deletion of autoscaler.
// 1. Create cluster with Autoscaling enabled.
// 2. Wait for autoscaler CR's creation.
// 3. Delete autoscaler CR.
// 4. Wait for CR recreation.
func TestAutoscalerDeleted(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterSize, []string{})

	// Check autoscaler was created
	autoscalerName := testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Must delete autoscaler
	e2eutil.MustDeleteCouchbaseAutoscaler(t, targetKube, testCouchbase.Namespace, autoscalerName)
	e2eutil.MustWaitForCouchbaseAutoscalerDeletion(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Operator must recreate autoscaler
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * AutoscalerCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestAutoscaleSelectiveMDS tests autoscaling enabled only for specific config
// 1. Create cluster with Autoscaling enabled for query config.
// 2. Wait for autoscaler CR's creation.
func TestAutoscaleSelectiveMDS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 2
	sizePerConfig := clusterSize / 2

	// Create the cluster with autoscaling
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName)

	// Check autoscaler was created only for query config
	autoscalerName := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * AutoscalerCreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestAutoscaleUp tests that operator reacts to upscale requests from HPA.
// 1. Create cluster with Autoscaling
// 2. Create associate HPA with query config
// 3. Set target value to 80
// 4. As the custom metrics adaptor increments to 100, HPA will scale up at least once
//      causing target value to drop within expected range
// 5. Verify scale up.
func TestAutoscaleUp(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 2
	sizePerConfig := clusterSize / 2

	// Create the cluster with autoscaling
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName)

	// Check autoscaler was created only for query config
	autoscalerName := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Start custom metric server
	cleanupMetrics := e2eutil.MustCreateCustomMetricServer(t, targetKube, testCouchbase.Namespace, testCouchbase.Name)
	defer cleanupMetrics()

	// Set HPA to scale when incrementing stat passes unit value of 80
	// cluster should scale up once and drop average value desirable range
	_ = e2eutil.MustCreateAverageValueHPA(t, targetKube, testCouchbase.Namespace, autoscalerName, 1, 6, testMetricIncrement, 80)

	// Allow cluster to finish rebalancing
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)

	// Verify events
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestAutoscaleDown tests that operator reacts to downscale requests from HPA.
// NOTE: depending on HPA configuration, can take 10-15 mins
//       before downscaling is triggered.
func TestAutoscaleDown(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 4
	sizePerConfig := clusterSize / 2

	// Create the cluster with autoscaling
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName)

	// Check autoscaler was created only for query config
	autoscalerName := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Start custom metric server
	cleanupMetrics := e2eutil.MustCreateCustomMetricServer(t, targetKube, testCouchbase.Namespace, testCouchbase.Name)
	defer cleanupMetrics()

	// Set HPA to scale when decrement metric drops below value of 100
	// cluster should scale down once and drop average value desirable range
	_ = e2eutil.MustCreateAverageValueHPA(t, targetKube, testCouchbase.Namespace, autoscalerName, 1, 6, testMetricDecrement, 100)

	// Allow cluster to finish rebalancing
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 20*time.Minute)

	// Verify events
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleDown},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestAutoscaleMultiConfigs tests that different configs can be scaled in different directions.
// This also validates that the label selection of our Custom Resource is working by only
// averaging metrics from Pods actually associated with each config.
func TestAutoscaleMultiConfigs(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 4
	sizePerConfig := clusterSize / 2

	// Create the cluster with autoscaling
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName)
	// explicitly enabling autoscaling for data config
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/AutoscaleEnabled", true), time.Minute)

	// Expect 2 autoscalers are created
	autoscalerConfig1 := testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerConfig1, 1*time.Minute)
	autoscalerConfig2 := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerConfig2, 1*time.Minute)

	// Start custom metric server
	cleanupMetrics := e2eutil.MustCreateCustomMetricServer(t, targetKube, testCouchbase.Namespace, testCouchbase.Name)
	defer cleanupMetrics()

	// Apply *incrementing* HPA to 1st config
	_ = e2eutil.MustCreateAverageValueHPA(t, targetKube, testCouchbase.Namespace, autoscalerConfig1, 1, 6, testMetricIncrement, 80)

	// Apply *decrementing* HPA to 2nd config
	_ = e2eutil.MustCreateAverageValueHPA(t, targetKube, testCouchbase.Namespace, autoscalerConfig2, 1, 6, testMetricDecrement, 100)

	// Allow cluster to finish rebalancing
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)

	// Verify events.
	// HPA is working as expected if we scale up without
	// being affected by dercrementing metrics
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestAutoscaleConflict tests that operator doesn't accept scale requests
// from any other sources while autoscaling.
// For instance, someone pointing 2 different HPA's at single CBA
// or manual change while handing HPA request (and vice versa).
func TestAutoscaleConflict(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 2
	sizePerConfig := clusterSize / 2

	// Create the cluster with autoscaling
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName)

	// Check autoscaler was created only for query config
	autoscalerName := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Start custom metric server
	cleanupMetrics := e2eutil.MustCreateCustomMetricServer(t, targetKube, testCouchbase.Namespace, testCouchbase.Name)
	defer cleanupMetrics()

	// Set HPA to scale when incrementing stat passes unit value of 80
	// cluster should scale up once and drop average value desirable range
	_ = e2eutil.MustCreateAverageValueHPA(t, targetKube, testCouchbase.Namespace, autoscalerName, 1, 6, testMetricIncrement, 80)

	// Wait for scale up event
	configName := testCouchbase.Spec.Servers[1].Name
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleUpEvent(testCouchbase, configName, 1, 2), 5*time.Minute)

	// Immediately attempt to set scale to 10 before cluster scales
	e2eutil.MustUpdateScale(t, targetKube, testCouchbase.Namespace, autoscalerName, 10)

	// Allow cluster to finish rebalancing
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)

	// Send scale up request
	testCouchbase = e2eutil.MustResizeCluster(t, 1, 3, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Verify events
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
