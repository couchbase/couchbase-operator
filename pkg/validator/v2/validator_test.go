/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package v2

import (
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	couchbasefake "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned/fake"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

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

		err := checkChangeConstraintsMigration(nil, currentCluster, updatedCluster)

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

func TestValidateEncryptionKeyCircularDependencies(t *testing.T) {
	testcases := []struct {
		name          string
		keyMap        map[string]*couchbasev2.CouchbaseEncryptionKey
		expectedError string
		shouldFail    bool
	}{
		{
			name: "no dependencies - should pass",
			keyMap: createKeyMap([]keySpec{
				{name: "key-a", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated},
				{name: "key-b", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated},
			}),
			shouldFail: false,
		},
		{
			name: "linear chain - should pass",
			keyMap: createKeyMap([]keySpec{
				{name: "key-a", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-b"},
				{name: "key-b", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-c"},
				{name: "key-c", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated},
			}),
			shouldFail: false,
		},
		{
			name: "simple cycle A->B->A - should fail",
			keyMap: createKeyMap([]keySpec{
				{name: "key-a", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-b"},
				{name: "key-b", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-a"},
			}),
			expectedError: "circular dependency detected in encryption keys",
			shouldFail:    true,
		},
		{
			name: "self-reference - should fail",
			keyMap: createKeyMap([]keySpec{
				{name: "key-a", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-a"},
			}),
			expectedError: "circular dependency detected in encryption keys",
			shouldFail:    true,
		},
		{
			name: "complex cycle A->B->C->A - should fail",
			keyMap: createKeyMap([]keySpec{
				{name: "key-a", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-b"},
				{name: "key-b", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-c"},
				{name: "key-c", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-a"},
			}),
			expectedError: "circular dependency detected in encryption keys:",
			shouldFail:    true,
		},
		{
			name: "multiple separate cycles - should fail",
			keyMap: createKeyMap([]keySpec{
				{name: "key-a", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-b"},
				{name: "key-b", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-a"},
				{name: "key-c", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-d"},
				{name: "key-d", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-c"},
			}),
			expectedError: "circular dependency detected in encryption keys:",
			shouldFail:    true,
		},
		{
			name: "mixed auto-generated and AWS keys - should pass",
			keyMap: createKeyMap([]keySpec{
				{name: "key-a", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated, encryptWith: "key-b"},
				{name: "key-b", keyType: couchbasev2.CouchbaseEncryptionKeyTypeAWS}, // Non-auto-generated keys don't participate in dependency checks
			}),
			shouldFail: false,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			err := validateEncryptionKeyCircularDependencies(testcase.keyMap)

			if testcase.shouldFail {
				if err == nil {
					t.Errorf("expected validation to fail but it passed")
					return
				}
				if testcase.expectedError != "" && !strings.Contains(err.Error(), testcase.expectedError) {
					t.Errorf("expected error to contain '%s', got '%s'", testcase.expectedError, err.Error())
				}
			} else if err != nil {
				t.Errorf("expected validation to pass but got error: %s", err.Error())
			}
		})
	}
}

func TestCheckConstraintLoggingSidecarTLS(t *testing.T) {
	testcases := []struct {
		name        string
		clusterSpec *couchbasev2.CouchbaseCluster
		expectedErr string
	}{
		{
			name: "should allow valid TLS configuration with secrets",
			clusterSpec: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Logging: couchbasev2.CouchbaseClusterLoggingSpec{
						Server: &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
							Enabled: true,
							Sidecar: &couchbasev2.LogShipperSidecarSpec{
								TLS: &couchbasev2.LogShipperSidecarTLSSpec{
									MountPath:   "/fluent-bit/certs/",
									SecretNames: []string{"fluent-bit-ca", "fluent-bit-client-cert"},
								},
							},
						},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "should reject TLS configuration without secret names",
			clusterSpec: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Logging: couchbasev2.CouchbaseClusterLoggingSpec{
						Server: &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
							Enabled: true,
							Sidecar: &couchbasev2.LogShipperSidecarSpec{
								TLS: &couchbasev2.LogShipperSidecarTLSSpec{
									MountPath:   "/fluent-bit/certs/",
									SecretNames: []string{},
								},
							},
						},
					},
				},
			},
			expectedErr: "spec.logging.server.sidecar.tls.secretNames must contain at least one secret when TLS is configured",
		},
		{
			name: "should reject TLS configuration with empty mount path",
			clusterSpec: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Logging: couchbasev2.CouchbaseClusterLoggingSpec{
						Server: &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
							Enabled: true,
							Sidecar: &couchbasev2.LogShipperSidecarSpec{
								TLS: &couchbasev2.LogShipperSidecarTLSSpec{
									MountPath:   "",
									SecretNames: []string{"fluent-bit-ca"},
								},
							},
						},
					},
				},
			},
			expectedErr: "spec.logging.server.sidecar.tls.mountPath cannot be empty when TLS is configured",
		},
		{
			name: "should allow if server logging is disabled even if TLS is configured incorrrectly",
			clusterSpec: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Logging: couchbasev2.CouchbaseClusterLoggingSpec{
						Server: &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
							Enabled: false,
							Sidecar: &couchbasev2.LogShipperSidecarSpec{
								TLS: &couchbasev2.LogShipperSidecarTLSSpec{
									MountPath:   "",
									SecretNames: []string{"fluent-bit-ca"},
								},
							},
						},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "should allow if logging sidecar TLS is nil",
			clusterSpec: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Logging: couchbasev2.CouchbaseClusterLoggingSpec{
						Server: &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
							Sidecar: &couchbasev2.LogShipperSidecarSpec{},
						},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "should allow if logging server is nil",
			clusterSpec: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Logging: couchbasev2.CouchbaseClusterLoggingSpec{},
				},
			},
			expectedErr: "",
		},
		{
			name: "should allow if logging is nil",
			clusterSpec: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{},
			},
			expectedErr: "",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			err := checkConstraintLoggingSidecarTLS(nil, testcase.clusterSpec)

			if (err == nil && testcase.expectedErr != "") || (err != nil && err.Error() != testcase.expectedErr) {
				t.Errorf("test %s failed, expected error %s, got %s", testcase.name, testcase.expectedErr, err)
			}
		})
	}
}

// keySpec is a helper struct for creating test encryption keys.
type keySpec struct {
	name        string
	keyType     couchbasev2.CouchbaseEncryptionKeyType
	encryptWith string
}

// createKeyMap creates a map of encryption keys for testing.
func createKeyMap(specs []keySpec) map[string]*couchbasev2.CouchbaseEncryptionKey {
	keyMap := make(map[string]*couchbasev2.CouchbaseEncryptionKey)

	for _, spec := range specs {
		key := &couchbasev2.CouchbaseEncryptionKey{
			ObjectMeta: metav1.ObjectMeta{
				Name: spec.name,
			},
			Spec: couchbasev2.CouchbaseEncryptionKeySpec{
				KeyType: spec.keyType,
			},
		}

		if spec.keyType == couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated {
			key.Spec.AutoGenerated = &couchbasev2.CouchbaseEncryptionKeyAutoGenerated{}
			if spec.encryptWith != "" {
				key.Spec.AutoGenerated.EncryptWithKey = spec.encryptWith
			}
		}

		keyMap[spec.name] = key
	}

	return keyMap
}

func TestValidateAWSKeyARN(t *testing.T) {
	testcases := []struct {
		keyARN string
		valid  bool
	}{
		{
			keyARN: "arn:aws:kms:us-west-2:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
			valid:  true,
		},
		{
			keyARN: "arn:aws:kms:eu-west-2:111122223333:key/1234abcd-12ab-34cd-56ef-1234567890ab",
			valid:  true,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:111122223333:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  true,
		},
		{
			keyARN: "arnald:aws:kms:us-west-2:111122223333:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aWS:kms:us-west-2:111122223333:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:iam:us-west-2:111122223333:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:11112 2223333:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:1112223333:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:11112223333:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:1111222233333:key/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:111122223333:app/mrk-1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:111122223333:key/mrk-1234abcd12ab34cd 56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:111122223333:key/mrk/1234abcd12ab34cd56ef1234567890ab",
			valid:  false,
		},
		{
			keyARN: "arn:aws:kms:us-west-2:111122223333:key/mrk-1234abcd12ab34cd56ef1234567890ab!",
			valid:  false,
		},
		{
			keyARN: "",
			valid:  false,
		},
	}

	for _, testcase := range testcases {
		err := validateAWSKeyARN(testcase.keyARN)
		if testcase.valid && err != nil {
			t.Errorf("expected validation to pass but got error: %s", err)
		}

		if !testcase.valid && err == nil {
			t.Errorf("expected validation to fail but it passed")
		}
	}
}

func TestIsFullyUpgraded(t *testing.T) {
	testcases := []struct {
		name           string
		specImage      string
		statusVersion  string
		mixedMode      v1.ConditionStatus
		expectedResult bool
	}{
		{
			name:           "fully upgraded - spec matches status, no mixed mode",
			specImage:      "couchbase/server:7.6.7",
			statusVersion:  "7.6.7",
			mixedMode:      v1.ConditionFalse,
			expectedResult: true,
		},
		{
			name:           "not fully upgraded - spec doesn't match status",
			specImage:      "couchbase/server:8.0.0",
			statusVersion:  "7.6.7",
			mixedMode:      v1.ConditionFalse,
			expectedResult: false,
		},
		{
			name:           "not fully upgraded - in mixed mode",
			specImage:      "couchbase/server:7.6.7",
			statusVersion:  "7.6.7",
			mixedMode:      v1.ConditionTrue,
			expectedResult: false,
		},
		{
			name:           "not fully upgraded - spec matches status but in mixed mode",
			specImage:      "couchbase/server:8.0.0",
			statusVersion:  "8.0.0",
			mixedMode:      v1.ConditionTrue,
			expectedResult: false,
		},
		{
			name:           "unknown cluster version - trust the user",
			specImage:      "couchbase/server@sha256:8485d9a4f6a9f288e0b1bac2c89383387f1091cf3b53c22f667748e2b4c5dd33",
			statusVersion:  "8.0.0",
			mixedMode:      v1.ConditionFalse,
			expectedResult: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Image: tc.specImage,
				},
				Status: couchbasev2.ClusterStatus{
					CurrentVersion: tc.statusVersion,
				},
			}

			// Set mixed mode condition if needed
			if tc.mixedMode != "" {
				cluster.Status.Conditions = []couchbasev2.ClusterCondition{
					{
						Type:   couchbasev2.ClusterConditionMixedMode,
						Status: tc.mixedMode,
					},
				}
			}

			result, err := isFullyUpgraded(cluster)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tc.expectedResult {
				t.Errorf("expected %v, got %v", tc.expectedResult, result)
			}
		})
	}
}

