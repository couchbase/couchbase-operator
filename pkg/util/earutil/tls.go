/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package earutil

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
)

// TLSClient is the client TLS configuration of a key type that reaches an
// external key server over TLS. It is internal: each key type keeps its own
// fields in the CRD, so the public Go types stay flat, and converts them with
// a function like KMIPTLSClient for the shared code to use.
type TLSClient struct {
	VerifyWithSystemCA    bool
	VerifyWithCouchbaseCA bool
	ClientSecret          string
}

// KMIPTLSClient returns the client TLS configuration of a KMIP key.
func KMIPTLSClient(kmip *couchbasev2.CouchbaseEncryptionKeyKMIP) TLSClient {
	if kmip == nil {
		return TLSClient{}
	}

	return TLSClient{
		VerifyWithSystemCA:    kmip.VerifyWithSystemCA,
		VerifyWithCouchbaseCA: kmip.VerifyWithCouchbaseCA,
		ClientSecret:          kmip.ClientSecret,
	}
}
