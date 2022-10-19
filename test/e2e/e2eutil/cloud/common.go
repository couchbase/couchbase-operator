package cloud

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/types"
	corev1 "k8s.io/api/core/v1"
)

type ProviderType int

const (
	NoCloudProvider = iota
	CloudProviderAWS
	CloudProviderAzure
	CloudProviderGCP
)

type Provider interface {
	CreateBucket(string) error
	GetBucket(string) (bool, error)
	CreateSecret(*types.Cluster) (*corev1.Secret, error)
	DeleteBucket(string) error
	SetupEnvironment(*testing.T, *types.Cluster) (*corev1.Secret, string, func())
	PrefixBucket(string) string
}

func NewProvider(providerType ProviderType, creds ...string) (Provider, error) {
	switch providerType {
	case CloudProviderAWS:
		return NewAWSProvider(creds...)
	case CloudProviderAzure:
		return NewAzureProvider(creds...)
	case CloudProviderGCP:
		return NewGCPProvider(creds...)
	default:
		return NewEmptyProvider()
	}
}
