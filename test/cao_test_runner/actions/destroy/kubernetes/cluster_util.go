package destroykubernetes

import (
	"errors"
	"fmt"

	"context"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
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
		case managedk8sservices.Kind:
			return &DeleteKindCluster{
				ClusterName: p.ClusterName,
			}, nil

		case managedk8sservices.Cloud:
			switch p.Provider {
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
