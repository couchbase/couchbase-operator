/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// skipAnalytics doesn't run analytics tests on Couchbase 5.x.x as the feature is
// beta and all the syntax changed between then and GA.
func skipAnalytics(t *testing.T) {
	f := framework.Global

	versionStr, err := k8sutil.CouchbaseVersion(f.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}

	version, err := couchbaseutil.NewVersion(versionStr)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if version.Major() < 6 {
		t.Skip("beta feature unsupported")
	}

	if f.BucketType == "memcached" {
		t.Skip("Bucket type not supported")
	}

	if version.Semver() == "6.5.0" && f.EnableIstio {
		t.Skip("Analytics broken on 6.5.0 with Istio")
	}
}

func TestAnalyticsServiceSettings(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipAnalytics(t)

	clusterSize := 3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	numReplicas := 3
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/analytics", &couchbasev2.CouchbaseClusterAnalyticSettings{NumReplicas: &numReplicas}), time.Minute)
	e2eutil.MustVerifyAnalyticsServiceSettings(t, kubernetes, cluster, &numReplicas, 2*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Settings updated
	// * Rebalance so the new setting takes effect (Handled by cb server)
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create cluster with Analytics service enabled
// Deploy analytics bucket and verify for bucket creation and data replication.
func TestAnalyticsCreateDataSet(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipAnalytics(t)

	// Static configuration.
	clusterSize := 3
	numOfDocs := f.DocsCount
	analyticsDataset := "testDataset1"
	queries := []string{
		"CREATE DATASET " + analyticsDataset + " ON `default`",
		"CONNECT LINK Local",
	}

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// When ready, insert documents into our bucket.  Create a dataset and link to our bucket.
	// Verify the number of documents in the dataset match those in the bucket.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, kubernetes, cluster, query, time.Minute)
	}

	time.Sleep(time.Minute) // let analytics catch up
	itemCount := e2eutil.MustGetItemCount(t, kubernetes, cluster, bucket.GetName(), time.Minute)
	datasetItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset, time.Minute)

	if datasetItemCount != itemCount {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v/%v", datasetItemCount, itemCount))
	}

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create analytics enabled couchbase cluster.
// Create analytics bucket with data sets and connect with couchbase bucket.
// Resize cluster with analytics nodes and check for data and functional consistency.
func TestAnalyticsResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipAnalytics(t)

	// Static configuration.
	clusterSize := 1
	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"
	queries := []string{
		"CREATE DATASET " + analyticsDataset1 + " ON `default`",
		"CREATE DATASET " + analyticsDataset2 + " ON `default` WHERE meta(default).id LIKE '%1%'",
		"CREATE DATASET " + analyticsDataset3 + " ON `default` WHERE meta(default).id NOT LIKE '%1%'",
		"CONNECT LINK Local",
	}

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// When ready generate workload and resize the cluster.  We expect the number of
	// documents in dataset1 to equal those in the bucket, and those in datasets
	// 2 and 3 to equal those in the bucket.
	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, kubernetes, cluster, query, time.Minute)
	}

	stop := e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())
	defer stop()

	for _, newClusterSize := range []int{2, 3, 2, 1} {
		cluster = e2eutil.MustResizeCluster(t, 0, newClusterSize, kubernetes, cluster, 20*time.Minute)
	}

	stop()

	time.Sleep(time.Minute) // let analytics catch up
	itemCount := e2eutil.MustGetItemCount(t, kubernetes, cluster, bucket.GetName(), time.Minute)
	dataset1ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset1, time.Minute)
	dataset2ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset2, time.Minute)
	dataset3ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset3, time.Minute)

	if dataset1ItemCount != itemCount {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v/%v", dataset1ItemCount, itemCount))
	}

	if dataset2ItemCount+dataset3ItemCount != itemCount {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v+%v/%v", dataset2ItemCount, dataset3ItemCount, itemCount))
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scales from 1 -> 2
	// * Cluster scales from 2 -> 3
	// * Cluster scales from 3 -> 2
	// * Cluster scales from 2 -> 1
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Deploy analytics enabled couchbase cluster and populate data.
// Kill analytics enabled node and check the cluster status.
func TestAnalyticsKillPods(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipAnalytics(t)

	// Static configuration.
	clusterSize := 3
	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"
	queries := []string{
		"CREATE DATASET " + analyticsDataset1 + " ON `default`",
		"CREATE DATASET " + analyticsDataset2 + " ON `default` WHERE meta(default).id LIKE '%1%'",
		"CREATE DATASET " + analyticsDataset3 + " ON `default` WHERE meta(default).id NOT LIKE '%1%'",
		"CONNECT LINK Local",
	}

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// When ready start generating workload and kill some pods.  We expect the number of
	// documents in dataset1 to equal those in the bucket, and those in datasets
	// 2 and 3 to equal those in the bucket.
	stop := e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())
	defer stop()

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, kubernetes, cluster, query, time.Minute)
	}

	for _, victim := range []int{0, 1, 2} {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim, true)
		e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	}

	stop()

	time.Sleep(time.Minute) // let analytics catch up
	itemCount := e2eutil.MustGetItemCount(t, kubernetes, cluster, bucket.GetName(), time.Minute)
	dataset1ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset1, time.Minute)
	dataset2ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset2, time.Minute)
	dataset3ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset3, time.Minute)

	if dataset1ItemCount != itemCount {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v/%v", dataset1ItemCount, itemCount))
	}

	if dataset2ItemCount+dataset3ItemCount != itemCount {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v+%v/%v", dataset2ItemCount, dataset3ItemCount, itemCount))
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Pod goes down, fails over and recovers N times
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{
			Times:     3,
			Validator: e2eutil.PodDownFailoverRecoverySequence(),
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Deploy analytics enabled couchbase cluster over PVC and populate data.
// Kill analytics enabled node and check the cluster and PVC status.
// Kill all analytics nodes at once and check for node recovery.
func TestAnalyticsKillPodsWithPVC(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	skipAnalytics(t)

	// Static configuration.
	clusterSize := 3
	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"
	queries := []string{
		"CREATE DATASET " + analyticsDataset1 + " ON `default`",
		"CREATE DATASET " + analyticsDataset2 + " ON `default` WHERE meta(default).id LIKE '%1%'",
		"CREATE DATASET " + analyticsDataset3 + " ON `default` WHERE meta(default).id NOT LIKE '%1%'",
		"CONNECT LINK Local",
	}

	// PV Configuration
	pvcName := e2eutil.GetPvcName(f.LocalPV)

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	cluster.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
	}
	cluster.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, f.LocalPV, 2),
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// When ready start generating workload and kill some pods.  We expect the number of
	// documents in dataset1 to equal those in the bucket, and those in datasets
	// 2 and 3 to equal those in the bucket.
	stop := e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())
	defer stop()

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, kubernetes, cluster, query, time.Minute)
	}

	for _, victim := range []int{0, 1, 2} {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim, false)
		e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	}

	stop()

	time.Sleep(time.Minute) // let analytics catch up
	itemCount := e2eutil.MustGetItemCount(t, kubernetes, cluster, bucket.GetName(), time.Minute)
	dataset1ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset1, time.Minute)
	dataset2ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset2, time.Minute)
	dataset3ItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset3, time.Minute)

	if dataset1ItemCount != itemCount {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v/%v", dataset1ItemCount, itemCount))
	}

	if dataset2ItemCount+dataset3ItemCount != itemCount {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v+%v/%v", dataset2ItemCount, dataset3ItemCount, itemCount))
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Pod goes down and recovers N times
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{
			Times:     3,
			Validator: e2eutil.PodDownFailedWithPVCRecoverySequence(1),
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create cluster with Analytics service enabled
// Deploy analytics bucket and verify for bucket creation and data replication.
// Analytics Datasets cannot be created on Scope only on Collectiions.
func TestAnalyticsCreateDataSetWithCollections(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	skipAnalytics(t)

	// Static configuration.
	clusterSize := 1
	numOfDocs := f.DocsCount

	scopeName := "pinky"
	collectionName := "brain"

	analyticsDataset := "testDataset1"
	queries := []string{
		"CREATE DATASET " + analyticsDataset + " ON `default`" + "." + scopeName + "." + collectionName,
		"CONNECT LINK Local",
	}

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs in the created scope.collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName, numOfDocs, 10*time.Minute)

	// insert docs in the default_scope.default_collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), 2*numOfDocs, 2*time.Minute)

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, kubernetes, cluster, query, time.Minute)
	}

	time.Sleep(time.Minute) // let analytics catch up
	// Verify dataset itemCount.
	e2eutil.MustVerifyDatasetItemCount(t, kubernetes, cluster, analyticsDataset, int64(numOfDocs), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
