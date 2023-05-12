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
	"io"
	"math/big"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/youmark/pkcs8"

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

type KeyEncodingType int

const (
	KeyEncodingPKCS1 KeyEncodingType = iota
	KeyEncodingPKCS8
	KeyEncodingSEC1
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
	keyType         KeyType
	keyEncodingType KeyEncodingType
	certType        CertType
	req             *x509.CertificateRequest
}

// Generate returns a PEM encoded private key and signed certificate
// from the specified CA.
func (req *KeyPairRequest) Generate(ca *CertificateAuthority, certValidFrom, certValidTo time.Time) (key, cert []byte, err error) {
	// Generate the private key
	var pkey crypto.PrivateKey

	if pkey, err = GeneratePrivateKey(req.keyType); err != nil {
		return
	}

	// PEM encode the private key
	if key, err = CreatePrivateKey(pkey, req.keyEncodingType); err != nil {
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
// encoded slice.
func CreatePrivateKey(key crypto.PrivateKey, keyEncoding KeyEncodingType) ([]byte, error) {
	var block *pem.Block

	switch t := key.(type) {
	case *rsa.PrivateKey:
		switch keyEncoding {
		case KeyEncodingPKCS1:
			block = &pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(t),
			}
		case KeyEncodingPKCS8:
			bytes, err := x509.MarshalPKCS8PrivateKey(t)
			if err != nil {
				return nil, err
			}

			block = &pem.Block{
				Type:  "PRIVATE KEY",
				Bytes: bytes,
			}
		default:
			return nil, fmt.Errorf("RSA key cannot be encoded")
		}
	case *ecdsa.PrivateKey:
		switch keyEncoding {
		case KeyEncodingPKCS8:
			bytes, err := x509.MarshalPKCS8PrivateKey(t)
			if err != nil {
				return nil, err
			}

			block = &pem.Block{
				Type:  "PRIVATE KEY",
				Bytes: bytes,
			}
		case KeyEncodingSEC1:
			bytes, err := x509.MarshalECPrivateKey(t)
			if err != nil {
				return nil, err
			}

			block = &pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: bytes,
			}
		default:
			return nil, fmt.Errorf("ECDSA key cannot be encoded")
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

// addPKCS8Passphrase converts an unencrypted PKCS#8 key
// to an encrypted key with specified passphrase.
func addPKCS8Passphrase(privateKey []byte, passphrase []byte) ([]byte, error) {
	// decode from pem and parse out binary key
	p, _ := pem.Decode(privateKey)

	pkcs8key, err := pkcs8.ParsePKCS8PrivateKey(p.Bytes)
	if err != nil {
		return []byte{}, err
	}

	// apply passphrase
	keyBytes, err := pkcs8.ConvertPrivateKeyToPKCS8(pkcs8key, passphrase)
	if err != nil {
		return []byte{}, err
	}

	// re-encode to pem
	block := &pem.Block{
		Type:  "ENCRYPTED PRIVATE KEY",
		Bytes: keyBytes,
	}

	data := &bytes.Buffer{}
	if err = pem.Encode(data, block); err != nil {
		return []byte{}, err
	}

	return data.Bytes(), nil
}

// CreateCertificate encodes ASN1 certificate data into a PEM encoded certificate.
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
// to the CSR and returns it.
func CreateCertificateRequest(req *x509.CertificateRequest, key crypto.PrivateKey) (*x509.CertificateRequest, error) {
	csr, err := x509.CreateCertificateRequest(rand.Reader, req, key)
	if err != nil {
		return nil, err
	}

	return x509.ParseCertificateRequest(csr)
}

// ParseCertificate accepts a PEM encoded certificate and returns the
// x509.Certificate representation of it.
func ParseCertificate(data []byte) (*x509.Certificate, error) {
	pem, _ := pem.Decode(data)
	if pem == nil {
		return nil, fmt.Errorf("unable to parse PEM certificate")
	}

	return x509.ParseCertificate(pem.Bytes)
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
func (ca *CertificateAuthority) NewIntermediateCertificateAuthority(keyType KeyType, commonName string, certValidFrom, certValidTo time.Time) (*CertificateAuthority, error) {
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

	pem, err := ca.SignCertificateRequest(req, CertTypeCA, certValidFrom, certValidTo)
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
		return nil, err
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

func CreateKeyPairReqData(keyType KeyType, keyEncodingType KeyEncodingType, certType CertType, certReq *x509.CertificateRequest) *KeyPairRequest {
	return &KeyPairRequest{
		keyType:         keyType,
		keyEncodingType: keyEncodingType,
		certType:        certType,
		req:             certReq,
	}
}

const (
	certManagerCAKey       = "ca.crt"
	operatorTLSSecretCA    = "ca.crt"
	operatorTLSSecretChain = "couchbase-operator.crt"
	operatorTLSSecretKey   = "couchbase-operator.key"
	clusterTLSSecretKey    = "pkey.key"
	clusterTLSSecretChain  = "chain.pem"
)

// CreateOperatorSecretData creates TLS for the Operator.
func CreateOperatorSecretData(namespace string, ctx *TLSContext) *corev1.Secret {
	if ctx.LegacyTLS() {
		// The legacy way also specifies the CA certificate.
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      ctx.OperatorSecretName,
			},
			Data: map[string][]byte{
				operatorTLSSecretCA:    ctx.CA.Certificate,
				operatorTLSSecretChain: ctx.ClientCert,
				operatorTLSSecretKey:   ctx.ClientKey,
			},
		}
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      ctx.OperatorSecretName,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       ctx.ClientCert,
			corev1.TLSPrivateKeyKey: ctx.ClientKey,
		},
	}
}

func CreateClusterSecretData(namespace string, ctx *TLSContext) *corev1.Secret {
	if ctx.LegacyTLS() {
		// The legacy way doesn't specify the CA.
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      ctx.ClusterSecretName,
			},
			Data: map[string][]byte{
				clusterTLSSecretKey:   ctx.ServerKey,
				clusterTLSSecretChain: ctx.ServerCert,
			},
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      ctx.ClusterSecretName,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       ctx.ServerCert,
			corev1.TLSPrivateKeyKey: ctx.ServerKey,
		},
	}

	// In cert-manager mode, the cluster secret contains the CA certificate.
	if ctx.Source == TLSSourceCertManagerSecret {
		secret.Data[certManagerCAKey] = ctx.CA.Certificate
	}

	return secret
}

