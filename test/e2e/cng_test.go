package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/gocbcoreps"
	"github.com/couchbase/goprotostellar/genproto/admin_bucket_v1"
	"github.com/couchbase/goprotostellar/genproto/kv_v1"
	v1 "k8s.io/api/core/v1"
)

var cluster *v2.CouchbaseCluster

// TestCreateCNG tests the ability to create a three node cluster with CNG enabled.
func TestCreateCNG(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion("7.2.0")

	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage).Generate(kubernetesCluster)

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)

	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetesCluster, cluster, 5)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetesCluster, cluster, expectedEvents)
}

// TestDeleteCNGBucket tests deleting a bucket via CNG.
func TestDeleteCNGBucket(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion("7.2.0")

	defer cleanup()

	ctx := context.Background()

	client := setupTests(ctx, t, kubernetesCluster)

	// Create a default bucket
	bucket := e2espec.DefaultBucket()

	e2eutil.MustNewBucket(t, kubernetesCluster, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetesCluster, cluster, bucket, 2*time.Minute)

	// Attempt to delete the bucket
	deleteResponse, err := client.BucketV1().DeleteBucket(ctx, &admin_bucket_v1.DeleteBucketRequest{BucketName: "default"})

	if err != nil {
		t.Log(deleteResponse)
		t.Log(err)
		e2eutil.Die(t, err)
	}

	listResponse, err := client.BucketV1().ListBuckets(ctx, &admin_bucket_v1.ListBucketsRequest{})

	if err != nil {
		e2eutil.Die(t, err)
	}

	// Check if the bucket exists
	for _, bucket := range listResponse.Buckets {
		if strings.Compare(bucket.BucketName, "default") == 0 {
			e2eutil.Die(t, fmt.Errorf("bucket still exists"))
		}
	}

	closeErr := client.Close()

	if closeErr != nil {
		e2eutil.Die(t, closeErr)
	}
}

// TestUpdateCNGBucket tests updating a bucket via CNG.
func TestUpdateCNGBucket(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion("7.2.0")

	defer cleanup()

	ctx := context.Background()

	client := setupTests(ctx, t, kubernetesCluster)

	// Create a default bucket
	bucket := e2espec.DefaultBucket()

	// Default bucket RAM quota
	var ramQuota int64 = 104857600

	bucket.Spec.MemoryQuota.Set(ramQuota)

	e2eutil.MustNewBucket(t, kubernetesCluster, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetesCluster, cluster, bucket, 2*time.Minute)

	// Attempt to update the bucket
	response, err := client.BucketV1().UpdateBucket(ctx, &admin_bucket_v1.UpdateBucketRequest{
		BucketName:             "default",
		RamQuotaBytes:          func() *uint64 { var b uint64 = 104857700; return &b }(),
		NumReplicas:            func() *uint32 { var b uint32 = 1; return &b }(),
		FlushEnabled:           func() *bool { b := true; return &b }(),
		ReplicaIndexes:         func() *bool { b := false; return &b }(),
		EvictionMode:           admin_bucket_v1.EvictionMode_EVICTION_MODE_FULL.Enum(),
		MaxExpirySecs:          func() *uint32 { var b uint32 = 10; return &b }(),
		CompressionMode:        admin_bucket_v1.CompressionMode_COMPRESSION_MODE_ACTIVE.Enum(),
		MinimumDurabilityLevel: kv_v1.DurabilityLevel_DURABILITY_LEVEL_MAJORITY.Enum(),
	})

	if err != nil {
		t.Log(response)
		e2eutil.Die(t, err)
	}

	newTest, err := client.BucketV1().ListBuckets(ctx, &admin_bucket_v1.ListBucketsRequest{})

	if err != nil {
		t.Log(newTest)
		e2eutil.Die(t, err)
	}

	// Check if the bucket RAM quota has been updated
	for _, bucket := range newTest.Buckets {
		if ramQuota == int64(bucket.RamQuotaBytes) {
			if bucket.RamQuotaBytes == uint64(ramQuota) {
				e2eutil.Die(t, fmt.Errorf("bucket did not update"))
			}
		}
	}

	closeErr := client.Close()

	if closeErr != nil {
		e2eutil.Die(t, closeErr)
	}
}

