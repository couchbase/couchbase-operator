/*
Copyright 2023-Present Couchbase, Inc.

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
	"net/http"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/couchbase-operator/test/e2e/util"
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

	framework.Requires(t, kubernetes).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

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

func TestScaleDownMarksPodUnreadyAndRemovedFromCNGEndpointSlices(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetes).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG)

	defer cleanup()

	// Static configuration.
	serverClassSize := constants.Size1

	// Create the cluster spec
	cluster := clusterOptions().WithMixedEphemeralTopology(serverClassSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).MustCreate(t, kubernetes)
	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetes, cluster, 5*time.Minute)

	cngService := fmt.Sprintf("%s-cloud-native-gateway-service", cluster.Name)
	staticMember := couchbaseutil.CreateMemberName(cluster.Name, 0)
	ejectMember := couchbaseutil.CreateMemberName(cluster.Name, 1)
	newMember := couchbaseutil.CreateMemberName(cluster.Name, 2)

	// Check the two initial members are ready and in the CNG endpoint slices.
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 0, v1.ConditionTrue, time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, staticMember, cngService, time.Minute)
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 1, v1.ConditionTrue, time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, ejectMember, cngService, time.Minute)

	// Scale down the cluster by removing a server class
	cluster = e2eutil.MustRemoveServices(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, 2*time.Minute)
	// Check pod readiness is false after the server class is removed and that the pod is removed from CNG endpoint slices.
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 1, v1.ConditionFalse, 5*time.Minute)
	e2eutil.MustWaitForPodIPNotInServiceEndpointSlice(t, kubernetes, ejectMember, cngService, 5*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 5*time.Minute)

	// Scale up the cluster
	e2eutil.MustScaleServices(t, kubernetes, cluster, map[string]int{cluster.Spec.Servers[0].Name: 2}, time.Minute)
	// Check pod readiness is disabled on the new pod until rebalance is complete and that the pod is added to CNG endpoint slices after rebalance.
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, 2), 5*time.Minute)
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 2, v1.ConditionFalse, time.Minute)
	e2eutil.MustWaitForPodIPNotInServiceEndpointSlice(t, kubernetes, newMember, cngService, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 5*time.Minute)
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 2, v1.ConditionTrue, time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, newMember, cngService, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(serverClassSize * 2),
		eventschema.Optional{
			Validator: eventschema.Event{
				Reason: k8sutil.EventReasonUserCreated,
			},
		},
		e2eutil.ClusterScaleDownSequenceWithMemberNames([]string{ejectMember}),
		e2eutil.ClusterScaleUpSequenceWithMemberNames([]string{newMember}),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestPreserveReadyCNGInstancesStopsUpgrade(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetes).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG).Upgradable()

	defer cleanup()
	// Static configuration.
	clusterSize := constants.Size3

	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(kubernetes)
	cluster.Spec.Networking.CloudNativeGateway.PreserveReadyInstances = util.IntPtr(2)

	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeNodes,
	}

	// Create the cluster and wait for the sidecar to be re ready.
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	firstUpgradeCandidate := couchbaseutil.CreateMemberName(cluster.Name, 0)
	secondUpgradeCandidate := couchbaseutil.CreateMemberName(cluster.Name, 1)
	thirdUpgradeCandidate := couchbaseutil.CreateMemberName(cluster.Name, 2)
	firstNewMember := couchbaseutil.CreateMemberName(cluster.Name, 3)
	secondNewMember := couchbaseutil.CreateMemberName(cluster.Name, 4)
	thirdNewMember := couchbaseutil.CreateMemberName(cluster.Name, 5)

	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetes, cluster, 5*time.Minute)

	// Wait until all the members are in the endpoint slice to ensure CNG is fully ready before we start the upgrade.
	cngService := fmt.Sprintf("%s-cloud-native-gateway-service", cluster.Name)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, firstUpgradeCandidate, cngService, 2*time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, secondUpgradeCandidate, cngService, 2*time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, thirdUpgradeCandidate, cngService, 2*time.Minute)

	// Start the upgrade and wait for the first member to be upgraded and rebalance to complete.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().
		Add("/spec/upgrade/upgradeOrder", []string{firstUpgradeCandidate, secondUpgradeCandidate, thirdUpgradeCandidate}).
		Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 10*time.Minute)

	// Sabotage the readiness of the CNG container on the second upgrade candidate and the new member.
	// This should mean the number of ready CNG contains is < 2 (the number to preserve) and the upgrade should be paused until we stop sabotaging.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sabotageCNGContainerReadiness(kubernetes, ctx, secondUpgradeCandidate)
	sabotageCNGContainerReadiness(kubernetes, ctx, firstNewMember)

	// We should not see a rebalance started event while there is only 1 ready CNG instance (third upgrade candidate).
	e2eutil.MustNotObserveClusterEventFor(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), time.Minute)

	// Stop sabotaging the readiness of the two members to allow the upgrade to continue.
	cancel()

	// Wait until the upgrade has completed and the cluster is healthy.
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewUpgradeFinishedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check that all new members are in the endpoint slice to ensure they were successfully added to the cluster and CNG updated the endpoint slices correctly.
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, firstNewMember, cngService, 2*time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, secondNewMember, cngService, 2*time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, thirdNewMember, cngService, 2*time.Minute)

	// Check the upgrade completed successfully and the new version is running.
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{
				Reason: k8sutil.EventReasonUserCreated,
			},
		},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Validator: e2eutil.SwapRebalanceSequence, Times: clusterSize},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestPreserveReadyCNGInstancesStopsScaleDown(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	framework.Requires(t, kubernetes).AtLeastVersion(podconsts.MinimumCouchbaseVersionForCNG).Upgradable()

	defer cleanup()
	// Static configuration.
	clusterSize := constants.Size4

	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).WithCloudNativeGateway(framework.Global.CouchbaseCloudNativeGatewayImage, nil).Generate(kubernetes)
	cluster.Spec.Networking.CloudNativeGateway.PreserveReadyInstances = util.IntPtr(2)

	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeNodes,
	}

	// Create the cluster and wait for the sidecar to be re ready.
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	firstMember := couchbaseutil.CreateMemberName(cluster.Name, 0)
	secondMember := couchbaseutil.CreateMemberName(cluster.Name, 1)
	thirdMember := couchbaseutil.CreateMemberName(cluster.Name, 2)
	fourthMember := couchbaseutil.CreateMemberName(cluster.Name, 3)

	e2eutil.MustWaitForCloudNativeGatewaySidecarReady(t, kubernetes, cluster, 5*time.Minute)

	// Wait until all the members are in the endpoint slice to ensure CNG is fully ready before we start the upgrade.
	cngService := fmt.Sprintf("%s-cloud-native-gateway-service", cluster.Name)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, firstMember, cngService, 2*time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, secondMember, cngService, 2*time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, thirdMember, cngService, 2*time.Minute)
	e2eutil.MustWaitForPodIPInServiceEndpointSlice(t, kubernetes, fourthMember, cngService, 2*time.Minute)

	// Start sabotaging the readiness of 3 containers. This should breach the preservation threshold and stop scale downs.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sabotageCNGContainerReadiness(kubernetes, ctx, firstMember)
	sabotageCNGContainerReadiness(kubernetes, ctx, secondMember)
	sabotageCNGContainerReadiness(kubernetes, ctx, thirdMember)

	// Start the upgrade and wait for the first member to be upgraded and rebalance to complete.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", 3), time.Minute)

	time.Sleep(20 * time.Second)

	// Make sure we aren't scaling down.
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, time.Minute, couchbasev2.ClusterConditionScalingDown)

	// Make sure we don't see a rebalance to remove any members.
	e2eutil.MustNotObserveClusterEventFor(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), time.Minute)

	// Stop sabotaging the readiness of the members to allow the scale down to continue.
	cancel()

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionScalingDown, v1.ConditionTrue, cluster, 2*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{
				Reason: k8sutil.EventReasonUserCreated,
			},
		},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// sabotageCNGContainerReadiness continuously sets the Ready condition to false on the  CNG container of the pod to prevent it from being marked ready by the Kubelet.
// Kubelet will try and update this field every 10 seconds. It's the job of this goroutine to update it more frequently than that to ensure it remains false for the duration of the test.
// Probably going to relate in some flakiness but...
func sabotageCNGContainerReadiness(k8s *types.Cluster, ctx context.Context, podName string) {
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond) // Patch 5 times a second to beat the Kubelet
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pod, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Get(ctx, podName, metav1.GetOptions{})
				if err != nil {
					continue
				}

				found := false
				for i, status := range pod.Status.ContainerStatuses {
					if status.Name == k8sutil.CloudNativeGatewayContainerName {
						pod.Status.ContainerStatuses[i].Ready = false
						found = true
						break
					}
				}

				if !found {
					continue
				}

				_, _ = k8s.KubeClient.CoreV1().Pods(k8s.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
			}
		}
	}()
}
