package setupkubernetes

import (
	"context"
	"errors"
	"fmt"

	installutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils"
)

var (
	ErrNotImplemented         = errors.New("not implemented")
	ErrUnknownEnvironmentType = errors.New("unknown environment type")
	ErrUnknownPlatformType    = errors.New("unknown platform type")
	ErrUnknownProviderType    = errors.New("unknown provider type")
)

type CreateClusterUtil interface {
	CreateCluster(ctx *context.Context) error
	ValidateParams(ctx *context.Context) error
}

func NewCreateClusterUtil(p *KubernetesSetupConfig) (CreateClusterUtil, error) {
	switch p.Platform {
	case installutils.Kubernetes:
		switch p.Environment {
		case Kind:
			return &CreateKindCluster{
				ClusterName:              p.ClusterName,
				NumControlPlane:          p.NumControlPlane,
				NumWorkers:               p.NumWorkers,
				ConfigDirectory:          "./tmp", // TODO : Move to results directory
				OperatorImage:            p.OperatorImage,
				AdmissionControllerImage: p.AdmissionControllerImage,
			}, nil

		case Cloud:
			switch p.Provider {
			case AWS:
				return &CreateEKSCluster{
					ClusterName:       p.ClusterName,
					Region:            p.EKSRegion,
					KubernetesVersion: p.KubernetesVersion,
					InstanceType:      p.InstanceType,
					NumNodeGroups:     p.NumNodeGroups,
					MinSize:           p.MinSize,
					MaxSize:           p.MaxSize,
					DesiredSize:       p.DesiredSize,
					DiskSize:          p.DiskSize,
					AMI:               p.AMI,
					KubeConfigPath:    p.KubeConfigPath,
				}, nil
			case Azure:
				return &CreateAKSCluster{
					ClusterName:       p.ClusterName,
					Region:            p.AKSRegion,
					KubernetesVersion: p.KubernetesVersion,
					DiskSize:          int32(p.DiskSize),
					KubeConfigPath:    p.KubeConfigPath,
					NumNodePools:      p.NumNodePools,
					OSSKU:             p.OSSKU,
					OSType:            p.OSType,
					VMSize:            p.VMSize,
					Count:             int32(p.Count),
				}, nil
			case GoogleCloud:
				return nil, ErrNotImplemented
			default:
				return nil, fmt.Errorf("unknown provider type %s: %w", p.Provider, ErrUnknownProviderType)
			}

		default:
			return nil, fmt.Errorf("unknown environment type %s: %w", p.Environment, ErrUnknownEnvironmentType)
		}

	case installutils.Openshift:
		return nil, ErrNotImplemented

	default:
		return nil, fmt.Errorf("unknown platform type %s: %w", p.Platform, ErrUnknownPlatformType)
	}
}
