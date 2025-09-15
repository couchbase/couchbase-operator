package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
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

// TestCreateCNG tests the ability to create a three node cluster with CNG enabled.
func TestCreateCNG(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(kubernetesCluster)

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)

	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetesCluster, cluster, 5*time.Minute)

	// Verify the CM exists
	e2eutil.MustGetCNGConfigMap(t, kubernetesCluster, cluster)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{
				Reason: k8sutil.EventReasonUserCreated,
			},
		},
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

	client, _, err := setupCNGTests(ctx, t, kubernetesCluster)
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
func setupCNGTests(ctx context.Context, t *testing.T, kubernetesCluster *types.Cluster) (*gocbcoreps.RoutingClient, *couchbasev2.CouchbaseCluster, error) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(kubernetesCluster)

	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	cluster.Name = clusterName
	cluster.Spec.Buckets.Managed = false

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)

	e2eutil.MustWaitForCloudNativeGatewayServiceReady(t, kubernetesCluster, cluster, 10*time.Minute)
	// this is here because the service may be created, but it takes some time (50 seconds or so) for CNG to fully start up
	time.Sleep(90 * time.Second)

	username := string(kubernetesCluster.DefaultSecret.Data["username"])
	password := string(kubernetesCluster.DefaultSecret.Data["password"])
	client, err := e2eutil.MustGetCNGClient(ctx, cluster, clusterName, username, password)

	return client, cluster, err
}

func TestCngOtlp(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(kubernetes)

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
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(kubernetesCluster)

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)

	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetesCluster, cluster, 5*time.Minute)

	// Verify the CM exists
	e2eutil.MustGetCNGConfigMap(t, kubernetesCluster, cluster)

	cluster = e2eutil.MustPatchCluster(t, kubernetesCluster, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking/cloudNativeGateway/logLevel", "debug"), time.Minute)
	e2eutil.MustFindLog(t, kubernetesCluster, cluster, k8sutil.CloudNativeGatewayContainerName, "updated log level")
}

// TestCNGDataAPI tests the ability to configure the data api and proxy services for CNG.
func TestCNGDataAPI(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	ctx := context.Background()

	framework.Requires(t, kubernetesCluster).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(kubernetesCluster)

	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}

	// Create the DAPI config with the mgmt service enabled
	cluster.Annotations["cao.couchbase.com/networking.cloudNativeGateway.dataAPI.enabled"] = "true"
	cluster.Annotations["cao.couchbase.com/networking.cloudNativeGateway.dataAPI.proxyServices"] = "mgmt"

	// We are going to use CNG to create a bucket, so we should disable operator management.
	cluster.Spec.Buckets.Managed = false

	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetesCluster, cluster, 5*time.Minute)
	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetesCluster, cluster, 5*time.Minute)

	// Check the HTTPS CNG service still works by creating a bucket
	username := string(kubernetesCluster.DefaultSecret.Data["username"])
	password := string(kubernetesCluster.DefaultSecret.Data["password"])

	httpClient, err := e2eutil.MustGetCNGClient(ctx, cluster, cluster.GetName(), username, password)

	if err != nil {
		e2eutil.Die(t, err)
	}

	bucketName := "dapiTestBucket"

	e2eutil.MustCreateBasicBucketWithCNGClient(t, ctx, httpClient, bucketName)

	// Create a DAPI client and test with a callerIdentity request
	dClient := e2eutil.NewDAPITestClient(kubernetesCluster, cluster, time.Minute)

	e2eutil.MustCheckCallerIdentityDAPI(t, dClient)

	// Using the DAPI client, check that the mgmt proxy service works correctly
	e2eutil.MustCheckBucketExistsDAPIMgmtService(t, dClient, bucketName)
}

