/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
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
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterOptions().WithEphemeralTopology(clusterSize).Options)

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
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterOptions().WithEphemeralTopology(clusterSize).Options)

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
	testCouchbase := e2eutil.MustNewAutoscaleCluster(t, targetKube, clusterOptions().WithEphemeralTopology(clusterSize).Options)

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
	testCouchbase := e2eutil.MustNewAutoscaleClusterMDS(t, targetKube, clusterOptions().WithEphemeralTopology(sizePerConfig).Options, nil, nil)

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

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster() // Requires access to kube-system!!

	// Create 1 node cluster with referencing HPA
	clusterSize := 1
	options := clusterOptions().WithEphemeralTopology(clusterSize).Options
	targetValue := e2espec.DefaultMetricTarget
	config := e2espec.NewIncrementingHPAConfig(targetValue).WithSinglePodScalingPolicy()

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options, config)
	defer hpaManager.Cleanup()

	// Wait for scale up event
	testCouchbase := hpaManager.CouchbaseCluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleUpEvent(testCouchbase, configName, 1, 2), 2*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionScalingUp, v1.ConditionTrue, testCouchbase, time.Minute)

	// Wait for rebalance after scale up
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// expected events for scaling up
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, targetKube, hpaManager.CouchbaseCluster, expectedEvents)
}

// Scale up 3 node cluster with TLS.
func TestAutoscaleUpMandatoryMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster() // Requires access to kube-system!!

	clusterSize := 3
	targetValue := e2espec.DefaultMetricTarget

	// Create 3 node cluster with referencing HPA
	policy := couchbasev2.ClientCertificatePolicyMandatory
	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	options := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(tls, &policy).Options
	config := e2espec.NewIncrementingHPAConfig(targetValue).WithSinglePodScalingPolicy()

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options, config)
	defer hpaManager.Cleanup()

	// Wait for scale up event
	testCouchbase := hpaManager.CouchbaseCluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleUpEvent(testCouchbase, configName, 3, 4), 2*time.Minute)

	// Wait for rebalance after scale up
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// expected events for scaling up
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, targetKube, hpaManager.CouchbaseCluster, expectedEvents)
}

// TestAutoscaleDown tests that operator reacts to downscale requests from HPA.
func TestAutoscaleDown(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster() // Requires access to kube-system!!

	// Create 2 node cluster with referencing HPA
	clusterSize := 2
	options := clusterOptions().WithEphemeralTopology(clusterSize).Options
	targetValue := e2espec.DefaultMetricTarget
	hpaStabilization := e2espec.LowHPAStabilizationPeriod
	config := e2espec.NewDecrementingHPAConfig(targetValue).WithScaleDownStabilizationWindow(hpaStabilization)

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options, config)
	defer hpaManager.Cleanup()

	// Wait for scale down event
	testCouchbase := hpaManager.CouchbaseCluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleDownEvent(testCouchbase, configName, 2, 1), 2*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionScalingDown, v1.ConditionTrue, testCouchbase, time.Minute)

	// Wait for rebalance after scale down
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// expected events for scaling down
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleDown},
		e2eutil.ClusterScaleDownSequence(1),
	}
	ValidateEvents(t, targetKube, hpaManager.CouchbaseCluster, expectedEvents)
}

// Scale down 4 node cluster with TLS.
func TestAutoscaleDownMandatoryMutualTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster() // Requires access to kube-system!!

	targetValue := e2espec.DefaultMetricTarget
	hpaStabilization := e2espec.DefaultHPAStabilizationPeriod
	config := e2espec.NewDecrementingHPAConfig(targetValue).WithScaleDownStabilizationWindow(hpaStabilization)
	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})

	// Create 4 node cluster with referencing HPA
	clusterSize := 4
	policy := couchbasev2.ClientCertificatePolicyMandatory
	options := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(tls, &policy).Options

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options, config)
	defer hpaManager.Cleanup()

	// Wait for scale down event
	testCouchbase := hpaManager.CouchbaseCluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleDownEvent(testCouchbase, configName, 4, 1), 2*time.Minute)

	// Wait for rebalance after scale down
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// expected events for scaling down
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleDown},
		e2eutil.ClusterScaleDownSequence(3),
	}
	ValidateEvents(t, targetKube, hpaManager.CouchbaseCluster, expectedEvents)
}