func TestCheckConstraintAutoFailoverOnDataDiskNonResponsiveness(t *testing.T) {
	testcases := []struct {
		name        string
		image       string
		enabled     bool
		expectedErr string
	}{
		{
			name:        "should allow disk non-responsiveness on 8.0.0+",
			image:       "couchbase/server:8.0.0",
			enabled:     true,
			expectedErr: "",
		},
		{
			name:        "should allow disk non-responsiveness disabled on any version",
			image:       "couchbase/server:7.6.0",
			enabled:     false,
			expectedErr: "",
		},
		{
			name:        "should reject disk non-responsiveness on 7.6.0",
			image:       "couchbase/server:7.6.0",
			enabled:     true,
			expectedErr: "annotation cao.couchbase.com/autoFailoverOnDataDiskNonResponsiveness is not supported in Couchbase Server versions lower than 8.0.0",
		},
		{
			name:        "should reject disk non-responsiveness on 7.2.0",
			image:       "couchbase/server:7.2.0",
			enabled:     true,
			expectedErr: "annotation cao.couchbase.com/autoFailoverOnDataDiskNonResponsiveness is not supported in Couchbase Server versions lower than 8.0.0",
		},
		{
			name:        "should allow on 8.0.1+",
			image:       "couchbase/server:8.0.1",
			enabled:     true,
			expectedErr: "",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			cluster := &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Image: testcase.image,
					ClusterSettings: couchbasev2.ClusterConfig{
						AutoFailoverOnDataDiskNonResponsiveness: testcase.enabled,
					},
				},
			}

			err := checkConstraintAutoFailoverOnDataDiskNonResponsiveness(nil, cluster)

			if testcase.expectedErr == "" {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing '%s' but got none", testcase.expectedErr)
				} else if !strings.Contains(err.Error(), testcase.expectedErr) {
					t.Errorf("expected error containing '%s' but got '%s'", testcase.expectedErr, err.Error())
				}
			}
		})
	}
}

