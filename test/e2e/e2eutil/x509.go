package e2eutil

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
// a private key and signed public key pair by a CA
type KeyPairRequest struct {
	keyType  KeyType
	certType CertType
	req      *x509.CertificateRequest
}

// Generate returns a PEM encoded private key and signed certificate
// from the specified CA
func (req *KeyPairRequest) Generate(ca *CertificateAuthority, certValidFrom, certValidTo time.Time) (key, cert []byte, err error) {
	// Generate the private key
	var pkey crypto.PrivateKey
	if pkey, err = GeneratePrivateKey(req.keyType); err != nil {
		return
	}

	// PEM encode the private key
	if key, err = CreatePrivateKey(pkey); err != nil {
		return
	}

	// Add the keying material to the CSR
	var csr *x509.CertificateRequest
	if csr, err = CreateCertificateRequest(req.req, pkey); err != nil {
		return
	}

	// Sign and PEM encode the certificate
	if cert, err = ca.SignCertificateRequest(csr, req.certType, certValidFrom, certValidTo); err != nil {
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
		return nil, fmt.Errorf("unhandled key type")
	}
}

// CreatePrivateKeyPEM takes a private key input and returns it as a PEM
// encoded slice
func CreatePrivateKey(key crypto.PrivateKey) ([]byte, error) {
	var block *pem.Block
	switch t := key.(type) {
	case *rsa.PrivateKey:
		block = &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(t),
		}
	case *ecdsa.PrivateKey:
		bytes, err := x509.MarshalECPrivateKey(t)
		if err != nil {
			return nil, err
		}
		block = &pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: bytes,
		}
	default:
		info := reflect.TypeOf(t)
		return nil, fmt.Errorf("unsupported key type %v", info.Name())
	}

	data := &bytes.Buffer{}
	if err := pem.Encode(data, block); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

// CreateCertificate encodes ASN1 certificate data into a PEM encoded certificate
func CreateCertificate(cert []byte) ([]byte, error) {
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	}
	data := &bytes.Buffer{}
	if err := pem.Encode(data, block); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

// CreateCertificateRequest applies the provided private(/public) key
// to the CSR and returns it
func CreateCertificateRequest(req *x509.CertificateRequest, key crypto.PrivateKey) (*x509.CertificateRequest, error) {
	csr, err := x509.CreateCertificateRequest(rand.Reader, req, key)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificateRequest(csr)
}

// ParseCertificate accepts a PEM encoded certificate and returns the
// x509.Certificate representation of it
func ParseCertificate(data []byte) (*x509.Certificate, error) {
	pem, _ := pem.Decode(data)
	if pem == nil {
		return nil, fmt.Errorf("unable to parse PEM certificate")
	}
	return x509.ParseCertificate(pem.Bytes)
}

// CertificateAuthority represents a certificate authority with public
// and private key pair
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
		return nil, fmt.Errorf("unable to generate CA key: %v", err)
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
		return nil, fmt.Errorf("unable to generate CA req: %v", err)
	}

	pem, err := ca.SignCertificateRequest(req, caCertType, certValidFrom, certValidTo)
	if err != nil {
		return nil, fmt.Errorf("unable to sign CA cert: %v", err)
	}

	cert, err := ParseCertificate(pem)
	if err != nil {
		return nil, fmt.Errorf("unable to parse CA cert: %v", err)
	}

	ca.certificate = cert
	ca.Certificate = pem

	return ca, nil
}

// NewIntermediateCertificateAuthority creates a new CA signed by another.
func (ca *CertificateAuthority) NewIntermediateCertificateAuthority(keyType KeyType, commonName string, certValidFrom, certValidTo time.Time) (*CertificateAuthority, error) {
	key, err := GeneratePrivateKey(keyType)
	if err != nil {
		return nil, fmt.Errorf("unable to generate CA key: %v", err)
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
		return nil, fmt.Errorf("unable to generate CA req: %v", err)
	}

	pem, err := ca.SignCertificateRequest(req, CertTypeCA, certValidFrom, certValidTo)
	if err != nil {
		return nil, fmt.Errorf("unable to sign CA cert: %v", err)
	}

	cert, err := ParseCertificate(pem)
	if err != nil {
		return nil, fmt.Errorf("unable to parse CA cert: %v", err)
	}

	intermediate.certificate = cert
	intermediate.Certificate = pem

	return intermediate, nil
}

