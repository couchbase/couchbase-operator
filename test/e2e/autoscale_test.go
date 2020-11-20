package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterSize)

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
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterSize)

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
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterSize)

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
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName, nil, nil)

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

func testAutoscale(t *testing.T, targetKube *types.Cluster, metricName string, metricValue int64, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := 4
	sizePerConfig := clusterSize / 2

	// Create the cluster with autoscaling with desired value of tls and it's policy
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName, tls, policy)

	if tls != nil {
		// When the cluster is healthy, check the TLS is correctly configured.
		e2eutil.MustCheckClusterTLS(t, targetKube, tls, 5*time.Minute)
	}

	// Check autoscaler was created only for query config
	autoscalerName := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Start custom metric server
	cleanupMetrics := e2eutil.MustCreateCustomMetricServer(t, targetKube, testCouchbase.Namespace, testCouchbase.Name)
	defer cleanupMetrics()

	// Set HPA to scale when incrementing stat passes unit value of 80
	// cluster should scale up once and drop average value desirable range
	_ = e2eutil.MustCreateAverageValueHPA(t, targetKube, testCouchbase.Namespace, autoscalerName, 1, 6, metricName, metricValue)

	// Allow cluster to finish rebalancing
	//e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)

	// expected events for scaling up with autoscaler
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Optional{
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
					e2eutil.ClusterScaleUpSequence(1),
				},
			},
		},
		eventschema.Optional{
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventAutoscaleDown},
					e2eutil.ClusterScaleDownSequence(1),
				},
			},
		},
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

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	testAutoscale(t, targetKube, testMetricIncrement, 80, nil, nil)
}

func TestAutoscaleUpTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testAutoscale(t, targetKube, testMetricIncrement, 80, tls, nil)
}

func TestAutoscaleUpMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testAutoscale(t, targetKube, testMetricIncrement, 80, tls, &policy)
}

func TestAutoscaleUpMandatoryMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testAutoscale(t, targetKube, testMetricIncrement, 80, tls, &policy)
}

// TestAutoscaleDown tests that operator reacts to downscale requests from HPA.
// NOTE: depending on HPA configuration, can take 10-15 mins
//       before downscaling is triggered.
func TestAutoscaleDown(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	testAutoscale(t, targetKube, testMetricDecrement, 100, nil, nil)
}

func TestAutoscaleDownTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testAutoscale(t, targetKube, testMetricDecrement, 100, tls, nil)
}

func TestAutoscaleDownMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testAutoscale(t, targetKube, testMetricDecrement, 100, tls, &policy)
}

func TestAutoscaleDownMandatoryMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testAutoscale(t, targetKube, testMetricDecrement, 100, tls, &policy)
}

// TestAutoscaleMultiConfigs tests that different configs can be scaled in different directions.
// This also validates that the label selection of our Custom Resource is working by only
// averaging metrics from Pods actually associated with each config.
func TestAutoscaleMultiConfigs(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 4
	sizePerConfig := clusterSize / 2

	// Create the cluster with autoscaling
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName, nil, nil)
	// explicitly enabling autoscaling for data config
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/EnablePreviewScaling", true), time.Minute)
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

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 2
	sizePerConfig := clusterSize / 2

	// Create the cluster with autoscaling
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName, nil, nil)

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

// TestAutoScalingDisabledOnData tests that autoscaling resources
// are not created for configs with data services.
func TestAutoScalingDisabledOnData(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 2
	sizePerConfig := clusterSize / 2

	// Create the cluster
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName, nil, nil)

	// Expect query config(1) to be created
	autoscalerName := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Data config(0) should not be created
	autoscalerName = testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	err := e2eutil.WaitUntilCouchbaseAutoscalerExists(targetKube, testCouchbase, autoscalerName, 20*time.Second)

	if err == nil {
		e2eutil.Die(t, fmt.Errorf("unexpected data config found for autoscaler cr"))
	}
}

