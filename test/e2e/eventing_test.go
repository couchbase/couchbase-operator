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
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	bucketNames = []string{
		sourceBucket.Name,
		destinationBucket.Name,
		metadataBucket.Name,
	}

	sourceBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "source",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(100),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	destinationBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "destination",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(100),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	metadataBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "metadata",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(100),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}
)

var (
	// dataServiceMemoryQuota is enough memory to support the bucket requirements.
	dataServiceMemoryQuota = e2espec.NewResourceQuantityMi(300)
)

const (
	// function is a basic test eventing function that populates a destination
	// bucket with a test value when a document appears in a source bucket.  It
	// deletes it when the corresponding source document is deleted.
	function = `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`
)

// Create eventing enabled cluster
// Create 3 buckets for eventing to work
// Deploy eventing function to verify the results in destination bucket.
func TestEventingCreateEventingCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	numOfDocs := f.DocsCount

	// Create the cluster.
	e2eutil.MustNewBucket(t, kubernetes, sourceBucket)
	e2eutil.MustNewBucket(t, kubernetes, destinationBucket)
	e2eutil.MustNewBucket(t, kubernetes, metadataBucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.EventingService)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = dataServiceMemoryQuota
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketsExist(t, kubernetes, cluster, bucketNames, time.Minute)

	// When ready, deploy an eventing function to create documents in a destination
	// bucket based on source bucket documents. Populate the source and ensure the
	// documents appear in the destination.
	e2eutil.MustDeployEventingFunction(t, kubernetes, cluster, "test", sourceBucket.Name, metadataBucket.Name, destinationBucket.Name, function, time.Minute)
	e2eutil.NewDocumentSet(sourceBucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, destinationBucket.Name, numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create eventing enabled couchbase cluster with eventing buckets.
// Resize the cluster when the eventing deployment is active and verify the results.
func TestEventingResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, kubernetes, sourceBucket)
	e2eutil.MustNewBucket(t, kubernetes, destinationBucket)
	e2eutil.MustNewBucket(t, kubernetes, metadataBucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.EventingService)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = dataServiceMemoryQuota
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketsExist(t, kubernetes, cluster, bucketNames, time.Minute)

	// When ready deploy the eventing function and generate workload.  Scale the cluster
	// up and down then stop the workload.  Expect the number of documents in the destination
	// bucket to equal those in the source.
	e2eutil.MustDeployEventingFunction(t, kubernetes, cluster, "test", sourceBucket.Name, metadataBucket.Name, destinationBucket.Name, function, time.Minute)

	stop := e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, sourceBucket.Name)
	defer stop()

	for _, newClusterSize := range []int{2, 3, 2} {
		cluster = e2eutil.MustResizeCluster(t, 0, newClusterSize, kubernetes, cluster, 20*time.Minute)
	}

	stop()
	time.Sleep(time.Minute) // Wait for eventing to catch up

	itemCount := e2eutil.MustGetItemCount(t, kubernetes, cluster, sourceBucket.Name, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, destinationBucket.Name, int(itemCount), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets created
	// * Cluster scales from 1 -> 2
	// * Cluster scales from 2 -> 3
	// * Cluster scales from 3 -> 2
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create couchbase cluster with eventing service and required buckets.
// Kill all the eventing nodes one by one and check for eventing stability.
func TestEventingKillEventingPods(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	e2eutil.MustNewBucket(t, kubernetes, sourceBucket)
	e2eutil.MustNewBucket(t, kubernetes, destinationBucket)
	e2eutil.MustNewBucket(t, kubernetes, metadataBucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.EventingService)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = dataServiceMemoryQuota
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketsExist(t, kubernetes, cluster, bucketNames, time.Minute)

	// When ready deploy the eventing function and generate workload.  Kill pods in sequence
	// then stop the workload.  Expect the number of documents in the destination bucket to
	// equal those in the source.
	e2eutil.MustDeployEventingFunction(t, kubernetes, cluster, "test", sourceBucket.Name, metadataBucket.Name, destinationBucket.Name, function, time.Minute)

	stop := e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, sourceBucket.Name)
	defer stop()

	for _, victim := range []int{0, 1, 2} {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim, true)
		e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 20*time.Minute)
	}

	stop()
	time.Sleep(time.Minute) // Wait for eventing to catch up

	itemCount := e2eutil.MustGetItemCount(t, kubernetes, cluster, sourceBucket.Name, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, destinationBucket.Name, int(itemCount), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster goes down, fails over and recovers N times
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Repeat{Times: 3, Validator: e2eutil.PodDownFailoverRecoverySequence()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