// rotateClientSecretData abstracts away secret updates for the various TLS
// configuration modes.
func rotateClientSecretData(ctx *TLSContext, secret *corev1.Secret, ca, cert, key []byte) {
	if ctx.LegacyTLS() {
		secret.Data[operatorTLSSecretChain] = cert
		secret.Data[operatorTLSSecretKey] = key

		// In legacy mode, the operator secret contains the CA unconditionally.
		if ca != nil {
			secret.Data[operatorTLSSecretCA] = ca
		}

		return
	}

	secret.Data[corev1.TLSCertKey] = cert
	secret.Data[corev1.TLSPrivateKeyKey] = key
}

// mustRoateClientSecret rotates the client certificate and key, and optionally the CA,
// regardless of the format.
func mustRotateClientSecret(t *testing.T, ctx *TLSContext, ca, cert, key []byte) {
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.OperatorSecretName)
	if err != nil {
		Die(t, err)
	}

	rotateClientSecretData(ctx, secret, ca, cert, key)

	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}

	if ctx.Source == TLSSourceKubernetesSecret && ca != nil {
		secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.CASecretName)
		if err != nil {
			Die(t, err)
		}

		secret.Data[corev1.TLSCertKey] = ca

		if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
			Die(t, err)
		}
	}
}

// rotateServerSecretData abstracts away secret updates for the various TLS
// configuration modes.
func rotateServerSecretData(ctx *TLSContext, secret *corev1.Secret, ca, cert, key []byte) {
	if ctx.LegacyTLS() {
		secret.Data[clusterTLSSecretChain] = cert
		secret.Data[clusterTLSSecretKey] = key

		return
	}

	secret.Data[corev1.TLSCertKey] = cert
	secret.Data[corev1.TLSPrivateKeyKey] = key

	// In cert-manager mode, the cluster secret contains the CA unconditionally.
	if ctx.Source == TLSSourceCertManagerSecret && ca != nil {
		secret.Data[operatorTLSSecretCA] = ca
	}
}

