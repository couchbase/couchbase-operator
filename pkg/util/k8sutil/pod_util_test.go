/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package k8sutil

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	member := couchbaseutil.NewMember("namespace", "cluster", "name", "version", "class", false, "")

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

		// No override: the zone equals the server group, so the CSI topology key gets the
		// server group value.
		addServerGroupAnnotations(pvc, serverGroup, serverGroup, cluster)

		expectedAnnotations := map[string]string{constants.AnnotationServerGroup: serverGroup, constants.AnnotationAvailabilityZone: serverGroup, topologyLabel: serverGroup}

		if eq := reflect.DeepEqual(expectedAnnotations, pvc.Annotations); !eq {
			t.Fatalf("Expected annotations: %v, but got: %v", expectedAnnotations, pvc.Annotations)
		}
	}

	// With a server-group label override the server group is a placement group, so the CSI
	// topology key and the platform-neutral AZ key must be pinned to the real availability zone,
	// not the placement group.
	for platformType, topologyLabel := range platformTopologyMap {
		cluster := &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{
			Platform: platformType,
		}}

		pvc := &v1.PersistentVolumeClaim{}

		pvc.Annotations = make(map[string]string)

		zone := "us-east-1a"

		addServerGroupAnnotations(pvc, serverGroup, zone, cluster)

		expectedAnnotations := map[string]string{constants.AnnotationServerGroup: serverGroup, constants.AnnotationAvailabilityZone: zone, topologyLabel: zone}

		if eq := reflect.DeepEqual(expectedAnnotations, pvc.Annotations); !eq {
			t.Fatalf("Expected annotations: %v, but got: %v", expectedAnnotations, pvc.Annotations)
		}
	}

	// No platform: no CSI topology key is written, but the platform-neutral AZ key still is.
	cluster := &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{}}

	pvc := &v1.PersistentVolumeClaim{}

	pvc.Annotations = make(map[string]string)

	addServerGroupAnnotations(pvc, serverGroup, serverGroup, cluster)

	expectedAnnotations := map[string]string{constants.AnnotationServerGroup: serverGroup, constants.AnnotationAvailabilityZone: serverGroup}

	if eq := reflect.DeepEqual(expectedAnnotations, pvc.Annotations); !eq {
		t.Fatalf("Expected annotations: %v, but got: %v", expectedAnnotations, pvc.Annotations)
	}

	// No zone (zone == ""): only the server-group (PG) bookkeeping is written; no AZ / CSI topology
	// annotations.
	awsCluster := &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{Platform: couchbasev2.PlatformTypeAWS}}

	pvcNoZone := &v1.PersistentVolumeClaim{}
	pvcNoZone.Annotations = make(map[string]string)

	addServerGroupAnnotations(pvcNoZone, serverGroup, "", awsCluster)

	expectedNoZone := map[string]string{constants.AnnotationServerGroup: serverGroup}

	if eq := reflect.DeepEqual(expectedNoZone, pvcNoZone.Annotations); !eq {
		t.Fatalf("Expected annotations: %v, but got: %v", expectedNoZone, pvcNoZone.Annotations)
	}
}

