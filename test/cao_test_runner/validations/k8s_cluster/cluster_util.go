package k8sclustervalidator

import (
	"errors"
	"fmt"

	"context"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/sirupsen/logrus"
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
		case managedk8sservices.Kubernetes:
			switch cluster.GetServiceProvider().GetEnvironment() {
			case managedk8sservices.Kind:
				return &ValidateKindCluster{ClusterName: v.ClusterName, KindConfig: v.KindConfig}, nil
			case managedk8sservices.Cloud:
				switch cluster.GetServiceProvider().GetProvider() {
				case managedk8sservices.AWS:
					return &ValidateEKSCluster{ClusterName: v.ClusterName, EKSConfig: v.EKSConfig}, nil
				case managedk8sservices.Azure:
					return &ValidateAKSCluster{ClusterName: v.ClusterName, AKSConfig: v.AKSConfig}, nil
				case managedk8sservices.GCP:
					return &ValidateGKECluster{ClusterName: v.ClusterName, GKEConfig: v.GKEConfig}, nil
				default:
					return nil, fmt.Errorf("new validate cluster util: %w", ErrNotImplemented)
				}
			default:
				return nil, fmt.Errorf("new validate cluster util: %w", ErrNotImplemented)
			}
		case managedk8sservices.Openshift:
			return nil, fmt.Errorf("new validate cluster util: %w", ErrNotImplemented)
		default:
			return nil, fmt.Errorf("new validate cluster util: %w", ErrNotImplemented)
		}
	} else {
		logrus.Debugf("KubernetesClusterValidator = %+v\n", v)

		if v.KindConfig != nil {
			return &ValidateKindCluster{ClusterName: v.ClusterName, KindConfig: v.KindConfig}, nil
		}
		if v.EKSConfig != nil {
			return &ValidateEKSCluster{ClusterName: v.ClusterName, EKSConfig: v.EKSConfig}, nil
		}
		if v.AKSConfig != nil {
			return &ValidateAKSCluster{ClusterName: v.ClusterName, AKSConfig: v.AKSConfig}, nil
		}
		if v.GKEConfig != nil {
			return &ValidateGKECluster{ClusterName: v.ClusterName, GKEConfig: v.GKEConfig}, nil
		}
	}
	return nil, fmt.Errorf("new validate cluster util: %w", ErrInvalidConfig)
}