// TestCNGDataAPIConfigChangeRestart tests the ability to change the data api configuration and have the CNG pods restart to use the new config.
func TestCNGDataAPIConfigChangeRestart(t *testing.T) {
	f := framework.Global

	k8sCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, k8sCluster).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

	defer cleanup()

	// Static configuration.
	clusterSize := 2

	// Create the cluster spec
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(k8sCluster)

	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}
	// Create the DAPI config with the mgmt service enabled
	cluster.Annotations["cao.couchbase.com/networking.cloudNativeGateway.dataAPI.enabled"] = "true"
	// Create the cluster
	cluster = e2eutil.CreateNewClusterFromSpec(t, k8sCluster, cluster, 5)
	e2eutil.MustWaitClusterStatusHealthy(t, k8sCluster, cluster, 5*time.Minute)
	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, k8sCluster, cluster, 5*time.Minute)

	// Create a DAPI client and test with a callerIdentity request
	dClient := e2eutil.NewDAPITestClient(k8sCluster, cluster, time.Minute)
	e2eutil.MustCheckCallerIdentityDAPI(t, dClient)

	// Check that the mgmt service is not available
	e2eutil.MustCheckDAPIMgmtService(t, dClient, http.StatusNotFound)

	// Update the DAPI config to enable the mgmt service

	cluster, err := e2eutil.GetCouchbaseCluster(k8sCluster.CRClient, cluster)
	if err != nil {
		e2eutil.Die(t, err)
	}

	cluster.Annotations["cao.couchbase.com/networking.cloudNativeGateway.dataAPI.proxyServices"] = "mgmt"

	cluster, err = e2eutil.UpdateCouchbaseCluster(k8sCluster.CRClient, cluster)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Check the update triggers a restart of the pods
	e2eutil.MustWaitForClusterEvent(t, k8sCluster, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, k8sCluster, cluster, 5*time.Minute)
	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, k8sCluster, cluster, 5*time.Minute)
	// Check the mgmt service is now available
	e2eutil.MustCheckDAPIMgmtService(t, dClient, http.StatusOK)
}

func TestCNGServiceTemplate(t *testing.T) {
	f := framework.Global

	kubernetesCluster, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetesCluster).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

	defer cleanup()

	clusterSize := 1

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(kubernetesCluster)
	cluster.Spec.Networking.CloudNativeGateway.ServiceTemplate = &couchbasev2.ServiceTemplateSpec{
		ObjectMeta: couchbasev2.ObjectMeta{
			Annotations: map[string]string{
				"my.annotation": "true",
			},
			Labels: map[string]string{
				"some-label": "true",
			},
		},
		Spec: &v1.ServiceSpec{
			Type:           v1.ServiceTypeLoadBalancer,
			LoadBalancerIP: "10.0.0.1",
		},
	}

	cluster = e2eutil.CreateNewClusterFromSpec(t, kubernetesCluster, cluster, 5)
	svc := e2eutil.MustWaitForCloudNativeGatewayServiceReady(t, kubernetesCluster, cluster, 10*time.Minute)

	// Check the service template has correctly been applied to the cng service.
	if svc.Annotations["my.annotation"] != "true" {
		e2eutil.Die(t, fmt.Errorf("annotation not set"))
	}

	if svc.Labels["some-label"] != "true" {
		e2eutil.Die(t, fmt.Errorf("label not set"))
	}

	if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
		e2eutil.Die(t, fmt.Errorf("service type not set"))
	}

	if svc.Spec.LoadBalancerIP != "10.0.0.1" {
		e2eutil.Die(t, fmt.Errorf("load balancer ip not set"))
	}

	// Remove the template spec and check the service template has been removed from the cng service.
	cluster = e2eutil.MustPatchCluster(t, kubernetesCluster, cluster, jsonpatch.NewPatchSet().Remove("/spec/networking/cloudNativeGateway/serviceTemplate"), time.Minute)
	time.Sleep(20 * time.Second)

	svc = e2eutil.MustWaitForCloudNativeGatewayServiceReady(t, kubernetesCluster, cluster, 10*time.Minute)

	if _, ok := svc.Annotations["my.annotation"]; ok {
		e2eutil.Die(t, fmt.Errorf("annotation not removed: %s", svc.Annotations["my.annotation"]))
	}

	if _, ok := svc.Labels["some-label"]; ok {
		e2eutil.Die(t, fmt.Errorf("label not removed: %s", svc.Labels["some-label"]))
	}

	if svc.Spec.Type != v1.ServiceTypeClusterIP {
		e2eutil.Die(t, fmt.Errorf("service type not set to clusterIP: %s", svc.Spec.Type))
	}

	if svc.Spec.LoadBalancerIP != "" {
		e2eutil.Die(t, fmt.Errorf("Load balancer ip not removed: %s", svc.Spec.LoadBalancerIP))
	}

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{
				Reason: k8sutil.EventReasonUserCreated,
			},
		},
	}
	ValidateEvents(t, kubernetesCluster, cluster, expectedEvents)
}
