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

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
)

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
