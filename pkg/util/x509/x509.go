// x509 implements basic x509 validity checking functions
package x509

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

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

// Verify checks the given chain and CA are valid to be installed
func Verify(caData, chainData, keyData []byte, clusterName, namespace string) (errors []error) {
	// Decode CA certificate
	caPem := DecodePEM(caData)
	switch {
	case len(caPem) < 1:
		errors = append(errors, fmt.Errorf("CA contains no PEM blocks"))
		return
	case len(caPem) > 1:
		errors = append(errors, fmt.Errorf("CA contains %d PEM blocks, expected 1", len(caPem)))
		return
	}
	ca, err := x509.ParseCertificate(caPem[0].Bytes)
	if err != nil {
		errors = append(errors, fmt.Errorf("CA failed to decode: %v", err))
		return
	}

	// Decode chain
	chainPem := DecodePEM(chainData)
	if len(chainPem) == 0 {
		errors = append(errors, fmt.Errorf("chain contains no PEM blocks"))
		return
	}
	var chain []*x509.Certificate
	for _, block := range chainPem {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			errors = append(errors, fmt.Errorf("chain failed to decode: %v", err))
			return
		}
		chain = append(chain, cert)
	}
	serverCert := chain[0]
	intermediates := chain[1:]
	verifyOptions := x509.VerifyOptions{
		DNSName:       fmt.Sprintf("test.%s.%s.svc", clusterName, namespace),
		Intermediates: x509.NewCertPool(),
		Roots:         x509.NewCertPool(),
	}
	verifyOptions.Roots.AddCert(ca)
	for _, cert := range intermediates {
		verifyOptions.Intermediates.AddCert(cert)
	}
	if _, err := serverCert.Verify(verifyOptions); err != nil {
		errors = append(errors, fmt.Errorf("certificate cannot be verified: %v", err))
	}

	// Decode key
	keyPem := DecodePEM(keyData)
	switch {
	case len(keyPem) < 1:
		errors = append(errors, fmt.Errorf("private key contains no PEM blocks"))
		return
	case len(keyPem) > 1:
		errors = append(errors, fmt.Errorf("private key contains %d PEM blocks, expected 1", len(keyPem)))
		return
	}
	if _, err := x509.ParsePKCS1PrivateKey(keyPem[0].Bytes); err != nil {
		// This is an annoying bug with NS server not supporting PKCS8 *sigh*
		errors = append(errors, fmt.Errorf("private key not formatted as PKCS1"))
	}

	return
}
