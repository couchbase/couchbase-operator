/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package tlsutil

import (
	"testing"
)

type testVersion struct {
	versionString string
	version       Version
}

func TestValidNewVersion(t *testing.T) {
	versions := []testVersion{
		{"TLS1.2", Version{[2]int{1, 2}}},
		{"TLs1.99", Version{[2]int{1, 99}}},
		{"tls1.3", Version{[2]int{1, 3}}},
		{"tLs1.0", Version{[2]int{1, 0}}},
	}

	for _, v := range versions {
		if version, err := NewVersion(v.versionString); err != nil {
			t.Errorf("expected version %s to be valid but got err %s", v.versionString, err)
		} else if v.version.semver != version.semver {
			t.Errorf("expected version %s to have semver %v but got %v", v.versionString, v.version.semver, version.semver)
		}
	}
}

func TestInvalidNewVersion(t *testing.T) {
	versions := []string{
		"SLT1.2",
		"TLS.2",
		"SSL1.2",
		"Kubernetes",
		"TLS1.2.3",
		"TLS12",
	}

	for _, v := range versions {
		if _, err := NewVersion(v); err == nil {
			t.Errorf("expected version %s to be invalid but NewVersion succeeded", v)
		}
	}
}
