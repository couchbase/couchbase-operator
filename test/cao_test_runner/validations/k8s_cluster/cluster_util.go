package k8sclustervalidator

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
	if err == nil {
		switch cluster.GetServiceProvider().GetPlatform() {
		case assets.Kubernetes:
			switch cluster.GetServiceProvider().GetEnvironment() {
			case assets.Kind:
				return &ValidateKindCluster{ClusterName: v.ClusterName, KindConfig: v.KindConfig}, nil
			case assets.Cloud:
				switch cluster.GetServiceProvider().GetProvider() {
				case assets.AWS:
					return nil, ErrNotImplemented
				case assets.Azure:
					return nil, ErrNotImplemented
				case assets.GCP:
					return nil, ErrNotImplemented
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
	} else {
		if !checkKindConfigNil(v.KindConfig) {
			return &ValidateKindCluster{ClusterName: v.ClusterName, KindConfig: v.KindConfig}, nil
		}
	}
	return nil, ErrInvalidConfig
}
