package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
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
}

// Create cluster with Analytics service enabled
// Deploy analytics bucket and verify for bucket creation and data replication.
func TestAnalyticsCreateDataSet(t *testing.T) {
	skipAnalytics(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3
	numOfDocs := 50
	analyticsDataset := "testDataset1"
	queries := []string{
		"CREATE DATASET " + analyticsDataset + " ON `default`",
		"CONNECT LINK Local",
	}

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, insert documents into our bucket.  Create a dataset and link to our bucket.
	// Verify the number of documents in the dataset match those in the bucket.
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 0, numOfDocs)

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, targetKube, testCouchbase, query, time.Minute)
	}

	time.Sleep(time.Minute) // let analytics catch up
	itemCount := e2eutil.MustGetItemCount(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, time.Minute)
	datasetItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset, time.Minute)

	if datasetItemCount != itemCount {
		e2eutil.Die(t, fmt.Errorf("dataset item mismatch %v/%v", datasetItemCount, itemCount))
	}

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create analytics enabled couchbase cluster.
// Create analytics bucket with data sets and connect with couchbase bucket.
// Resize cluster with analytics nodes and check for data and functional consistency.
func TestAnalyticsResizeCluster(t *testing.T) {
	skipAnalytics(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready generate workload and resize the cluster.  We expect the number of
	// documents in dataset1 to equal those in the bucket, and those in datasets
	// 2 and 3 to equal those in the bucket.
	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, targetKube, testCouchbase, query, time.Minute)
	}

	stop := e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)
	defer stop()

	for _, newClusterSize := range []int{2, 3, 2, 1} {
		testCouchbase = e2eutil.MustResizeCluster(t, 0, newClusterSize, targetKube, testCouchbase, 20*time.Minute)
	}

	stop()

	time.Sleep(time.Minute) // let analytics catch up
	itemCount := e2eutil.MustGetItemCount(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, time.Minute)
	dataset1ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset1, time.Minute)
	dataset2ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset2, time.Minute)
	dataset3ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset3, time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy analytics enabled couchbase cluster and populate data.
// Kill analytics enabled node and check the cluster status.
func TestAnalyticsKillPods(t *testing.T) {
	skipAnalytics(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready start generating workload and kill some pods.  We expect the number of
	// documents in dataset1 to equal those in the bucket, and those in datasets
	// 2 and 3 to equal those in the bucket.
	stop := e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)
	defer stop()

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, targetKube, testCouchbase, query, time.Minute)
	}

	for _, victim := range []int{0, 1, 2} {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, true)
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)
	}

	stop()

	time.Sleep(time.Minute) // let analytics catch up
	itemCount := e2eutil.MustGetItemCount(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, time.Minute)
	dataset1ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset1, time.Minute)
	dataset2ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset2, time.Minute)
	dataset3ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset3, time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy analytics enabled couchbase cluster over PVC and populate data.
// Kill analytics enabled node and check the cluster and PVC status.
// Kill all analytics nodes at once and check for node recovery.
func TestAnalyticsKillPodsWithPVC(t *testing.T) {
	skipAnalytics(t)

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

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
	pvcName := "test"

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	pvcTemplate := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, couchbasev2.AnalyticsService)
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		pvcTemplate,
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready start generating workload and kill some pods.  We expect the number of
	// documents in dataset1 to equal those in the bucket, and those in datasets
	// 2 and 3 to equal those in the bucket.
	stop := e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)
	defer stop()

	for _, query := range queries {
		e2eutil.MustExecuteAnalyticsQuery(t, targetKube, testCouchbase, query, time.Minute)
	}

	for _, victim := range []int{0, 1, 2} {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, false)
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)
	}

	stop()

	time.Sleep(time.Minute) // let analytics catch up
	itemCount := e2eutil.MustGetItemCount(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, time.Minute)
	dataset1ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset1, time.Minute)
	dataset2ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset2, time.Minute)
	dataset3ItemCount := e2eutil.MustGetDatasetItemCount(t, targetKube, testCouchbase, analyticsDataset3, time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
