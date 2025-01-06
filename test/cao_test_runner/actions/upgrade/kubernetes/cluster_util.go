package upgradekubernetes

import (
	"context"
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	caoinstallutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils/cao_install_utils"
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
	switch p.Platform {
	case caoinstallutils.Kubernetes:
		switch p.Environment {
		case managedk8sservices.Kind:
			return nil, ErrNotImplemented

		case managedk8sservices.Cloud:
			switch p.Provider {
			case managedk8sservices.AWS:
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
			case managedk8sservices.Azure:
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
			case managedk8sservices.GCP:
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
