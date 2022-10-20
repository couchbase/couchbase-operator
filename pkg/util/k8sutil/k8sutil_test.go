package k8sutil

import (
	"testing"

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