func TestCheckConstraintAutoFailoverOnDataDiskNonResponsivenessTimePeriod(t *testing.T) {
	testcases := []struct {
		name        string
		enabled     bool
		timePeriod  *metav1.Duration
		expectedErr string
	}{
		{
			name:        "should allow 5 seconds (minimum)",
			enabled:     true,
			timePeriod:  &metav1.Duration{Duration: 5 * 1e9},
			expectedErr: "",
		},
		{
			name:        "should allow 120 seconds",
			enabled:     true,
			timePeriod:  &metav1.Duration{Duration: 120 * 1e9},
			expectedErr: "",
		},
		{
			name:        "should allow 3600 seconds (maximum)",
			enabled:     true,
			timePeriod:  &metav1.Duration{Duration: 3600 * 1e9},
			expectedErr: "",
		},
		{
			name:        "should reject 4 seconds (below minimum)",
			enabled:     true,
			timePeriod:  &metav1.Duration{Duration: 4 * 1e9},
			expectedErr: "annotation cao.couchbase.com/autoFailoverOnDataDiskNonResponsivenessTimePeriod in body should be greater than or equal to 5s",
		},
		{
			name:        "should reject 3601 seconds (above maximum)",
			enabled:     true,
			timePeriod:  &metav1.Duration{Duration: 3601 * 1e9},
			expectedErr: "annotation cao.couchbase.com/autoFailoverOnDataDiskNonResponsivenessTimePeriod in body should be less than or equal to 3600s",
		},
		{
			name:        "should reject nil when enabled",
			enabled:     true,
			timePeriod:  nil,
			expectedErr: "annotation cao.couchbase.com/autoFailoverOnDataDiskNonResponsivenessTimePeriod in body is required",
		},
		{
			name:        "should skip validation when feature is disabled",
			enabled:     false,
			timePeriod:  nil,
			expectedErr: "",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			cluster := &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					ClusterSettings: couchbasev2.ClusterConfig{
						AutoFailoverOnDataDiskNonResponsiveness:           testcase.enabled,
						AutoFailoverOnDataDiskNonResponsivenessTimePeriod: testcase.timePeriod,
					},
				},
			}

			err := checkConstraintAutoFailoverOnDataDiskNonResponsivenessTimePeriod(nil, cluster)

			if testcase.expectedErr == "" {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing '%s' but got none", testcase.expectedErr)
				} else if !strings.Contains(err.Error(), testcase.expectedErr) {
					t.Errorf("expected error containing '%s' but got '%s'", testcase.expectedErr, err.Error())
				}
			}
		})
	}
}