func TestPvcAvailabilityZone(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		expected    string
	}{
		{"new key", map[string]string{constants.AnnotationAvailabilityZone: "az-a"}, "az-a"},
		{"legacy fallback (pre-override PVC: group == AZ under the zone key)", map[string]string{constants.ServerGroupLabel: "az-b"}, "az-b"},
		{"new key wins over legacy", map[string]string{constants.AnnotationAvailabilityZone: "az-a", constants.ServerGroupLabel: "az-b"}, "az-a"},
		{"neither", map[string]string{}, ""},
	}

	for _, tc := range cases {
		pvc := &v1.PersistentVolumeClaim{}
		pvc.Annotations = tc.annotations

		if got := pvcAvailabilityZone(pvc); got != tc.expected {
			t.Fatalf("%s: expected %q, got %q", tc.name, tc.expected, got)
		}
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

func TestCheckIfPVCRequiresUpdate(t *testing.T) {
	type testcase struct {
		name         string
		existingSpec v1.PersistentVolumeClaimSpec
		requiredSpec v1.PersistentVolumeClaimSpec
		expected     bool
	}

	testcases := []testcase{
		{
			name: "equal specs, different format for resource fields",
			existingSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("1Gi")},
					Limits:   map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("2Gi")},
				},
			},
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("1024Mi")},
					Limits:   map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("2048Mi")},
				},
			},
			expected: false,
		},
		{
			name: "equal specs, different parsing methods",
			existingSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					// 1024 * 1024 * 1024 = 1Gi
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: *resource.NewQuantity(1024*1024*1024, resource.BinarySI)},
				},
			},
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					// 1Gi in decimal SI = "1073741824m"
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: *resource.NewQuantity(1073741824, resource.DecimalSI)},
				},
			},
			expected: false,
		},
		{
			name: "different specs for resource requests",
			existingSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("1Gi")},
				},
			},
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("5Gi")},
				},
			},
			expected: true,
		},
		{
			name: "different specs for resource limits",
			existingSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("2Gi")},
				},
			},
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
			expected: true,
		},
		{
			name:         "different specs, existing from json string",
			existingSpec: pvcClaimFromJSON(`{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"3078632557772800m"}},"storageClassName":"static-local-path"}`, t),
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("100Mi")},
				},
				StorageClassName: strPtr("static-local-path"),
			},
			expected: true,
		},
		{
			name:         "equal specs, existing from json string",
			existingSpec: pvcClaimFromJSON(`{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"3078632557772800m"}},"storageClassName":"static-local-path"}`, t),
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("2.8Ti")},
				},
				StorageClassName: strPtr("static-local-path"),
			},
			expected: false,
		},
		{
			name: "equal specs, empty resources",
			existingSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources:   v1.VolumeResourceRequirements{},
			},
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources:   v1.VolumeResourceRequirements{},
			},
			expected: false,
		},
		{
			name: "equal specs, different access mode",
			existingSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources:   v1.VolumeResourceRequirements{},
			},
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany},
				Resources:   v1.VolumeResourceRequirements{},
			},
			expected: true,
		},
		{
			name:         "equal resources, different storage class name",
			existingSpec: pvcClaimFromJSON(`{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"3078632557772800m"}},"storageClassName":"static-local-path"}`, t),
			requiredSpec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.VolumeResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{v1.ResourceStorage: resource.MustParse("3078632557772800m")},
				},
				StorageClassName: strPtr("dynamic-local-path"),
			},
			expected: true,
		},
	}

	for _, testcase := range testcases {
		result := checkIfPVCRequiresUpdate(testcase.existingSpec, testcase.requiredSpec)
		if result != testcase.expected {
			t.Errorf("test %v failed, expected %v, got %v", testcase.name, testcase.expected, result)
		}
	}
}

func pvcClaimFromJSON(jsonString string, t *testing.T) v1.PersistentVolumeClaimSpec {
	var pvcClaim v1.PersistentVolumeClaimSpec
	if err := json.Unmarshal([]byte(jsonString), &pvcClaim); err != nil {
		t.Fatal(err)
	}

	return pvcClaim
}

func strPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func TestCalculatePodResourcesOverhead(t *testing.T) {
	tests := []struct {
		name            string
		overheadPercent *int
		overheadMemory  *resource.Quantity
		expectedMemory  resource.Quantity
	}{
		{
			name:            "OverheadPercent only (35% of 100Mi base = 135Mi total)",
			overheadPercent: intPtr(35),
			overheadMemory:  nil,
			expectedMemory:  resource.MustParse("135Mi"),
		},
		{
			name:            "OverheadMemory only (1Gi overhead = 100Mi base + 1Gi overhead = 1124Mi total)",
			overheadPercent: nil,
			overheadMemory:  resource.NewQuantity(1*1024*1024*1024, resource.BinarySI),
			expectedMemory:  resource.MustParse("1124Mi"),
		},
		{
			name:            "OverheadMemory takes precedence (1Gi overhead = 100Mi base + 1Gi overhead = 1124Mi total)",
			overheadPercent: intPtr(25),
			overheadMemory:  resource.NewQuantity(1*1024*1024*1024, resource.BinarySI),
			expectedMemory:  resource.MustParse("1124Mi"),
		},
		{
			name:            "Neither specified (default 25% overhead = 125Mi total)",
			overheadPercent: nil,
			overheadMemory:  nil,
			expectedMemory:  resource.MustParse("125Mi"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cluster := &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					AutoResourceAllocation: &couchbasev2.AutoResourceAllocation{
						Enabled:         true,
						OverheadPercent: test.overheadPercent,
						OverheadMemory:  test.overheadMemory,
					},
					ClusterSettings: couchbasev2.ClusterConfig{
						DataServiceMemQuota: resource.NewQuantity(100*1024*1024, resource.BinarySI), // Base memory for DataService
					},
				},
			}

			config := &couchbasev2.ServerConfig{
				Services: []couchbasev2.Service{couchbasev2.DataService},
			}

			// Call couchbaseContainer which internally uses the logic we modified
			container := couchbaseContainer(cluster, config, "some-image")

			actualMemory := container.Resources.Requests[v1.ResourceMemory]

			if !actualMemory.Equal(test.expectedMemory) {
				t.Errorf("for %s: expected total memory %s, got %s", test.name, test.expectedMemory.String(), actualMemory.String())
			}
		})
	}
}

func TestPasswordCompliesWithCouchbasePasswordPolicy(t *testing.T) {
	testcases := []struct {
		name     string
		policy   *couchbasev2.PasswordPolicySpec
		password string
		expected bool
	}{
		{
			name:     "no policy",
			password: "Password123!",
			expected: true,
		},
		{
			name: "valid password with all character types",
			policy: &couchbasev2.PasswordPolicySpec{
				MinLength:           intPtr(8),
				EnforceUppercase:    boolPtr(true),
				EnforceLowercase:    boolPtr(true),
				EnforceDigits:       boolPtr(true),
				EnforceSpecialChars: boolPtr(true),
			},
			password: "Password123!",
			expected: true,
		},
		{
			name: "valid password with all character types with enforcement turned off",
			policy: &couchbasev2.PasswordPolicySpec{
				MinLength:           intPtr(8),
				EnforceUppercase:    boolPtr(false),
				EnforceLowercase:    boolPtr(false),
				EnforceDigits:       boolPtr(false),
				EnforceSpecialChars: boolPtr(false),
			},
			password: "Password123!",
			expected: true,
		},
		{
			name: "valid password short with only minlength",
			policy: &couchbasev2.PasswordPolicySpec{
				MinLength: intPtr(4),
			},
			password: "pass",
			expected: true,
		},
		{
			name: "invalid password with too short length",
			policy: &couchbasev2.PasswordPolicySpec{
				MinLength: intPtr(8),
			},
			password: "passwor",
			expected: false,
		},
		{
			name: "invalid password with no uppercase letters",
			policy: &couchbasev2.PasswordPolicySpec{
				EnforceUppercase: boolPtr(true),
				MinLength:        intPtr(5),
			},
			password: "password123!",
			expected: false,
		},
		{
			name: "invalid password with no lowercase letters",
			policy: &couchbasev2.PasswordPolicySpec{
				EnforceLowercase:    boolPtr(true),
				EnforceDigits:       boolPtr(true),
				EnforceSpecialChars: boolPtr(true),
			},
			password: "PASSWORD123!",
			expected: false,
		},
		{
			name: "invalid password with no special characters",
			policy: &couchbasev2.PasswordPolicySpec{
				EnforceSpecialChars: boolPtr(true),
				MinLength:           intPtr(5),
				EnforceUppercase:    boolPtr(true),
				EnforceLowercase:    boolPtr(false),
			},
			password: "PASSWORD123",
			expected: false,
		},
		{
			name: "invalid password with no digits",
			policy: &couchbasev2.PasswordPolicySpec{
				EnforceDigits: boolPtr(true),
				MinLength:     intPtr(30),
			},
			password: "PasswordPasswordPasswordPasswordPassword!",
			expected: false,
		},
	}

	for _, testcase := range testcases {
		result := PasswordCompliesWithCouchbasePasswordPolicy(testcase.policy, testcase.password)
		if result != testcase.expected {
			t.Errorf("test %v failed, expected %v, got %v", testcase.name, testcase.expected, result)
		}
	}
}

