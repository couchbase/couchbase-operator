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
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
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

// TestGenerateConsoleServiceType checks the admin console service type resolution.
// An unset adminConsoleServiceType must fall back to NodePort, an explicit type must
// be followed, and adminConsoleServiceTemplate must take precedence over both.
func TestGenerateConsoleServiceType(t *testing.T) {
	tests := []struct {
		name     string
		spec     couchbasev2.CouchbaseClusterNetworkingSpec
		expected v1.ServiceType
	}{
		{
			name:     "unset defaults to NodePort",
			spec:     couchbasev2.CouchbaseClusterNetworkingSpec{ExposeAdminConsole: true},
			expected: v1.ServiceTypeNodePort,
		},
		{
			name: "explicit LoadBalancer is honoured",
			spec: couchbasev2.CouchbaseClusterNetworkingSpec{
				ExposeAdminConsole:      true,
				AdminConsoleServiceType: v1.ServiceTypeLoadBalancer,
			},
			expected: v1.ServiceTypeLoadBalancer,
		},
		{
			name: "template takes precedence over the default",
			spec: couchbasev2.CouchbaseClusterNetworkingSpec{
				ExposeAdminConsole: true,
				AdminConsoleServiceTemplate: &couchbasev2.ServiceTemplateSpec{
					Spec: &v1.ServiceSpec{Type: v1.ServiceTypeClusterIP},
				},
			},
			expected: v1.ServiceTypeClusterIP,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &couchbasev2.CouchbaseCluster{
				ObjectMeta: v12.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       couchbasev2.ClusterSpec{Networking: tc.spec},
			}

			if got := generateConsoleService(cluster).Spec.Type; got != tc.expected {
				t.Errorf("expected admin console service type %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestApplyIPFamilyConfigPriority(t *testing.T) {
	tests := []struct {
		family couchbasev2.AddressFamily
		want   v1.IPFamily
	}{
		{couchbasev2.IPv4Priority, v1.IPv4Protocol},
		{couchbasev2.IPv6Priority, v1.IPv6Protocol},
	}

	for _, test := range tests {
		family := test.family
		cluster := &couchbasev2.CouchbaseCluster{}
		cluster.Spec.Networking.AddressFamily = &family

		service := &v1.Service{}
		applyIPFamilyConfig(service, cluster)

		// Only the primary family may be requested, single-stack clusters reject the secondary.
		if len(service.Spec.IPFamilies) != 1 || service.Spec.IPFamilies[0] != test.want {
			t.Errorf("%s: expected ipFamilies [%s], got %v", family, test.want, service.Spec.IPFamilies)
		}

		if service.Spec.IPFamilyPolicy == nil || *service.Spec.IPFamilyPolicy != v1.IPFamilyPolicyPreferDualStack {
			t.Errorf("%s: expected PreferDualStack policy, got %v", family, service.Spec.IPFamilyPolicy)
		}
	}
}

func TestPreferDualStackSatisfied(t *testing.T) {
	newService := func(policy v1.IPFamilyPolicy, families ...v1.IPFamily) *v1.Service {
		return &v1.Service{Spec: v1.ServiceSpec{IPFamilyPolicy: &policy, IPFamilies: families}}
	}

	tests := []struct {
		name      string
		current   *v1.Service
		requested *v1.Service
		want      bool
	}{
		{
			name:      "secondary family allocated by dual-stack cluster",
			current:   newService(v1.IPFamilyPolicyPreferDualStack, v1.IPv4Protocol, v1.IPv6Protocol),
			requested: newService(v1.IPFamilyPolicyPreferDualStack, v1.IPv4Protocol),
			want:      true,
		},
		{
			name:      "single-stack cluster",
			current:   newService(v1.IPFamilyPolicyPreferDualStack, v1.IPv4Protocol),
			requested: newService(v1.IPFamilyPolicyPreferDualStack, v1.IPv4Protocol),
			want:      true,
		},
		{
			name:      "primary family differs",
			current:   newService(v1.IPFamilyPolicyPreferDualStack, v1.IPv6Protocol, v1.IPv4Protocol),
			requested: newService(v1.IPFamilyPolicyPreferDualStack, v1.IPv4Protocol),
			want:      false,
		},
		{
			name:      "current is single stack",
			current:   newService(v1.IPFamilyPolicySingleStack, v1.IPv4Protocol),
			requested: newService(v1.IPFamilyPolicyPreferDualStack, v1.IPv4Protocol),
			want:      false,
		},
		{
			name:      "requested is single stack",
			current:   newService(v1.IPFamilyPolicyPreferDualStack, v1.IPv4Protocol, v1.IPv6Protocol),
			requested: newService(v1.IPFamilyPolicySingleStack, v1.IPv4Protocol),
			want:      false,
		},
	}

	for _, test := range tests {
		if got := preferDualStackSatisfied(test.current, test.requested); got != test.want {
			t.Errorf("%s: expected %v, got %v", test.name, test.want, got)
		}
	}
}
