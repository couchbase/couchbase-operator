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
	"math/big"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
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
type keyPairRequest struct {
	keyType  KeyType
	certType CertType
	req      *x509.CertificateRequest
}

// Generate returns a PEM encoded private key and signed certificate
// from the specified CA
func (req *keyPairRequest) Generate(ca *CertificateAuthority, certValidFrom, certValidTo time.Time) (key, cert []byte, err error) {
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

	subjectKeyId, err := generateSubjectKeyIdentifier(req.PublicKey)
	if err != nil {
		return nil, err
	}

	cert := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               req.Subject,
		NotBefore:             validFrom,
		NotAfter:              validTo,
		BasicConstraintsValid: true,
		SubjectKeyId:          subjectKeyId,
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

func CreateOperatorCertReq(commonName string) *x509.CertificateRequest {
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: commonName,
		},
	}
}

func CreateClusterCertReq(commonName string, dnsNames []string) *x509.CertificateRequest {
	return &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: commonName,
		},
		DNSNames: dnsNames,
	}
}

func CreateKeyPairReqData(keyType KeyType, certType CertType, certReq *x509.CertificateRequest) *keyPairRequest {
	return &keyPairRequest{
		keyType:  keyType,
		certType: certType,
		req:      certReq,
	}
}

func CreateOperatorSecretData(namespace, secretName string, caCertData []uint8, certPEM, keyPEM []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			"ca.crt":                 caCertData,
			"couchbase-operator.crt": certPEM,
			"couchbase-operator.key": keyPEM,
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
			"chain.pem": certPEM,
			"pkey.key":  keyPEM,
		},
	}
}

var (
	caCN       = "Couchbase CA"
	operatorCN = "Couchbase Operator"
	clusterCN  = "Couchbase Cluster"
)

// tlsContext is generated per test, it contains certificates to be injected into
// the cluster and the CA used to test they have been correctly installed.
type TlsContext struct {
	// ClusterName is used to explicitly set the cluster name
	ClusterName string
	// ca is the certificate authority used to sign the certificates.
	CA *CertificateAuthority
	// operatorSecretName is the name of the secret created for operator certificates.
	OperatorSecretName string
	// clusterSecretName is the name of the secret created for cluster certificates.
	ClusterSecretName string
}

type TlsOpts struct {
	// keyType is the type of key to use e.g. RSA or EC.  Defaults to KeyTypeRSA.
	KeyType *KeyType
	// altNames is the set of DNS alternative names to use.  Defaults to the cluster wildcard and localhost.
	AltNames []string
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
}

// InitClusterTLS accepts a key type (only RSA works for now) and returns a context
// containing all the certificates, and a tear down function to be deferred.
func InitClusterTLS(client kubernetes.Interface, namespace string, opts *TlsOpts) (ctx *TlsContext, teardown func(), err error) {
	// Create the context
	ctx = &TlsContext{}

	// Generate an explicit cluster name to use
	ctx.ClusterName = "test-couchbase-" + RandomSuffix()

	// Generate alt names.  We **need** localhost in here to check the TLS is valid
	// over a port forward from within the cluster.
	altNames := []string{
		fmt.Sprintf("*.%s.%s.svc", ctx.ClusterName, namespace),
		"localhost",
	}
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
	operatorReq := CreateOperatorCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(keyType, operatorCertType, operatorReq)
	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		return
	}

	ctx.OperatorSecretName = "operator-secret-tls-" + RandomSuffix()
	operatorSecretData := CreateOperatorSecretData(namespace, ctx.OperatorSecretName, ctx.CA.Certificate, operatorCert, operatorKey)
	if _, err = CreateSecret(client, namespace, operatorSecretData); err != nil {
		return
	}

	// Create the cluster secret
	clusterReq := CreateClusterCertReq(clusterCN, altNames)
	clusterReqKeyPair := CreateKeyPairReqData(keyType, clusterCertType, clusterReq)
	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		DeleteSecret(client, namespace, ctx.OperatorSecretName, &metav1.DeleteOptions{})
		return
	}

	ctx.ClusterSecretName = "cluster-secret-tls-" + RandomSuffix()
	clusterSecretData := CreateClusterSecretData(namespace, ctx.ClusterSecretName, clusterCert, clusterKey)
	if _, err = CreateSecret(client, namespace, clusterSecretData); err != nil {
		DeleteSecret(client, namespace, ctx.OperatorSecretName, &metav1.DeleteOptions{})
		return
	}

	teardown = func() {
		DeleteSecret(client, namespace, ctx.ClusterSecretName, &metav1.DeleteOptions{})
		DeleteSecret(client, namespace, ctx.OperatorSecretName, &metav1.DeleteOptions{})
	}

	return
}

func TlsCheckForPod(t *testing.T, namespace, podName string, kubeConfig *restclient.Config, ca *CertificateAuthority) error {
	// Start the port forwarder
	pf := PortForwarder{
		Config:    kubeConfig,
		Namespace: namespace,
		Pod:       podName,
		Port:      "18091",
	}
	if err := pf.ForwardPorts(); err != nil {
		return err
	}
	defer pf.Close(t)

	// Get the server certificate
	tlsConfig := &tls.Config{
		RootCAs: x509.NewCertPool(),
	}
	tlsConfig.RootCAs.AddCert(ca.certificate)

	conn, err := tls.Dial("tcp", "localhost:18091", tlsConfig)
	if err != nil {
		return err
	}
	podCert := conn.ConnectionState().PeerCertificates[0]

	t.Logf("Serial:     %v\n", podCert.SerialNumber)
	t.Logf("Subject CN: %v\n", podCert.Subject.CommonName)
	t.Logf("Not Before: %v\n", podCert.NotBefore)
	t.Logf("Not After:  %v\n", podCert.NotAfter)
	for _, dnsAltName := range podCert.DNSNames {
		t.Logf("DNS Alt Name: %v\n", dnsAltName)
	}
	return nil
}
