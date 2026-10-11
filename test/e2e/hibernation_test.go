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
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	utilconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestHibernateEphemeralImmediate tests the somewhat pointless killing
// of an ephemeral cluster and the restore.
func TestHibernateEphemeralImmediate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	hibernationStrategy := couchbasev2.ImmediateHibernation

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.HibernationStrategy = &hibernationStrategy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Hibernate the cluster, wait for the hibernating status then allow recovery.
	patchset := jsonpatch.NewPatchSet().Add("/spec/hibernate", true)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, time.Minute)

	patchset = jsonpatch.NewPatchSet().Remove("/spec/hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster entered hibernation
	// * Cluster left hibernation
	// * Cluster recreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}}},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationStarted},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationEnded},
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestHibernateSupportableImmediate tests that a supportable cluster
// can be hibernated and brought back.
func TestHibernateSupportableImmediate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	hibernationStrategy := couchbasev2.ImmediateHibernation
	recoveryStrategy := couchbasev2.PrioritizeUptime

	// Create the cluster.
	cluster := clusterOptions().WithMixedTopology(mdsGroupSize).Generate(kubernetes)
	cluster.Spec.HibernationStrategy = &hibernationStrategy
	cluster.Spec.RecoveryPolicy = &recoveryStrategy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Hibernate the cluster, wait for the hibernating status then allow recovery.
	patchset := jsonpatch.NewPatchSet().Add("/spec/hibernate", true)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, 3*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, 3*time.Minute)

	patchset = jsonpatch.NewPatchSet().Remove("/spec/hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster entered hibernation
	// * Cluster left hibernation
	// * Cluster recovered
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}}},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationStarted},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationEnded},
		e2eutil.PodDownWithPVCRecoverySequenceWithEphemeral(t, clusterSize, mdsGroupSize, mdsGroupSize, f.CouchbaseServerImage),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestHibernateOccursAfterUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := 3
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Create the cluster.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Upgrade the cluster to the new version.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the cluster to start upgrading then enable hibernation.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)

	patchset := jsonpatch.NewPatchSet().Add("/spec/hibernate", true)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)

	// Expect the cluster to enter hibernation at some point.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, 10*time.Minute)

	// Disable hibernation and expect the cluster to recover back to the upgraded version.
	patchset = jsonpatch.NewPatchSet().Remove("/spec/hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Validate the events match what we expect:
	// * Cluster created
	// * Cluster upgraded
	// * Cluster entered hibernation
	// * Cluster left hibernation
	// * Cluster recreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		RollingUpgradeSequence(clusterSize, 1),
		eventschema.Optional{Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}}},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationStarted},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationEnded},
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestHibernateNewCluster tests that a cluster created with hibernation enabled goes straight
// into hibernation without being built, and is built normally once hibernation is turned off.
func TestHibernateNewCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster already hibernated.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Hibernate = true
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, 2*time.Minute)

	// Nothing should be built while it's hibernated, give the operator time to do so if it was going to.
	time.Sleep(time.Minute)

	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: utilconstants.LabelCluster + "=" + cluster.Name})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if len(pods.Items) != 0 {
		e2eutil.Die(t, fmt.Errorf("expected no pods for a cluster created hibernated, got %d", len(pods.Items)))
	}

	hibernated, err := kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(kubernetes.Namespace).Get(context.Background(), cluster.Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if hibernated.Status.ClusterID != "" {
		e2eutil.Die(t, fmt.Errorf("expected no cluster ID for a cluster created hibernated, got %s", hibernated.Status.ClusterID))
	}

	// Turn hibernation off, the cluster should then be built as normal.
	patchset := jsonpatch.NewPatchSet().Remove("/spec/hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster entered hibernation
	// * Cluster left hibernation
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonHibernationStarted},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationEnded},
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestOfflineUpgradePreviousVersionPodCount upgrades with upgradeProcess
// Offline, holding one pod on the old version, then releases it. Each step must
// be a hibernate/wake on the same pods and volumes: no swaps, no data loss.
func TestOfflineUpgradePreviousVersionPodCount(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	clusterSize := constants.Size3
	numOfDocs := 100
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{UpgradeProcess: couchbasev2.OfflineUpgrade, PreviousVersionPodCount: 1}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)

	podNames := func() []string {
		pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: utilconstants.LabelCluster + "=" + cluster.Name})
		if err != nil {
			e2eutil.Die(t, err)
		}

		names := []string{}
		for _, pod := range pods.Items {
			names = append(names, pod.Name)
		}

		sort.Strings(names)

		return names
	}
	originalPods := podNames()

	// Checks the cluster upgraded offline: settled, data intact, same pods, and
	// the expected number of hibernations.
	mustHaveUpgradedOffline := func(hibernations int, images map[string]int) {
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 15*time.Minute)
		e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 5*time.Minute, couchbasev2.ClusterConditionUpgrading)
		e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, images)
		e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

		if pods := podNames(); !reflect.DeepEqual(pods, originalPods) {
			e2eutil.Die(t, fmt.Errorf("expected pods %v to be reused, got %v", originalPods, pods))
		}

		events, err := e2eutil.GetCouchbaseEvents(kubernetes.KubeClient, cluster)
		if err != nil {
			e2eutil.Die(t, err)
		}

		count := 0
		for _, event := range events {
			if event.Reason == k8sutil.EventReasonHibernationStarted {
				count++
			}
		}

		if count != hibernations {
			e2eutil.Die(t, fmt.Errorf("expected %d hibernations, got %d", hibernations, count))
		}
	}

	// The image change hibernates and wakes all but one pod on the new version.
	// Hibernation lasts one reconcile, so wait for the mixed cluster instead.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionMixedMode, v1.ConditionTrue, cluster, 15*time.Minute)
	mustHaveUpgradedOffline(1, map[string]int{f.CouchbaseServerImageUpgrade: 1, f.CouchbaseServerImage: clusterSize - 1})

	// Releasing the held pod upgrades it with a second hibernation.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 15*time.Minute, couchbasev2.ClusterConditionMixedMode)
	mustHaveUpgradedOffline(2, map[string]int{f.CouchbaseServerImage: clusterSize})
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
}