// mustRotateServerSecret rotates the server certificate and key, and optionally the CA,
// regardless of the format.
func mustRotateServerSecret(t *testing.T, ctx *TLSContext, ca, cert, key []byte) {
	secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.ClusterSecretName)
	if err != nil {
		Die(t, err)
	}

	rotateServerSecretData(ctx, secret, ca, cert, key)

	if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
		Die(t, err)
	}

	if ctx.Source == TLSSourceKubernetesSecret && ca != nil {
		secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.CASecretName)
		if err != nil {
			Die(t, err)
		}

		secret.Data[corev1.TLSCertKey] = ca

		if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
			Die(t, err)
		}
	}
}

const (
	caCN             = "Couchbase CA"
	intermediateCACN = "Couchbase Intermediate CA"
	operatorCN       = "Administrator"
	clusterCN        = "Couchbase Cluster"
)

// This defines how to configure the operator and data formats.
type TLSSource string

const (
	// Legacy sources are all just wrong and are inflexible caused by being
	// too heavily tied to Couchbase's requirements.
	TLSSourceLegacy TLSSource = "legacy"

	// TLS kubernets secret source use Kubernetes standards.
	TLSSourceKubernetesSecret TLSSource = "kubernetesSecret"

	// TLS cert-manager secret sources use Kubernetes standards with cert-manager add ons,
	// and allow us to slip an abstraction layer in to hide all the badness.
	TLSSourceCertManagerSecret TLSSource = "certManagerSecret"
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
	// ServerCertificate is the server certifcate at the leaf of the chain
	ServerCertificate *x509.Certificate
	// ServerCert is the full certificate chain.
	ServerCert []byte
	// Server key is the private key.
	ServerKey []byte
	// ClientCert is the client certificate chain used for verification
	ClientCert []byte
	// CLientKey is the client key used for verification
	ClientKey []byte
	// OperatorSecretName is the name of the secret created for operator certificates.
	OperatorSecretName string
	// ClusterSecretName is the name of the secret created for cluster certificates.
	ClusterSecretName string
	// CASecretName is the name of the CA secret create when using kubernetes secret mode.
	CASecretName string
	// Source is the type of TLS to use for the Operator.
	Source TLSSource
}

type TLSOpts struct {
	// ClusterName is the name of the cluster
	ClusterName string
	// keyType is the type of key to use e.g. RSA or EC.  Defaults to KeyTypeRSA.
	KeyType *KeyType
	// KeyEncoding is the type of key encoding e.g. PKCS#1 or PKCS#8 etc.  Defaults to PKCS#1.
	KeyEncoding *KeyEncodingType
	// altNames is the set of DNS alternative names to use.  Defaults to the cluster wildcard and localhost.
	AltNames []string
	// ExtraAltNames is the set of additional alternative names to append to the AltNames.
	ExtraAltNames []string
	// validFrom is the valid from date for the certificate. Defaults to now.
	ValidFrom *time.Time
	// validTo is the valid to date for the certificate.  Defaults to 10 years from now.
	ValidTo *time.Time
	// ClientValidFrom overrides valid from date for the client certificate. Defaults to ValidFrom.
	ClientValidFrom *time.Time
	// ClientValidTo overrides valid to date for the client certificate.  Defaults to ValidTo.
	ClientValidTo *time.Time
	// caCertType sets the CA certificate type. Defaults to CertTypeCA.
	CaCertType *CertType
	// operatorCertType sets the operator certificate type.  Defaults to CertTypeClient.
	OperatorCertType *CertType
	// clusterCertType sets the cluster certificate type.  Defaults to CertTypeServer.
	ClusterCertType *CertType
	// ldapAltName is DNS name of ldap server to optionally add to AltNames
	// ldapCertType sets the ldap certificate type.  Defaults to CertTypeServer.
	LDAPCertType *CertType
	// Source is the type of TLS to use for the Operator.
	Source TLSSource
	// MultipleCAs defines whether or not multiple CAs should be generated within the secret. Defaults to false.
	MultipleCAs bool
	// KeyPassphrase is the passphrase used to encrypt private key.
	KeyPassphrase string
}

