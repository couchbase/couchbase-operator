package destroyk8sclustervalidator

import (
	"errors"

	"context"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
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
	case managedk8sservices.Kubernetes:
		switch cluster.GetServiceProvider().GetEnvironment() {
		case managedk8sservices.Kind:
			return &ValidateKindCluster{ClusterName: v.ClusterName}, nil
		case managedk8sservices.Cloud:
			switch cluster.GetServiceProvider().GetProvider() {
			case managedk8sservices.AWS:
				return &ValidateEKSCluster{ClusterName: v.ClusterName}, nil
			case managedk8sservices.Azure:
				return &ValidateAKSCluster{ClusterName: v.ClusterName}, nil
			case managedk8sservices.GCP:
				return &ValidateGKECluster{ClusterName: v.ClusterName}, nil
			default:
				return nil, ErrNotImplemented
			}
		default:
			return nil, ErrNotImplemented
		}
	case managedk8sservices.Openshift:
		return nil, ErrNotImplemented
	default:
		return nil, ErrNotImplemented
	}
}
