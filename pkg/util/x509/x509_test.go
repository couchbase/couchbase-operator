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

// reqTemplate is a valid request template.
var reqTemplate = KeyPairRequest{
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

// rootCAsFromCA returns a CA pool from a single CA.
func rootCAsFromCA(ca *CertificateAuthority) [][]byte {
	return [][]byte{ca.Certificate}
}

// mustVerify checks that the PKI is valid for the given cluster.
func mustVerify(t *testing.T, rootCAs [][]byte, chain, key []byte, extKeyUsage x509.ExtKeyUsage, zone string) {
	if _, err := Verify(rootCAs, chain, key, extKeyUsage, []string{zone}, true); err != nil {
		t.Log(err)
		t.FailNow()
	}
}

// mustNotVerify checks that the PKI is invalid for the given cluster.
func mustNotVerify(t *testing.T, rootCAs [][]byte, chain, key []byte, extKeyUsage x509.ExtKeyUsage, zone string) {
	if _, err := Verify(rootCAs, chain, key, extKeyUsage, []string{zone}, true); err == nil {
		t.Fatal("verification succeeded unexpectedly")
	}
}

// TestVerify checks that a valid PKI succeeds.
func TestVerify(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	mustVerify(t, rootCAsFromCA(ca), cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyChain checks that a valid PKI chain succeeds.
func TestVerifyChain(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	intermediate, _ := ca.NewIntermediateCertificateAuthority(KeyTypeRSA, intermediateCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(intermediate)
	mustVerify(t, rootCAsFromCA(ca), append(cert, intermediate.Certificate...), key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidPrivateKeyFormat checks that an invalid private key type is rejected.
func TestVerifyInvalidPrivateKeyFormat(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	req := reqTemplate
	req.KeyEncodingPKCS8 = true
	key, cert, _ := req.Generate(ca)
	mustNotVerify(t, rootCAsFromCA(ca), cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidPrivateKeyMultiple checks that multiple private keys are rejected.
func TestVerifyInvalidPrivateKeyMultiple(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	mustNotVerify(t, rootCAsFromCA(ca), cert, append(key, key...), x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCertificateNotYetValid checks that a certificate that is not yet valid is rejected.
func TestVerifyInvalidCertificateNotYetValid(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	req := reqTemplate
	req.ValidFrom = time.Now().Add(1 * time.Hour)
	req.ValidTo = time.Now().Add(2 * time.Hour)
	key, cert, _ := req.Generate(ca)
	mustNotVerify(t, rootCAsFromCA(ca), cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCertificateExpired checks that a certificate that has expired is rejected.
func TestVerifyInvalidCertificateExpired(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	req := reqTemplate
	req.ValidFrom = time.Now().Add(-2 * time.Hour)
	req.ValidTo = time.Now().Add(-1 * time.Hour)
	key, cert, _ := req.Generate(ca)
	mustNotVerify(t, rootCAsFromCA(ca), cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCertificateType checks that a client certificate is rejected.
func TestVerifyInvalidCertificateType(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	req := reqTemplate
	req.CertType = CertTypeClient
	key, cert, _ := req.Generate(ca)
	mustNotVerify(t, rootCAsFromCA(ca), cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyInvalidCertificateSubjectAddressName checks that invalid subject alternate names are rejected.
func TestVerifyInvalidCertificateSubjectAddressName(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	mustNotVerify(t, rootCAsFromCA(ca), cert, key, x509.ExtKeyUsageServerAuth, invalidZone)
}

// TestVerifyInvalidCACertificate checks that a certificate that is not signed by a CA is rejected.
func TestVerifyInvalidCACertificate(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	ca2, _ := NewCertificateAuthority(KeyTypeRSA, "invalid", time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	mustNotVerify(t, rootCAsFromCA(ca2), cert, key, x509.ExtKeyUsageServerAuth, validZone)
}

// TestVerifyCACertificateMultiple checks that multiple CA certificates are allowed.
func TestVerifyCACertificateMultiple(t *testing.T) {
	ca, _ := NewCertificateAuthority(KeyTypeRSA, caCN, time.Now(), time.Now().Add(time.Hour), CertTypeCA)
	key, cert, _ := reqTemplate.Generate(ca)
	mustVerify(t, [][]byte{append(ca.Certificate, ca.Certificate...)}, cert, key, x509.ExtKeyUsageServerAuth, validZone)
}