func TestCheckConstraintBackupNameLength(t *testing.T) {
	testcases := []struct {
		name        string
		backupName  string
		strategy    couchbasev2.Strategy
		expectedErr string
	}{
		{
			name:       "short name with full_incremental is valid",
			backupName: "my-backup",
			strategy:   couchbasev2.FullIncremental,
		},
		{
			name:       "name at max 44 chars for logs collector is valid with immediate strategy",
			backupName: strings.Repeat("a", 44),
			strategy:   couchbasev2.ImmediateFull,
		},
		{
			name:        "name exceeding 44 chars fails logs collector limit",
			backupName:  strings.Repeat("a", 45),
			strategy:    couchbasev2.FullIncremental,
			expectedErr: "collect-logs job name would exceed 63 characters",
		},
		{
			name:       "name at max 40 chars with full_incremental is valid",
			backupName: strings.Repeat("b", 40),
			strategy:   couchbasev2.FullIncremental,
		},
		{
			name:        "name exceeding cronjob limit with full_incremental",
			backupName:  strings.Repeat("b", 41),
			strategy:    couchbasev2.FullIncremental,
			expectedErr: "CronJob suffix '-incremental' would exceed limit",
		},
		{
			name:       "name at max 40 chars with periodic_merge is valid",
			backupName: strings.Repeat("c", 40),
			strategy:   couchbasev2.PeriodicMerge,
		},
		{
			name:        "name exceeding cronjob limit with periodic_merge",
			backupName:  strings.Repeat("c", 41),
			strategy:    couchbasev2.PeriodicMerge,
			expectedErr: "CronJob suffix '-incremental' would exceed limit",
		},
		{
			name:       "name at max 47 chars with full_only is valid",
			backupName: strings.Repeat("d", 44),
			strategy:   couchbasev2.FullOnly,
		},
		{
			name:        "name exceeding logs collector limit with full_only",
			backupName:  strings.Repeat("d", 45),
			strategy:    couchbasev2.FullOnly,
			expectedErr: "collect-logs job name would exceed 63 characters",
		},
		{
			name:       "long name with immediate_full strategy only checks logs limit",
			backupName: strings.Repeat("e", 44),
			strategy:   couchbasev2.ImmediateFull,
		},
		{
			name:        "name exceeding logs limit with immediate_full strategy",
			backupName:  strings.Repeat("e", 45),
			strategy:    couchbasev2.ImmediateFull,
			expectedErr: "collect-logs job name would exceed 63 characters",
		},
		{
			name:       "immediate_incremental at logs limit is valid",
			backupName: strings.Repeat("f", 44),
			strategy:   couchbasev2.ImmediateIncremental,
		},
		{
			name:        "immediate_incremental over logs limit",
			backupName:  strings.Repeat("f", 45),
			strategy:    couchbasev2.ImmediateIncremental,
			expectedErr: "collect-logs job name would exceed 63 characters",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			backup := &couchbasev2.CouchbaseBackup{
				ObjectMeta: metav1.ObjectMeta{Name: testcase.backupName},
				Spec: couchbasev2.CouchbaseBackupSpec{
					Strategy: testcase.strategy,
				},
			}

			err := checkConstraintBackupNameLength(backup)

			if testcase.expectedErr == "" {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q but got none", testcase.expectedErr)
				} else if !strings.Contains(err.Error(), testcase.expectedErr) {
					t.Errorf("expected error containing %q but got %q", testcase.expectedErr, err.Error())
				}
			}
		})
	}
}