// TestAutoscaleMultiConfigs tests that different configs can be scaled in different directions.
// This also validates that the label selection of our Custom Resource is working by only
// averaging metrics from Pods actually associated with each config.
func TestAutoscaleMultiConfigs(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster() // Requires access to kube-system!!

	// Static configuration.
	clusterSize := 4
	sizePerConfig := clusterSize / 2

	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyMandatory
	options := clusterOptions().WithMixedEphemeralTopology(sizePerConfig).WithMutualTLS(tls, &policy)

	// Create the mixed toplogy cluster with 2 HPAs.
	// The scale up HPA will is expected to be triggered first since it has
	// no stabilization window by default, but after 1 Pod is added, HPA should
	// stop making recommendations to the corresponding server group.
	targetValue := e2espec.DefaultMetricTarget
	scaleUpConfig := e2espec.NewIncrementingHPAConfig(targetValue).WithSinglePodScalingPolicy()

	// Define a scale down HPA with 30 second delay before any action can be taken
	// and is expected to scale down it's corresponding server group.
	hpaStabilization := e2espec.DefaultHPAStabilizationPeriod

	// In order to create a scaling conflict, the decrementing metric is given a target
	// value of 100 so that HPA will immediate request a scale down action.
	// However, the scale down stablization window is 30s so we expect cluster
	// to first scale up before scaling down (as verified by events).
	targetValue = 100
	scaleDownConfig := e2espec.NewDecrementingHPAConfig(targetValue).WithScaleDownStabilizationWindow(hpaStabilization)

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options.Options, scaleUpConfig, scaleDownConfig)
	defer hpaManager.Cleanup()

	// Wait for scale up event from 2 -> 3 of data/index Pods
	testCouchbase := hpaManager.CouchbaseCluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleUpEvent(testCouchbase, configName, 2, 3), 5*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionScalingUp, v1.ConditionTrue, testCouchbase, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// Wait for scale down event from 2 -> 1 of query Pods
	configName = testCouchbase.Spec.Servers[1].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleDownEvent(testCouchbase, configName, 2, 1), 5*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionScalingDown, v1.ConditionTrue, testCouchbase, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// Verify events.
	// HPA is working as expected if we scale up without
	// being affected by dercrementing metrics
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		e2eutil.ClusterScaleUpSequence(1),
		eventschema.Event{Reason: k8sutil.EventAutoscaleDown},
		e2eutil.ClusterScaleDownSequence(1),
	}
	ValidateEvents(t, targetKube, hpaManager.CouchbaseCluster, expectedEvents)
}

// TestAutoscaleConflict tests that operator doesn't accept scale requests
// from any other sources while autoscaling is in maintenance mode.
// For instance, someone pointing 2 different HPA's at single CouchbaseAutoscaler
// resource, or manual change to CouchbaseAutoscaler itself while cluster is resizing.
func TestAutoscaleConflict(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// Create 2 node cluster with referencing HPA
	clusterSize := 2
	// Cluster must wait at least 30 seconds after rebalance before allowing changes
	// made to the CouchbaseAutoscaler resource
	targetValue := e2espec.DefaultMetricTarget
	clusterStabilization := e2espec.DefaultClusterStabilizationPeriod
	options := clusterOptions().WithEphemeralTopology(clusterSize).WithAutoscaleStabilizationPeriod(clusterStabilization).Options
	config := e2espec.NewIncrementingHPAConfig(targetValue).WithSinglePodScalingPolicy()
	config.Behavior.ScaleUp.Policies[0].PeriodSeconds = 120

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options, config)
	defer hpaManager.Cleanup()

	// Wait for scale up event
	testCouchbase := hpaManager.CouchbaseCluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleUpEvent(testCouchbase, configName, 2, 3), 5*time.Minute)

	// Change CouchbaseAutoscaler size to 6 (max Pods) before cluster finishes scaling.
	// This imitates behavior of HPA wanting to scale while rebalance is occurring.
	autoscalerName := testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	e2eutil.MustUpdateScale(t, targetKube, testCouchbase.Namespace, autoscalerName, 6)

	// Allow cluster to finish rebalancing
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)

	// Also Change CouchbaseCluster size to 2 before cluster finishes scaling.
	// This would occur if user observes an autoscale happening but wanted to manually go
	// back to original size.  When the operator finishes scaling it will have conflicting
	// requests, but since the Autoscaler is in maintenance mode the change to CouchbaseCluster wins.
	testCouchbase = e2eutil.MustResizeCluster(t, 0, 2, targetKube, testCouchbase, 5*time.Minute)

	// Verify Cluster autoscales up and then rollsback to 2
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestAutoscaleEnabledAllowsStatefulTLS tests that autoscaling is enabled
// when using data buckets and stateful configs.
func TestAutoscaleEnabledAllowsStatefulTLS(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster() // Requires access to kube-system!!

	// Create bucket resource
	bucket := e2espec.DefaultBucket()
	bucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(256)
	bucket.Spec.EnableIndexReplica = true
	e2eutil.MustNewBucket(t, targetKube, bucket)

	// Create 2 node cluster with referencing HPA
	clusterSize := 2
	tls := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	options := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(tls).Options
	targetValue := e2espec.DefaultMetricTarget
	config := e2espec.NewIncrementingHPAConfig(targetValue).WithSinglePodScalingPolicy()

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options, config)
	defer hpaManager.Cleanup()

	// Wait for scale up event
	testCouchbase := hpaManager.CouchbaseCluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleUpEvent(testCouchbase, configName, 2, 3), 2*time.Minute)

	// Wait for rebalance after scale up
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// expected events for scaling up
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, targetKube, hpaManager.CouchbaseCluster, expectedEvents)
}

