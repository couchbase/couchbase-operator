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
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
)

// splitAuditUser splits a "name/domain" audit entry into its two parts.
// ok is false if the entry is not exactly one name and one domain.
func splitAuditUser(entry string) (name, domain string, ok bool) {
	parts := strings.Split(entry, "/")
	if len(parts) != 2 {
		return "", "", false
	}

	return parts[0], parts[1], true
}

// isGlobPattern reports whether the username portion of an audit entry contains
// glob wildcards understood by path.Match ('*', '?', '['), meaning these
// characters must be expanded against the cluster's user list rather
// than matched literally.
func isGlobPattern(name string) bool {
	return strings.ContainsAny(name, "*?[")
}

// disabledUsersNeedExpansion reports whether any entry uses a glob pattern, in
// which case the cluster's user list must be fetched to expand it.
func disabledUsersNeedExpansion(entries []couchbasev2.AuditDisabledUser) bool {
	for _, entry := range entries {
		name, _, ok := splitAuditUser(string(entry))
		if ok && isGlobPattern(name) {
			return true
		}
	}

	return false
}

// auditUserValidator checks an entry has the "name/domain" form. The name may
// contain glob wildcards. It does not check the user exists, the REST API may
// still reject it.
var auditUserValidator = regexp.MustCompile("^.+/(local|external)$")

// expandDisabledUsers turns the CRD disabledUsers entries into the user list to
// send to the audit REST API. Glob entries are expanded against the given users
// (matched by domain), plain entries are kept as it is. The result is de-duplicated
// and sorted so it can be compared with the current server state.
func expandDisabledUsers(entries []couchbasev2.AuditDisabledUser, users couchbaseutil.UserList) ([]couchbaseutil.AuditUser, error) {
	// At least one result per literal entry, glob entries may add more.
	result := make([]couchbaseutil.AuditUser, 0, len(entries))
	seen := map[string]struct{}{}

	// add appends a user to the result, skipping any it has already added.
	add := func(name, domain string) {
		key := fmt.Sprintf("%s/%s", name, domain)
		if _, ok := seen[key]; ok {
			return
		}

		seen[key] = struct{}{}
		result = append(result, couchbaseutil.AuditUser{Name: name, Domain: domain})
	}

	for _, entry := range entries {
		value := string(entry)

		if !auditUserValidator.MatchString(value) {
			return nil, fmt.Errorf("%w: audit disabled user is invalid: %s",
				errors.NewStackTracedError(errors.ErrConfigurationInvalid), value)
		}

		name, domain, ok := splitAuditUser(value)
		if !ok {
			return nil, fmt.Errorf("%w: audit disabled user has no domain: %s",
				errors.NewStackTracedError(errors.ErrConfigurationInvalid), value)
		}

		// A literal user (no wildcards) is sent as it is. This includes internal
		// users such as @eventing/local that are not in the RBAC user list.
		if !isGlobPattern(name) {
			add(name, domain)
			continue
		}

		// A glob is expanded against every user sharing its domain. Usernames may
		// be stored in a different Unicode normalization form than the pattern
		// (e.g. NFC vs NFD), so normalize both to NFC before matching, otherwise
		// visually identical names would fail to match.
		normalizedName := norm.NFC.String(name)

		for _, user := range users {
			if string(user.Domain) != domain {
				continue
			}

			matched, err := path.Match(normalizedName, norm.NFC.String(user.ID))
			if err != nil {
				return nil, fmt.Errorf("%w: audit disabled user has an invalid glob pattern %q: %s",
					errors.NewStackTracedError(errors.ErrConfigurationInvalid), name, err.Error())
			}

			// Emit the server's original ID, not the normalized form, so what we
			// send back matches what the server stores.
			if matched {
				add(user.ID, domain)
			}
		}
	}

	sortAuditUsers(result)

	return result, nil
}

// sortAuditUsers orders the list deterministically (by domain, then name) so two
// lists with the same membership compare equal under reflect.DeepEqual regardless
// of the order the REST API returned the underlying users in.
func sortAuditUsers(users []couchbaseutil.AuditUser) {
	sort.Slice(users, func(i, j int) bool {
		if users[i].Domain != users[j].Domain {
			return users[i].Domain < users[j].Domain
		}

		return users[i].Name < users[j].Name
	})
}
