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

// TestXDCRRemoteClusterUpdatesStaging checks which side of the update/staging split a given
// desired-vs-actual pair lands on.  A credential-only change is staged; anything else takes the
// existing update path.
func TestXDCRRemoteClusterUpdatesStaging(t *testing.T) {
	t.Parallel()

	current := couchbaseutil.RemoteCluster{
		Name:       "west-operator-managed",
		Hostname:   "couchbases://cb-west.example.com",
		Username:   "xdcr_user",
		Password:   "pass1",
		UUID:       "uuid",
		SecureType: couchbaseutil.RemoteClusterSecurityTLS,
		CA:         "ca-cert",
	}

	// The same reference on client certificate auth - no credentials to rotate.
	mutualTLS := couchbaseutil.RemoteCluster{
		Name:        "west-operator-managed",
		Hostname:    "couchbases://cb-west.example.com",
		UUID:        "uuid",
		SecureType:  couchbaseutil.RemoteClusterSecurityTLS,
		CA:          "ca-cert",
		Certificate: "client-cert",
		Key:         "client-key",
	}

	plaintext := couchbaseutil.RemoteCluster{
		Name:     "west-operator-managed",
		Hostname: "couchbase://cb-west.example.com",
		Username: "xdcr_user",
		Password: "pass1",
		UUID:     "uuid",
	}

	// No password recorded in persistence, so there is nothing to update on behalf of.
	noPassword := couchbaseutil.RemoteCluster{
		Name:       "west-operator-managed",
		Hostname:   "couchbases://cb-west.example.com",
		Username:   "xdcr_user",
		UUID:       "uuid",
		SecureType: couchbaseutil.RemoteClusterSecurityTLS,
		CA:         "ca-cert",
	}

	with := func(base couchbaseutil.RemoteCluster, mutate func(*couchbaseutil.RemoteCluster)) couchbaseutil.RemoteCluster {
		mutate(&base)

		return base
	}

	// The same reference with a rotation staged on the server, which a HTTP GET reports and the
	// spec never does.
	stagePending := with(current, func(r *couchbaseutil.RemoteCluster) {
		r.Stage = &couchbaseutil.RemoteClusterStagedCredentials{Username: "xdcr_user2"}
	})

	tests := []struct {
		name            string
		current         *couchbaseutil.RemoteCluster // defaults to the TLS reference above
		requested       couchbaseutil.RemoteCluster
		supportsStaging bool
		wantUpdate      bool
		wantStage       bool
	}{
		{
			name:      "no change",
			requested: current,
		},
		{
			// The server reports a stage, the spec never does.  Without normalising it, a
			// pending rotation would look like a difference and update every reconcile.
			name:            "no change, but a stage is pending",
			current:         &stagePending,
			requested:       current,
			supportsStaging: true,
		},
		{
			name:            "credentials changed while a stage is pending",
			current:         &stagePending,
			requested:       with(current, func(r *couchbaseutil.RemoteCluster) { r.Username, r.Password = "xdcr_user2", "pass2" }),
			supportsStaging: true,
			wantStage:       true,
		},
		{
			name:            "username and password changed",
			requested:       with(current, func(r *couchbaseutil.RemoteCluster) { r.Username, r.Password = "xdcr_user2", "pass2" }),
			supportsStaging: true,
			wantStage:       true,
		},
		{
			name:            "password only changed",
			requested:       with(current, func(r *couchbaseutil.RemoteCluster) { r.Password = "pass2" }),
			supportsStaging: true,
			wantStage:       true,
		},
		{
			name:            "username only changed",
			requested:       with(current, func(r *couchbaseutil.RemoteCluster) { r.Username = "xdcr_user2" }),
			supportsStaging: true,
			wantStage:       true,
		},
		{
			// Split: the hostname is applied on the old credentials, the new ones are staged.
			name: "hostname and credentials changed together",
			requested: with(current, func(r *couchbaseutil.RemoteCluster) {
				r.Hostname, r.Username = "couchbases://cb-west-2.example.com", "xdcr_user2"
			}),
			supportsStaging: true,
			wantUpdate:      true,
			wantStage:       true,
		},
		{
			// Nothing to update on behalf of, so apply the new credentials as before.
			name:    "hostname and credentials changed, no recorded password",
			current: &noPassword,
			requested: with(noPassword, func(r *couchbaseutil.RemoteCluster) {
				r.Hostname, r.Password = "couchbases://cb-west-2.example.com", "pass2"
			}),
			supportsStaging: true,
			wantUpdate:      true,
		},
		{
			// A client certificate rotation must not be mistaken for a credential-only change.
			name:            "client certificate only changed",
			current:         &mutualTLS,
			requested:       with(mutualTLS, func(r *couchbaseutil.RemoteCluster) { r.Certificate = "new-client-cert" }),
			supportsStaging: true,
			wantUpdate:      true,
		},
		{
			name:            "client key only changed",
			current:         &mutualTLS,
			requested:       with(mutualTLS, func(r *couchbaseutil.RemoteCluster) { r.Key = "new-client-key" }),
			supportsStaging: true,
			wantUpdate:      true,
		},
		{
			// Staging this would leave a username and a client certificate on one reference.
			name:            "credentials added to a client certificate reference",
			current:         &mutualTLS,
			requested:       with(mutualTLS, func(r *couchbaseutil.RemoteCluster) { r.Username, r.Password = "xdcr_user", "pass1" }),
			supportsStaging: true,
			wantUpdate:      true,
		},
		{
			name:            "CA only changed",
			requested:       with(current, func(r *couchbaseutil.RemoteCluster) { r.CA = "new-ca" }),
			supportsStaging: true,
			wantUpdate:      true,
		},
		{
			// Same rotation on a plaintext reference, to prove TLS material isn't what makes it work.
			name:            "password changed on a plaintext reference",
			current:         &plaintext,
			requested:       with(plaintext, func(r *couchbaseutil.RemoteCluster) { r.Password = "pass2" }),
			supportsStaging: true,
			wantStage:       true,
		},
		{
			name:            "hostname only changed",
			requested:       with(current, func(r *couchbaseutil.RemoteCluster) { r.Hostname = "couchbases://cb-west-2.example.com" }),
			supportsStaging: true,
			wantUpdate:      true,
		},
		{
			// Below 8.5 there is nothing to stage onto, so credentials are applied as before.
			name:       "credentials changed, server below 8.5",
			requested:  with(current, func(r *couchbaseutil.RemoteCluster) { r.Username, r.Password = "xdcr_user2", "pass2" }),
			wantUpdate: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := Cluster{
				cluster: &couchbasev2.CouchbaseCluster{},
			}

			actual := current
			if test.current != nil {
				actual = *test.current
			}

			updates, staging := c.remoteClusterUpdates(
				couchbaseutil.RemoteClusters{actual},
				couchbaseutil.RemoteClusters{test.requested},
				test.supportsStaging,
			)

			if got := len(updates) == 1; got != test.wantUpdate {
				t.Errorf("update: got %v, want %v", got, test.wantUpdate)
			}

			if got := len(staging) == 1; got != test.wantStage {
				t.Errorf("staging: got %v, want %v", got, test.wantStage)
			}

			// A split change must update on the current credentials, or it restarts the pipelines
			// it was meant to spare, and stage the requested ones.
			if len(updates) == 1 && len(staging) == 1 {
				if updates[0].Username != actual.Username || updates[0].Password != actual.Password {
					t.Errorf("split update credentials: got %v/%v, want %v/%v",
						updates[0].Username, updates[0].Password, actual.Username, actual.Password)
				}

				if staging[0].Username != test.requested.Username || staging[0].Password != test.requested.Password {
					t.Errorf("split staged credentials: got %v/%v, want %v/%v",
						staging[0].Username, staging[0].Password, test.requested.Username, test.requested.Password)
				}
			}
		})
	}
}
