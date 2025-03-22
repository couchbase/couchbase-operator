package setupkubernetes

import (
	"context"
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
)

var (
	ErrNotImplemented         = errors.New("not implemented")
	ErrUnknownEnvironmentType = errors.New("unknown environment type")
	ErrUnknownPlatformType    = errors.New("unknown platform type")
	ErrUnknownProviderType    = errors.New("unknown provider type")
)

type CreateClusterUtil interface {
	CreateCluster(ctx context.Context) error
	ValidateParams(ctx context.Context) error
}

func NewCreateClusterUtil(p *KubernetesSetupConfig) (CreateClusterUtil, error) {
	switch p.Platform {
	case managedk8sservices.Kubernetes:
		switch p.Environment {
		case managedk8sservices.Kind:
			return &CreateKindCluster{
				ClusterName:              p.ClusterName,
				NumControlPlane:          p.NumControlPlane,
				NumWorkers:               p.NumWorkers,
				ConfigDirectory:          p.resultsDirectory,
				OperatorImage:            p.OperatorImage,
				AdmissionControllerImage: p.AdmissionControllerImage,
				LoadDockerImageToKind:    p.LoadDockerImageToKind,
			}, nil

		case managedk8sservices.Cloud:
			switch p.Provider {
			case managedk8sservices.AWS:
				return &CreateEKSCluster{
					ClusterName:            p.ClusterName,
					Region:                 p.EKSRegion,
					KubernetesVersion:      p.KubernetesVersion,
					EKSNodegroups:          p.EKSNodegroups,
					KubeConfigPath:         p.kubeconfigPath,
					ManagedServiceProvider: p.ms,
				}, nil
			case managedk8sservices.Azure:
				return &CreateAKSCluster{
					ClusterName:            p.ClusterName,
					Region:                 p.AKSRegion,
					KubernetesVersion:      p.KubernetesVersion,
					DiskSize:               int32(p.DiskSize),
					KubeConfigPath:         p.kubeconfigPath,
					NumNodePools:           p.NumNodePools,
					OSSKU:                  p.OSSKU,
					OSType:                 p.OSType,
					VMSize:                 p.VMSize,
					Count:                  int32(p.Count),
					ManagedServiceProvider: p.ms,
				}, nil
			case managedk8sservices.GCP:
				return &CreateGKECluster{
					ClusterName:            p.ClusterName,
					Region:                 p.GKERegion,
					KubernetesVersion:      p.KubernetesVersion,
					GKENodePools:           p.GKENodePools,
					ReleaseChannel:         p.ReleaseChannel,
					KubeConfigPath:         p.kubeconfigPath,
					ManagedServiceProvider: p.ms,
				}, nil
			default:
				return nil, fmt.Errorf("unknown provider type %s: %w", p.Provider, ErrUnknownProviderType)
			}
		default:
			return nil, fmt.Errorf("unknown environment type %s: %w", p.Environment, ErrUnknownEnvironmentType)
		}

	case managedk8sservices.Openshift:
		return nil, ErrNotImplemented

	default:
		return nil, fmt.Errorf("unknown platform type %s: %w", p.Platform, ErrUnknownPlatformType)
	}
}