// TestGetCNGBucket tests getting a list of buckets from CNG.
func TestGetCNGBucket(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion("7.2.0")

	ctx := context.Background()

	client := setupTests(ctx, t, kubernetesCluster)

	defer cleanup()

	// Create a test bucket
	bucket := e2espec.DefaultBucket()

	e2eutil.MustNewBucket(t, kubernetesCluster, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetesCluster, cluster, bucket, 2*time.Minute)

	// Get a list of the buckets
	response, err := client.BucketV1().ListBuckets(ctx, &admin_bucket_v1.ListBucketsRequest{})

	if err != nil {
		e2eutil.Die(t, err)
	}

	bucketName := "default"

	if compareBucketNames(bucketName, response) {
		e2eutil.Die(t, fmt.Errorf("bucket name does not match expected bucket name"))
	}

	closeErr := client.Close()

	if closeErr != nil {
		t.Log(closeErr)
	}
}

// TestCreateCNGBucket tests creating a bucket via CNG.
func TestCreateCNGBucket(t *testing.T) {
	ctx := context.Background()

	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion("7.2.0")

	client := setupTests(ctx, t, kubernetesCluster)

	bucketName := "Test-123"

	defer cleanup()

	// Attempt to create a bucket
	output, err := client.BucketV1().CreateBucket(ctx, &admin_bucket_v1.CreateBucketRequest{
		BucketName:    bucketName,
		BucketType:    admin_bucket_v1.BucketType(0),
		RamQuotaBytes: 100,
		NumReplicas:   1,
	})

	if err != nil {
		t.Log(output)
		t.Log(err)
	}

	response, err := client.BucketV1().ListBuckets(ctx, &admin_bucket_v1.ListBucketsRequest{})

	if err != nil {
		e2eutil.Die(t, err)
	}

	// Check if the bucket exists
	if compareBucketNames(bucketName, response) {
		e2eutil.Die(t, fmt.Errorf("bucket name does not match expected bucket name"))
	}

	closeErr := client.Close()

	if closeErr != nil {
		t.Log(closeErr)
	}
}

// compareBucketNames checks if a ListBucketsResponse contains the bucket we expect.
func compareBucketNames(bucketName string, response *admin_bucket_v1.ListBucketsResponse) bool {
	testFailed := true

	for _, bucket := range response.Buckets {
		if strings.Compare(bucket.BucketName, bucketName) == 0 {
			testFailed = false
		}
	}

	return testFailed
}

// setupTests performs the basic setup for all CNG tests.
func setupTests(ctx context.Context, t *testing.T, kubernetesCluster *types.Cluster) *gocbcoreps.RoutingClient {
	// Static configuration.
	clusterSize := 3

	// Create the cluster spec
	cluster = clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage).Generate(kubernetesCluster)

	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	cluster.Name = clusterName

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)

	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetesCluster, cluster, 5)

	dialopts := gocbcoreps.DialOptions{
		Username:           "Administrator",
		Password:           "password",
		InsecureSkipVerify: true,
		PoolSize:           1,
	}

	ip, port, err := k8sutil.GetClusterCNGService(kubernetesCluster, cluster, v1.ServiceTypeClusterIP, 2*time.Minute)

	if len(port) == 0 || err != nil {
		e2eutil.Die(t, err)
	}

	client, err := gocbcoreps.DialContext(ctx, ip+":443", &dialopts)

	if err != nil {
		e2eutil.Die(t, err)
	}

	connectionErr := retryutil.RetryFor(10*time.Minute, func() error {
		if client.ConnectionState() != gocbcoreps.ConnStateOnline {
			return fmt.Errorf("CNG container not ready")
		}
		return nil
	})

	if connectionErr != nil {
		e2eutil.Die(t, connectionErr)
	}

	return client
}
