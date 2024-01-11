package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
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

	framework.Requires(t, kubernetesCluster).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage).Generate(kubernetesCluster)

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)

	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetesCluster, cluster, 5)

	// Verify the CM exists
	mustGetCNGConfigMap(t, kubernetesCluster, cluster)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetesCluster, cluster, expectedEvents)
}

// TestCNGBucketOps tests CNG bucket operations.
func TestCNGBucketOps(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetesCluster).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

	ctx := context.Background()

	client, err := setupCNGTests(ctx, t, kubernetesCluster)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// create bucket
	var createRAMQuota uint64 = 100

	var createNumReplica uint32 = 1

	// Attempt to create a bucket
	_, err = client.BucketV1().CreateBucket(ctx, &admin_bucket_v1.CreateBucketRequest{
		BucketName:  "my-bucket",
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
	if !compareBucketNames("my-bucket", response) {
		e2eutil.Die(t, fmt.Errorf("bucket name does not match expected bucket name"))
	}

	// upadate bucket
	var updatedRAMQuota uint64 = 150

	var updatedNumReplica uint32 = 1

	flushEnabled := true

	var maxExpirySecs uint32 = 10

	// Attempt to update the bucket.
	_, err = client.BucketV1().UpdateBucket(ctx, &admin_bucket_v1.UpdateBucketRequest{
		BucketName:             "my-bucket",
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

	// Attempt to delete the bucket.
	_, err = client.BucketV1().DeleteBucket(ctx, &admin_bucket_v1.DeleteBucketRequest{BucketName: "my-bucket"})
	if err != nil {
		e2eutil.Die(t, err)
	}

	err = client.Close()
	if err != nil {
		e2eutil.Die(t, fmt.Errorf("error closing routing client: %w", err))
	}
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
	cluster.Spec.Buckets.Managed = false

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)

	e2eutil.MustWaitForCloudNativeGatewayServiceReady(t, kubernetesCluster, cluster, 10*time.Minute)

	return getCloudNativeGatewayClient(ctx, clusterName)
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

// TestCNGLiveConfigReload tests the ability to propagate log level changes to CNG without restarting the pod.
func TestCNGLiveConfigReload(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage).Generate(kubernetesCluster)

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)

	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetesCluster, cluster, 5)

	// Verify the CM exists
	mustGetCNGConfigMap(t, kubernetesCluster, cluster)

	cluster = e2eutil.MustPatchCluster(t, kubernetesCluster, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking/cloudNativeGateway/logLevel", "debug"), time.Minute)
	e2eutil.MustFindLog(t, kubernetesCluster, cluster, k8sutil.CloudNativeGatewayContainerName, "updated log level")

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetesCluster, cluster, expectedEvents)
}

func getCloudNativeGatewayClient(ctx context.Context, clusterName string) (*gocbcoreps.RoutingClient, error) {
	var cngClient *gocbcoreps.RoutingClient

	dialopts := gocbcoreps.DialOptions{
		Username:           constants.CbClusterUsername,
		Password:           constants.CbClusterPassword,
		InsecureSkipVerify: true,
		PoolSize:           1,
	}

	cngSvcName := clusterName + "-cloud-native-gateway-service"
	connStr := fmt.Sprintf("%s.%s.svc.cluster.local:%d", cngSvcName, cluster.Namespace, 443)

	cngConnErr := retryutil.Retry(ctx, 10*time.Minute, func() error {
		var err error
		cngClient, err = gocbcoreps.DialContext(ctx, connStr, &dialopts)
		if err != nil {
			return err
		}

		return nil
	})

	if cngConnErr != nil {
		return nil, cngConnErr
	}

	cngToCBConnErr := retryutil.Retry(ctx, 10*time.Minute, func() error {
		if cngClient.ConnectionState() == gocbcoreps.ConnStateDegraded {
			return fmt.Errorf("CNG container not ready")
		}
		return nil
	})

	if cngToCBConnErr != nil {
		return nil, cngToCBConnErr
	}

	return cngClient, nil
}

func mustGetCNGConfigMap(t *testing.T, kubernetesCluster *types.Cluster, cluster *v2.CouchbaseCluster) {
	configMapName := k8sutil.GetCNGConfigMapName(cluster)

	err := retryutil.RetryFor(1*time.Minute, func() error {
		_, k8sErr := kubernetesCluster.KubeClient.CoreV1().ConfigMaps(kubernetesCluster.Namespace).Get(context.Background(), configMapName, metav1.GetOptions{})
		if k8sErr == nil {
			return nil
		}

		return fmt.Errorf("%s configmap not found for cluster: %s", configMapName, cluster.Name)
	})

	if err != nil {
		e2eutil.Die(t, err)
	}
}