func TestCheckConstraintRestoreNameLength(t *testing.T) {
	testcases := []struct {
		name        string
		restoreName string
		expectedErr string
	}{
		{
			name:        "short restore name is valid",
			restoreName: "my-restore",
		},
		{
			name:        "restore name at 63 chars is valid",
			restoreName: strings.Repeat("a", 63),
		},
		{
			name:        "restore name at 64 chars exceeds label limit",
			restoreName: strings.Repeat("a", 64),
			expectedErr: "cannot be longer than 63 characters",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			restore := &couchbasev2.CouchbaseBackupRestore{
				ObjectMeta: metav1.ObjectMeta{Name: testcase.restoreName},
			}

			err := checkConstraintRestoreNameLength(nil, restore)

			if testcase.expectedErr == "" {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q but got none", testcase.expectedErr)
				} else if !strings.Contains(err.Error(), testcase.expectedErr) {
					t.Errorf("expected error containing %q but got %q", testcase.expectedErr, err.Error())
				}
			}
		})
	}
}

func TestCheckConstraintServerGroupsLabelOverride(t *testing.T) {
	testcases := []struct {
		name        string
		annotation  string
		expectedErr string
	}{
		{
			name:        "no annotation is valid",
			annotation:  "",
			expectedErr: "",
		},
		{
			name:        "standard zone label is valid",
			annotation:  "topology.kubernetes.io/zone",
			expectedErr: "",
		},
		{
			name:        "EKS nodegroup label is valid",
			annotation:  "eks.amazonaws.com/nodegroup",
			expectedErr: "",
		},
		{
			name:        "simple label key is valid",
			annotation:  "rack",
			expectedErr: "",
		},
		{
			name:        "label key with space is invalid",
			annotation:  "eks.amazonaws.com/node group",
			expectedErr: "not a valid Kubernetes label key",
		},
		{
			name:        "empty annotation value is treated as absent",
			annotation:  "",
			expectedErr: "",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			// The check reads the populated spec field (annotations.Populate runs before it in the
			// real path); set it directly here.
			cluster := &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{ServerGroupsLabelOverride: testcase.annotation},
			}

			err := checkConstraintServerGroupsLabelOverride(nil, cluster)

			if testcase.expectedErr == "" {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q but got none", testcase.expectedErr)
				} else if !strings.Contains(err.Error(), testcase.expectedErr) {
					t.Errorf("expected error containing %q but got %q", testcase.expectedErr, err.Error())
				}
			}
		})
	}
}

