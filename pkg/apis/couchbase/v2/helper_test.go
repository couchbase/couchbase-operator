package v2

import (
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	LowVersionImage      = "couchbase/server:6.5.0"
	LowVersionHashImage  = "couchbase/server@sha256:b21765563ba510c0b1ca43bc9287567761d901b8d00fee704031e8f405bfa501"
	MedVersionImage      = "couchbase/server:7.0.1"
	MedVersionHashImage  = "couchbase/server@sha256:fa5d031059e005cd9d85983b1a120dab37fc60136cb699534b110f49d27388f7"
	HighVersionImage     = "couchbase/server:7.1.3"
	HighVersionHashImage = "couchbase/server@sha256:d0d1734a98fea7639793873d9a54c27d6be6e7838edad2a38e8d451d66be3497"
)

func TestIsNativeAuditCleanupEnabled(t *testing.T) {
	c := CouchbaseCluster{
		Spec: ClusterSpec{
			Logging: CouchbaseClusterLoggingSpec{
				Audit: &CouchbaseClusterAuditLoggingSpec{
					Enabled: true,
					Rotation: &CouchbaseClusterLogRotationSpec{
						PruneAge: &v1.Duration{Duration: 0 * time.Second},
					},
				},
			},
		},
	}

	if c.IsNativeAuditCleanupEnabled() {
		t.Error("expected IsNativeAuditCleanupEnabled to return false, but returned true")
	}

	c.Spec.Logging.Audit.Rotation.PruneAge = &v1.Duration{Duration: 10 * time.Second}

	if !c.IsNativeAuditCleanupEnabled() {
		t.Error("expected IsNativeAuditCleanupEnabled to return true, but returned false")
	}
}

func TestUnmarshalDurationWithNegativeOverride(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		crdEntry   string
		negValue   time.Duration
		defaultVal time.Duration
		expected   time.Duration
	}{
		{
			crdEntry:   "1s",
			negValue:   time.Second,
			defaultVal: time.Second,
			expected:   time.Second,
		},
		{
			crdEntry: "-1",
			negValue: time.Second * -1,
			expected: time.Second * -1,
		},
		{
			crdEntry: "-1",
			negValue: time.Millisecond * -1,
			expected: time.Millisecond * -1,
		},
		{
			crdEntry: "0",
			expected: time.Second * 0,
		},
		{
			crdEntry:   "",
			defaultVal: time.Second,
			expected:   time.Second,
		},
		{
			crdEntry:   "",
			defaultVal: time.Minute,
			expected:   time.Minute,
		},
	}

	for _, testcase := range testcases {
		duration, err := unmarshalDurationWithNegativeOverride(testcase.crdEntry, testcase.negValue, testcase.defaultVal)
		if err != nil {
			t.Fatal(err)
		}

		expected := v1.Duration{Duration: testcase.expected}
		if !reflect.DeepEqual(duration, &expected) {
			t.Errorf("expected duration to be %v, but got %v", expected, duration)
		}
	}
}

func TestMarshalDurationWithNegativeOverride(t *testing.T) {
	testcases := []struct {
		duration *v1.Duration
		negValue time.Duration
		expected string
	}{
		{
			duration: &v1.Duration{Duration: time.Second},
			expected: "1s",
		},
		{
			duration: &v1.Duration{Duration: time.Minute},
			expected: "1m0s",
		},
		{
			duration: &v1.Duration{Duration: time.Second * -1},
			negValue: time.Second * -1,
			expected: "-1",
		},
		{
			duration: &v1.Duration{Duration: time.Minute * -1},
			negValue: time.Minute * -1,
			expected: "-1",
		},
		{
			duration: &v1.Duration{Duration: time.Minute * -1},
			negValue: time.Second * -1,
			expected: "-1m0s",
		},
		{
			duration: &v1.Duration{Duration: time.Second * 0},
			negValue: time.Second * -1,
			expected: "0",
		},
		{
			duration: nil,
			expected: "",
		},
	}

	for _, testcase := range testcases {
		expected := testcase.expected

		actual := marshalDurationWithNegativeOverride(testcase.duration, testcase.negValue)
		if actual != expected {
			t.Errorf("expected %v, but got %v", expected, actual)
		}
	}
}

func TestMigrateDeprecatedRoles(t *testing.T) {
	tests := []struct {
		name     string
		input    []Role
		expected []Role
	}{
		{
			name: "migrate security_admin_local",
			input: []Role{
				{Name: RoleSecurityAdminLocal},
			},
			expected: []Role{
				{Name: RoleSecurityAdmin},
				{Name: RoleUserAdminLocal},
			},
		},
		{
			name: "migrate security_admin_external",
			input: []Role{
				{Name: RoleSecurityAdminExternal},
			},
			expected: []Role{
				{Name: RoleSecurityAdmin},
				{Name: RoleUserAdminExternal},
			},
		},
		{
			name: "no migration for new roles",
			input: []Role{
				{Name: RoleUserAdminLocal},
				{Name: RoleSecurityAdmin},
			},
			expected: []Role{
				{Name: RoleUserAdminLocal},
				{Name: RoleSecurityAdmin},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MigrateDeprecatedRoles(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %v, but got %v", tt.expected, result)
			}
		})
	}
}
