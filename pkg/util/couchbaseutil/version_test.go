package couchbaseutil

import (
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
)

func TestCouchbaseVersionEnterprise(t *testing.T) {
	versionStr := "enterprise-5.5.0"

	version, err := NewVersion(versionStr)
	if err != nil {
		t.Fatal(err)
	}

	if version.Version() != versionStr {
		t.Errorf("expected version=%s, got=%s", versionStr, version.Version())
	}

	if version.Prefix() != "enterprise" {
		t.Errorf("expected prefix=%s, got=%s", "enterprise", version.Prefix())
	}

	if len(version.semver) != 3 {
		t.Errorf("incorrect version format: %v", version.semver)
	} else if version.Major() != 5 || version.Minor() != 5 || version.Patch() != 0 {
		t.Errorf("expected semver=%s, got=%s", "enterprise", version.prefix)
	}
}

func TestCouchbaseVersionBeta(t *testing.T) {
	versionStr := "5.5.0-beta"

	version, err := NewVersion(versionStr)
	if err != nil {
		t.Fatal(err)
	}

	if version.Version() != versionStr {
		t.Errorf("expected version=%s, got=%s", versionStr, version.Version())
	}

	if version.Prefix() != "beta" {
		t.Errorf("expected prefix=%s, got=%s", "beta", version.Prefix())
	}

	if len(version.semver) != 3 {
		t.Errorf("incorrect version format: %v", version.semver)
	} else if version.Major() != 5 || version.Minor() != 5 || version.Patch() != 0 {
		t.Errorf("expected semver=%s, got=%s", "enterprise", version.prefix)
	}
}

func TestCouchbaseVersionOnly(t *testing.T) {
	versionStr := "5.5.0"

	version, err := NewVersion(versionStr)
	if err != nil {
		t.Fatal(err)
	}

	if version.Version() != versionStr {
		t.Errorf("expected version=%s, got=%s", versionStr, version.Version())
	}

	if version.Prefix() != "" {
		t.Errorf(`expected prefix="", got=%s"`, version.Prefix())
	}

	if len(version.semver) != 3 {
		t.Errorf("incorrect version format: %v", version.semver)
	} else if version.Major() != 5 || version.Minor() != 5 || version.Patch() != 0 {
		t.Errorf("expected semver=%s, got=%s", "enterprise", version.prefix)
	}
}

func TestCouchbaseVersionMin(t *testing.T) {
	versionStr := "enterprise-5.0.1"

	err := VerifyVersion(versionStr)
	if err == nil {
		t.Errorf("allowed version %s below min version: %s", versionStr, constants.CouchbaseVersionMin)
	}
}

func TestCouchbaseVersionInvalidSemver(t *testing.T) {
	versionStr := "enterprise-5.a.0"

	_, err := NewVersion(versionStr)
	if err == nil {
		t.Errorf("expected version parsing to fail: %s", versionStr)
	}
}

func TestCouchbaseVersionMissingSemver(t *testing.T) {
	versionStr := "beta"

	_, err := NewVersion(versionStr)
	if err == nil {
		t.Errorf("expected version parsing to fail: %s", versionStr)
	}
}

func TestCouchbaseVersionEmpty(t *testing.T) {
	versionStr := ""

	_, err := NewVersion(versionStr)
	if err == nil {
		t.Errorf("expected version parsing to fail: %s", versionStr)
	}
}