func TestCheckConstraintServerGroupsLabelOverrideTopologies(t *testing.T) {
	zone := func(az string) *couchbasev2.PodTemplate {
		return &couchbasev2.PodTemplate{Spec: v1.PodSpec{NodeSelector: map[string]string{"topology.kubernetes.io/zone": az}}}
	}

	testcases := []struct {
		name        string
		cluster     *couchbasev2.CouchbaseCluster
		expectedErr string
	}{
		{
			name: "override but a class declares no zone → rejected",
			cluster: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					ServerGroups: []string{"pg1", "pg2", "pg3"},
					Servers:      []couchbasev2.ServerConfig{{Name: "data"}},
				},
			},
			expectedErr: "must set the",
		},
		{
			name: "single AZ: every class declares the same zone + global serverGroups",
			cluster: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					ServerGroups: []string{"pg1", "pg2", "pg3"},
					Servers: []couchbasev2.ServerConfig{
						{Name: "data", Pod: zone("us-east-1a")},
						{Name: "query", Pod: zone("us-east-1a")},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "multi-AZ: per-class zone + per-class serverGroups",
			cluster: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Servers: []couchbasev2.ServerConfig{
						{Name: "data-a", ServerGroups: []string{"a1", "a2"}, Pod: zone("az-a")},
						{Name: "data-b", ServerGroups: []string{"b1", "b2"}, Pod: zone("az-b")},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "multi-AZ: a class missing its zone → rejected",
			cluster: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					Servers: []couchbasev2.ServerConfig{
						{Name: "data-a", ServerGroups: []string{"a1"}, Pod: zone("az-a")},
						{Name: "data-b", ServerGroups: []string{"b1"}},
					},
				},
			},
			expectedErr: "must set the",
		},
		{
			name: "global serverGroups with a per-class zone is allowed",
			cluster: &couchbasev2.CouchbaseCluster{
				Spec: couchbasev2.ClusterSpec{
					ServerGroups: []string{"pg1", "pg2"},
					Servers:      []couchbasev2.ServerConfig{{Name: "data", Pod: zone("az-a")}},
				},
			},
			expectedErr: "",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			// The check reads the populated spec field (annotations.Populate maps the
			// serverGroupsLabelOverride annotation here in the real path); set it directly.
			testcase.cluster.Spec.ServerGroupsLabelOverride = "eks.amazonaws.com/nodegroup"

			err := checkConstraintServerGroupsLabelOverride(nil, testcase.cluster)

			switch {
			case testcase.expectedErr == "" && err != nil:
				t.Errorf("expected no error but got: %s", err.Error())
			case testcase.expectedErr != "" && err == nil:
				t.Errorf("expected error containing %q but got none", testcase.expectedErr)
			case testcase.expectedErr != "" && !strings.Contains(err.Error(), testcase.expectedErr):
				t.Errorf("expected error containing %q but got %q", testcase.expectedErr, err.Error())
			}
		})
	}
}

// TestCheckBucketThrottleSettings checks the per bucket KV rate limiting validation, a bucket's
// reserved throughput must never be greater than its hard limit, otherwise the bucket would be
// guaranteed more than it is ever allowed to use. This validation runs on the raw CR values
// before any defaulting (the 0 / max-uint64 defaults are applied later, during reconcile, not
// here), so an omitted field is nil at this point and there is nothing to compare, so no error
// is expected.
func TestCheckBucketThrottleSettings(t *testing.T) {
	// The throttle fields are pointers so that nil can mean "the user did not set this". int64Ptr
	// is a small helper to build those pointers inline in the test cases below.
	int64Ptr := func(v int64) *int64 { return &v }

	testcases := []struct {
		name        string
		reserved    *int64
		hardLimit   *int64
		expectedErr string
	}{
		{
			name:        "reserved below hard limit is allowed",
			reserved:    int64Ptr(3000),
			hardLimit:   int64Ptr(6000),
			expectedErr: "",
		},
		{
			name:        "reserved equal to hard limit is allowed",
			reserved:    int64Ptr(6000),
			hardLimit:   int64Ptr(6000),
			expectedErr: "",
		},
		{
			name:        "reserved above hard limit is rejected",
			reserved:    int64Ptr(7000),
			hardLimit:   int64Ptr(6000),
			expectedErr: "spec.throttleReserved (7000) must be less than or equal to spec.throttleHardLimit (6000)",
		},
		{
			name:        "only reserved set is allowed",
			reserved:    int64Ptr(3000),
			hardLimit:   nil,
			expectedErr: "",
		},
		{
			name:        "only hard limit set is allowed",
			reserved:    nil,
			hardLimit:   int64Ptr(6000),
			expectedErr: "",
		},
		{
			name:        "neither set is allowed",
			reserved:    nil,
			hardLimit:   nil,
			expectedErr: "",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			err := checkBucketThrottleSettings(testcase.reserved, testcase.hardLimit)

			// An empty expectedErr means we expect success, otherwise the returned error must
			// contain the expected message.
			switch {
			case testcase.expectedErr == "" && err != nil:
				t.Errorf("expected no error but got: %s", err.Error())
			case testcase.expectedErr != "" && err == nil:
				t.Errorf("expected error containing %q but got none", testcase.expectedErr)
			case testcase.expectedErr != "" && !strings.Contains(err.Error(), testcase.expectedErr):
				t.Errorf("expected error containing %q but got %q", testcase.expectedErr, err.Error())
			}
		})
	}
}