// clusterSANs generates a valid set of SANs for a cluster.
func (ctx *TLSContext) clusterSANs() []string {
	return util_x509.MandatorySANs(ctx.ClusterName, ctx.Namespace)
}

// LegacyTLS returns whether or not the specified TLS Context uses the Legacy TLS system.
func (ctx *TLSContext) LegacyTLS() bool {
	return ctx.Source == "" || ctx.Source == TLSSourceLegacy
}

// InitClusterTLS accepts a key type (only RSA works for now) and returns a context
// containing all the certificates, and a tear down function to be deferred.
func InitClusterTLS(k8s *types.Cluster, opts *TLSOpts) (ctx *TLSContext, err error) {
	// Create the context
	ctx = &TLSContext{
		Client:    k8s.KubeClient,
		Namespace: k8s.Namespace,
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

	keyEncoding := KeyEncodingPKCS1

	if opts.KeyEncoding != nil {
		keyEncoding = *opts.KeyEncoding
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

	ctx.Source = opts.Source

	// Generate the CA.
	if ctx.CA, err = NewCertificateAuthority(keyType, caCN, validFrom, validTo, caCertType); err != nil {
		return
	}

	if opts.MultipleCAs {
		var ca2 *CertificateAuthority

		if ca2, err = NewCertificateAuthority(keyType, caCN, validFrom, validTo, caCertType); err != nil {
			return
		}

		ctx.CA.Certificate = append(ctx.CA.Certificate, ca2.Certificate...)
	}

	// Create the client TLS certifcates.
	operatorReq := CreateCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(keyType, keyEncoding, operatorCertType, operatorReq)

	clientValidFrom := validFrom
	if opts.ClientValidFrom != nil {
		clientValidFrom = *opts.ClientValidFrom
	}

	clientValidTo := validTo
	if opts.ClientValidTo != nil {
		clientValidTo = *opts.ClientValidTo
	}

	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ctx.CA, clientValidFrom, clientValidTo)
	if err != nil {
		return
	}

	ctx.ClientKey = operatorKey
	ctx.ClientCert = operatorCert

	// Create the server TLS certificates.
	clusterReq := CreateCertReqDNS(clusterCN, altNames)
	clusterReqKeyPair := CreateKeyPairReqData(keyType, keyEncoding, clusterCertType, clusterReq)

	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		return
	}

	// Convert to password protected key if passphrase is provided
	if opts.KeyPassphrase != "" {
		clusterKey, err = addPKCS8Passphrase(clusterKey, []byte(opts.KeyPassphrase))
		if err != nil {
			return
		}
	}

	ctx.ServerKey = clusterKey
	ctx.ServerCert = clusterCert

	if ctx.ServerCertificate, err = ParseCertificate(clusterCert); err != nil {
		return
	}

	// Create the actual secrets.
	ctx.OperatorSecretName = "operator-secret-tls-" + RandomSuffix()
	operatorSecretData := CreateOperatorSecretData(k8s.Namespace, ctx)

	if _, err = CreateSecret(k8s, operatorSecretData); err != nil {
		return
	}

	ctx.ClusterSecretName = "cluster-secret-tls-" + RandomSuffix()
	clusterSecretData := CreateClusterSecretData(k8s.Namespace, ctx)

	if _, err = CreateSecret(k8s, clusterSecretData); err != nil {
		return
	}

	// In kubernetes mode, the CA certificate is injected as a separate secret
	// from the operator and cluster secrets.
	if opts.Source == TLSSourceKubernetesSecret {
		ctx.CASecretName = "ca-secret-tls-" + RandomSuffix()

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: ctx.CASecretName,
			},
			Data: map[string][]byte{
				corev1.TLSCertKey: ctx.CA.Certificate,
			},
		}

		if _, err = CreateSecret(k8s, secret); err != nil {
			return
		}
	}

	return
}

