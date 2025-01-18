package upgradekubernetes

import (
	"context"
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
)

var (
	ErrNotImplemented         = errors.New("not implemented")
	ErrUnknownEnvironmentType = errors.New("unknown environment type")
	ErrUnknownPlatformType    = errors.New("unknown platform type")
	ErrUnknownProviderType    = errors.New("unknown provider type")
)

type UpgradeClusterUtil interface {
	UpgradeCluster(ctx context.Context) error
	ValidateParams(ctx context.Context) error
}

func NewUpgradeClusterUtil(p *KubernetesUpgradeConfig) (UpgradeClusterUtil, error) {
	switch p.ms.GetPlatform() {
	case assets.Kubernetes:
		switch p.ms.GetEnvironment() {
		case assets.Kind:
			return nil, ErrNotImplemented

		case assets.Cloud:
			switch p.ms.GetProvider() {
			case assets.AWS:
				return &UpgradeEKSCluster{
					ClusterName:            p.ClusterName,
					Region:                 p.EKSRegion,
					KubernetesVersion:      p.KubernetesVersion,
					UpgradeClusterVersion:  p.UpgradeClusterVersion,
					WaitForClusterUpgrade:  p.WaitForClusterUpgrade,
					UpgradeNodeGroups:      p.UpgradeEKSNodeGroups,
					NodeGroupsToUpgrade:    p.EKSNodeGroupsToUpgrade,
					ManagedServiceProvider: p.ms,
				}, nil
			case assets.Azure:
				return &UpgradeAKSCluster{
					ClusterName:            p.ClusterName,
					Region:                 p.AKSRegion,
					KubernetesVersion:      p.KubernetesVersion,
					UpgradeClusterVersion:  p.UpgradeClusterVersion,
					WaitForClusterUpgrade:  p.WaitForClusterUpgrade,
					UpgradeNodePools:       p.UpgradeAKSNodePools,
					NodePoolsToUpgrade:     p.AKSNodePoolsToUpgrade,
					ManagedServiceProvider: p.ms,
				}, nil
			case assets.GCP:
				return &UpgradeGKECluster{
					ClusterName:            p.ClusterName,
					Region:                 p.GKERegion,
					KubernetesVersion:      p.KubernetesVersion,
					UpgradeClusterVersion:  p.UpgradeClusterVersion,
					WaitForClusterUpgrade:  p.WaitForClusterUpgrade,
					UpgradeMaster:          p.UpgradeGKEMaster,
					WaitForMasterUpgrade:   p.WaitForGKEMasterUpgrade,
					UpgradeNodePool:        p.UpgradeGKENodePool,
					NodePoolsToUpgrade:     p.GKENodePoolsToUpgrade,
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