// TestCheckConstraintsReplicationMissingBucket covers the admission behaviour of a
// CouchbaseReplication whose source bucket resource does not exist yet. Such a
// replication must be admitted with a warning so that it can be applied before, or
// alongside, the bucket it references, a bucket that does exist but cannot be
// replicated remains a hard failure.
func TestCheckConstraintsReplicationMissingBucket(t *testing.T) {
	const (
		namespace  = "default"
		bucketName = "source"
	)

	cluster := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: namespace,
		},
		Spec: couchbasev2.ClusterSpec{
			Buckets: couchbasev2.Buckets{
				Managed: true,
			},
			XDCR: couchbasev2.XDCR{
				Managed: true,
				RemoteClusters: []couchbasev2.RemoteCluster{
					{
						Name:     "remote",
						Hostname: "remote.example.com",
					},
				},
			},
		},
	}

	replication := &couchbasev2.CouchbaseReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "replication",
			Namespace: namespace,
		},
		Spec: couchbasev2.CouchbaseReplicationSpec{
			Bucket:       bucketName,
			RemoteBucket: bucketName,
		},
	}

	couchbaseBucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucketName,
			Namespace: namespace,
		},
	}

	memcachedBucket := &couchbasev2.CouchbaseMemcachedBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucketName,
			Namespace: namespace,
		},
	}

	testcases := []struct {
		name            string
		bucket          runtime.Object
		expectedErr     string
		expectedWarning string
	}{
		{
			name:            "missing bucket resource is admitted with a warning",
			bucket:          nil,
			expectedWarning: "bucket source referenced by spec.bucket does not exist",
		},
		{
			name:   "existing bucket resource is admitted without a warning",
			bucket: couchbaseBucket,
		},
		{
			name:        "memcached bucket remains a hard failure",
			bucket:      memcachedBucket,
			expectedErr: "memcached bucket source cannot be replicated",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			objects := []runtime.Object{cluster, replication}
			if testcase.bucket != nil {
				objects = append(objects, testcase.bucket)
			}

			v := types.New(k8sfake.NewSimpleClientset(), couchbasefake.NewSimpleClientset(objects...), nil)

			warnings, err := CheckConstraintsReplication(v, replication.DeepCopy())

			switch {
			case testcase.expectedErr == "" && err != nil:
				t.Errorf("expected no error but got: %s", err.Error())
			case testcase.expectedErr != "" && err == nil:
				t.Errorf("expected error containing %q but got none", testcase.expectedErr)
			case testcase.expectedErr != "" && !strings.Contains(err.Error(), testcase.expectedErr):
				t.Errorf("expected error containing %q but got %q", testcase.expectedErr, err.Error())
			}

			joinedWarnings := strings.Join(warnings, "; ")

			switch {
			case testcase.expectedWarning == "" && len(warnings) != 0:
				t.Errorf("expected no warnings but got: %s", joinedWarnings)
			case testcase.expectedWarning != "" && !strings.Contains(joinedWarnings, testcase.expectedWarning):
				t.Errorf("expected warning containing %q but got %q", testcase.expectedWarning, joinedWarnings)
			}
		})
	}
}
