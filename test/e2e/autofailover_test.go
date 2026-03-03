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
	"sort"
	"strconv"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create 3 server groups.
// Failover all pods in selected server group.
// This should trigger server group failover in the cluster.
func TestServerGroupAutoFailover(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	recoveryPolicy := couchbasev2.PrioritizeUptime

	framework.Requires(t, kubernetes).StaticCluster().ServerGroups(3)

	availableServerGroupList := getAvailabilityZones(t, kubernetes)

	// Create cluster spec for RZA feature.  Size the cluster such that
	// there are 2 nodes per available server group/AZ.
	clusterSize := len(availableServerGroupList) * 2

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(10)
	cluster.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	cluster.Spec.ClusterSettings.AutoFailoverServerGroup = true
	cluster.Spec.RecoveryPolicy = &recoveryPolicy
	cluster.Spec.ServerGroups = availableServerGroupList
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroupList)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	victimGroup := 0
	victims := []int{}

	for i := 0; i < clusterSize; i++ {
		if i%len(availableServerGroupList) == victimGroup {
			victims = append(victims, i)
		}
	}

	// Loop to kill the nodes
	for _, podMemberToKill := range victims {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, podMemberToKill, true)
	}

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
		eventschema.Optional{Validator: eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMultiNodeAutoFailover tests Couchbase's ability to handle multiple failover
// events in series, and the Operator's ability to recover from it.
func TestMultiNodeAutoFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 9
	victims := []int{2, 3, 4}
	victimCount := len(victims)
	autoFailoverTimeout := 10 * time.Second

	e2eutil.MustNewBucket(t, kubernetes, e2espec.DefaultBucketThreeReplicas())
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = &metav1.Duration{Duration: autoFailoverTimeout}
	cluster.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, e2espec.DefaultBucketThreeReplicas(), time.Minute)

	// When ready, kill the victim nodes, waiting long enough for server to perform
	// auto failover, then expect recovery.  Yes to test this we have to be not running
	// in order for the condition to manifest.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	for _, victim := range victims {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim, true)
		time.Sleep(2 * autoFailoverTimeout)
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Nodes go down, and failover
	// * New members balanced in, killed members removed
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Optional{
			Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
		},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestServicesRunningAfterFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	scopeName := "_default"
	collectionName := "_default"

	// Static configuration.
	clusterSize := 4
	victims := []int{0}
	autoFailoverTimeout := 10 * time.Second

	framework.Requires(t, kubernetes).CouchbaseBucket()

	bucket1 := e2espec.DefaultBucketThreeReplicas()
	bucket1.SetName("bucket1")
	bucket1.Spec.CompressionMode = couchbasev2.CouchbaseBucketCompressionModePassive
	bucket1.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(100)

	bucket2 := e2espec.DefaultBucketThreeReplicas()
	bucket2.SetName("bucket2")
	bucket2.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(100)
	bucket2.Spec.CompressionMode = couchbasev2.CouchbaseBucketCompressionModePassive

	bucket3 := e2espec.DefaultBucketThreeReplicas()
	bucket3.SetName("bucket3")
	bucket3.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(100)
	bucket2.Spec.CompressionMode = couchbasev2.CouchbaseBucketCompressionModePassive

	e2eutil.MustNewBucket(t, kubernetes, bucket1)
	e2eutil.MustNewBucket(t, kubernetes, bucket2)
	e2eutil.MustNewBucket(t, kubernetes, bucket3)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.EventingService, couchbasev2.SearchService, couchbasev2.AnalyticsService)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(300)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = &metav1.Duration{Duration: autoFailoverTimeout}
	cluster.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket1, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket2, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket3, time.Minute)

	// When ready, kill the victim nodes, waiting long enough for server to perform
	// auto failover, then expect recovery.  Yes to test this we have to be not running
	// in order for the condition to manifest.

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	for _, victim := range victims {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim, true)
		time.Sleep(2 * autoFailoverTimeout)
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, time.Minute)

	// Couchbase Cluster Instance.
	host, sdkCleanup := e2eutil.MustGetCouchbaseClientSDK(t, kubernetes, cluster)
	defer sdkCleanup()

	docIDInDestinationBucket := "test value"

	// function is a basic test eventing function that populates a destination
	// bucket with a test value when a document appears in a source bucket.  It
	// deletes it when the corresponding source document is deleted.
	eventFunction := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"` + docIDInDestinationBucket + `\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	// When ready, deploy an eventing function to create documents in a destination
	// bucket based on source bucket documents. Populate the source and ensure the
	// documents appear in the destination.
	docsToCreate := 3

	e2eutil.MustDeployEventingFunction(t, cluster, "test", bucket1.Name, bucket3.Name, bucket2.Name, eventFunction, time.Minute)
	e2eutil.NewDocumentSet(bucket1.GetName(), docsToCreate).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket2.Name, docsToCreate, time.Minute)

	// Test adding and deleting a secondary index
	orgIndexesLength := len(e2eutil.MustGetAllIndexes(t, host.QueryIndexes(), bucket1.GetName()))
	e2eutil.MustCreateSecondaryIndex(t, host.QueryIndexes(), bucket1.GetName())

	indexesLength := len(e2eutil.MustGetAllIndexes(t, host.QueryIndexes(), bucket1.GetName()))
	if indexesLength != orgIndexesLength+1 {
		e2eutil.Die(t, fmt.Errorf("The was a problem adding a secondary index. Expected: %s indexes Found: %s", strconv.Itoa(orgIndexesLength+1), strconv.Itoa(indexesLength)))
	}

	e2eutil.MustDropSecondaryIndex(t, host.QueryIndexes(), bucket1.GetName())

	indexesLength = len(e2eutil.MustGetAllIndexes(t, host.QueryIndexes(), bucket1.GetName()))
	if indexesLength != orgIndexesLength {
		e2eutil.Die(t, fmt.Errorf("The was a problem deleting a secondary index. Expected: %s indexes Found: %s", strconv.Itoa(orgIndexesLength), strconv.Itoa(indexesLength)))
	}

	// Test FTS indexing
	searchManager := host.SearchIndexes()
	searchIndex := e2eutil.NewFTSIndex(bucket1.GetName())

	query := "SELECT COUNT(*) FROM `" + bucket1.GetName() + "`." + scopeName + "." + collectionName + ` USE INDEX(USING FTS) where key1="dummyVal1";`
	// create FTS Index and execute a query against it.
	e2eutil.MustExecuteFTSOps(t, searchIndex, searchManager, host, query)

	// Test basic N1QL queries
	// Create a primary index for the default scope/collection using N1QL
	query = "CREATE PRIMARY INDEX ON `" + bucket2.GetName() + "`." + scopeName + "." + collectionName + ";"
	e2eutil.MustExecuteN1qlQuery(t, host, query)
	query = "SELECT * FROM " + bucket2.GetName() + "." + scopeName + "." + collectionName + ";"
	documentsInBucket := e2eutil.MustExecuteN1qlQuery(t, host, query)

	var result map[string]string

	for documentsInBucket.Next() {
		err := documentsInBucket.Row(&result)
		if err != nil {
			e2eutil.Die(t, fmt.Errorf("There was an error with the N1Q1 query to get the docs from the bucket: %w", err))
		}

		if result["_default"] != docIDInDestinationBucket {
			e2eutil.Die(t, fmt.Errorf("Did not find the exepcted document from the N1QL query. Expected:%s Found:%s", docIDInDestinationBucket, result["_default"]))
		}
	}
	documentsInBucket.Close()

	analyticsDataset1 := "analyticsDataset1"
	query = "CREATE DATASET " + analyticsDataset1 + " ON `" + bucket1.GetName() + "`"
	e2eutil.MustExecuteAnalyticsQuery(t, kubernetes, cluster, query, time.Minute)
	time.Sleep(time.Minute) // let analytics catch up

	datasetItemCount := e2eutil.MustGetDatasetItemCount(t, kubernetes, cluster, analyticsDataset1, time.Minute)
	if datasetItemCount != int64(docsToCreate) {
		e2eutil.Die(t, fmt.Errorf(`The number of analytic datasets is not as expecetd
								   This could be because there is a problem with the analytics service after a failover.
								   Expected:%s  Actual:%s`, strconv.Itoa(docsToCreate), strconv.Itoa(int(datasetItemCount))))
	}
}