// generateSerial creates a unique certificate serial number as defined
// in RFC 3280.  It is upto 20 octets in length and non-negative
func generateSerial() (*big.Int, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Abs(serialNumber), nil
}

// generateSubjectKeyIdentifier creates a hash of the public key as defined in
// RFC3280 used to create certificate paths from a leaf to a CA
func generateSubjectKeyIdentifier(pub interface{}) ([]byte, error) {
	var subjectPublicKey []byte
	var err error
	switch pub := pub.(type) {
	case *rsa.PublicKey:
		subjectPublicKey, err = asn1.Marshal(*pub)
	case *ecdsa.PublicKey:
		subjectPublicKey = elliptic.Marshal(pub.Curve, pub.X, pub.Y)
	default:
		return nil, fmt.Errorf("invalid public key type")
	}
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum(subjectPublicKey)
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
		return nil, fmt.Errorf("invalid certificate type")
	}

	// If the CA certificate is nil we just want to self sign
	cacert := ca.certificate
	if cacert == nil {
		cacert = cert
	}

	data, err := x509.CreateCertificate(rand.Reader, cert, cacert, req.PublicKey, ca.key)
	if err != nil {
		return nil, err
	}

	pem, err := CreateCertificate(data)
	if err != nil {
		return nil, err
	}

	return pem, nil
}

func CreateCertReq(commonName string) *x509.CertificateRequest {
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: commonName,
		},
	}
}

func CreateCertReqDNS(commonName string, dnsNames []string) *x509.CertificateRequest {
	req := CreateCertReq(commonName)
	req.DNSNames = dnsNames
	return req
}

func CreateIntermedateCACertReq(commonName string) *x509.CertificateRequest {
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: commonName,
		},
	}
}

func CreateKeyPairReqData(keyType KeyType, certType CertType, certReq *x509.CertificateRequest) *KeyPairRequest {
	return &KeyPairRequest{
		keyType:  keyType,
		certType: certType,
		req:      certReq,
	}
}

const (
	operatorTLSSecretCA    = "ca.crt"
	operatorTLSSecretChain = "couchbase-operator.crt"
	operatorTLSSecretKey   = "couchbase-operator.key"
	clusterTLSSecretKey    = "pkey.key"
	clusterTLSSecretChain  = "chain.pem"
)

func CreateOperatorSecretData(namespace, secretName string, caCertData []uint8, certPEM, keyPEM []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      secretName,
			Labels: map[string]string{
				"group": constants.LDAPLabelSelector,
			},
		},
		Data: map[string][]byte{
			operatorTLSSecretCA:    caCertData,
			operatorTLSSecretChain: certPEM,
			operatorTLSSecretKey:   keyPEM,
		},
	}
}

func CreateClusterSecretData(namespace, secretName string, certPEM, keyPEM []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			clusterTLSSecretChain: certPEM,
			clusterTLSSecretKey:   keyPEM,
		},
	}
}

const (
	caCN             = "Couchbase CA"
	intermediateCACN = "Couchbase Intermediate CA"
	operatorCN       = "Administrator"
	clusterCN        = "Couchbase Cluster"
)

// tlsContext is generated per test, it contains certificates to be injected into
// the cluster and the CA used to test they have been correctly installed.
type TLSContext struct {
	// Client is the kubernetes client
	Client kubernetes.Interface
	// Namespace is the namespace the cluster lives in.
	Namespace string
	// ClusterName is used to explicitly set the cluster name
	ClusterName string
	// CA is the certificate authority used to sign the certificates.
	CA *CertificateAuthority
	// ServerCert is the server certifcate at the leaf of the chain
	ServerCert *x509.Certificate
	// ClientCert is the client certificate chain used for verification
	ClientCert []byte
	// CLientKey is the client key used for verification
	ClientKey []byte
	// OperatorSecretName is the name of the secret created for operator certificates.
	OperatorSecretName string
	// ClusterSecretName is the name of the secret created for cluster certificates.
	ClusterSecretName string
	// LDAPSecretName is the name of the secret created for ldap certificates.
	LDAPSecretName string
}

