// x509 implements basic x509 validity checking functions
package x509

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

// rootDomain is a lazy cache.
var rootDomain string

var rootDomainLock sync.Mutex

// getRootDomain determines the root domain for the cluster, typically cluster.local.
// however as it can be changed, people will change it, so we need to support this.
func getRootDomain() string {
	rootDomainLock.Lock()
	defer rootDomainLock.Unlock()

	// If the cache is populated, use that, this cannot change.
	if rootDomain != "" {
		return rootDomain
	}

	// Set a sane default in the event of an error.
	rootDomain = "cluster.local"

	// Parse /etc/resolv.conf as this will be filled in by kubelet for every
	// pod and features the cluster root domain.  The DNS server doesn't respond
	// to PTR lookups, so we just hack it.
	resolv, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return rootDomain
	}

	// The line looks something like:
	//
	//   search default.svc.cluster.local svc.cluster.local cluster.local Home
	//
	// As something like svc.svc.cluster.local can occur, and give the wrong result
	// then we need to pick the last match, hoping that precedence yields the right
	// result.  Even more unlikely is something like svc.svc.com, which is going to
	// break still...
	for _, line := range strings.Split(string(resolv), "\n") {
		if !strings.HasPrefix(line, "search") {
			continue
		}

		domains := regexp.MustCompile(`\s+`).Split(line, -1)

		for _, domain := range domains[1:] {
			matches := regexp.MustCompile(`svc\.(.*)`).FindStringSubmatch(domain)
			if len(matches) == 2 {
				rootDomain = matches[1]
			}
		}
	}

	return rootDomain
}

// MandatorySANs returns the list of SANs that all server certificates must implement.
func MandatorySANs(clusterName, namespace string) []string {
	root := getRootDomain()

	return []string{
		fmt.Sprintf("*.%s", clusterName),
		fmt.Sprintf("*.%s.%s", clusterName, namespace),
		// Used by the Operator for node connections.
		fmt.Sprintf("*.%s.%s.svc", clusterName, namespace),
		// Used for GCCCP SRV connectons.
		fmt.Sprintf("*.%s.%s.svc.%s", clusterName, namespace, root),
		// Used by clients for connection in the same namespace.
		fmt.Sprintf("%s-srv", clusterName),
		// Used by clients for connection in a different/remote namespace.
		fmt.Sprintf("%s-srv.%s", clusterName, namespace),
		fmt.Sprintf("%s-srv.%s.svc", clusterName, namespace),
		// Used for CCCP SRV connectons.
		fmt.Sprintf("*.%s-srv.%s.svc.%s", clusterName, namespace, root),
		// Used for prometheus side-car and UI access.
		"localhost",
	}
}

// DecodePEM takes a raw blob of data and tries to decode PEM encoded data.
func DecodePEM(data []byte) (blocks []*pem.Block) {
	for len(data) != 0 {
		var block *pem.Block

		if block, data = pem.Decode(data); block == nil {
			break
		}

		blocks = append(blocks, block)
	}

	return
}

// Verify checks the given chain and CA are valid to be installed.
func Verify(rootCAs [][]byte, chainData, keyData []byte, extKeyUsage x509.ExtKeyUsage, subjectAltNames []string, legacy bool, validateSan bool) ([][]*x509.Certificate, error) {
	roots := x509.NewCertPool()

	for _, caData := range rootCAs {
		// Decode CA certificate
		caPem := DecodePEM(caData)

		if len(caPem) < 1 {
			return nil, fmt.Errorf("%w: CA contains no PEM blocks", errors.NewStackTracedError(errors.ErrCertificateInvalid))
		}

		// Parse each Root CA
		for _, caBlock := range caPem {
			ca, err := x509.ParseCertificate(caBlock.Bytes)
			if err != nil {
				return nil, fmt.Errorf("CA failed to decode: %w", errors.NewStackTracedError(err))
			}

			roots.AddCert(ca)
		}
	}

	// Decode chain
	chainPem := DecodePEM(chainData)
	if len(chainPem) == 0 {
		return nil, fmt.Errorf("%w: chain contains no PEM blocks", errors.NewStackTracedError(errors.ErrCertificateInvalid))
	}

	var chain []*x509.Certificate

	for _, block := range chainPem {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("chain failed to decode: %w", errors.NewStackTracedError(err))
		}

		chain = append(chain, cert)
	}

	cert := chain[0]
	intermediates := chain[1:]
	verifyOptions := x509.VerifyOptions{
		Intermediates: x509.NewCertPool(),
		Roots:         roots,
		KeyUsages: []x509.ExtKeyUsage{
			extKeyUsage,
		},
	}

	for _, cert := range intermediates {
		verifyOptions.Intermediates.AddCert(cert)
	}

	// Verify the certificate validates on its own (valid for both server and client)
	chains, err := cert.Verify(verifyOptions)
	if err != nil {
		return nil, fmt.Errorf("certificate cannot be verified: %w", errors.NewStackTracedError(err))
	}

	// Verify the certificate validates for each supplied zone (valid for server only)
	if validateSan {
		for _, san := range subjectAltNames {
			hostname := san
			if san[0] == '*' {
				hostname = fmt.Sprintf("host%s", san[1:])
			}

			verifyOptions.DNSName = hostname
			if _, err := cert.Verify(verifyOptions); err != nil {
				return nil, fmt.Errorf("certificate cannot be verified for zone: %w", errors.NewStackTracedError(err))
			}
		}
	}

	// Decode key
	keyPem := DecodePEM(keyData)

	switch {
	case len(keyPem) < 1:
		return nil, fmt.Errorf("%w: private key contains no PEM blocks", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid))
	case len(keyPem) > 1:
		return nil, fmt.Errorf("%w: private key contains %d PEM blocks, expected 1", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid), len(keyPem))
	}

	if extKeyUsage == x509.ExtKeyUsageServerAuth {
		if legacy {
			if _, err := x509.ParsePKCS1PrivateKey(keyPem[0].Bytes); err != nil {
				// This is an annoying bug with NS server not supporting PKCS8 *sigh*
				return nil, fmt.Errorf("%w: private key not formatted as PKCS1", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid))
			}
		}
	}

	return chains, nil
}
