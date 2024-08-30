package k8sutil

import (
	"fmt"
	"reflect"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func (l volumeMountList) getVolumeMount(t *testing.T, name volumeMountName) volumeMount {
	for _, mapping := range l {
		if mapping.name == name {
			return mapping
		}
	}

	t.Fatalf("missing %v claim", name)

	return volumeMount{}
}

// Test paths to persist method returns appropriate claims
// for known volume mounts.
func TestPathsToPersist(t *testing.T) {
	claimName := "couchbase"
	mounts := &couchbasev2.VolumeMounts{
		DefaultClaim: claimName,
		DataClaim:    claimName,
		IndexClaim:   claimName,
	}

	member := couchbaseutil.NewMember("namespace", "cluster", "name", "version", "class", false)

	paths, err := getPathsToPersist(member, mounts)
	if err != nil {
		t.Fatal(err)
	}

	defaultClaim := paths.getVolumeMount(t, defaultVolumeMount)
	if defaultClaim.persistentVolumeClaimTemplateName != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, defaultClaim, claimName)
	}

	dataClaim := paths.getVolumeMount(t, dataVolumeMount)
	if dataClaim.persistentVolumeClaimTemplateName != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, dataClaim, claimName)
	}

	indexClaim := paths.getVolumeMount(t, indexVolumeMount)
	if indexClaim.persistentVolumeClaimTemplateName != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, indexClaim, claimName)
	}
}

// Test that expected path is returned for the specified volume mount.
func TestPathsForVolumeMountName(t *testing.T) {
	path := pathForVolumeMountName(defaultVolumeMount)
	if path != couchbaseVolumeDefaultConfigDir {
		t.Fatalf(`invalid path for default volume: "%s", expected: "%s"`, path, couchbaseVolumeDefaultConfigDir)
	}

	path = pathForVolumeMountName(dataVolumeMount)
	if path != CouchbaseVolumeMountDataDir {
		t.Fatalf(`invalid path for data volume: "%s", expected: "%s"`, path, CouchbaseVolumeMountDataDir)
	}

	path = pathForVolumeMountName(indexVolumeMount)
	if path != CouchbaseVolumeMountIndexDir {
		t.Fatalf(`invalid path for index volume: "%s", expected: "%s"`, path, CouchbaseVolumeMountIndexDir)
	}
}

// Tests the metrics container created for scraping metrics from CB pods.
func TestCreateMetricsContainer(t *testing.T) {
	type test struct {
		// inputs
		expImage    string
		promEnabled bool
		refreshRate uint64

		// outputs
		readinessCheckURL string
		containerArgs     []string
	}

	testcases := []test{
		{
			expImage:          "couchbase/exporter:1.0.0",
			promEnabled:       true,
			refreshRate:       20,
			readinessCheckURL: "/metrics",
			containerArgs:     []string{"--per-node-refresh", fmt.Sprintf("%d", 20)},
		},
		{
			expImage:          "myrepo/my-malformed-format",
			promEnabled:       true,
			readinessCheckURL: "/metrics",
			containerArgs:     []string{"--per-node-refresh", fmt.Sprintf("%d", 60)},
		},
		{
			expImage:          "myrepo/myimage:1.0.8",
			promEnabled:       true,
			readinessCheckURL: "/readiness-probe",
			containerArgs:     []string{"--per-node-refresh", fmt.Sprintf("%d", 60)},
		},
		{
			expImage:          "myrepo/myimage:1.0.6",
			promEnabled:       false,
			readinessCheckURL: "/metrics",
			containerArgs:     []string{"--per-node-refresh", fmt.Sprintf("%d", 60)},
		},
	}

	for _, tc := range testcases {
		cs := couchbasev2.ClusterSpec{
			Monitoring: &couchbasev2.CouchbaseClusterMonitoringSpec{
				Prometheus: &couchbasev2.CouchbaseClusterMonitoringPrometheusSpec{
					Enabled:     tc.promEnabled,
					Image:       tc.expImage,
					RefreshRate: tc.refreshRate,
				},
			},
		}

		c := createMetricsContainer(cs)

		if tc.readinessCheckURL != c.ReadinessProbe.ProbeHandler.HTTPGet.Path {
			t.Errorf("readiness check url: expected: %v, got: %v", tc.readinessCheckURL, c.ReadinessProbe.ProbeHandler.HTTPGet.Path)
		}

		if !reflect.DeepEqual(tc.containerArgs, c.Args) {
			t.Errorf("metrics cpntainer args: expected: %v, got: %v", tc.containerArgs, c.Args)
		}
	}
}