type TLSOpts struct {
	// ClusterName is the name of the cluster
	ClusterName string
	// keyType is the type of key to use e.g. RSA or EC.  Defaults to KeyTypeRSA.
	KeyType *KeyType
	// altNames is the set of DNS alternative names to use.  Defaults to the cluster wildcard and localhost.
	AltNames []string
	// ExtraAltNames is the set of additional alternative names to append to the AltNames.
	ExtraAltNames []string
	// validFrom is the valid from date for the certificate. Defaults to now.
	ValidFrom *time.Time
	// validTo is the valid to date for the certificate.  Defaults to 10 years from now.
	ValidTo *time.Time
	// caCertType sets the CA certificate type. Defaults to CertTypeCA.
	CaCertType *CertType
	// operatorCertType sets the operator certificate type.  Defaults to CertTypeClient.
	OperatorCertType *CertType
	// clusterCertType sets the cluster certificate type.  Defaults to CertTypeServer.
	ClusterCertType *CertType
	// ldapAltName is DNS name of ldap server to optionally add to AltNames
	// ldapCertType sets the ldap certificate type.  Defaults to CertTypeServer.
	LDAPCertType *CertType
}

// clusterSANs generates a valid set of SANs for a cluster.
func (ctx *TLSContext) clusterSANs() []string {
	return util_x509.MandatorySANs(ctx.ClusterName, ctx.Namespace)
}

// InitClusterTLS accepts a key type (only RSA works for now) and returns a context
// containing all the certificates, and a tear down function to be deferred.
func InitClusterTLS(client kubernetes.Interface, namespace string, opts *TLSOpts) (ctx *TLSContext, teardown func(), err error) {
	// Create the context
	ctx = &TLSContext{
		Client:    client,
		Namespace: namespace,
	}

	// Generate an explicit cluster name to use
	ctx.ClusterName = "test-couchbase-" + RandomSuffix()
	if opts.ClusterName != "" {
		ctx.ClusterName = opts.ClusterName
	}

	// Generate alt names.
	altNames := ctx.clusterSANs()
	if len(opts.AltNames) > 0 {
		altNames = opts.AltNames
	}
	altNames = append(altNames, opts.ExtraAltNames...)

	// Set the certificate parameters
	keyType := KeyTypeRSA
	if opts.KeyType != nil {
		keyType = *opts.KeyType
	}

	validFrom := time.Now().In(time.UTC)
	if opts.ValidFrom != nil {
		validFrom = *opts.ValidFrom
	}

	validTo := validFrom.AddDate(10, 0, 0)
	if opts.ValidTo != nil {
		validTo = *opts.ValidTo
	}

	caCertType := CertTypeCA
	if opts.CaCertType != nil {
		caCertType = *opts.CaCertType
	}

	operatorCertType := CertTypeClient
	if opts.OperatorCertType != nil {
		operatorCertType = *opts.OperatorCertType
	}

	clusterCertType := CertTypeServer
	if opts.ClusterCertType != nil {
		clusterCertType = *opts.ClusterCertType
	}

	// Generate the CA
	if ctx.CA, err = NewCertificateAuthority(keyType, caCN, validFrom, validTo, caCertType); err != nil {
		return
	}

	// Create the operator secret
	operatorReq := CreateCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(keyType, operatorCertType, operatorReq)
	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		return
	}
	ctx.ClientKey = operatorKey
	ctx.ClientCert = operatorCert

	ctx.OperatorSecretName = "operator-secret-tls-" + RandomSuffix()
	operatorSecretData := CreateOperatorSecretData(namespace, ctx.OperatorSecretName, ctx.CA.Certificate, operatorCert, operatorKey)
	if _, err = CreateSecret(client, namespace, operatorSecretData); err != nil {
		return
	}

	// Create the cluster secret
	clusterReq := CreateCertReqDNS(clusterCN, altNames)
	clusterReqKeyPair := CreateKeyPairReqData(keyType, clusterCertType, clusterReq)
	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		_ = DeleteSecret(client, namespace, ctx.OperatorSecretName, &metav1.DeleteOptions{})
		return
	}
	if ctx.ServerCert, err = ParseCertificate(clusterCert); err != nil {
		return
	}

	ctx.ClusterSecretName = "cluster-secret-tls-" + RandomSuffix()
	clusterSecretData := CreateClusterSecretData(namespace, ctx.ClusterSecretName, clusterCert, clusterKey)
	if _, err = CreateSecret(client, namespace, clusterSecretData); err != nil {
		_ = DeleteSecret(client, namespace, ctx.OperatorSecretName, &metav1.DeleteOptions{})
		return
	}

	teardown = func() {
		_ = DeleteSecret(client, namespace, ctx.ClusterSecretName, &metav1.DeleteOptions{})
		_ = DeleteSecret(client, namespace, ctx.OperatorSecretName, &metav1.DeleteOptions{})
	}

	return
}

