/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package x509

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
	"time"
)

const (
	// zone is a valid zone for the cert.
	validZone = "*.cluster.namespace.svc"
	// invalidZone is an invalid zone for the cert.
	invalidZone = "*.invalid.namespace.svc"
	// caCN is the default root CA's common name.
	caCN = "Root CA"
	// intermediateCN is the default intermediate CA's common name.
	intermediateCN = "Intermediate CA"
	// serverCN is the default server certificate common name.
	serverCN = "Server"
)

var (
	// reqTemplate is a valid request template.
	reqTemplate = KeyPairRequest{
		KeyType:   KeyTypeRSA,
		CertType:  CertTypeServer,
		ValidFrom: time.Now(),
		ValidTo:   time.Now().Add(time.Hour),
		Req: &x509.CertificateRequest{
			Subject: pkix.Name{
				CommonName: serverCN,
			},
			DNSNames: []string{
				validZone,
			},
		},
	}
)

// mustVerify checks that the PKI is valid for the given cluster.
func mustVerify(t *testing.T, ca, chain, key []byte, extKeyUsage x509.ExtKeyUsage, zone string) {
	if errors := Verify(ca, chain, key, extKeyUsage, []string{zone}); len(errors) > 0 {
		for _, err := range errors {
			t.Log(err)
		}
		t.FailNow()
	}
}

// mustNotVerify checks that the PKI is invalid for the given cluster.
func mustNotVerify(t *testing.T, ca, chain, key []byte, extKeyUsage x509.ExtKeyUsage, zone string) {
	if errors := Verify(ca, chain, key, extKeyUsage, []string{zone}); len(errors) == 0 {
		t.Fatal("verification succeeded unexpectedly")
	}
}

// TestVerify checks that a valid PKI succeeds.
func TestVerify(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	mustVerify(t, ca.Certificate, cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyChain checks that a valid PKI chain succeeds.
func TestVerifyChain(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	intermediate, _ := ca.NewIntermediateCertificateAuthority(KeyTypeRSA, intermediateCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(intermediate)
	mustVerify(t, ca.Certificate, append(cert, intermediate.Certificate...), key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidPrivateKeyFormat checks that an invalid private key type is rejected.
func TestVerifyInvalidPrivateKeyFormat(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	req := reqTemplate
	req.KeyEncodingPKCS8 = true
	key, cert, _ := req.Generate(ca)
	mustNotVerify(t, ca.Certificate, cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidPrivateKeyMultiple checks that multiple private keys are rejected.
func TestVerifyInvalidPrivateKeyMultiple(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	mustNotVerify(t, ca.Certificate, cert, append(key, key...), x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCertificateNotYetValid checks that a certificate that is not yet valid is rejected.
func TestVerifyInvalidCertificateNotYetValid(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	req := reqTemplate
	req.ValidFrom = time.Now().Add(1 * time.Hour)
	req.ValidTo = time.Now().Add(2 * time.Hour)
	key, cert, _ := req.Generate(ca)
	mustNotVerify(t, ca.Certificate, cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCertificateExpired checks that a certificate that has expired is rejected.
func TestVerifyInvalidCertificateExpired(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	req := reqTemplate
	req.ValidFrom = time.Now().Add(-2 * time.Hour)
	req.ValidTo = time.Now().Add(-1 * time.Hour)
	key, cert, _ := req.Generate(ca)
	mustNotVerify(t, ca.Certificate, cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCertificateType checks that a client certificate is rejected.
func TestVerifyInvalidCertificateType(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	req := reqTemplate
	req.CertType = CertTypeClient
	key, cert, _ := req.Generate(ca)
	mustNotVerify(t, ca.Certificate, cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCertificateSubjectAddressName checks that invalid subject alternate names are rejected.
func TestVerifyInvalidCertificateSubjectAddressName(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	mustNotVerify(t, ca.Certificate, cert, key, x509.ExtKeyUsageServerAuth, invalidZone)
}

// TestVerifyInvalidCACertificate checks that a certificate that is not signed by a CA is rejected.
func TestVerifyInvalidCACertificate(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	ca2, _ := NewCertificateAuthority(KeyTypeRSA, "invalid", time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	mustNotVerify(t, ca2.Certificate, cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCACertificateMultiple checks that multiple CA certificates are rejected.
func TestVerifyInvalidCACertificateMultiple(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	mustNotVerify(t, append(ca.Certificate, ca.Certificate...), cert, key, x509.ExtKeyUsageServerAuth, validZone)
}
