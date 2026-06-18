/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"reflect"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
)

func TestExpandDisabledUsers(t *testing.T) {
	// A fake cluster user list. In production reconcile also injects
	// the internal users into this list, we include a couple
	// here directly so we can test glob matching against them.
	users := couchbaseutil.UserList{
		{ID: "fwws-app1", Domain: "local"},
		{ID: "fwws-app2", Domain: "local"},
		{ID: "alice", Domain: "local"},
		{ID: "fwws-svc", Domain: "external"},
		{ID: "@eventing", Domain: "local"},
		{ID: "@cbq-engine", Domain: "local"},
	}

	cases := []struct {
		name    string
		entries []couchbasev2.AuditDisabledUser
		want    []couchbaseutil.AuditUser
		wantErr bool
	}{
		{
			name:    "no entries returns empty (not nil)",
			entries: nil,
			want:    []couchbaseutil.AuditUser{},
		},
		{
			name:    "literal local user is kept as it is",
			entries: []couchbasev2.AuditDisabledUser{"alice/local"},
			want:    []couchbaseutil.AuditUser{{Name: "alice", Domain: "local"}},
		},
		{
			name:    "literal internal user passes through without a user list",
			entries: []couchbasev2.AuditDisabledUser{"@eventing/local"},
			want:    []couchbaseutil.AuditUser{{Name: "@eventing", Domain: "local"}},
		},
		{
			name:    "prefix glob matches only same domain users",
			entries: []couchbasev2.AuditDisabledUser{"fwws-*/local"},
			want: []couchbaseutil.AuditUser{
				{Name: "fwws-app1", Domain: "local"},
				{Name: "fwws-app2", Domain: "local"},
			},
		},
		{
			name:    "glob on external domain matches external user",
			entries: []couchbasev2.AuditDisabledUser{"fwws-*/external"},
			want:    []couchbaseutil.AuditUser{{Name: "fwws-svc", Domain: "external"}},
		},
		{
			name:    "glob matches internal users",
			entries: []couchbasev2.AuditDisabledUser{"@*/local"},
			want: []couchbaseutil.AuditUser{
				{Name: "@cbq-engine", Domain: "local"},
				{Name: "@eventing", Domain: "local"},
			},
		},
		{
			name:    "literal and glob hitting the same user are de-duplicated",
			entries: []couchbasev2.AuditDisabledUser{"fwws-app1/local", "fwws-*/local"},
			want: []couchbaseutil.AuditUser{
				{Name: "fwws-app1", Domain: "local"},
				{Name: "fwws-app2", Domain: "local"},
			},
		},
		{
			name:    "glob matching nothing expands to empty",
			entries: []couchbasev2.AuditDisabledUser{"zzz-*/local"},
			want:    []couchbaseutil.AuditUser{},
		},
		{
			name:    "entry without a domain is rejected",
			entries: []couchbasev2.AuditDisabledUser{"fwws-app1"},
			wantErr: true,
		},
		{
			name:    "malformed glob pattern is rejected",
			entries: []couchbasev2.AuditDisabledUser{"[/local"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandDisabledUsers(tc.entries, users)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error but got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSplitAuditUser(t *testing.T) {
	cases := []struct {
		entry      string
		wantName   string
		wantDomain string
		wantOK     bool
	}{
		{"alice/local", "alice", "local", true},
		{"fwws-*/external", "fwws-*", "external", true},
		{"@eventing/local", "@eventing", "local", true},
		{"nodomain", "", "", false},
		{"a/b/c", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			name, domain, ok := splitAuditUser(tc.entry)
			if ok != tc.wantOK || name != tc.wantName || domain != tc.wantDomain {
				t.Errorf("splitAuditUser(%q) = (%q, %q, %t), want (%q, %q, %t)",
					tc.entry, name, domain, ok, tc.wantName, tc.wantDomain, tc.wantOK)
			}
		})
	}
}

func TestIsGlobPattern(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"fwws-*", true},
		{"app-?", true},
		{"app[12]", true},
		{"alice", false},
		{"@eventing", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGlobPattern(tc.name); got != tc.want {
				t.Errorf("isGlobPattern(%q) = %t, want %t", tc.name, got, tc.want)
			}
		})
	}
}

func TestDisabledUsersNeedExpansion(t *testing.T) {
	cases := []struct {
		name    string
		entries []couchbasev2.AuditDisabledUser
		want    bool
	}{
		{"nil", nil, false},
		{"all literal", []couchbasev2.AuditDisabledUser{"alice/local", "@eventing/local"}, false},
		{"contains glob", []couchbasev2.AuditDisabledUser{"alice/local", "fwws-*/local"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := disabledUsersNeedExpansion(tc.entries); got != tc.want {
				t.Errorf("disabledUsersNeedExpansion(%v) = %t, want %t", tc.entries, got, tc.want)
			}
		})
	}
}
