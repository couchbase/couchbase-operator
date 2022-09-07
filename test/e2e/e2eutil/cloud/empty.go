package cloud

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/types"
	corev1 "k8s.io/api/core/v1"
)

type EmptyProvider struct {
}

func NewEmptyProvider() (Provider, error) {
	return &EmptyProvider{}, nil
}

func (provider *EmptyProvider) CreateBucket(string) error {
	return nil
}

func (provider *EmptyProvider) GetBucket(string) (bool, error) {
	return false, nil
}

func (provider *EmptyProvider) DeleteBucket(string) error {
	return nil
}

func (provider *EmptyProvider) CreateSecret(*types.Cluster) (*corev1.Secret, error) {
	return nil, nil
}

func (provider *EmptyProvider) SetupEnvironment(*testing.T, *types.Cluster) (*corev1.Secret, string, func()) {
	return nil, "", func() {}
}

func (provider *EmptyProvider) PrefixBucket(bucketName string) string {
	return bucketName
}
