/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/earutil"
)

// caSelection maps the two verify flags to the server's caSelection value.
func caSelection(t earutil.TLSClient) couchbaseutil.CASelection {
	switch {
	case t.VerifyWithSystemCA && t.VerifyWithCouchbaseCA:
		return couchbaseutil.CASelectionBoth
	case t.VerifyWithSystemCA:
		return couchbaseutil.CASelectionSystem
	case t.VerifyWithCouchbaseCA:
		return couchbaseutil.CASelectionCouchbase
	default:
		return couchbaseutil.CASelectionSkip
	}
}

// buildTLSClient fills the server's client TLS fields for a key type that reaches
// an external key server over TLS. The certificate and private key are mounted
// from the key shadow Secret; the passphrase is read and sent inline. specPrefix
// is the path of the key type's settings, for example "spec.kmipKey".
func (c *Cluster) buildTLSClient(t earutil.TLSClient, specPrefix string) (couchbaseutil.TLSClientData, error) {
	out := couchbaseutil.TLSClientData{
		CASelection: caSelection(t),
	}

	creds := earutil.TLSClientCredentials(t, specPrefix)

	certFile := creds.FileFor(earutil.ServerFieldTLSClientCertPath)
	keyFile := creds.FileFor(earutil.ServerFieldTLSClientKeyPath)

	if certFile == nil || keyFile == nil || len(creds.Values) != 1 {
		c.log.Error(errors.ErrResourceRequired, "client secret not set", "field", specPrefix+".clientSecret")
		return out, errors.ErrResourceRequired
	}

	out.CertPath = certFile.MountPath()
	out.KeyPath = keyFile.MountPath()

	passphrase, err := c.readCredential(creds.Values[0].SecretName, creds.Values[0].DataKey)
	if err != nil {
		return out, err
	}

	out.KeyPassphrase = string(passphrase)

	return out, nil
}
