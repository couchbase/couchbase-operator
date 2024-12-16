package v2

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	v1 "k8s.io/api/core/v1"

	"testing"
)

func TestCheckChangeConstraintsMigration(t *testing.T) {
	testcases := []struct {
		name                 string
		currentMigrationSpec *couchbasev2.ClusterAssimilationSpec
		updatedMigrationSpec *couchbasev2.ClusterAssimilationSpec
		migratingCondition   v1.ConditionStatus
		expectedErr          string
	}{
		{
			name:                 "reject adding migration to existing cluster",
			currentMigrationSpec: nil,
			updatedMigrationSpec: &couchbasev2.ClusterAssimilationSpec{},
			migratingCondition:   v1.ConditionFalse,
			expectedErr:          "spec.migration cannot be added to a pre-existing cluster",
		},
		{
			name:                 "reject removing migration field during migration",
			currentMigrationSpec: &couchbasev2.ClusterAssimilationSpec{},
			updatedMigrationSpec: nil,
			migratingCondition:   v1.ConditionTrue,
			expectedErr:          "spec.migration cannot be removed during migration",
		},
		{
			name: "reject changing host name during migration",
			currentMigrationSpec: &couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "some.host.name",
			},
			updatedMigrationSpec: &couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "different.host.name",
			},
			migratingCondition: v1.ConditionTrue,
			expectedErr:        "spec.migration.unmanagedClusterHost cannot be changed during migration",
		},
		{
			name: "allow increasing numUnmanagedNodes during migration",
			currentMigrationSpec: &couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "some.host.name",
				NumUnmanagedNodes:    2,
			},
			updatedMigrationSpec: &couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "some.host.name",
				NumUnmanagedNodes:    4,
			},
			migratingCondition: v1.ConditionTrue,
			expectedErr:        "",
		},
	}

	for _, testcase := range testcases {
		status := couchbasev2.ClusterStatus{Conditions: []couchbasev2.ClusterCondition{{Status: testcase.migratingCondition, Type: couchbasev2.ClusterConditionMigrating}}}

		currentCluster := &couchbasev2.CouchbaseCluster{
			Spec: couchbasev2.ClusterSpec{Migration: testcase.currentMigrationSpec,
				Image: "couchbase/server:7.6.2"},
			Status: status,
		}

		updatedCluster := &couchbasev2.CouchbaseCluster{
			Spec: couchbasev2.ClusterSpec{Migration: testcase.updatedMigrationSpec,
				Image: "couchbase/server:7.6.2"},
		}

		err := checkChangeConstraintsMigration(currentCluster, updatedCluster)

		if (err == nil && testcase.expectedErr != "") || (err != nil && (testcase.expectedErr == "" || err.Error() != testcase.expectedErr)) {
			t.Errorf("test %s failed, expected error %s, got %s", testcase.name, testcase.expectedErr, err)
		}
	}
}

func TestCheckForVersionChange(t *testing.T) {
	testcases := []struct {
		name           string
		currentVersion string
		updatedVersion string
		expectedChange bool
		expectedError  string
	}{
		{
			name:           "has version downgrade",
			currentVersion: "couchbase/server:7.6.2",
			updatedVersion: "couchbase/server:7.6.1",
			expectedChange: true,
			expectedError:  "",
		},
		{
			name:           "has version upgrade",
			currentVersion: "couchbase/server:7.6.0",
			updatedVersion: "couchbase/server:7.6.2",
			expectedChange: true,
			expectedError:  "",
		},
		{
			name:           "no version change",
			currentVersion: "couchbase/server:7.6.2",
			updatedVersion: "couchbase/server:7.6.2",
			expectedChange: false,
			expectedError:  "",
		},
		{
			name:           "invalid version string",
			currentVersion: "couchbase/server:7.6.2",
			updatedVersion: "couchbase/server",
			expectedChange: false,
			expectedError:  "version error: invalid image string couchbase/server",
		},
	}

	for _, testcase := range testcases {
		res, err := checkForVersionChange(
			&couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{Image: testcase.currentVersion}},
			&couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{Image: testcase.updatedVersion}})
		if res != testcase.expectedChange || (err == nil && testcase.expectedError != "") || (err != nil && (testcase.expectedError == "" || err.Error() != testcase.expectedError)) {
			t.Errorf("%s failed, expected check to return %t with error %s, got %t with error %s", testcase.name, testcase.expectedChange, testcase.expectedError, res, err)
		}
	}
}