// MustInitClusterTLS does the same as InitClusterTLS, dying on failure
func MustInitClusterTLS(t *testing.T, k8s *types.Cluster, namespace string, opts *TLSOpts) (ctx *TLSContext, teardown func()) {
	ctx, teardown, err := InitClusterTLS(k8s.KubeClient, namespace, opts)
	if err != nil {
		Die(t, err)
	}
	return ctx, teardown
}

// MustRotateServerCertificate generates a new server certificate and updates the existing secret.
func MustRotateServerCertificate(t *testing.T, ctx *TLSContext, subjectAltNames []string) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Generate a new server certificate
	if len(subjectAltNames) == 0 {
		subjectAltNames = ctx.clusterSANs()
	}
	clusterReq := CreateCertReqDNS(clusterCN, subjectAltNames)
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeServer, clusterReq)
	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}
	if ctx.ServerCert, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Update the existing server secret
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.ClusterSecretName)
	if err != nil {
		Die(t, err)
	}
	secret.Data[clusterTLSSecretKey] = clusterKey
	secret.Data[clusterTLSSecretChain] = clusterCert
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}
}

// MustRotateClientCertificate generates a new client certificate and updates the existing secret.
func MustRotateClientCertificate(t *testing.T, ctx *TLSContext) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Generate a new client certificate
	operatorReq := CreateCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeClient, operatorReq)
	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	// Update the existing client secret
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.OperatorSecretName)
	if err != nil {
		Die(t, err)
	}
	secret.Data[operatorTLSSecretKey] = operatorKey
	secret.Data[operatorTLSSecretChain] = operatorCert
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}
}

// MustRotateServerCertificateChain generates a new intermediate CA and server certificate and updates the existing secret.
func MustRotateServerCertificateChain(t *testing.T, ctx *TLSContext) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Create an intermediate CA
	intermediate, err := ctx.CA.NewIntermediateCertificateAuthority(KeyTypeRSA, intermediateCACN, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	// Generate a new server certificate
	clusterReq := CreateCertReqDNS(clusterCN, ctx.clusterSANs())
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeServer, clusterReq)
	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(intermediate, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}
	if ctx.ServerCert, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Update the existing server secret
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.ClusterSecretName)
	if err != nil {
		Die(t, err)
	}

	chain := append(clusterCert, intermediate.Certificate...)

	secret.Data[clusterTLSSecretKey] = clusterKey
	secret.Data[clusterTLSSecretChain] = chain
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}
}

// MustRotateClientCertificateChain generates a new intermediate CA and client certificate and updates the existing secret.
func MustRotateClientCertificateChain(t *testing.T, ctx *TLSContext) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Create an intermediate CA
	intermediate, err := ctx.CA.NewIntermediateCertificateAuthority(KeyTypeRSA, intermediateCACN, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	// Generate a new client certificate
	operatorReq := CreateCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeClient, operatorReq)
	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(intermediate, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}
	ctx.ClientKey = operatorKey
	ctx.ClientCert = operatorCert
	ctx.ClientCert = append(ctx.ClientCert, []byte("\n")...)
	ctx.ClientCert = append(ctx.ClientCert, intermediate.Certificate...)

	chain := append(operatorCert, intermediate.Certificate...)

	// Update the existing client secret
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.OperatorSecretName)
	if err != nil {
		Die(t, err)
	}
	secret.Data[operatorTLSSecretKey] = operatorKey
	secret.Data[operatorTLSSecretChain] = chain
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}
}

// MustRotateServerCertificateAndCA generates a new CA and server certificate and updates the existing secret.
func MustRotateServerCertificateAndCA(t *testing.T, ctx *TLSContext) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Create a new CA
	var err error
	if ctx.CA, err = NewCertificateAuthority(KeyTypeRSA, caCN, validFrom, validTo, CertTypeCA); err != nil {
		Die(t, err)
	}

	// Generate a new server certificate
	clusterReq := CreateCertReqDNS(clusterCN, ctx.clusterSANs())
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeServer, clusterReq)
	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}
	if ctx.ServerCert, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Update the existing operator secret
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.OperatorSecretName)
	if err != nil {
		Die(t, err)
	}
	secret.Data[operatorTLSSecretCA] = ctx.CA.Certificate
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}

	// Update the existing server secret
	if secret, err = GetSecret(ctx.Client, ctx.Namespace, ctx.ClusterSecretName); err != nil {
		Die(t, err)
	}
	secret.Data[clusterTLSSecretKey] = clusterKey
	secret.Data[clusterTLSSecretChain] = clusterCert
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}
}