func TestPopulateCNG(t *testing.T) {
	cluster := &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{}}

	annotation := map[string]string{
		"cao.couchbase.com/networking.cloudNativeGateway.otlp.endpoint": "foo.bar",
	}

	err := annotations.Populate(&cluster.Spec, annotation)
	if err != nil {
		t.Fatal(err)
	}

	if cluster.Spec.Networking.CloudNativeGateway.OTLP.Endpoint != "foo.bar" {
		t.Fatal("expected foo.bar but not found")
	}
}

func TestAddServerGroupAnnotations(t *testing.T) {
	serverGroup := "cb-manc-3"
	platformTopologyMap := map[couchbasev2.PlatformType]string{
		couchbasev2.PlatformTypeAWS:   constants.EKSTopologyLabel,
		couchbasev2.PlatformTypeAzure: constants.AzureTopologyLabel,
		couchbasev2.PlatformTypeGCE:   constants.GKETopologyLabel,
	}

	for platformType, topologyLabel := range platformTopologyMap {
		cluster := &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{
			Platform: platformType,
		}}

		pvc := &v1.PersistentVolumeClaim{}

		pvc.Annotations = make(map[string]string)

		addServerGroupAnnotations(pvc, serverGroup, cluster)

		expectedAnnotations := map[string]string{constants.ServerGroupLabel: serverGroup, topologyLabel: serverGroup}

		if eq := reflect.DeepEqual(expectedAnnotations, pvc.Annotations); !eq {
			t.Fatalf("Expected annotations: %v, but got: %v", expectedAnnotations, pvc.Annotations)
		}
	}

	cluster := &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{}}

	pvc := &v1.PersistentVolumeClaim{}

	pvc.Annotations = make(map[string]string)

	addServerGroupAnnotations(pvc, serverGroup, cluster)

	expectedAnnotations := map[string]string{constants.ServerGroupLabel: serverGroup}

	if eq := reflect.DeepEqual(expectedAnnotations, pvc.Annotations); !eq {
		t.Fatalf("Expected annotations: %v, but got: %v", expectedAnnotations, pvc.Annotations)
	}
}

func TestGetResourceRequestQuantity(t *testing.T) {
	type totalRequestedMemoryTestCase struct {
		podContainers []v1.Container
		resource      v1.ResourceName
		expected      resource.Quantity
	}

	testcases := []totalRequestedMemoryTestCase{
		{
			podContainers: []v1.Container{{
				Resources: v1.ResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{
						v1.ResourceMemory: resource.MustParse("5Gi"),
						v1.ResourceCPU:    resource.MustParse("3"),
						v1.ResourcePods:   resource.MustParse("1"),
					},
				},
			},
			},
			resource: v1.ResourceMemory,
			expected: resource.MustParse("5Gi"),
		},
		{
			podContainers: []v1.Container{{
				Resources: v1.ResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{
						v1.ResourceMemory: resource.MustParse("256Mi"),
						v1.ResourcePods:   resource.MustParse("2"),
						v1.ResourceCPU:    resource.MustParse("500m"),
					},
				},
			},
				{
					Resources: v1.ResourceRequirements{
						Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceMemory: resource.MustParse("1Gi"),
							v1.ResourceCPU:    resource.MustParse("2"),
						},
					},
				},
			},
			resource: v1.ResourceMemory,
			expected: resource.MustParse("1.25Gi"),
		},
		{
			podContainers: []v1.Container{},
			resource:      v1.ResourceMemory,
			expected:      resource.MustParse("0"),
		},
	}

	for _, testcase := range testcases {
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				Containers: testcase.podContainers,
			},
		}

		result := GetResourceRequestQuantity(pod, testcase.resource)

		if !result.Equal(testcase.expected) {
			t.Errorf("expected resource %s by pod to be %s, got %s", testcase.resource.String(), testcase.expected.String(), result.String())
		}
	}
}
