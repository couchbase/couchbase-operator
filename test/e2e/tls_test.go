package e2e

import (
	"crypto"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/rand"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// randomSuffix generates a 5 character random suffix to be appended to
// k8s resources to avoid namespace collisions (especially events)
func randomSuffix() string {
	// Seed the PRNG so we get vagely random suffixes across runs
	rand.Seed(time.Now().UnixNano())

	// Generate a random 5 character suffix for the cluster name
	suffix := ""
	for i := 0; i < 5; i++ {
		// Our alphabet is 0-9 a-z, so 36 characters
		ordinal := rand.Intn(36)
		// Less than 10 places it in the 0-9 range, otherwise in
		// the a-z range
		if ordinal < 10 {
			ordinal = ordinal + int('0')
		} else {
			ordinal = ordinal - 10 + int('a')
		}
		// Append to the name
		suffix = suffix + string(rune(ordinal))
	}

	return suffix
}

// randomClusterName returns a randomized cluster name
func randomClusterName() string {
	return "test-couchbase-" + randomSuffix()
}

// KeyPairRequest contains the necessary configuration to generate
// a private key and signed public key pair by a CA
type keyPairRequest struct {
	keyType  e2eutil.KeyType
	certType e2eutil.CertType
	req      *x509.CertificateRequest
}

// Generate returns a PEM encoded private key and signed certificate
// from the specified CA
func (req *keyPairRequest) generate(ca *e2eutil.CertificateAuthority) (key, cert []byte, err error) {
	// Generate the private key
	var pkey crypto.PrivateKey
	if pkey, err = e2eutil.GeneratePrivateKey(req.keyType); err != nil {
		return
	}

	// PEM encode the private key
	if key, err = e2eutil.CreatePrivateKey(pkey); err != nil {
		return
	}

	// Add the keying material to the CSR
	var csr *x509.CertificateRequest
	if csr, err = e2eutil.CreateCertificateRequest(req.req, pkey); err != nil {
		return
	}

	// Sign and PEM encode the certificate
	if cert, err = ca.SignCertificateRequest(csr, req.certType); err != nil {
		return
	}

	return
}

// tlsDecorator accepts a test function and key type and returns a decorated
// version of the function which creates cluster specific TLS certificates and
// installs them into K8S before running the test(s)
func tlsDecorator(test TestFunc, keyType e2eutil.KeyType) TestFunc {
	wrapper := func(t *testing.T) {
		f := framework.Global

		// Create the root CA (self signed)
		ca, err := e2eutil.NewCertificateAuthority(keyType, "Couchbase CA")
		if err != nil {
			t.Fatal("unable to generate CA:", err)
		}

		// Create the operator client certificate
		req := &keyPairRequest{
			keyType:  keyType,
			certType: e2eutil.CertTypeClient,
			req: &x509.CertificateRequest{
				Subject: pkix.Name{
					CommonName: "Couchbase Operator",
				},
			},
		}
		operatorKeyPEM, operatorCertPEM, err := req.generate(ca)
		if err != nil {
			t.Fatal("unable to generate operator key pair", err)
		}

		// Create the cluster certificate, this is cluster specific with static
		// TLS configuration due to the dynamic nature of k8s DNS domains
		cluserName := randomClusterName()

		req = &keyPairRequest{
			keyType:  keyType,
			certType: e2eutil.CertTypeServer,
			req: &x509.CertificateRequest{
				Subject: pkix.Name{
					CommonName: "Couchbase Cluster",
				},
				DNSNames: []string{
					"*." + cluserName + "." + f.Namespace + ".svc",
				},
			},
		}
		clusterKeyPEM, clusterCertPEM, err := req.generate(ca)
		if err != nil {
			t.Fatal("unable to generate cluster key pair", err)
		}

		// Create the operator secret
		operatorSecretData := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: f.Namespace,
				Name:      "operator-secret-tls-" + randomSuffix(),
			},
			Data: map[string][]byte{
				"ca.crt":                 ca.Certificate,
				"couchbase-operator.crt": operatorCertPEM,
				"couchbase-operator.key": operatorKeyPEM,
			},
		}

		operatorSecret, err := e2eutil.CreateSecret(f.KubeClient, f.Namespace, operatorSecretData)
		if err != nil {
			t.Fatal("unable to create operator secret:", err)
		}

		defer e2eutil.DeleteSecret(f.KubeClient, f.Namespace, operatorSecret.Name, &metav1.DeleteOptions{})

		// Create the cluster secret
		clusterSecretData := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: f.Namespace,
				Name:      "cluster-secret-tls-" + randomSuffix(),
			},
			Data: map[string][]byte{
				"chain.pem": clusterCertPEM,
				"pkey.key":  clusterKeyPEM,
			},
		}

		clusterSecret, err := e2eutil.CreateSecret(f.KubeClient, f.Namespace, clusterSecretData)
		if err != nil {
			t.Fatal("unable to create cluster secret:", err)
		}

		defer e2eutil.DeleteSecret(f.KubeClient, f.Namespace, clusterSecret.Name, &metav1.DeleteOptions{})

		tls := &api.TLSPolicy{
			Static: &api.StaticTLS{
				Member: &api.MemberSecret{
					ServerSecret: clusterSecret.Name,
				},
				OperatorSecret: operatorSecret.Name,
			},
		}

		// Update cluster parameters
		e2espec.SetClusterName(cluserName)
		defer e2espec.ResetClusterName()

		e2espec.SetTLS(tls)
		defer e2espec.ResetTLS()

		// Run the test!
		test(t)
	}

	return wrapper
}

// rsaDecorator runs a test with static TLS and RSA based keys
func rsaDecorator(test TestFunc) TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeRSA)
}

// ellipticP224Decorator runs a test with static TLS and EC P224 based keys
func ellipticP224Decorator(test TestFunc) TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeEllipticP224)
}

// ellipticP256Decorator runs a test with static TLS and EC P256 based keys
func ellipticP256Decorator(test TestFunc) TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeEllipticP256)
}

// ellipticP384Decorator runs a test with static TLS and EC P384 based keys
func ellipticP384Decorator(test TestFunc) TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeEllipticP384)
}

// ellipticP521Decorator runs a test with static TLS and EC P521 based keys
func ellipticP521Decorator(test TestFunc) TestFunc {
	return tlsDecorator(test, e2eutil.KeyTypeEllipticP521)
}
