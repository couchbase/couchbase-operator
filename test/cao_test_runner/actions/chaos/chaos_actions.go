package chaos

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	managedsvcs "github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
	"github.com/sirupsen/logrus"
)

// ActionName defines the name for the various chaos actions supported.
type ActionName string

const (
	// RestartNodes chaos action performs a restart / reboot of the k8s node.
	RestartNodes ActionName = "RestartNodes"

	// DeleteNodes chaos action deletes / removes a k8s node from k8s cluster.
	DeleteNodes ActionName = "DeleteNodes"

	// DeletePods deletes the k8s pod using kubectl.
	DeletePods ActionName = "DeletePods"

	// CBServiceChaos performs a service / process related chaos action for a CB pod.
	CBServiceChaos ActionName = "CBServiceChaos"
)

// CBPodChaosConfig contains the chaos config for one CB pod.
/*
 * ChaosConfig contains the chaos action name, managed svc name and the slice ChaosList.
 * ChaosList includes the information for chaos like CBPodFilter, the triggers etc.
 * Now, based on the number of pods filtered using CBPodFilter, ChaosList has that many Trigger, CBServiceChaos etc.
 * Finally the chaos config for each CB pod is stored in CBPodChaosConfig.
 */
type CBPodChaosConfig struct {
	ChaosAction        ActionName
	ManagedSvcName     managedsvcs.ManagedServiceProvider
	ChaosIterations    int64
	CBPod              string
	TriggerConfig      triggers.TriggerConfig
	CBServiceChaos     CBService
	CBUpgradeVersion   string
	CBPodsAfterScaling int
	ClusterName        string // TODO to be removed. K8S Cluster Name to be taken from context
}

// ChaosActionsInterface contains all the methods for executing chaos actions.
type ChaosActionsInterface interface {
	// RestartNodes restarts or reboots the k8s node.
	RestartNodes(context *context.Context, chaosConfig *CBPodChaosConfig) error

	// DeleteNodes deletes or removes the k8s node from the k8s cluster.
	DeleteNodes(context *context.Context, chaosConfig *CBPodChaosConfig) error

	// DeletePods deletes the pods.
	DeletePods(context *context.Context, chaosConfig *CBPodChaosConfig) error

	// CBServiceChaos performs chaos actions on CB services.
	CBServiceChaos(context *context.Context, chaosConfig *CBPodChaosConfig) error
}

// NewChaosAction initializes ChaosActionsInterface for the provided managed service.
func NewChaosAction(context *context.Context, managedService managedsvcs.ManagedServiceProvider, clusterName string) (ChaosActionsInterface, error) {
	switch managedService {
	case managedsvcs.EKSManagedService:
		{
			return NewEKSChaos(context, clusterName)
		}
	case managedsvcs.KindManagedService:
		{
			return NewKindChaos()
		}
	default:
		{
			return nil, fmt.Errorf("new chaos action: %w", managedsvcs.ErrManagedServiceNotFound)
		}
	}
}

// ExecuteChaosAction executes the required chaos action for the ActionName.
func ExecuteChaosAction(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	// TODO get the cluster name and managed service from the context
	chaos, err := NewChaosAction(context, chaosConfig.ManagedSvcName, chaosConfig.ClusterName)
	if err != nil {
		return fmt.Errorf("execute chaos action %s: %w", chaosConfig.ChaosAction, err)
	}

	for range chaosConfig.ChaosIterations {
		switch chaosConfig.ChaosAction {
		case RestartNodes:
			{
				err := chaos.RestartNodes(context, chaosConfig)
				if err != nil {
					return fmt.Errorf("execute chaos action `%s`: %w", chaosConfig.ChaosAction, err)
				}

				return nil
			}
		case DeletePods:
			{
				logrus.Infof("Starting to delete the pod: %s", chaosConfig.CBPod)

				err := chaos.DeletePods(context, chaosConfig)
				if err != nil {
					return fmt.Errorf("execute chaos action %s: %w", chaosConfig.ChaosAction, err)
				}

				logrus.Infof("Successfully deleted the pod: %s", chaosConfig.CBPod)

				return nil
			}
		case DeleteNodes:
			{
				err := chaos.DeleteNodes(context, chaosConfig)
				if err != nil {
					return fmt.Errorf("execute chaos action %s: %w", chaosConfig.ChaosAction, err)
				}

				return nil
			}
		case CBServiceChaos:
			{
				err := chaos.CBServiceChaos(context, chaosConfig)
				if err != nil {
					return fmt.Errorf("execute chaos action %s: %w", chaosConfig.ChaosAction, err)
				}

				return nil
			}
		}
	}

	return fmt.Errorf("execute chaos action %s: %w", chaosConfig.ChaosAction, ErrChaosActionNotFound)
}

// populateCBPodChaos populates the CBPodChaosConfig using the ChaosConfig and ChaosList.
func populateCBPodChaos(chaosConfig *ChaosConfig, chaosAction *ChaosList, cbPodNames []string) []*CBPodChaosConfig {
	var cbPodChaosConfigs []*CBPodChaosConfig

	for i, cbPodName := range cbPodNames {
		cbPodChaos := &CBPodChaosConfig{
			ChaosAction:        chaosAction.ChaosActions[i],
			ManagedSvcName:     chaosConfig.ManagedSvcName,
			ChaosIterations:    chaosAction.ChaosIterations,
			CBPod:              cbPodName,
			TriggerConfig:      chaosAction.Trigger[i],
			CBServiceChaos:     chaosAction.CBServiceChaos[i],
			ClusterName:        chaosConfig.ClusterName,
			CBUpgradeVersion:   chaosAction.CBUpgradeVersion,
			CBPodsAfterScaling: chaosAction.CBPodsAfterScaling,
		}

		cbPodChaosConfigs = append(cbPodChaosConfigs, cbPodChaos)
	}

	return cbPodChaosConfigs
}

// populateCBInfoUsingChaosConfig helps to populate triggers.TriggerConfig CBInfo using CBPodChaosConfig.
func populateCBInfoUsingChaosConfig(tc *triggers.TriggerConfig, cbPodChaosConfig *CBPodChaosConfig) {
	triggers.SetCBPodName(tc, cbPodChaosConfig.CBPod)
	triggers.SetCBPodVersion(tc, cbPodChaosConfig.CBUpgradeVersion)
	triggers.SetCBPodsAfterScaling(tc, cbPodChaosConfig.CBPodsAfterScaling)
}
