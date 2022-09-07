package cluster

import (
	"strings"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
)

var replicationSpec = couchbasev2.CouchbaseReplicationSpec{
	Bucket:           "bucket",
	RemoteBucket:     "remoteBucket",
	CompressionType:  "Auto",
	FilterExpression: "",
	Paused:           false,
}

func TestXDCRGenerateMigrationMappings(t *testing.T) {
	t.Parallel()

	// We want to ensure we create the right JSON for the API to use
	migration := couchbasev2.CouchbaseMigrationReplication{
		Spec: replicationSpec,
	}

	tests := []struct {
		rules      []couchbasev2.CouchbaseMigrationMapping
		jsonOutput string
	}{
		{
			rules: []couchbasev2.CouchbaseMigrationMapping{
				{
					Filter: "_default._default",
					TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
						Scope:      "scope",
						Collection: "collection",
					},
				},
			},
			jsonOutput: "{\"_default._default\":\"scope.collection\"}",
		},
		{
			rules: []couchbasev2.CouchbaseMigrationMapping{
				{
					Filter: "city==San Francisco",
					TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
						Scope:      "California",
						Collection: "SanFrancisco",
					},
				},
			},
			jsonOutput: "{\"city==San Francisco\":\"California.SanFrancisco\"}",
		},
		{
			rules: []couchbasev2.CouchbaseMigrationMapping{
				{
					Filter: "city==San Francisco",
					TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
						Scope:      "California",
						Collection: "SanFrancisco",
					},
				},
				{
					Filter: "type == \"airline\" && country == \"United States\"",
					TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
						Scope:      "US-Scope",
						Collection: "AirlineCollection",
					},
				},
				{
					Filter: "type == \"airport\" && country == \"United Kingdom\"",
					TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
						Scope:      "UK-Scope",
						Collection: "AirportCollection",
					},
				},
			},
			// escape me baby - can't use single quotes as we do want escaped JSON quotes!
			jsonOutput: "{\"city==San Francisco\":\"California.SanFrancisco\",\"type == \\\"airline\\\" \\u0026\\u0026 country == \\\"United States\\\"\":\"US-Scope.AirlineCollection\",\"type == \\\"airport\\\" \\u0026\\u0026 country == \\\"United Kingdom\\\"\":\"UK-Scope.AirportCollection\"}",
		},
	}

	for index, test := range tests {
		migration.MigrationMapping.Mappings = test.rules

		actual, err := generateMigrationMappingRules(&migration)
		if err != nil {
			t.Errorf("failed test case %d with error: %s", index, err.Error())
		}

		if actual != test.jsonOutput {
			t.Errorf("failed test case %d: %q != %q", index, actual, test.jsonOutput)
		}
	}
}

func TestXDCRGenerateReplicationMappings(t *testing.T) {
	t.Parallel()

	replication := couchbasev2.CouchbaseReplication{
		Spec: replicationSpec,
	}

	tests := []struct {
		rules      couchbasev2.CouchbaseExplicitMappingSpec
		jsonOutput string
	}{
		// test empty rules
		{
			rules:      couchbasev2.CouchbaseExplicitMappingSpec{},
			jsonOutput: "{}",
		},
		{
			rules: couchbasev2.CouchbaseExplicitMappingSpec{
				AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{},
				DenyRules:  []couchbasev2.CouchbaseDenyReplicationMapping{},
			},
			jsonOutput: "{}",
		},
		// now let's get some bad boys going
		{
			rules: couchbasev2.CouchbaseExplicitMappingSpec{
				AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "source_scope",
							Collection: "",
						},
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "",
						},
					},
				},
				DenyRules: []couchbasev2.CouchbaseDenyReplicationMapping{
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "source_scope",
							Collection: "deny_collection",
						},
					},
				},
			},
			jsonOutput: "{\"source_scope\":\"target_scope\",\"source_scope.deny_collection\":null}",
		},
		{
			rules: couchbasev2.CouchbaseExplicitMappingSpec{
				AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "source_scope",
							Collection: "source_collection",
						},
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "target_collection",
						},
					},
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope: "allow_scope",
						},
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope: "allow_target_scope",
						},
					},
				},
				DenyRules: []couchbasev2.CouchbaseDenyReplicationMapping{
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "allow_scope",
							Collection: "deny_collection",
						},
					},
				},
			},
			// alphabetical ordering
			jsonOutput: "{\"allow_scope\":\"allow_target_scope\",\"allow_scope.deny_collection\":null,\"source_scope.source_collection\":\"target_scope.target_collection\"}",
		},
	}

	for index, test := range tests {
		replication.ExplicitMapping = test.rules

		actual, err := generateExplicitMappingRules(&replication)
		if err != nil {
			t.Errorf("failed test case %d with error: %s", index, err.Error())
		}

		if actual != test.jsonOutput {
			t.Errorf("failed test case %d: %q != %q", index, actual, test.jsonOutput)
		}
	}
}

