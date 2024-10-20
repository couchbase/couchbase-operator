package destroykubernetes

import (
	"errors"
	"fmt"

	"context"

	caoinstallutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils/cao_install_utils"
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
	switch p.Platform {
	case caoinstallutils.Kubernetes:
		switch p.Environment {
		case Kind:
			return &DeleteKindCluster{
				ClusterName: p.ClusterName,
			}, nil

		case Cloud:
			switch p.Provider {
			case AWS:
				return &DeleteEKSCluster{
					ClusterName: p.ClusterName,
					Region:      p.EKSRegion,
				}, nil
			case Azure:
				return &DeleteAKSCluster{
					ClusterName: p.ClusterName,
					Region:      p.AKSRegion,
				}, nil
			case GoogleCloud:
				return &DeleteGKECluster{
					ClusterName: p.ClusterName,
					Region:      p.GKERegion,
				}, nil
			default:
				return nil, fmt.Errorf("unknown provider type %s: %w", p.Provider, ErrUnknownProviderType)
			}

		default:
			return nil, fmt.Errorf("unknown environment type %s: %w", p.Environment, ErrUnknownEnvironmentType)
		}

	case caoinstallutils.Openshift:
		return nil, ErrNotImplemented

	default:
		return nil, fmt.Errorf("unknown platform type %s: %w", p.Platform, ErrUnknownPlatformType)
	}
}
