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
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"math/big"
	"reflect"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

// KeyType defines the supported types of private key that can be used with
// this package.
type KeyType int

// RSA and all supported elliptic curves may be specified.
const (
	KeyTypeRSA KeyType = iota
	KeyTypeEllipticP224
	KeyTypeEllipticP256
	KeyTypeEllipticP384
	KeyTypeEllipticP521
)

// CertType defines the type of certificate generated when a certificate
// request is signed.
type CertType int

// When the type is CertTypeServer the certificate will allow use as a
// server certificate and allow digital signatures and encryption of keying
// material.
// When the type is CertTypeClient the certificate will allow use as a
// client certificate and allow digital signatures only.
const (
	CertTypeServer CertType = iota
	CertTypeClient
	CertTypeCA
)

// KeyPairRequest contains the necessary configuration to generate
// a private key and signed public key pair by a CA.
type KeyPairRequest struct {
	// keyType is the type of private key to generate.
	KeyType KeyType
	// keyEncodingPKCS8 creates a PKCS8 rather than a PKCS1 private key.
	KeyEncodingPKCS8 bool
	// certType is the type of certificate to generate.
	CertType CertType
	// validFrom is the date the certificate is valid from.
	ValidFrom time.Time
	// validTo is the date the certificate is valid until.
	ValidTo time.Time
	// req is the certificate request.
	Req *x509.CertificateRequest
}

// Generate returns a PEM encoded private key and signed certificate
// from the specified CA.
func (req *KeyPairRequest) Generate(ca *CertificateAuthority) (key, cert []byte, err error) {
	// Generate the private key
	var pkey crypto.PrivateKey

	if pkey, err = GeneratePrivateKey(req.KeyType); err != nil {
		return
	}

	// PEM encode the private key
	if key, err = CreatePrivateKey(pkey, req.KeyEncodingPKCS8); err != nil {
		return
	}

	// Add the keying material to the CSR
	var csr *x509.CertificateRequest

	if csr, err = CreateCertificateRequest(req.Req, pkey); err != nil {
		return
	}

	// Sign and PEM encode the certificate
	if cert, err = ca.SignCertificateRequest(csr, req.CertType, req.ValidFrom, req.ValidTo); err != nil {
		return
	}

	return
}

// GeneratePrivateKey generates a private key as defined by KeyType.
func GeneratePrivateKey(keyType KeyType) (crypto.PrivateKey, error) {
	switch keyType {
	case KeyTypeRSA:
		return rsa.GenerateKey(rand.Reader, 2048)
	case KeyTypeEllipticP224:
		return ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	case KeyTypeEllipticP256:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case KeyTypeEllipticP384:
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case KeyTypeEllipticP521:
		return ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	default:
		return nil, errors.NewStackTracedError(errors.ErrPrivateKeyInvalid)
	}
}

// CreatePrivateKey takes a private key input and returns it as a PEM
// encoded slice.
func CreatePrivateKey(key crypto.PrivateKey, pkcs8 bool) ([]byte, error) {
	var block *pem.Block

	if pkcs8 {
		bytes, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, errors.NewStackTracedError(err)
		}

		block = &pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: bytes,
		}
	} else {
		switch t := key.(type) {
		case *rsa.PrivateKey:
			block = &pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(t),
			}
		case *ecdsa.PrivateKey:
			bytes, err := x509.MarshalECPrivateKey(t)
			if err != nil {
				return nil, errors.NewStackTracedError(err)
			}

			block = &pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: bytes,
			}
		default:
			info := reflect.TypeOf(t)
			return nil, fmt.Errorf("%w: %v", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid), info.Name())
		}
	}

	data := &bytes.Buffer{}
	if err := pem.Encode(data, block); err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	return data.Bytes(), nil
}

// CreateRSAPrivateKey creates a PEM formated private key using RSA algorithm.
func CreateRSAPrivateKey() ([]byte, error) {
	// generate private key.
	key, err := GeneratePrivateKey(KeyTypeRSA)
	if err != nil {
		return []byte{}, err
	}

	// pem formated PrivateKey. and intentionally not using pkcs8
	// because when we derive the public key from this there doesn't
	// seem to be a way to then encode cypher text using std pkgs.
	return CreatePrivateKey(key, false)
}

// ParseCertificate accepts a PEM encoded key and
// returns the rsa.PrivateKey representation of it.
func ParsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	pem, _ := pem.Decode(data)
	if pem == nil {
		return nil, errors.NewStackTracedError(errors.ErrCertificateInvalid)
	}

	key, err := x509.ParsePKCS1PrivateKey(pem.Bytes)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	return key, nil
}

// CreateCertificate encodes ASN1 certificate data into a PEM encoded certificate.
func CreateCertificate(cert []byte) ([]byte, error) {
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	}

	data := &bytes.Buffer{}
	if err := pem.Encode(data, block); err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	return data.Bytes(), nil
}

// CreateCertificateRequest applies the provided private(/public) key
// to the CSR and returns it.
func CreateCertificateRequest(req *x509.CertificateRequest, key crypto.PrivateKey) (*x509.CertificateRequest, error) {
	csr, err := x509.CreateCertificateRequest(rand.Reader, req, key)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	crt, err := x509.ParseCertificateRequest(csr)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	return crt, nil
}