// MustRotateServerCertificateClientCertificateAndCA generates a new CA and client and server certificates and updates the existing secrets.
func MustRotateServerCertificateClientCertificateAndCA(t *testing.T, ctx *TLSContext) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Create a new CA
	var err error
	if ctx.CA, err = NewCertificateAuthority(KeyTypeRSA, caCN, validFrom, validTo, CertTypeCA); err != nil {
		Die(t, err)
	}

	// Generate a new server certificate
	clusterReq := CreateCertReqDNS(clusterCN, ctx.clusterSANs())
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeServer, clusterReq)
	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}
	if ctx.ServerCert, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Generate a new client certificate
	operatorReq := CreateCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeClient, operatorReq)
	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}
	ctx.ClientKey = operatorKey
	ctx.ClientCert = operatorCert

	// Update the existing operator secret
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.OperatorSecretName)
	if err != nil {
		Die(t, err)
	}
	secret.Data[operatorTLSSecretCA] = ctx.CA.Certificate
	secret.Data[operatorTLSSecretKey] = operatorKey
	secret.Data[operatorTLSSecretChain] = operatorCert
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}

	// Update the existing server secret
	if secret, err = GetSecret(ctx.Client, ctx.Namespace, ctx.ClusterSecretName); err != nil {
		Die(t, err)
	}
	secret.Data[clusterTLSSecretKey] = clusterKey
	secret.Data[clusterTLSSecretChain] = clusterCert
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}
}

// MustRotateServerCertificateWrongCA generates a new CA and server certificate, but only updates the server cert.
func MustRotateServerCertificateWrongCA(t *testing.T, ctx *TLSContext) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Create a new CA, don't add to the context
	ca, err := NewCertificateAuthority(KeyTypeRSA, caCN, validFrom, validTo, CertTypeCA)
	if err != nil {
		Die(t, err)
	}

	// Generate a new server certificate
	clusterReq := CreateCertReqDNS(clusterCN, ctx.clusterSANs())
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeServer, clusterReq)
	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ca, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}
	if ctx.ServerCert, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Update the existing server secret
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.ClusterSecretName)
	if err != nil {
		Die(t, err)
	}
	secret.Data[clusterTLSSecretKey] = clusterKey
	secret.Data[clusterTLSSecretChain] = clusterCert
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}
}

// MustRotateClientCertificateWrongCA generates a new CA and client certificate, but only updates the client cert.
func MustRotateClientCertificateWrongCA(t *testing.T, ctx *TLSContext) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Create a new CA, don't add to the context
	ca, err := NewCertificateAuthority(KeyTypeRSA, caCN, validFrom, validTo, CertTypeCA)
	if err != nil {
		Die(t, err)
	}

	// Generate a new client certificate
	operatorReq := CreateCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, CertTypeClient, operatorReq)
	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ca, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}
	ctx.ClientKey = operatorKey
	ctx.ClientCert = operatorCert

	// Update the existing client secret
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.OperatorSecretName)
	if err != nil {
		Die(t, err)
	}
	secret.Data[operatorTLSSecretKey] = operatorKey
	secret.Data[operatorTLSSecretChain] = operatorCert
	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}
}

