/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"encoding/json"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// ServerCredentialTypeAWS is the credential type Couchbase Server uses for S3 and anything else
// reached with AWS credentials.
const ServerCredentialTypeAWS = "aws"

// ServerCredential is an entry in Couchbase Server's credential store, held under
// /settings/credentials/<id>.
//
// The store exists so that features which reach outside the cluster, continuous backup to object
// storage among them, can refer to a credential by name rather than carrying the secret itself.
// The operator never sees the secret: a bucket names the credential ID and the server resolves it.
type ServerCredential struct {
	Type       string                     `json:"type"`
	Fields     ServerCredentialFields     `json:"fields"`
	Guardrails ServerCredentialGuardrails `json:"guardrails"`
}

// ServerCredentialFields carries the credential itself.  The field names are the server's, not the
// operator's, and differ from the ones used in CouchbaseCluster secrets.
type ServerCredentialFields struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	Region          string `json:"region"`

	// SessionToken is only present for temporary credentials.  Sending it empty is not the same
	// as leaving it out, so it is omitted when unset.
	SessionToken string `json:"sessionToken,omitempty"`
}

// ServerCredentialGuardrails restricts what a credential may be used to reach.
type ServerCredentialGuardrails struct {
	URLWhitelist ServerCredentialURLWhitelist `json:"urlWhitelist"`
}

// ServerCredentialURLWhitelist is the set of URLs a credential may be used against.
type ServerCredentialURLWhitelist struct {
	// AllAccess lifts the restriction entirely.  Tests set this because the bucket they write to
	// is supplied from outside and its URL is not known when the credential is created.
	AllAccess bool `json:"allAccess"`
}

// NewAWSServerCredential builds an AWS credential with no URL restriction.  Pass an empty session
// token for long lived access keys.
func NewAWSServerCredential(accessKeyID, secretAccessKey, region, sessionToken string) ServerCredential {
	return ServerCredential{
		Type: ServerCredentialTypeAWS,
		Fields: ServerCredentialFields{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
			Region:          region,
			SessionToken:    sessionToken,
		},
		Guardrails: ServerCredentialGuardrails{
			URLWhitelist: ServerCredentialURLWhitelist{AllAccess: true},
		},
	}
}

// CreateServerCredential stores a credential in the cluster under the given ID, replacing any
// credential already held under it.
//
// Storing the credential is all this does.  Granting the backup service credential_consumer for it
// is the operator's job, which it does while reconciling any bucket that names the credential, so
// doing it here as well would only mask a regression in that.
func CreateServerCredential(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, id string, credential ServerCredential) error {
	client := MustCreateAdminConsoleClient(t, k8s, cluster)

	body, err := json.Marshal(credential)
	if err != nil {
		return err
	}

	request := newRequest("/settings/credentials/"+id, body, nil)

	return client.client.PostJSON(request, client.host)
}

// DeleteServerCredential removes a credential from the cluster.
func DeleteServerCredential(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, id string) error {
	client := MustCreateAdminConsoleClient(t, k8s, cluster)

	request := newRequest("/settings/credentials/"+id, nil, nil)

	return client.client.Delete(request, client.host)
}

// MustCreateServerCredential stores a credential and returns a function that removes it again.
//
// The credential outlives the resources that use it, so removing it is the caller's job.  Deleting
// it while a bucket still names it would leave that bucket unable to reach its storage, so call
// the returned function only once those are gone, or at the end of the test.
func MustCreateServerCredential(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, id string, credential ServerCredential) func() {
	if err := CreateServerCredential(t, k8s, cluster, id, credential); err != nil {
		Die(t, err)
	}

	return func() {
		// Deliberately not fatal.  This runs during teardown, where the cluster may already be on
		// its way out, and failing here would mask whatever the test was actually reporting.
		if err := DeleteServerCredential(t, k8s, cluster, id); err != nil {
			t.Logf("unable to delete server credential %q: %v", id, err)
		}
	}
}
