/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/youmark/pkcs8"
	v1 "k8s.io/api/core/v1"
	"software.sslmate.com/src/go-pkcs12"
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
// If includeBareHostnames is false, bare hostname entries (like "{clusterName}-srv") are excluded.
func MandatorySANs(clusterName, namespace string, includeBareHostnames bool) []string {
	root := getRootDomain()

	sans := []string{
		fmt.Sprintf("*.%s", clusterName),
		fmt.Sprintf("*.%s.%s", clusterName, namespace),
		// Used by the Operator for node connections.
		fmt.Sprintf("*.%s.%s.svc", clusterName, namespace),
		// Used for GCCCP SRV connectons.
		fmt.Sprintf("*.%s.%s.svc.%s", clusterName, namespace, root),
		// Used by clients for connection in a different/remote namespace.
		fmt.Sprintf("%s-srv.%s", clusterName, namespace),
		fmt.Sprintf("%s-srv.%s.svc", clusterName, namespace),
		// Used for CCCP SRV connectons.
		fmt.Sprintf("*.%s-srv.%s.svc.%s", clusterName, namespace, root),
		// Used for prometheus side-car and UI access.
		"localhost",
	}

	// Add bare hostname entries only if requested
	if includeBareHostnames {
		sans = append(sans, fmt.Sprintf("%s-srv", clusterName))
	}

	return sans
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

// DecodePKCS12file will return the chain and key contained in a PKCS12 file.
func DecodePKCS12file(keystore []byte, passphrase string, c *v2.CouchbaseCluster) ([]byte, []byte, [][]byte, error) {
	ok, err := c.IsAtLeastVersion("7.6.0")
	if err != nil {
		return nil, nil, nil, errors.NewStackTracedError(err)
	}

	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: pkcs12 support requires server version 7.6.0", errors.NewStackTracedError(errors.ErrPKCS12NotSupported))
	}

	// Decode PKCS12 file and extract the cert and key.
	pkey, cert, chain, err := pkcs12.DecodeChain(keystore, passphrase)
	if err != nil {
		return nil, nil, nil, err
	}

	var chainBytes [][]byte

	for _, item := range chain {
		chainBytes = append(chainBytes, item.Raw)
	}

	// Convert the cert to pem format.
	block := pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}

	certPem := pem.EncodeToMemory(&block)

	// Make the key PKCS8 (it already is but we don't know that). Then convert it to pem format.
	pkcs8Pkey, err := x509.MarshalPKCS8PrivateKey(pkey)
	if err != nil {
		return nil, nil, nil, err
	}

	pkeyBlock := pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Pkey,
	}

	key := pem.EncodeToMemory(&pkeyBlock)

	return certPem, key, chainBytes, nil
}

// ExtractPKCS12Info extracts the PKCS12 file and passphrase from the secret.
func ExtractPKCS12Info(secret *v1.Secret) ([]byte, string, bool, error) {
	keystore, pkcs12 := secret.Data[constants.PKCS12FileName]
	if pkcs12 {
		bytePassphrase, ok := secret.Data[constants.TLSPassword]
		if !ok {
			return nil, "", false, fmt.Errorf("%w: PKCS12 requires a passphrase", errors.ErrPKCS12NotSupported)
		}

		b64Passphrase := string(bytePassphrase)

		if b64Passphrase == "" {
			return nil, "", false, fmt.Errorf("%w: PKCS12 requires a passphrase", errors.ErrPKCS12NotSupported)
		}

		return keystore, b64Passphrase, true, nil
	}

	return nil, "", false, nil
}

// ValidateKMIPKeyAndPassphrase validates the KMIP key and passphrase.
func ValidateKMIPKeyAndPassphrase(keyData, passphrase []byte) error {
	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return fmt.Errorf("%w: private key contains no PEM blocks", errors.NewStackTracedError(errors.ErrPrivateKeyInvalid))
	}

	_, err := pkcs8.ParsePKCS8PrivateKey(keyBlock.Bytes, passphrase)
	if err != nil {
		return err
	}

	return nil
}
