package destroykubernetes

import (
	"errors"
	"fmt"

	"context"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
)

var (
	ErrNotImplemented         = errors.New("not implemented")
	ErrUnknownEnvironmentType = errors.New("unknown environment type")
	ErrUnknownPlatformType    = errors.New("unknown platform type")
	ErrUnknownProviderType    = errors.New("unknown provider type")
)

type DeleteClusterUtil interface {
	DeleteCluster(ctx context.Context) error
	ValidateParams(ctx context.Context) error
}

func NewDeleteClusterUtil(p *KubernetesDestroyConfig) (DeleteClusterUtil, error) {
	switch p.ms.GetPlatform() {
	case managedk8sservices.Kubernetes:
		switch p.ms.GetEnvironment() {
		case managedk8sservices.Kind:
			return &DeleteKindCluster{
				ClusterName: p.ClusterName,
			}, nil

		case managedk8sservices.Cloud:
			switch p.ms.GetProvider() {
			case managedk8sservices.AWS:
				return &DeleteEKSCluster{
					ClusterName:            p.ClusterName,
					Region:                 p.EKSRegion,
					ManagedServiceProvider: p.ms,
				}, nil
			case managedk8sservices.Azure:
				return &DeleteAKSCluster{
					ClusterName:            p.ClusterName,
					Region:                 p.AKSRegion,
					ManagedServiceProvider: p.ms,
				}, nil
			case managedk8sservices.GCP:
				return &DeleteGKECluster{
					ClusterName:            p.ClusterName,
					Region:                 p.GKERegion,
					ManagedServiceProvider: p.ms,
				}, nil
			default:
				return nil, fmt.Errorf("unknown provider type %s: %w", p.ms.GetProvider(), ErrUnknownProviderType)
			}

		default:
			return nil, fmt.Errorf("unknown environment type %s: %w", p.ms.GetEnvironment(), ErrUnknownEnvironmentType)
		}

	case managedk8sservices.Openshift:
		return nil, ErrNotImplemented

	default:
		return nil, fmt.Errorf("unknown platform type %s: %w", p.ms.GetPlatform(), ErrUnknownPlatformType)
	}
}