func TestCreateReadinessProbe(t *testing.T) {
	testCases := []struct {
		name string
		port int
	}{
		{"default_port", 8091},
		{"TLS_port", 18091},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			readinessConfig := PodReadinessConfig{
				PodReadinessDelay:  10 * time.Second,
				PodReadinessPeriod: 20 * time.Second,
			}

			probe := createReadinessProbe(tc.port, readinessConfig)
			assertReadinessProbe(t, probe, tc.port)
		})
	}
}

func assertReadinessProbe(t *testing.T, probe *v1.Probe, port int) {
	t.Helper()

	if probe == nil {
		t.Fatal("expected probe to be non-nil")
	}

	if probe.TCPSocket != nil {
		t.Fatal("expected no TCP socket probe (exec probe should be used instead)")
	}

	if probe.Exec == nil {
		t.Fatal("expected exec probe, got nil")
	}

	if len(probe.Exec.Command) == 0 {
		t.Fatal("expected exec probe command to be non-empty")
	}

	// Verify the command references the expected port (both IPv6 and IPv4 loopback).
	portStr := fmt.Sprintf("%d", port)
	cmd := strings.Join(probe.Exec.Command, " ")
	if !strings.Contains(cmd, portStr) {
		t.Fatalf("expected exec probe command to contain port %s, got: %s", portStr, cmd)
	}

	assertCommonProbeSettings(t, probe)
}

func assertCommonProbeSettings(t *testing.T, probe *v1.Probe) {
	t.Helper()

	if probe.InitialDelaySeconds != 10 {
		t.Fatalf("expected InitialDelaySeconds 10, got %d", probe.InitialDelaySeconds)
	}
	if probe.PeriodSeconds != 20 {
		t.Fatalf("expected PeriodSeconds 20, got %d", probe.PeriodSeconds)
	}
	if probe.TimeoutSeconds != 5 {
		t.Fatalf("expected TimeoutSeconds 5, got %d", probe.TimeoutSeconds)
	}
	if probe.FailureThreshold != 1 {
		t.Fatalf("expected FailureThreshold 1, got %d", probe.FailureThreshold)
	}
}

func boolPtr(b bool) *bool {
	return &b
}

