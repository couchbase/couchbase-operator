package destroyk8sclustervalidator

import (
	"errors"

	"context"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidConfig  = errors.New("invalid config")
)

type ValidateClusterUtil interface {
	ValidateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error
}

func NewValidateClusterUtil(v *KubernetesClusterValidator, testAssets assets.TestAssetGetterSetter) (ValidateClusterUtil, error) {
	cluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(v.ClusterName)
	if err != nil {
		return nil, ErrInvalidConfig
	}

	switch cluster.GetServiceProvider().GetPlatform() {
	case assets.Kubernetes:
		switch cluster.GetServiceProvider().GetEnvironment() {
		case assets.Kind:
			return &ValidateKindCluster{ClusterName: v.ClusterName}, nil
		case assets.Cloud:
			switch cluster.GetServiceProvider().GetProvider() {
			case assets.AWS:
				return &ValidateEKSCluster{ClusterName: v.ClusterName}, nil
			case assets.Azure:
				return &ValidateAKSCluster{ClusterName: v.ClusterName}, nil
			case assets.GCP:
				return &ValidateGKECluster{ClusterName: v.ClusterName}, nil
			default:
				return nil, ErrNotImplemented
			}
		default:
			return nil, ErrNotImplemented
		}
	case assets.Openshift:
		return nil, ErrNotImplemented
	default:
		return nil, ErrNotImplemented
	}
}
