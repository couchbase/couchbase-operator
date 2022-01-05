package e2eutil

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
)

func CreateAWSSession(accessKey string, secretID string, region string, endpoint string, cert []byte) *session.Session {
	token := ""
	config := &aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKey, secretID, token),
	}

	if endpoint != "" {
		config.Endpoint = &endpoint
		config.S3ForcePathStyle = aws.Bool(true)
	}

	if cert != nil {
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(cert)

		t := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		}
		client := http.Client{Transport: t, Timeout: 15 * time.Second}
		config.HTTPClient = &client
	}

	return session.Must(session.NewSession(config))
}