// tlsCheckForPod checks a single pod's TLS configuration.  Don't export this, instead consider
// using TlsCheckForCluster which is safer.
func tlsCheckForPod(t *testing.T, k8s *types.Cluster, namespace, podName string, ctx *TLSContext) error {
	// Allocate a free port to use
	sport, err := getFreePort()
	if err != nil {
		return err
	}

	// Start the port forwarder
	pf := portforward.PortForwarder{
		Config:    k8s.Config,
		Client:    k8s.KubeClient,
		Namespace: namespace,
		Pod:       podName,
		Port:      sport + ":18091",
	}
	if err := pf.ForwardPorts(); err != nil {
		return err
	}
	defer pf.Close()

	clientCert, err := tls.X509KeyPair(ctx.ClientCert, ctx.ClientKey)
	if err != nil {
		return err
	}

	// Validate that the server certificate is valid for the context's CA.
	tlsConfig := &tls.Config{
		RootCAs: x509.NewCertPool(),
		Certificates: []tls.Certificate{
			clientCert,
		},
	}
	tlsConfig.RootCAs.AddCert(ctx.CA.certificate)

	conn, err := tls.Dial("tcp", "localhost:"+sport, tlsConfig)
	if err != nil {
		return err
	}

	// Verify the certificate is as the context expects.
	podCert := conn.ConnectionState().PeerCertificates[0]
	if !podCert.Equal(ctx.ServerCert) {
		t.Logf("Unexpected server certificate!\n")
		t.Logf("Expected:\n")
		t.Logf("  Issuer: %v\n", ctx.ServerCert.Issuer.String())
		t.Logf("  Serial: %v\n", ctx.ServerCert.SerialNumber)
		t.Logf("Actual:\n")
		t.Logf("  Issuer: %v\n", podCert.Issuer.String())
		t.Logf("  Serial: %v\n", podCert.SerialNumber)
		return fmt.Errorf("certificate mismatch")
	}

	// Get the cluster CA and verify it matches the context's CA.
	dialer := func(network, addr string) (net.Conn, error) {
		return tls.DialWithDialer(&net.Dialer{}, network, addr, tlsConfig)
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialTLS: dialer,
		},
	}
	request, err := http.NewRequest("GET", "https://localhost:"+sport+"/pools/default/certificate", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	cacert, err := ParseCertificate(raw)
	if err != nil {
		return err
	}
	if !cacert.Equal(ctx.CA.certificate) {
		t.Logf("Unexpected CA certificate!\n")
		t.Logf("Expected:\n")
		t.Logf("  Issuer: %v\n", ctx.CA.certificate.Issuer.String())
		t.Logf("  Serial: %v\n", ctx.CA.certificate.SerialNumber)
		t.Logf("Actual:\n")
		t.Logf("  Issuer: %v\n", cacert.Issuer.String())
		t.Logf("  Serial: %v\n", cacert.SerialNumber)
		return fmt.Errorf("certificate mismatch")
	}

	return nil
}

// InitLDAPTLS accepts a key type (only RSA works for now) and returns a context
// containing all the certificates, and a tear down function to be deferred.
func InitLDAPTLS(client kubernetes.Interface, namespace string, opts *TLSOpts) (ctx *TLSContext, teardown func(), err error) {
	// Create the context
	ctx = &TLSContext{
		Client:    client,
		Namespace: namespace,
	}

	// Generate alt names.
	altNames := ctx.clusterSANs()
	if len(opts.AltNames) > 0 {
		altNames = opts.AltNames
	}

	// Set the certificate parameters
	keyType := KeyTypeRSA
	if opts.KeyType != nil {
		keyType = *opts.KeyType
	}

	validFrom := time.Now().In(time.UTC)
	if opts.ValidFrom != nil {
		validFrom = *opts.ValidFrom
	}

	validTo := validFrom.AddDate(10, 0, 0)
	if opts.ValidTo != nil {
		validTo = *opts.ValidTo
	}

	caCertType := CertTypeCA
	if opts.CaCertType != nil {
		caCertType = *opts.CaCertType
	}

	ldapCertType := CertTypeServer
	if opts.LDAPCertType != nil {
		ldapCertType = *opts.LDAPCertType
	}
	// Generate the CA
	if ctx.CA, err = NewCertificateAuthority(keyType, caCN, validFrom, validTo, caCertType); err != nil {
		return
	}

	// Create the req
	ldapReq := CreateCertReqDNS(operatorCN, altNames)
	ldapReqKeyPair := CreateKeyPairReqData(keyType, ldapCertType, ldapReq)
	ldapKey, ldapCert, err := ldapReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		return
	}

	ctx.LDAPSecretName = "ldap-secret-tls-" + RandomSuffix()
	ldapSecretData := CreateOperatorSecretData(namespace, ctx.LDAPSecretName, ctx.CA.Certificate, ldapCert, ldapKey)
	if _, err = CreateSecret(client, namespace, ldapSecretData); err != nil {
		return
	}

	teardown = func() {
		_ = DeleteSecret(client, namespace, ctx.LDAPSecretName, &metav1.DeleteOptions{})
	}

	return
}

// MustInitLDAPTLS does the same as InitLDAPTLS, dying on failure
func MustInitLDAPTLS(t *testing.T, k8s *types.Cluster, namespace string, opts *TLSOpts) (ctx *TLSContext, teardown func()) {
	ctx, teardown, err := InitLDAPTLS(k8s.KubeClient, namespace, opts)
	if err != nil {
		Die(t, err)
	}
	return ctx, teardown
}