// TestIsPodPendingAnyEjection checks how the reconciler tells whether a pod has
// been rebalanced out yet. Scale down and swap rebalance upgrades use separate
// conditions, because the upgrade one also gates upgrade completion, but either
// one must stop the pod being deleted.
func TestIsPodPendingAnyEjection(t *testing.T) {
	pod := func(conditions ...v1.PodConditionType) *v1.Pod {
		p := &v1.Pod{}
		for _, c := range conditions {
			p.Status.Conditions = append(p.Status.Conditions, v1.PodCondition{Type: c, Status: v1.ConditionTrue})
		}

		return p
	}

	cases := []struct {
		name                   string
		pod                    *v1.Pod
		wantScaleDown, wantAny bool
	}{
		{"unmarked pod is not pending ejection", pod(), false, false},
		{"scale down mark", pod(PodPendingEjectionCondition), true, true},
		{"upgrade mark is not the scale down mark, but still blocks deletion", pod(PodPendingUpgradeBeforeEjectionCondition), false, true},
		{"both marks", pod(PodPendingEjectionCondition, PodPendingUpgradeBeforeEjectionCondition), true, true},
		{"an unrelated condition does not count", pod(PodPendingInitializationCondition), false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPodPendingEjection(tc.pod); got != tc.wantScaleDown {
				t.Errorf("IsPodPendingEjection = %v, want %v", got, tc.wantScaleDown)
			}

			if got := IsPodPendingAnyEjection(tc.pod); got != tc.wantAny {
				t.Errorf("IsPodPendingAnyEjection = %v, want %v", got, tc.wantAny)
			}
		})
	}

	// A false status condition must not read as marked.
	notMarked := &v1.Pod{Status: v1.PodStatus{Conditions: []v1.PodCondition{
		{Type: PodPendingEjectionCondition, Status: v1.ConditionFalse},
	}}}
	if IsPodPendingAnyEjection(notMarked) {
		t.Errorf("condition with status False: IsPodPendingAnyEjection = true, want false")
	}
}

// TestSnapshotMountPaths checks the mount paths a snapshot backup takes match the paths the pod
// mounts, and that logs, which aren't snapshotted, are left out.
func TestSnapshotMountPaths(t *testing.T) {
	testcases := []struct {
		name     string
		mounts   *couchbasev2.VolumeMounts
		expected []string
	}{
		{
			name:     "no volume mounts",
			expected: nil,
		},
		{
			name:     "logs only",
			mounts:   &couchbasev2.VolumeMounts{LogsClaim: "couchbase"},
			expected: nil,
		},
		{
			name:     "default only",
			mounts:   &couchbasev2.VolumeMounts{DefaultClaim: "couchbase"},
			expected: []string{"/opt/couchbase/var/lib/couchbase"},
		},
		{
			name: "default, data, index and two analytics",
			mounts: &couchbasev2.VolumeMounts{
				DefaultClaim:    "couchbase",
				DataClaim:       "couchbase",
				IndexClaim:      "couchbase",
				AnalyticsClaims: []string{"couchbase", "couchbase"},
			},
			expected: []string{"/opt/couchbase/var/lib/couchbase", "/mnt/data", "/mnt/index", "/mnt/analytics-00", "/mnt/analytics-01"},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			if got := SnapshotMountPaths(testcase.mounts); !reflect.DeepEqual(got, testcase.expected) {
				t.Errorf("expected %v, got %v", testcase.expected, got)
			}
		})
	}
}

