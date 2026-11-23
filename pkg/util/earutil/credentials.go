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
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

// Server request fields that receive the path of a mounted credential file.
const (
	ServerFieldAWSCredentialsFile = "credentialsFile"
	ServerFieldTLSClientCertPath  = "certPath"
	ServerFieldTLSClientKeyPath   = "keyPath"
)

// CredentialFile is a Secret value that the operator copies into the key shadow
// Secret, which is mounted into every Couchbase Server pod. The server is then
// given the path of the mounted file.
type CredentialFile struct {
	// SecretName is the user's Secret holding the value.
	SecretName string

	// DataKey is the key inside that Secret, for example "tls.crt".
	DataKey string

	// SpecField names the field that references the Secret, for error messages.
	SpecField string

	// ServerField is the request field that receives MountPath().
	ServerField string
}

// ShadowName is the key of this file in the key shadow Secret, and so its file
// name in the pod. It must stay "<secret>-<dataKey>": changing it would change
// existing shadow Secrets and the paths already stored on the server.
func (f CredentialFile) ShadowName() string {
	return f.SecretName + "-" + f.DataKey
}

// MountPath is where the file appears inside a Couchbase Server pod.
func (f CredentialFile) MountPath() string {
	return k8sutil.CouchbaseKeyShadowVolumeMountDir + "/" + f.ShadowName()
}

// CredentialValue is a Secret value that the operator reads and sends inline in
// the request. It is never mounted.
type CredentialValue struct {
	SecretName string
	DataKey    string
	SpecField  string
}

// KeyCredentials is everything an encryption key reads from Secrets.
type KeyCredentials struct {
	Files  []CredentialFile
	Values []CredentialValue
}

// ServerFields lists the request fields that carry mounted file paths.
func (k KeyCredentials) ServerFields() []string {
	fields := make([]string, 0, len(k.Files))

	for _, f := range k.Files {
		fields = append(fields, f.ServerField)
	}

	return fields
}

// FileFor returns the file that fills the given server field, or nil.
func (k KeyCredentials) FileFor(serverField string) *CredentialFile {
	for i := range k.Files {
		if k.Files[i].ServerField == serverField {
			return &k.Files[i]
		}
	}

	return nil
}

// CredentialsFor returns the Secret values the key needs. A key type that reads
// nothing from Secrets returns an empty result. New key types add a case here.
func CredentialsFor(key *couchbasev2.CouchbaseEncryptionKey) KeyCredentials {
	switch key.Spec.KeyType {
	case couchbasev2.CouchbaseEncryptionKeyTypeAWS:
		if f := AWSCredentialsFile(key.Spec.AWSKey); f != nil {
			return KeyCredentials{Files: []CredentialFile{*f}}
		}
	case couchbasev2.CouchbaseEncryptionKeyTypeKMIP:
		if key.Spec.KMIPKey != nil {
			return TLSClientCredentials(KMIPTLSClient(key.Spec.KMIPKey), "spec.kmipKey")
		}
	}

	return KeyCredentials{}
}

// AWSCredentialsFile returns the AWS credentials file, or nil when the key uses
// IMDS or the server's default credentials.
func AWSCredentialsFile(aws *couchbasev2.CouchbaseEncryptionKeyAWS) *CredentialFile {
	if aws == nil || aws.CredentialsSecret == "" {
		return nil
	}

	return &CredentialFile{
		SecretName:  aws.CredentialsSecret,
		DataKey:     constants.AWSCredentialsSecretKey,
		SpecField:   "spec.awsKey.credentialsSecret",
		ServerField: ServerFieldAWSCredentialsFile,
	}
}

// TLSClientCredentials returns the client certificate and private key, which are
// mounted, and the private key passphrase, which is sent inline. specPrefix is the
// path of the key type's settings, for example "spec.kmipKey". An unset client
// Secret returns nothing; admission requires it for the types that use it.
func TLSClientCredentials(t TLSClient, specPrefix string) KeyCredentials {
	if t.ClientSecret == "" {
		return KeyCredentials{}
	}

	field := specPrefix + ".clientSecret"

	return KeyCredentials{
		Files: []CredentialFile{
			{SecretName: t.ClientSecret, DataKey: constants.KMIPClientSecretCertKey, SpecField: field, ServerField: ServerFieldTLSClientCertPath},
			{SecretName: t.ClientSecret, DataKey: constants.KMIPClientSecretKeyKey, SpecField: field, ServerField: ServerFieldTLSClientKeyPath},
		},
		Values: []CredentialValue{
			{SecretName: t.ClientSecret, DataKey: constants.KMIPClientSecretPassphraseKey, SpecField: field},
		},
	}
}