// MustInitClusterTLS does the same as InitClusterTLS, dying on failure.
func MustInitClusterTLS(t *testing.T, k8s *types.Cluster, opts *TLSOpts) (ctx *TLSContext) {
	ctx, err := InitClusterTLS(k8s, opts)
	if err != nil {
		Die(t, err)
	}

	return ctx
}

// MustRotateServerCertificate generates a new server certificate and updates the existing secret.
func MustRotateServerCertificate(t *testing.T, ctx *TLSContext, opts *TLSOpts) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)
	subjectAltNames := ctx.clusterSANs()
	keyEncoding := KeyEncodingPKCS1

	// allow overrides via opts
	if opts != nil {
		if len(opts.AltNames) != 0 {
			subjectAltNames = opts.AltNames
		}

		if opts.KeyEncoding != nil {
			keyEncoding = *opts.KeyEncoding
		}
	}

	// Generate a new server certificate
	clusterReq := CreateCertReqDNS(clusterCN, subjectAltNames)
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, keyEncoding, CertTypeServer, clusterReq)

	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	// Encrypt key if passphrase provided
	if opts != nil && opts.KeyPassphrase != "" {
		clusterKey, err = addPKCS8Passphrase(clusterKey, []byte(opts.KeyPassphrase))
		if err != nil {
			Die(t, err)
		}
	}

	if ctx.ServerCertificate, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Update the existing server secret
	mustRotateServerSecret(t, ctx, nil, clusterCert, clusterKey)
}

// MustRotateClientCertificate generates a new client certificate and updates the existing secret.
func MustRotateClientCertificate(t *testing.T, ctx *TLSContext) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Generate a new client certificate
	operatorReq := CreateCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS1, CertTypeClient, operatorReq)

	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	ctx.ClientKey = operatorKey
	ctx.ClientCert = operatorCert

	// Update the existing client secret
	mustRotateClientSecret(t, ctx, nil, operatorCert, operatorKey)
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
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS1, CertTypeServer, clusterReq)

	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(intermediate, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	if ctx.ServerCertificate, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Update the existing server secret
	chain := append(clusterCert, intermediate.Certificate...)

	mustRotateServerSecret(t, ctx, nil, chain, clusterKey)
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
	operatorReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS1, CertTypeClient, operatorReq)

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
	mustRotateClientSecret(t, ctx, nil, chain, operatorKey)
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
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS1, CertTypeServer, clusterReq)

	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	if ctx.ServerCertificate, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	if ctx.LegacyTLS() {
		// Update the existing operator secret
		secret, err := GetSecret(ctx.Client, ctx.Namespace, ctx.OperatorSecretName)
		if err != nil {
			Die(t, err)
		}

		secret.Data[operatorTLSSecretCA] = ctx.CA.Certificate

		if err := UpdateSecret(ctx.Client, ctx.Namespace, secret); err != nil {
			Die(t, err)
		}
	}

	// Update the existing server secret
	mustRotateServerSecret(t, ctx, ctx.CA.Certificate, clusterCert, clusterKey)
}