// TestAutoScalingDisabledOnData tests that autoscaling resources
// are not created when cluster has data bucket.
// Even if services are ephemeral.
func TestAutoScalingDisabledOnCouchbaseBucket(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 2
	sizePerConfig := clusterSize / 2

	// Create bucket resource
	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "data-bucket",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(256),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: true,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}
	e2eutil.MustNewBucket(t, targetKube, bucket)

	// Create the cluster.
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName, nil, nil)

	// Set autoscale enable on data config even though it should not be able to scale
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/AutoscaleEnabled", true), time.Minute)

	// Query config(1) should not be created
	autoscalerName := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	err := e2eutil.WaitUntilCouchbaseAutoscalerExists(targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	if err == nil {
		e2eutil.Die(t, fmt.Errorf("unexpected query config found for autoscaler cr"))
	}

	// Data config(0) should not be created
	autoscalerName = testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	err = e2eutil.WaitUntilCouchbaseAutoscalerExists(targetKube, testCouchbase, autoscalerName, 20*time.Second)

	if err == nil {
		e2eutil.Die(t, fmt.Errorf("unexpected data config found for autoscaler cr"))
	}
}

// TestPreviewModeAllowsEphemeral tests that autoscaling is enabled
// when buckets are ephemeral but only for ephemeral configs.
func testPreviewMode(t *testing.T, targetKube *types.Cluster, stateful bool, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := 2
	sizePerConfig := clusterSize / 2

	// create required buckets
	if stateful {
		bucket := e2espec.DefaultBucket()
		bucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(256)
		bucket.Spec.EnableIndexReplica = true
		e2eutil.MustNewBucket(t, targetKube, bucket)
	} else {
		bucket := e2espec.DefaultEphemeralBucket()
		bucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(101)
		bucket.Spec.CompressionMode = e2eutil.GetCompressionMode("passive")
		e2eutil.MustNewBucket(t, targetKube, bucket)
	}

	// Create the cluster
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, sizePerConfig, queryConfigName, tls, policy)
	// Set autoscale enable on data config even though it should not be able to scale
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/AutoscaleEnabled", true), time.Minute)

	// Enable or Disable preview scaling.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/EnablePreviewScaling", stateful), time.Minute)

	// Expect query config(1) to be created
	autoscalerName := testCouchbase.Spec.Servers[1].AutoscalerName(testCouchbase.Name)
	e2eutil.MustWaitUntilCouchbaseAutoscalerExists(t, targetKube, testCouchbase, autoscalerName, 1*time.Minute)

	// Data config(0) should not be created
	autoscalerName = testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	err := e2eutil.WaitUntilCouchbaseAutoscalerExists(targetKube, testCouchbase, autoscalerName, 20*time.Second)

	if err == nil && !stateful {
		e2eutil.Die(t, fmt.Errorf("unexpected data config found for autoscaler cr"))
	}
}

func TestPreviewModeAllowsEphemeral(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testPreviewMode(t, targetKube, false, nil, nil)
}

func TestPreviewModeAllowsEphemeralTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testPreviewMode(t, targetKube, false, tls, nil)
}

func TestPreviewModeAllowsEphemeralMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPreviewMode(t, targetKube, false, tls, &policy)
}

func TestPreviewModeAllowsEphemeralMandatoryMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPreviewMode(t, targetKube, false, tls, &policy)
}

// TestPreviewModeAllowsEphemeral tests that autoscaling is enabled
// for stateless config even when using data buckets and stateful configs.
func TestPreviewModeEnabledAllowsStateful(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	testPreviewMode(t, targetKube, true, nil, nil)
}

func TestPreviewModeEnabledAllowsStatefulTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	testPreviewMode(t, targetKube, true, tls, nil)
}

func TestPreviewModeEnabledAllowsStatefulMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable

	testPreviewMode(t, targetKube, true, tls, &policy)
}

func TestPreviewModeEnabledAllowsStatefulMandatoryMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory

	testPreviewMode(t, targetKube, true, tls, &policy)
}
