/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"reflect"
	"strings"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
)

var replicationSpec = couchbasev2.CouchbaseReplicationSpec{
	Bucket:       "bucket",
	RemoteBucket: "remoteBucket",
	CompressionType: func() *string {
		s := "Auto"
		return &s
	}(),
	FilterExpression: func() *string {
		s := ""
		return &s
	}(),
	Paused: func() *bool {
		s := false
		return &s
	}(),
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

// TestXDCRComputeSettingsPatchExplicitMappingRemoval checks that removing
// the mapping from the CR actually clears it on the server. When the CR has no
// mapping the desired state is"off" (false + empty rules). The patch should differ
// from a server that still has old rules (so we send an update), but match a server
// that already has no mapping (so we don't keep sending the same update every loop).
func TestXDCRComputeSettingsPatchExplicitMappingRemoval(t *testing.T) {
	t.Parallel()

	boolPtr := func(b bool) *bool { return &b }
	rules := func(m couchbaseutil.ColMappingRules) *couchbaseutil.ColMappingRules { return &m }
	strPtr := func(s string) *string { return &s }

	// Server >= 7.0.0 so scopes/collections (and therefore mapping) are supported.
	c := Cluster{
		cluster: &couchbasev2.CouchbaseCluster{
			Spec: couchbasev2.ClusterSpec{
				Image: "couchbase:7.6.0",
			},
		},
	}

	// Desired state for a replication whose CR has no explicit mapping, asserts "off".
	desiredOff := DesiredReplicationState{
		Spec:                       &couchbasev2.CouchbaseReplicationSpec{},
		CollectionsExplicitMapping: boolPtr(false),
		ColMappingRules:            rules(couchbaseutil.ColMappingRules{}),
	}

	tests := []struct {
		name         string
		current      couchbaseutil.ReplicationSettings
		desired      DesiredReplicationState
		expectUpdate bool
	}{
		{
			// mapping was configured, then removed from the CR.
			name: "mapping removed, server still has stale rules",
			current: couchbaseutil.ReplicationSettings{
				CollectionsExplicitMapping: boolPtr(true),
				ColMappingRules:            rules(couchbaseutil.ColMappingRules{"source-scope-1": strPtr("source-scope-2")}),
			},
			desired:      desiredOff,
			expectUpdate: true,
		},
		{
			// Replication that never had a mapping, server omits the fields.
			// Must be a no-op.
			name:         "never set, server omits mapping fields",
			current:      couchbaseutil.ReplicationSettings{},
			desired:      desiredOff,
			expectUpdate: false,
		},
		{
			// Steady state after a previous clear, server reports off + empty.
			// Must be a no-op.
			name: "steady off, server reports false and empty rules",
			current: couchbaseutil.ReplicationSettings{
				CollectionsExplicitMapping: boolPtr(false),
				ColMappingRules:            rules(couchbaseutil.ColMappingRules{}),
			},
			desired:      desiredOff,
			expectUpdate: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			current := test.current
			patch := c.computeSettingsPatch(&test.desired, &current)

			// This mirrors the decision made in diffReplicationStates, an update
			// is issued only when the patch differs from the (normalized) current
			// server state.
			gotUpdate := !reflect.DeepEqual(patch, &current)
			if gotUpdate != test.expectUpdate {
				t.Fatalf("expectUpdate=%v, got=%v\npatch.CollectionsExplicitMapping=%v patch.ColMappingRules=%v",
					test.expectUpdate, gotUpdate, patch.CollectionsExplicitMapping, patch.ColMappingRules)
			}

			// When an update is issued to clear mapping, the payload must actually
			// carry the "off" assertion (non-nil) or omitempty would drop it and the
			// server would never clear.
			if test.expectUpdate {
				if patch.CollectionsExplicitMapping == nil || *patch.CollectionsExplicitMapping {
					t.Errorf("expected patch to assert collectionsExplicitMapping=false, got %v", patch.CollectionsExplicitMapping)
				}
				if patch.ColMappingRules == nil || len(*patch.ColMappingRules) != 0 {
					t.Errorf("expected patch to assert an empty colMappingRules, got %v", patch.ColMappingRules)
				}
			}
		})
	}
}
