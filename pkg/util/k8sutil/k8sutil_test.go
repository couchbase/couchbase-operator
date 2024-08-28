package k8sutil

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
)

func TestCouchbaseVersion(t *testing.T) {
	type test struct {
		input string
		want  string
	}

	tests := []test{
		{input: "couchbase/server:7.0.3", want: "7.0.3"},
		{input: "couchbase/server:community-7.1.1", want: "community-7.1.1"},
		{input: "couchbase/server:enterprise-7.1.0-aarch64", want: "enterprise-7.1.0-aarch64"},
		{input: "couchbase/exporter:1.0.6", want: "1.0.6"},
		{input: "iamarebel/iusemyownrepo:41.90.980", want: "41.90.980"},
	}

	for _, tc := range tests {
		got, err := CouchbaseVersion(tc.input)
		if err != nil {
			t.Fatal(err)
		}

		if got != tc.want {
			t.Errorf("expected: %v, got: %v", tc.want, got)
		}
	}
}

func TestKubernetesVersion(t *testing.T) {
	version, err := ParseKubernetesVersion("1", "9", "d283541654")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_9 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_9, version)
	}

	version, err = ParseKubernetesVersion("1", "9+", "")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_9 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_9, version)
	}

	version, err = ParseKubernetesVersion("", "", "v1.9.1+a0ce1bc657")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_9 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_9, version)
	}

	version, err = ParseKubernetesVersion("", "", "v1.9.1111+a0ce1bc657")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_9 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_9, version)
	}

	version, err = ParseKubernetesVersion("", "", "v1.9.111111a0ce1bc657")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_9 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_9, version)
	}

	version, err = ParseKubernetesVersion("1", "9+", "v1.9.7-2+231cc32d0a1119")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_9 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_9, version)
	}

	version, err = ParseKubernetesVersion("", "", "v1.9.7-2+231cc32d0a1119")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_9 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_9, version)
	}

	version, err = ParseKubernetesVersion("1", "10", "v1.10.1-2+7d2976e4bcbeb9")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_10 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_10, version)
	}

	version, err = ParseKubernetesVersion("1", "10+", "v1.10.1-2+7d2976e4bcbeb9")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_10 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_10, version)
	}

	version, err = ParseKubernetesVersion("", "", "v1.10.1-2+7d2976e4bcbeb9")
	if err != nil {
		t.Fatal(err)
	}

	if version != constants.KubernetesVersion1_10 {
		t.Errorf("expected version=%s, got=%s", constants.KubernetesVersion1_10, version)
	}
}

func TestNegKubernetesVersion(t *testing.T) {
	version, err := ParseKubernetesVersion("1", "", "d283541654")
	if err == nil || version != constants.KubernetesVersionUnknown {
		t.Errorf("expected parse error for unknown version =%s", version)
	}

	version, err = ParseKubernetesVersion("", "", "1.10.1-2+7d2976e4bcbeb9")
	if err == nil || version != constants.KubernetesVersionUnknown {
		t.Errorf("expected parse error for unknown version =%s", version)
	}

	version, err = ParseKubernetesVersion("1", "a", "")
	if err == nil || version != constants.KubernetesVersionUnknown {
		t.Errorf("expected parse error for unknown version =%s", version)
	}

	version, err = ParseKubernetesVersion("b10", "7", "")
	if err == nil || version != constants.KubernetesVersionUnknown {
		t.Errorf("expected parse error for unknown version =%s", version)
	}

	version, err = ParseKubernetesVersion("", "", "+110")
	if err == nil || version != constants.KubernetesVersionUnknown {
		t.Errorf("expected parse error for unknown version =%s", version)
	}

	version, err = ParseKubernetesVersion("1", "+1", "+110")
	if err == nil || version != constants.KubernetesVersionUnknown {
		t.Errorf("expected parse error for unknown version =%s", version)
	}
}

func TestGetTotalPVCMemoryByApp(t *testing.T) {
	type pvcMockData struct {
		label  string
		phase  v1.PersistentVolumeClaimPhase
		memory string
	}

	type totalPVCMemoryTestCase struct {
		appLabel string
		pvcData  []pvcMockData
		expected int64
	}

	testcases := []totalPVCMemoryTestCase{
		{
			appLabel: "couchbase",
			pvcData: []pvcMockData{
				{
					label:  "couchbase",
					phase:  v1.ClaimBound,
					memory: "5Gi",
				},
				{
					label:  "couchbase",
					phase:  v1.ClaimBound,
					memory: "500Mi",
				},
				{
					label:  "couchbase",
					phase:  v1.ClaimPending,
					memory: "1Gi",
				},
			},
			expected: int64(5892997120),
		},
		{
			appLabel: "couchbase",
			pvcData: []pvcMockData{
				{
					label:  "invalid-label",
					phase:  v1.ClaimBound,
					memory: "5Gi",
				},
			},
			expected: 0,
		},
		{
			appLabel: "invalid-app-label",
			pvcData: []pvcMockData{
				{
					label:  "couchbase",
					phase:  v1.ClaimBound,
					memory: "5Gi",
				},
			},
			expected: 0,
		},
		{
			appLabel: "couchbase",
			pvcData:  []pvcMockData{},
			expected: 0,
		},
	}

	for _, testcase := range testcases {
		var pvcs []*v1.PersistentVolumeClaim

		for _, mockPVCData := range testcase.pvcData {
			pvc := v1.PersistentVolumeClaim{
				Status: v1.PersistentVolumeClaimStatus{
					Capacity: map[v1.ResourceName]resource.Quantity{
						v1.ResourceStorage: resource.MustParse(mockPVCData.memory),
					},
					Phase: mockPVCData.phase,
				},
				ObjectMeta: v12.ObjectMeta{Labels: map[string]string{constants.LabelApp: mockPVCData.label}},
			}
			pvcs = append(pvcs, &pvc)
		}

		result := GetTotalPVCMemoryByApp(pvcs, testcase.appLabel)

		if result != testcase.expected {
			t.Errorf("expected total PVC memory in use by %s to be %d, got %d", testcase.appLabel, testcase.expected, result)
		}
	}
}
