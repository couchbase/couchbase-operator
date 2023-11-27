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
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/gocbcoreps"
	"github.com/couchbase/goprotostellar/genproto/admin_bucket_v1"
	"github.com/couchbase/goprotostellar/genproto/kv_v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	podconsts "github.com/couchbase/couchbase-operator/pkg/util/constants"
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

// TestCNGBucketOps tests CNG bucket operations.
func TestCNGBucketOps(t *testing.T) {
	ctx := context.Background()

	kubernetesCluster, cleanup := framework.Global.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion("7.2.0")

	client, err := setupCNGTests(ctx, t, kubernetesCluster)
	if err != nil {
		e2eutil.Die(t, err)
	}

	bucketName := "Test-123"

	defer func(t *testing.T, client *gocbcoreps.RoutingClient) {
		err := client.Close()
		if err != nil {
			e2eutil.Die(t, err)
		}

		cleanup()
	}(t, client)

	t.Run("TestCreateCNGBucket", func(t *testing.T) {
		cleanup := framework.Global.SetupSubTest(t)
		defer cleanup()

		// create bucket
		var createRAMQuota uint64 = 100
		var createNumReplica uint32 = 1

		// Attempt to create a bucket
		_, err := client.BucketV1().CreateBucket(ctx, &admin_bucket_v1.CreateBucketRequest{
			BucketName:  bucketName,
			BucketType:  admin_bucket_v1.BucketType(0),
			RamQuotaMb:  &createRAMQuota,
			NumReplicas: &createNumReplica,
		})
		if err != nil {
			e2eutil.Die(t, err)
		}

		response, err := client.BucketV1().ListBuckets(ctx, &admin_bucket_v1.ListBucketsRequest{})

		if err != nil {
			e2eutil.Die(t, err)
		}

		// Check if the bucket exists
		if !compareBucketNames(bucketName, response) {
			e2eutil.Die(t, fmt.Errorf("bucket name does not match expected bucket name"))
		}
	})

	t.Run("TestUpdateCNGBucket", func(t *testing.T) {
		cleanup := framework.Global.SetupSubTest(t)
		defer cleanup()

		// upadate bucket
		var updatedRAMQuota uint64 = 150
		var updatedNumReplica uint32 = 1
		flushEnabled := true
		var maxExpirySecs uint32 = 10

		// Attempt to update the bucket.
		_, err := client.BucketV1().UpdateBucket(ctx, &admin_bucket_v1.UpdateBucketRequest{
			BucketName:             bucketName,
			RamQuotaMb:             &updatedRAMQuota,
			NumReplicas:            &updatedNumReplica,
			FlushEnabled:           &flushEnabled,
			EvictionMode:           admin_bucket_v1.EvictionMode_EVICTION_MODE_FULL.Enum(),
			MaxExpirySecs:          &maxExpirySecs,
			CompressionMode:        admin_bucket_v1.CompressionMode_COMPRESSION_MODE_ACTIVE.Enum(),
			MinimumDurabilityLevel: kv_v1.DurabilityLevel_DURABILITY_LEVEL_MAJORITY.Enum(),
		})

		if err != nil {
			e2eutil.Die(t, err)
		}

		lb, err := client.BucketV1().ListBuckets(ctx, &admin_bucket_v1.ListBucketsRequest{})
		if err != nil {
			e2eutil.Die(t, err)
		}

		// Check if the bucket RAM quota has been updated.
		for _, bucket := range lb.Buckets {
			if bucket.RamQuotaMb != updatedRAMQuota {
				e2eutil.Die(t, fmt.Errorf("bucket did not update"))
			}
		}
	})

	t.Run("TestDeleteCNGBucket", func(t *testing.T) {
		cleanup := framework.Global.SetupSubTest(t)
		defer cleanup()

		_, err := client.BucketV1().DeleteBucket(ctx, &admin_bucket_v1.DeleteBucketRequest{BucketName: bucketName})
		if err != nil {
			e2eutil.Die(t, err)
		}

		listResponse, err := client.BucketV1().ListBuckets(ctx, &admin_bucket_v1.ListBucketsRequest{})
		if err != nil {
			e2eutil.Die(t, err)
		}

		// Check if the bucket exists
		for _, bucket := range listResponse.Buckets {
			if strings.Compare(bucket.BucketName, bucketName) == 0 {
				e2eutil.Die(t, fmt.Errorf("bucket still exists"))
			}
		}
	})
}

// compareBucketNames checks if a ListBucketsResponse contains the bucket we expect.
func compareBucketNames(bucketName string, response *admin_bucket_v1.ListBucketsResponse) (containsBucket bool) {
	for _, bucket := range response.Buckets {
		if strings.Compare(bucket.BucketName, bucketName) == 0 {
			return true
		}
	}

	return false
}

// setupCNGTests performs the basic setup for all CNG tests.
func setupCNGTests(ctx context.Context, t *testing.T, kubernetesCluster *types.Cluster) (*gocbcoreps.RoutingClient, error) {
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
		Username:           constants.CbClusterUsername,
		Password:           constants.CbClusterPassword,
		InsecureSkipVerify: true,
		PoolSize:           1,
	}

	cngSvcName := clusterName + "-cloud-native-gateway-service"
	connStr := fmt.Sprintf("%s.%s.svc.cluster.local:%d", cngSvcName, cluster.Namespace, 443)

	client, err := gocbcoreps.DialContext(ctx, connStr, &dialopts)
	if err != nil {
		return nil, err
	}

	connectionErr := retryutil.RetryFor(10*time.Minute, func() error {
		if client.ConnectionState() == gocbcoreps.ConnStateDegraded {
			return fmt.Errorf("CNG container not ready")
		}
		return nil
	})

	if connectionErr != nil {
		return nil, connectionErr
	}

	return client, nil
}

func TestCngOtlp(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage).Generate(kubernetes)

	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}

	otelURL := "https://otel:1234"
	cluster.Annotations["cao.couchbase.com/networking.cloudNativeGateway.otlp.endpoint"] = otelURL
	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetes, cluster, -1)

	// get the pod and check the args
	var container v1.Container

	err := retryutil.RetryFor(5*time.Minute, func() error {
		listOptions := metav1.ListOptions{
			LabelSelector: constants.CouchbaseServerClusterKey + "=" + cluster.Name,
		}
		pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), listOptions)
		if err != nil {
			return err
		}

		for _, pod := range pods.Items {
			for _, container = range pod.Spec.Containers {
				if container.Name == k8sutil.CloudNativeGatewayContainerName {
					return nil
				}
			}
		}

		return fmt.Errorf("%s container not found", k8sutil.CloudNativeGatewayContainerName)
	})

	if err != nil {
		e2eutil.Die(t, err)
	}

	for index, arg := range container.Args {
		if arg == podconsts.CloudNativeGatewayOtlpFlag && container.Args[index+1] == "https://otel:1234" {
			return
		}
	}

	e2eutil.Die(t, fmt.Errorf("%s flag not set", podconsts.CloudNativeGatewayOtlpFlag))
}