// TestSnapshotRestoreMembers checks each backed up member gets the server class
// it was recorded in, or for older runs the class with the same volumes, and that
// a class with different volumes is refused, since some snapshots would have
// nowhere to go.
func TestSnapshotRestoreMembers(t *testing.T) {
	const (
		defaultPath = "/opt/couchbase/var/lib/couchbase"
		dataPath    = "/mnt/data"
	)

	// target has class data with default and data volumes, and class other with only default.
	target := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "target"},
		Spec: couchbasev2.ClusterSpec{
			Servers: []couchbasev2.ServerConfig{
				{Name: "data", VolumeMounts: &couchbasev2.VolumeMounts{DefaultClaim: "couchbase", DataClaim: "couchbase"}},
				{Name: "other", VolumeMounts: &couchbasev2.VolumeMounts{DefaultClaim: "couchbase"}},
			},
		},
	}

	// run makes a run of members cb-0001 and cb-0000, in that order, each with the given volumes
	// and server class.
	run := func(class string, paths ...string) *couchbasev2.CouchbaseSnapshotBackupRun {
		r := &couchbasev2.CouchbaseSnapshotBackupRun{ObjectMeta: metav1.ObjectMeta{Name: "daily-run"}}

		for _, member := range []string{"cb-0001", "cb-0000"} {
			for _, path := range paths {
				r.Status.Snapshots = append(r.Status.Snapshots, couchbasev2.CouchbaseSnapshotBackupRunSnapshot{
					Member:           member,
					SnapshotName:     member + path,
					MountPath:        path,
					ServerClass:      class,
					ServerGroup:      "group-" + member,
					AvailabilityZone: "zone-" + member,
				})
			}
		}

		return r
	}

	testcases := []struct {
		name          string
		run           *couchbasev2.CouchbaseSnapshotBackupRun
		expectedClass string
		expectedErr   string
	}{
		{
			name:          "the recorded class is used",
			run:           run("other", defaultPath),
			expectedClass: "other",
		},
		{
			name:          "an older run gets the class with the same volumes",
			run:           run("", defaultPath, dataPath),
			expectedClass: "data",
		},
		{
			name:        "a recorded class the target doesn't have",
			run:         run("query", defaultPath),
			expectedErr: "server class query, which cluster target doesn't have",
		},
		{
			name:        "a recorded class with different volumes is refused, even though another class matches",
			run:         run("data", defaultPath),
			expectedErr: "but server class data in cluster target has",
		},
		{
			name:        "an older run with no class that matches",
			run:         run("", dataPath),
			expectedErr: "no server class in cluster target has the same volumes",
		},
		{
			name:        "a run without mount paths",
			run:         run("data", ""),
			expectedErr: "doesn't record where member cb-0001's volumes were mounted",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			members, err := SnapshotRestoreMembers(target, testcase.run)

			if testcase.expectedErr != "" {
				if err == nil || !strings.Contains(err.Error(), testcase.expectedErr) {
					t.Fatalf("expected an error containing %q, got %v", testcase.expectedErr, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(members) != 2 || members[0].Name != "cb-0000" || members[1].Name != "cb-0001" {
				t.Fatalf("expected members cb-0000 and cb-0001 in order, got %v", members)
			}

			for _, member := range members {
				checkSnapshotRestoreMember(t, member, testcase.expectedClass, defaultPath)
			}
		})
	}
}

// checkSnapshotRestoreMember checks a member got the expected class, kept its
// group and zone, and has the right snapshot for path.
func checkSnapshotRestoreMember(t *testing.T, member SnapshotRestoreMember, class, path string) {
	t.Helper()

	if member.Config.Name != class {
		t.Errorf("member %s: expected class %s, got %s", member.Name, class, member.Config.Name)
	}

	if member.ServerGroup != "group-"+member.Name || member.AvailabilityZone != "zone-"+member.Name {
		t.Errorf("member %s: group and zone not kept, got %s and %s", member.Name, member.ServerGroup, member.AvailabilityZone)
	}

	if member.Snapshots[path] != member.Name+path {
		t.Errorf("member %s: wrong snapshot for %s, got %v", member.Name, path, member.Snapshots)
	}
}

// TestIsBackupPVCByLabel checks that a PVC with the backup label counts as a
// backup PVC even when its backup is gone, so it isn't treated as a server PVC.
func TestIsBackupPVCByLabel(t *testing.T) {
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "nightly",
			Labels: map[string]string{constants.LabelBackup: "nightly"},
		},
	}

	// The client has no backup cache at all, so this only passes if the label is enough.
	if !IsBackupPVC(&client.Client{}, pvc) {
		t.Errorf("expected a PVC with the backup label to be a backup PVC")
	}
}