// MustRotateServerCertificateClientCertificateAndCA generates a new CA and client and server certificates and updates the existing secrets.
func MustRotateServerCertificateClientCertificateAndCA(t *testing.T, ctx *TLSContext) {
	// valid from an hour ago
	validFrom := time.Now().Add(time.Duration(-60) * time.Minute).In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	// Create a new CA
	var err error

	if ctx.CA, err = NewCertificateAuthority(KeyTypeRSA, caCN, validFrom, validTo, CertTypeCA); err != nil {
		Die(t, err)
	}

	// Generate a new server certificate
	clusterReq := CreateCertReqDNS(clusterCN, ctx.clusterSANs())
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS1, CertTypeServer, clusterReq)

	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	if ctx.ServerCertificate, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Generate a new client certificate
	operatorReq := CreateCertReq(operatorCN)
	operatorReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS1, CertTypeClient, operatorReq)

	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	ctx.ClientKey = operatorKey
	ctx.ClientCert = operatorCert

	// Update the existing operator secret
	mustRotateClientSecret(t, ctx, ctx.CA.Certificate, operatorCert, operatorKey)

	// Update the existing server secret
	mustRotateServerSecret(t, ctx, ctx.CA.Certificate, clusterCert, clusterKey)
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
	clusterReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS1, CertTypeServer, clusterReq)

	clusterKey, clusterCert, err := clusterReqKeyPair.Generate(ca, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	if ctx.ServerCertificate, err = ParseCertificate(clusterCert); err != nil {
		Die(t, err)
	}

	// Update the existing server secret
	mustRotateServerSecret(t, ctx, nil, clusterCert, clusterKey)
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
	operatorReqKeyPair := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS1, CertTypeClient, operatorReq)

	operatorKey, operatorCert, err := operatorReqKeyPair.Generate(ca, validFrom, validTo)
	if err != nil {
		Die(t, err)
	}

	ctx.ClientKey = operatorKey
	ctx.ClientCert = operatorCert

	// Update the existing client secret
	mustRotateClientSecret(t, ctx, nil, operatorCert, operatorKey)
}

// newTLSAPI determines whether to use new TLS verification or legacy.
func newTLSAPI(cluster *couchbasev2.CouchbaseCluster) (bool, error) {
	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return false, err
	}

	version, err := couchbaseutil.NewVersion(tag)
	if err != nil {
		return false, err
	}

	return version.GreaterEqualString("7.1.0"), nil
}

