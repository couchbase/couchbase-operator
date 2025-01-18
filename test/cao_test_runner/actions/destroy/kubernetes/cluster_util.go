package destroykubernetes

import (
	"errors"
	"fmt"

	"context"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
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
	case assets.Kubernetes:
		switch p.ms.GetEnvironment() {
		case assets.Kind:
			return &DeleteKindCluster{
				ClusterName: p.ClusterName,
			}, nil

		case assets.Cloud:
			switch p.ms.GetProvider() {
			case assets.AWS:
				return &DeleteEKSCluster{
					ClusterName:            p.ClusterName,
					Region:                 p.EKSRegion,
					ManagedServiceProvider: p.ms,
				}, nil
			case assets.Azure:
				return &DeleteAKSCluster{
					ClusterName:            p.ClusterName,
					Region:                 p.AKSRegion,
					ManagedServiceProvider: p.ms,
				}, nil
			case assets.GCP:
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

	case assets.Openshift:
		return nil, ErrNotImplemented

	default:
		return nil, fmt.Errorf("unknown platform type %s: %w", p.ms.GetPlatform(), ErrUnknownPlatformType)
	}
}
