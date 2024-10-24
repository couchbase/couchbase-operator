package chaosbaremetal

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	baremetal "github.com/couchbase/couchbase-operator/test/cao_test_runner/bare_metal_sdks"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
)

// ActionName defines the name for the various chaos actions supported.
type ActionName string

const (
	// RestartNodes chaos action performs a restart / reboot of the underlying vm of the CB node.
	RestartNodes ActionName = "RestartNodes"

	// CBServiceChaos performs a service / process related chaos action for a CB node.
	CBServiceChaos ActionName = "CBServiceChaos"
)

// CBNodeChaosConfig contains the chaos config for one CB node.
/*
 * ChaosConfig contains the chaos action name, managed svc name and the slice ChaosList.
 * ChaosList includes the information for chaos like CBBareMetalFilter, the triggers etc.
 * Now, based on the number of CB nodes filtered using CBBareMetalFilter, ChaosList has that many Trigger, CBServiceChaos etc.
 * Finally the chaos config for each CB node is stored in CBNodeChaosConfig.
 */
type CBNodeChaosConfig struct {
	ChaosAction     ActionName
	CloudProvider   baremetal.CloudProvider
	CloudRegion     string
	ChaosIterations int64
	CBHostname      string
	TriggerConfig   triggers.TriggerConfig
	// CBServiceChaos     chaos.CBService
	CBUpgradeVersion    string
	CBNodesAfterScaling int
}

// BareMetalChaosActions contains all the methods for executing chaos actions on bare metal CB cluster.
type BareMetalChaosActions interface {
	// RestartNodes restarts or reboots the k8s node.
	RestartNodes(context *context.Context, chaosConfig *CBNodeChaosConfig) error

	// CBServiceChaos performs chaos actions on CB services.
	CBServiceChaos(context *context.Context, chaosConfig *CBNodeChaosConfig) error
}

// NewBareMetalChaosAction initializes BareMetalChaosActions for the provided cloud.
func NewBareMetalChaosAction(context *context.Context, cloudProvider baremetal.CloudProvider, region string) (BareMetalChaosActions, error) {
	switch cloudProvider {
	case baremetal.AWS:
		{
			return NewAWSChaos(context, region)
		}
	default:
		{
			return nil, fmt.Errorf("new bare metal chaos action: %w", baremetal.ErrCloudProviderNotFound)
		}
	}
}

// ExecuteChaosAction executes the required chaos action for the ActionName.
func ExecuteChaosAction(context *context.Context, chaosConfig *CBNodeChaosConfig) error {
	bareMetalChaos, err := NewBareMetalChaosAction(context, chaosConfig.CloudProvider, chaosConfig.CloudRegion)
	if err != nil {
		return fmt.Errorf("execute bare metal chaos action %s: %w", chaosConfig.ChaosAction, err)
	}

	for range chaosConfig.ChaosIterations {
		switch chaosConfig.ChaosAction {
		case RestartNodes:
			{
				err := bareMetalChaos.RestartNodes(context, chaosConfig)
				if err != nil {
					return fmt.Errorf("execute bare metal chaos action `%s`: %w", chaosConfig.ChaosAction, err)
				}

				return nil
			}
		case CBServiceChaos:
			{
				err := bareMetalChaos.CBServiceChaos(context, chaosConfig)
				if err != nil {
					return fmt.Errorf("execute bare metal chaos action %s: %w", chaosConfig.ChaosAction, err)
				}

				return nil
			}
		}
	}

	return fmt.Errorf("execute bare metal chaos action %s: %w", chaosConfig.ChaosAction, ErrChaosActionNotFound)
}

// populateCBNodeChaos populates the CBNodeChaosConfig using the ChaosConfig and ChaosList.
func populateCBNodeChaos(chaosConfig *BareMetalChaosConfig, chaosAction *ChaosList, cbHostnames []string) []*CBNodeChaosConfig {
	var cbNodeChaosConfigs []*CBNodeChaosConfig

	for i, cbHostName := range cbHostnames {
		cbNodeChaos := &CBNodeChaosConfig{
			ChaosAction:     chaosAction.ChaosActions[i],
			CloudProvider:   chaosConfig.CloudProvider,
			CloudRegion:     chaosConfig.CloudRegion,
			ChaosIterations: chaosAction.ChaosIterations,
			CBHostname:      cbHostName,
			TriggerConfig:   chaosAction.Trigger[i],
			// CBServiceChaos:     chaosAction.CBServiceChaos[i],
			CBUpgradeVersion:    chaosAction.CBUpgradeVersion,
			CBNodesAfterScaling: chaosAction.CBNodesAfterScaling,
		}

		cbNodeChaosConfigs = append(cbNodeChaosConfigs, cbNodeChaos)
	}

	return cbNodeChaosConfigs
}

// populateCBInfoUsingChaosConfig helps to populate triggers.TriggerConfig CBInfo using CBNodeChaosConfig.
func populateCBInfoUsingChaosConfig(tc *triggers.TriggerConfig, cbNodeChaosConfig *CBNodeChaosConfig) {
	triggers.SetCBPodName(tc, cbNodeChaosConfig.CBHostname)
	triggers.SetCBPodVersion(tc, cbNodeChaosConfig.CBUpgradeVersion)
	triggers.SetCBPodsAfterScaling(tc, cbNodeChaosConfig.CBNodesAfterScaling)
}