// Test that autoscaler only enters maintenance mode when stabilization period is set.
func TestAutoscaleVerifyMaintenanceMode(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster() // Requires access to kube-system!!

	// Create 1 node cluster with referencing HPA
	clusterSize := 1
	clusterStabilization := e2espec.DefaultClusterStabilizationPeriod
	options := clusterOptions().WithEphemeralTopology(clusterSize).WithAutoscaleStabilizationPeriod(clusterStabilization).Options
	targetValue := e2espec.DefaultMetricTarget
	config := e2espec.NewIncrementingHPAConfig(targetValue).WithSinglePodScalingPolicy()

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options, config)
	defer hpaManager.Cleanup()

	// Wait for scale up event
	testCouchbase := hpaManager.CouchbaseCluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleUpEvent(testCouchbase, configName, 1, 2), 2*time.Minute)

	// Autoscaler must be in Maintenance Mode after event is posted
	autoscalerName := testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	e2eutil.MustValidateAutoscalerSize(t, targetKube, testCouchbase.Namespace, autoscalerName, 0)
	e2eutil.MustValidateAutoscaleReadyStatus(t, targetKube, testCouchbase.Namespace, testCouchbase.Name, v1.ConditionFalse)

	// Wait for rebalance after scale up
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// Remove Stabilization Period
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Remove("/spec/autoscaleStabilizationPeriod"), time.Minute)

	// Wait for next scale up event
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleUpEvent(testCouchbase, configName, 2, 3), 2*time.Minute)

	// Autoscale condition must remain ready and not go into maintenance mode
	e2eutil.MustValidateAutoscaleReadyStatus(t, targetKube, testCouchbase.Namespace, testCouchbase.Name, v1.ConditionTrue)
	// The autoscaler size should sync with cluster size
	e2eutil.MustValidateAutoscalerSize(t, targetKube, testCouchbase.Namespace, autoscalerName, 3)

	// Wait for rebalance after scale up
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// expected events for scaling up
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		e2eutil.ClusterScaleUpSequence(1),
		eventschema.Event{Reason: k8sutil.EventAutoscaleUp},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, targetKube, hpaManager.CouchbaseCluster, expectedEvents)
}

// Test that autoscaler goes into maintenance mode when cluster is manually scaled.
// As cluster is being manually scaled up HPA will recommend downscaling because
// this 'overprovisions' the cluster.  Nevertheless, in both scenarios, the
// autoscaler should be go into maintenance mode.
func TestAutoscaleManualMaintenanceMode(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster() // Requires access to kube-system!!

	// Use decrementing metric so that HPA thinks we are over provisioned after scale up occurs.
	clusterSize := 1
	clusterStabilization := e2espec.LowClusterStabilizationPeriod
	options := clusterOptions().WithEphemeralTopology(clusterSize).WithAutoscaleStabilizationPeriod(clusterStabilization).Options
	targetValue := e2espec.DefaultMetricTarget
	config := e2espec.NewDecrementingHPAConfig(targetValue).WithSinglePodScalingPolicy()

	hpaManager := e2eutil.MustNewHPAManager(t, targetKube, options, config)
	defer hpaManager.Cleanup()

	// Manually scale up server config to from 1 -> 3
	serverConfigIndex := 0
	testCouchbase := hpaManager.CouchbaseCluster
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, serverConfigIndex, 3, targetKube, testCouchbase)

	// Once rebalance has started, the autoscaler should be in maintenance mode
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	autoscalerName := testCouchbase.Spec.Servers[0].AutoscalerName(testCouchbase.Name)
	e2eutil.MustValidateAutoscaleReadyStatus(t, targetKube, testCouchbase.Namespace, testCouchbase.Name, v1.ConditionFalse)
	e2eutil.MustValidateAutoscalerSize(t, targetKube, testCouchbase.Namespace, autoscalerName, 0)

	// Wait for rebalance complete
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Expecting HPA to scale down cluster
	configName := testCouchbase.Spec.Servers[0].Name

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.AutoscaleDownEvent(testCouchbase, configName, 3, 2), 2*time.Minute)

	// Wait for rebalance after scale down
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 2*time.Minute)

	// expected events for scaling up
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventAutoscalerCreated},
		e2eutil.ClusterScaleUpSequence(2),
		eventschema.Event{Reason: k8sutil.EventAutoscaleDown},
		e2eutil.ClusterScaleDownSequence(1),
	}
	ValidateEvents(t, targetKube, hpaManager.CouchbaseCluster, expectedEvents)
}