func TestXDCRNegGenerateReplicationMappings(t *testing.T) {
	t.Parallel()

	replication := couchbasev2.CouchbaseReplication{
		Spec: replicationSpec,
	}

	tests := []struct {
		rules         couchbasev2.CouchbaseExplicitMappingSpec
		errorExpected error
	}{
		// test failing rules
		{
			rules: couchbasev2.CouchbaseExplicitMappingSpec{
				AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "source_scope",
							Collection: "source_collection",
						},
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "target_collection",
						},
					},
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "source_scope",
							Collection: "source_collection",
						},
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "target_collection",
						},
					},
				},
				DenyRules: []couchbasev2.CouchbaseDenyReplicationMapping{},
			},
			errorExpected: ErrXDCRReplicationInvalidMappingRule,
		},
		{
			rules: couchbasev2.CouchbaseExplicitMappingSpec{
				AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "source_scope",
							Collection: "source_collection",
						},
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "target_collection",
						},
					},
				},
				DenyRules: []couchbasev2.CouchbaseDenyReplicationMapping{
					{
						SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "source_scope",
							Collection: "source_collection",
						},
					},
				},
			},
			errorExpected: ErrXDCRReplicationInvalidMappingRule,
		},
	}

	for index, test := range tests {
		replication.ExplicitMapping = test.rules

		_, err := generateExplicitMappingRules(&replication)
		if err == nil {
			t.Errorf("unexpectedly passed test case %d with no errors", index)
		} else if !strings.Contains(err.Error(), test.errorExpected.Error()) {
			t.Errorf("failed test case %d with unexpected error (%q): %q", index, test.errorExpected, err.Error())
		}
	}
}

func TestXDCRNegGenerateMigrationMappings(t *testing.T) {
	t.Parallel()

	migration := couchbasev2.CouchbaseMigrationReplication{
		Spec: replicationSpec,
	}

	tests := []struct {
		rules         couchbasev2.CouchbaseMigrationMappingSpec
		errorExpected error
	}{
		// test failing rules
		{
			rules: couchbasev2.CouchbaseMigrationMappingSpec{
				Mappings: []couchbasev2.CouchbaseMigrationMapping{},
			},
			errorExpected: ErrXDCRMigrationNoRules,
		},
		{
			rules: couchbasev2.CouchbaseMigrationMappingSpec{
				Mappings: []couchbasev2.CouchbaseMigrationMapping{
					{
						Filter: "_default._default",
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "target_collection",
						},
					},
					{
						Filter: "doesnot=matterasonlyonethingcanmigratedefault",
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "target_collection",
						},
					},
				},
			},
			errorExpected: ErrXDCRMigrationDefaultFilterInUse,
		},
		{
			rules: couchbasev2.CouchbaseMigrationMappingSpec{
				Mappings: []couchbasev2.CouchbaseMigrationMapping{
					{
						Filter: "abc",
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "target_collection",
						},
					},
					{
						Filter: "def",
						TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
							Scope:      "target_scope",
							Collection: "",
						},
					},
				},
			},
			errorExpected: ErrXDCRMigrationNoTargetCollection,
		},
	}

	for index, test := range tests {
		migration.MigrationMapping = test.rules

		_, err := generateMigrationMappingRules(&migration)
		if err == nil {
			t.Errorf("unexpectedly passed test case %d with no errors", index)
		} else if !strings.Contains(err.Error(), test.errorExpected.Error()) {
			t.Errorf("failed test case %d with unexpected error (%q): %q", index, test.errorExpected, err.Error())
		}
	}
}
