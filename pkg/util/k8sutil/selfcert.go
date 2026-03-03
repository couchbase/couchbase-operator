/*
Copyright 2023-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package k8sutil

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"time"

	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
)

type SelfSignedCerts struct {
	key  []byte
	cert []byte
	ca   *util_x509.CertificateAuthority
}

func GenerateSelfSignedCert(namespace string, certExpiryDuration time.Duration, subjectName string, dnsNames ...string) (*SelfSignedCerts, error) {
	// Generate Self-Signed TLS configuration for any subject.
	validFrom := time.Now()
	validTo := time.Now().Add(certExpiryDuration)

	// Create a Issuer CA.
	caCommonName := subjectName + " CA"

	ca, err := util_x509.NewCertificateAuthority(util_x509.KeyTypeRSA, caCommonName, validFrom, validTo, util_x509.CertTypeCA)
	if err != nil {
		return nil, err
	}

	req := util_x509.KeyPairRequest{
		KeyType:   util_x509.KeyTypeRSA,
		CertType:  util_x509.CertTypeServer,
		ValidFrom: validFrom,
		ValidTo:   validTo,
		Req: &x509.CertificateRequest{
			Subject: pkix.Name{
				CommonName: subjectName,
			},
			DNSNames: []string{
				subjectName + "." + namespace + ".svc",
			},
		},
	}

	if len(dnsNames) > 0 {
		req.Req.DNSNames = append(req.Req.DNSNames, dnsNames...)
	}

	key, cert, err := req.Generate(ca)
	if err != nil {
		return nil, err
	}

	return &SelfSignedCerts{key: key, cert: cert, ca: ca}, nil
}