// tlsCheckForPod checks a single pod's TLS configuration.  Don't export this, instead consider
// using TlsCheckForCluster which is safer.
func tlsCheckForPod(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, podName string, ctx *TLSContext) error {
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

	host := fmt.Sprintf("%s.%s.%s.svc:18091", podName, cluster.Name, cluster.Namespace)

	conn, err := tls.Dial("tcp", host, tlsConfig)
	if err != nil {
		return err
	}

	// Verify the certificate is as the context expects.
	podCert := conn.ConnectionState().PeerCertificates[0]
	if !podCert.Equal(ctx.ServerCertificate) {
		return fmt.Errorf("server certificate mismatch, expected %v/%v, got %v/%v", ctx.ServerCertificate.Issuer.String(), ctx.ServerCertificate.SerialNumber, podCert.Issuer.String(), podCert.SerialNumber)
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

	ok, err := newTLSAPI(cluster)
	if err != nil {
		return err
	}

	if ok {
		// Handle 7.1+
		adminClient, err := CreateAdminConsoleClient(k8s, cluster)
		if err != nil {
			return err
		}

		var trustedCAs couchbaseutil.TrustedCAList

		if err := couchbaseutil.ListCAs(&trustedCAs).On(adminClient.client, adminClient.host); err != nil {
			return err
		}

		// TODO: check the error type for 404
		for _, ca := range trustedCAs {
			cacert, err := ParseCertificate([]byte(ca.PEM))
			if err != nil {
				return err
			}

			if cacert.Equal(ctx.CA.certificate) {
				return nil
			}
		}

		return fmt.Errorf("no matching CA found in Couchbase Server")
	}

	// Fall back to legacy 6.5/6.6/7.0
	requestLegacy, err := http.NewRequest("GET", "https://"+host+"/pools/default/certificate", nil)
	if err != nil {
		return err
	}

	response, err := client.Do(requestLegacy)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	cacert, err := ParseCertificate(raw)
	if err != nil {
		return err
	}

	if !cacert.Equal(ctx.CA.certificate) {
		return fmt.Errorf("ca certificate mismatch, expected %v/%v, got %v/%v", ctx.CA.certificate.Issuer.String(), ctx.CA.certificate.SerialNumber, cacert.Issuer.String(), cacert.SerialNumber)
	}

	return nil
}

// InitLDAPTLS accepts a key type (only RSA works for now) and returns a context
// containing all the certificates, and a tear down function to be deferred.
func InitLDAPTLS(k8s *types.Cluster, opts *TLSOpts) (ctx *TLSContext, err error) {
	// Create the context
	ctx = &TLSContext{
		Client:    k8s.KubeClient,
		Namespace: k8s.Namespace,
	}

	// Generate alt names.
	altNames := ctx.clusterSANs()

	if len(opts.AltNames) > 0 {
		altNames = opts.AltNames
	}

	// Set the certificate parameters
	keyType := KeyTypeRSA
	keyEncoding := KeyEncodingPKCS8

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
	ldapReqKeyPair := CreateKeyPairReqData(keyType, keyEncoding, ldapCertType, ldapReq)

	ldapKey, ldapCert, err := ldapReqKeyPair.Generate(ctx.CA, validFrom, validTo)
	if err != nil {
		return
	}

	ctx.ClusterSecretName = "ldap-secret-tls-" + RandomSuffix()
	ctx.ServerKey = ldapKey
	ctx.ServerCert = ldapCert
	ctx.Source = TLSSourceCertManagerSecret

	ldapSecretData := CreateClusterSecretData(k8s.Namespace, ctx)
	ldapSecretData.Labels = map[string]string{
		"group": constants.LDAPLabelSelector,
		"name":  constants.TestLabelSelector,
	}

	if _, err = CreateSecret(k8s, ldapSecretData); err != nil {
		return
	}

	return
}

// MustInitLDAPTLS does the same as InitLDAPTLS, dying on failure.
func MustInitLDAPTLS(t *testing.T, k8s *types.Cluster, opts *TLSOpts) (ctx *TLSContext) {
	ctx, err := InitLDAPTLS(k8s, opts)
	if err != nil {
		Die(t, err)
	}

	return ctx
}

// certificatesEqual accepts two PEM encoded certificates and compare them.
func certificatesEqual(a, b []byte) (bool, error) {
	cert1, err := util_x509.ParseCertificate(a)
	if err != nil {
		return false, err
	}

	cert2, err := util_x509.ParseCertificate(b)
	if err != nil {
		return false, err
	}

	return cert1.Equal(cert2), nil
}

// serverHasCA Checks to see if the CA is already present, the problem here is server
// may have done all kinds of things with the certificate, thus tainting our
// original input, so we cannot do a straight match, and we need to decode
// it.
func serverHasCA(serverCAs couchbaseutil.TrustedCAList, requestedCA []byte) (bool, error) {
	for _, serverCA := range serverCAs {
		ok, err := certificatesEqual(requestedCA, []byte(serverCA.PEM))
		if err != nil {
			return false, err
		}

		if ok {
			return true, nil
		}
	}

	return false, nil
}

// validateCAPool checks the server CA pool matches the CAs provided.
func validateCAPool(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, rootCAs []*TLSContext) error {
	client, err := CreateAdminConsoleClient(k8s, cluster)
	if err != nil {
		return err
	}

	// Get all CAs that server knows about.
	var serverCAs couchbaseutil.TrustedCAList

	if err := couchbaseutil.ListCAs(&serverCAs).On(client.client, client.host); err != nil {
		return err
	}

	if len(serverCAs) != len(rootCAs) {
		return fmt.Errorf("CA certificate number mismatch expected %d, got %d", len(rootCAs), len(serverCAs))
	}

	for _, ca := range rootCAs {
		ok, err := serverHasCA(serverCAs, ca.CA.Certificate)
		if err != nil {
			return err
		}

		if !ok {
			return fmt.Errorf("expected CA certificate not found in server")
		}
	}

	return nil
}

// MustValidateCAPool checks the server CA pool matches the CAs provided.
func MustValidateCAPool(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration, rootCAs ...*TLSContext) {
	callback := func() error {
		return validateCAPool(k8s, cluster, rootCAs)
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}