// ParseCertificate accepts a PEM encoded certificate and returns the
// x509.Certificate representation of it.
func ParseCertificate(data []byte) (*x509.Certificate, error) {
	pem, _ := pem.Decode(data)
	if pem == nil {
		return nil, errors.NewStackTracedError(errors.ErrCertificateInvalid)
	}

	crt, err := x509.ParseCertificate(pem.Bytes)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	return crt, nil
}

// CertificateAuthority represents a certificate authority with public
// and private key pair.
type CertificateAuthority struct {
	// Private certificate used to sign CSRs
	certificate *x509.Certificate
	// Private key used to sign CSRs
	key crypto.PrivateKey
	// Public certificate
	Certificate []byte
}

// NewCertificateAuthority creates a new CA.  It automatically generates a new
// private key based on preference, then self signs a certificate.
func NewCertificateAuthority(keyType KeyType, commonName string, certValidFrom, certValidTo time.Time, caCertType CertType) (*CertificateAuthority, error) {
	key, err := GeneratePrivateKey(keyType)
	if err != nil {
		return nil, fmt.Errorf("unable to generate CA key: %w", err)
	}

	ca := &CertificateAuthority{
		key: key,
	}

	req := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: commonName,
		},
	}

	req, err = CreateCertificateRequest(req, key)
	if err != nil {
		return nil, fmt.Errorf("unable to generate CA req: %w", err)
	}

	pem, err := ca.SignCertificateRequest(req, caCertType, certValidFrom, certValidTo)
	if err != nil {
		return nil, fmt.Errorf("unable to sign CA cert: %w", err)
	}

	cert, err := ParseCertificate(pem)
	if err != nil {
		return nil, fmt.Errorf("unable to parse CA cert: %w", err)
	}

	ca.certificate = cert
	ca.Certificate = pem

	return ca, nil
}

// NewIntermediateCertificateAuthority creates a new CA signed by another.
func (ca *CertificateAuthority) NewIntermediateCertificateAuthority(keyType KeyType, commonName string, certValidFrom, certValidTo time.Time, caCertType CertType) (*CertificateAuthority, error) {
	key, err := GeneratePrivateKey(keyType)
	if err != nil {
		return nil, fmt.Errorf("unable to generate CA key: %w", err)
	}

	intermediate := &CertificateAuthority{
		key: key,
	}

	req := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: commonName,
		},
	}

	req, err = CreateCertificateRequest(req, key)
	if err != nil {
		return nil, fmt.Errorf("unable to generate CA req: %w", err)
	}

	pem, err := ca.SignCertificateRequest(req, caCertType, certValidFrom, certValidTo)
	if err != nil {
		return nil, fmt.Errorf("unable to sign CA cert: %w", err)
	}

	cert, err := ParseCertificate(pem)
	if err != nil {
		return nil, fmt.Errorf("unable to parse CA cert: %w", err)
	}

	intermediate.certificate = cert
	intermediate.Certificate = pem

	return intermediate, nil
}

// generateSerial creates a unique certificate serial number as defined
// in RFC 3280.  It is upto 20 octets in length and non-negative.
func generateSerial() (*big.Int, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)

	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	return new(big.Int).Abs(serialNumber), nil
}

// generateSubjectKeyIdentifier creates a hash of the public key as defined in
// RFC3280 used to create certificate paths from a leaf to a CA.
func generateSubjectKeyIdentifier(pub interface{}) ([]byte, error) {
	var subjectPublicKey []byte

	var err error

	switch pub := pub.(type) {
	case *rsa.PublicKey:
		subjectPublicKey, err = asn1.Marshal(*pub)
	case *ecdsa.PublicKey:
		subjectPublicKey = elliptic.MarshalCompressed(pub.Curve, pub.X, pub.Y)
	default:
		return nil, errors.NewStackTracedError(errors.ErrPublicKeyInvalid)
	}

	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	sum := sha1.Sum(subjectPublicKey) //nolint:gosec

	return sum[:], nil
}

// SignCertificateRequest accepts a certificate request data structure
// validates it has been signed by the private key associated with the
// request's public key and authenticates the certificate by the CA.
// The certType defines the key usage and extended key usage defined
// by the CertType.
// The returned slice is the PEM encoded certificate.
func (ca *CertificateAuthority) SignCertificateRequest(req *x509.CertificateRequest, certType CertType, validFrom, validTo time.Time) ([]byte, error) {
	serialNumber, err := generateSerial()
	if err != nil {
		return nil, err
	}

	subjectKeyID, err := generateSubjectKeyIdentifier(req.PublicKey)
	if err != nil {
		return nil, err
	}

	cert := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               req.Subject,
		NotBefore:             validFrom,
		NotAfter:              validTo,
		BasicConstraintsValid: true,
		SubjectKeyId:          subjectKeyID,
		DNSNames:              req.DNSNames,
	}

	switch certType {
	case CertTypeServer:
		cert.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	case CertTypeClient:
		cert.KeyUsage = x509.KeyUsageDigitalSignature
		cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	case CertTypeCA:
		cert.IsCA = true
		cert.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	default:
		return nil, errors.NewStackTracedError(errors.ErrCertificateInvalid)
	}

	// If the CA certificate is nil we just want to self sign
	cacert := ca.certificate
	if cacert == nil {
		cacert = cert
	}

	data, err := x509.CreateCertificate(rand.Reader, cert, cacert, req.PublicKey, ca.key)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	pem, err := CreateCertificate(data)
	if err != nil {
		return nil, err
	}

	return pem, nil
}