// TestHasJoinedNote checks that a member counts as joined when at least one of its
// PVCs has the joined note, and doesn't count as joined when none of them do.
func TestHasJoinedNote(t *testing.T) {
	withNote := &v1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{constants.PVCJoinedAnnotation: "true"}}}
	withoutNote := &v1.PersistentVolumeClaim{}

	cases := []struct {
		name string
		pvcs []*v1.PersistentVolumeClaim
		want bool
	}{
		{
			name: "no PVCs",
			pvcs: nil,
			want: false,
		},
		{
			name: "PVCs without the note",
			pvcs: []*v1.PersistentVolumeClaim{withoutNote, withoutNote},
			want: false,
		},
		{
			name: "one of the PVCs has the note",
			pvcs: []*v1.PersistentVolumeClaim{withoutNote, withNote},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasJoinedNote(tc.pvcs); got != tc.want {
				t.Errorf("hasJoinedNote = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMemberPersistentVolumeClaims checks a restore gets the same volumes the operator would make
// for a member, one per mount, with the operator's names, labels and annotations.
func TestMemberPersistentVolumeClaims(t *testing.T) {
	cluster := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "rx", Namespace: "default"},
		Spec: couchbasev2.ClusterSpec{
			Image: "couchbase/server:7.6.8",
			VolumeClaimTemplates: []couchbasev2.PersistentVolumeClaimTemplate{
				{ObjectMeta: couchbasev2.NamedObjectMeta{Name: "couchbase"}},
			},
		},
	}

	config := couchbasev2.ServerConfig{
		Name:         "data",
		VolumeMounts: &couchbasev2.VolumeMounts{DefaultClaim: "couchbase", DataClaim: "couchbase"},
	}

	member := couchbaseutil.NewMember("default", "rx", "rx-0000", "7.6.8", "data", false, "couchbase/server:7.6.8")

	pvcs, err := MemberPersistentVolumeClaims(cluster, member, config)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expected := map[string]string{
		"rx-0000-default-00": "/opt/couchbase/var/lib/couchbase",
		"rx-0000-data-00":    "/mnt/data",
	}

	if len(pvcs) != len(expected) {
		t.Fatalf("expected %d volumes, got %d", len(expected), len(pvcs))
	}

	for _, pvc := range pvcs {
		path, ok := expected[pvc.Name]
		if !ok {
			t.Errorf("unexpected volume %s", pvc.Name)
			continue
		}

		if pvc.Annotations[constants.AnnotationVolumeMountPath] != path {
			t.Errorf("expected %s to be mounted at %s, got %s", pvc.Name, path, pvc.Annotations[constants.AnnotationVolumeMountPath])
		}

		if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != v1.ReadWriteOnce {
			t.Errorf("expected %s to be ReadWriteOnce, got %v", pvc.Name, pvc.Spec.AccessModes)
		}

		if pvc.Annotations[constants.AnnotationVolumeNodeConf] != "data" || pvc.Labels[constants.LabelNode] != "rx-0000" || pvc.Labels[constants.LabelCluster] != "rx" {
			t.Errorf("expected %s to carry the member, cluster and server class, got labels %v annotations %v", pvc.Name, pvc.Labels, pvc.Annotations)
		}
	}
}

// remapTestPod builds a member's pod, with its default volume marked as made by a snapshot
// restore when restored is set.
func remapTestPod(t *testing.T, restored bool) *v1.Pod {
	cluster := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "rx", Namespace: "default"},
		Spec: couchbasev2.ClusterSpec{
			Image: "couchbase/server:7.6.8",
			VolumeClaimTemplates: []couchbasev2.PersistentVolumeClaimTemplate{
				{ObjectMeta: couchbasev2.NamedObjectMeta{Name: "couchbase"}},
			},
		},
	}

	config := couchbasev2.ServerConfig{
		Name:         "data",
		VolumeMounts: &couchbasev2.VolumeMounts{DefaultClaim: "couchbase", DataClaim: "couchbase"},
	}

	member := couchbaseutil.NewMember("default", "rx", "rx-0000", "7.6.8", "data", false, "couchbase/server:7.6.8")

	pvcs, err := MemberPersistentVolumeClaims(cluster, member, config)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, pvc := range pvcs {
		if restored && isPathPersisted(pvc, couchbaseVolumeDefaultConfigDir) {
			pvc.Annotations[constants.AnnotationSnapshotRestoreRemap] = "true"
			pvc.Annotations[constants.AnnotationSnapshotRestoreRemapID] = "3f2a9c1e-5b7d-4e8f-9a6c-2d1b0e4f7a83"
		}
	}

	pod, err := CreateCouchbasePodSpec(member, cluster, config, "", &PersistentVolumeClaimState{pvcs: pvcs}, "couchbase/server:7.6.8", PodReadinessConfig{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	return pod
}

// TestRemapInitContainer checks a pod only gets the node remap init container when its default
// volume was made by a snapshot restore, so normal pods stay the same.
func TestRemapInitContainer(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(fmt.Sprintf("restored=%v", restored), func(t *testing.T) {
			pod := remapTestPod(t, restored)

			names := []string{}
			for _, container := range pod.Spec.InitContainers {
				names = append(names, container.Name)
			}

			expected := []string{"couchbase-server-init"}
			if restored {
				expected = append(expected, "couchbase-server-remap")
			}

			if !reflect.DeepEqual(names, expected) {
				t.Fatalf("expected init containers %v, got %v", expected, names)
			}
		})
	}

	remap := remapTestPod(t, true).Spec.InitContainers[1]

	if !strings.Contains(strings.Join(remap.Args, " "), "--just-disable-auto-failover") {
		t.Errorf("expected the remap to turn off auto failover, got %v", remap.Args)
	}

	// The tool breaks the restored config if it writes over the folder it reads from.
	if !strings.Contains(strings.Join(remap.Args, " "), `--output-path "$D/.snapshot-restore-remap"`) {
		t.Errorf("expected the remap to write to its own folder, got %v", remap.Args)
	}

	// A done file that came back in the backup from an earlier restore mustn't stop the remap.
	script := strings.Join(remap.Args, " ")
	if !strings.Contains(script, `[ ! -e "$D/.snapshot-restore-remapped-3f2a9c1e-5b7d-4e8f-9a6c-2d1b0e4f7a83" ]`) || !strings.Contains(script, `"$D"/.snapshot-restore-remapped*`) {
		t.Errorf("expected the done file to be named after the restore, and older ones removed, got %v", remap.Args)
	}

	mounts := map[string]string{}
	for _, mount := range remap.VolumeMounts {
		mounts[mount.MountPath] = mount.SubPath
	}

	if !reflect.DeepEqual(mounts, map[string]string{couchbaseVolumeDefaultConfigDir: defaultSubPathName, couchbaseVolumeDefaultEtcDir: etcSubPathName}) {
		t.Errorf("expected the remap to mount the volume like the Couchbase container, got %v", remap.VolumeMounts)
	}

	pod := remapTestPod(t, true)
	if !reflect.DeepEqual(pod.Spec.InitContainers[1].Resources, pod.Spec.Containers[0].Resources) {
		t.Errorf("expected the remap to get the Couchbase container's resources, got %v", pod.Spec.InitContainers[1].Resources)
	}
}

// TestRemapInitContainerBadRestoreID checks a restored volume whose restore UID isn't one k8s would
// make doesn't get a pod, since the UID goes into the init container's script.
func TestRemapInitContainerBadRestoreID(t *testing.T) {
	cluster := &couchbasev2.CouchbaseCluster{ObjectMeta: metav1.ObjectMeta{Name: "rx", Namespace: "default"}}

	if _, err := couchbaseRemapInitContainer(cluster, "rx-0000-default-00", couchbasev2.ServerConfig{}, "couchbase/server:7.6.8", "x\"; touch /tmp/y; \""); err == nil {
		t.Errorf("expected a bad restore UID to be refused")
	}

	if _, err := couchbaseRemapInitContainer(cluster, "rx-0000-default-00", couchbasev2.ServerConfig{}, "couchbase/server:7.6.8", ""); err == nil {
		t.Errorf("expected a missing restore UID to be refused")
	}
}
