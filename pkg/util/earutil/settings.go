/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// Package earutil holds the per-type tables for encryption at rest that both the
// reconciler and the admission validator read: which kinds of system data can be
// encrypted, what each encryption key type reads from Secrets, and which Couchbase
// Server release each type needs. Adding a type means adding an entry here rather
// than another copy of the code that handles it.
package earutil

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
)

// SettingsType is one kind of system data that the server encrypts at rest
// under /settings/security/encryptionAtRest/<ServerName>.
type SettingsType struct {
	// Name is the spec field name, used in errors, logs and events.
	Name string

	// ServerName is the path segment under /settings/security/encryptionAtRest/.
	ServerName string

	// KeyUsage is the usage string a key needs on the server to encrypt this type.
	KeyUsage string

	// Spec returns the cluster's settings for this type, or nil if unset.
	Spec func(*couchbasev2.EncryptionAtRestSpec) *couchbasev2.EncryptionAtRestUsageConfiguration

	// KeyAllows reports whether a key's spec.usage permits encrypting this type.
	KeyAllows func(couchbasev2.CouchbaseEncryptionKeyUsage) bool
}

// SettingsTypes lists every settings type, in the order they are reconciled.
var SettingsTypes = []SettingsType{
	{
		Name:       constants.EncryptionAtRestUsageConfiguration,
		ServerName: "config",
		KeyUsage:   couchbaseutil.EncryptionKeyUsageConfigEncryption,
		Spec: func(s *couchbasev2.EncryptionAtRestSpec) *couchbasev2.EncryptionAtRestUsageConfiguration {
			return s.Configuration
		},
		KeyAllows: func(u couchbasev2.CouchbaseEncryptionKeyUsage) bool { return u.Configuration },
	},
	{
		Name:       constants.EncryptionAtRestUsageAudit,
		ServerName: "audit",
		KeyUsage:   couchbaseutil.EncryptionKeyUsageAuditEncryption,
		Spec: func(s *couchbasev2.EncryptionAtRestSpec) *couchbasev2.EncryptionAtRestUsageConfiguration {
			return s.Audit
		},
		KeyAllows: func(u couchbasev2.CouchbaseEncryptionKeyUsage) bool { return u.Audit },
	},
	{
		Name:       constants.EncryptionAtRestUsageLog,
		ServerName: "log",
		KeyUsage:   couchbaseutil.EncryptionKeyUsageLogsEncryption,
		Spec: func(s *couchbasev2.EncryptionAtRestSpec) *couchbasev2.EncryptionAtRestUsageConfiguration {
			return s.Log
		},
		KeyAllows: func(u couchbasev2.CouchbaseEncryptionKeyUsage) bool { return u.Log },
	},
}

// SettingsFor returns the cluster's settings for t, or nil if encryption at rest
// or this type is unset.
func (t SettingsType) SettingsFor(spec *couchbasev2.EncryptionAtRestSpec) *couchbasev2.EncryptionAtRestUsageConfiguration {
	if spec == nil {
		return nil
	}

	return t.Spec(spec)
}
