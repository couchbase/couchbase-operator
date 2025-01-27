package chaos

import (
	"fmt"

	managedsvcs "github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	managedsvc "github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
	"github.com/sirupsen/logrus"
)

// EKSChaos implements the ChaosActionsInterface for executing chaos actions on EKS.
type EKSChaos struct {
	EKSCred *managedsvc.ManagedServiceCredentials
	EKSSess *managedsvc.EKSSession
	EKSSvc  managedsvc.ManagedService
}

// NewEKSChaos returns an instance of EKSChaos to perform chaos actions on EKS cluster.
func NewEKSChaos(ctxt *context.Context, clusterName string,
	managedServiceProvider *managedsvcs.ManagedServiceProvider) (*EKSChaos, error) {
	eksCred, err := managedsvc.NewManagedServiceCredentials(
		[]*managedsvcs.ManagedServiceProvider{managedServiceProvider}, clusterName)
	if err != nil {
		return nil, fmt.Errorf("new eks chaos: %w", err)
	}

	eksSvc := managedsvc.NewManagedService(managedServiceProvider)

	eksSessStore := managedsvc.NewEKSSessionStore()

	eksSess, err := eksSessStore.GetSession(ctxt.Context(), eksCred)
	if err != nil {
		return nil, fmt.Errorf("new eks chaos: %w", err)
	}

	return &EKSChaos{
		EKSCred: eksCred,
		EKSSvc:  eksSvc,
		EKSSess: eksSess,
	}, nil
}

// RestartNodes reboots the ec2 instance (i.e. k8s node of EKS).
func (ec *EKSChaos) RestartNodes(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	k8sNodeName, err := pods.GetNodeNameForPod(chaosConfig.CBPod, "default")
	if err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	instanceIds, err := ec.EKSSvc.GetInstancesByK8sNodeName(context.Context(), ec.EKSCred, []string{k8sNodeName})
	if err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	logrus.Infof("Starting to restart eks node `%s` with ec2 instance id: %s", k8sNodeName, instanceIds[0])

	// Checking the triggers
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err = triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	if err = ec.EKSSess.RebootInstances(context.Context(), instanceIds); err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	logrus.Infof("Successfully restarted eks node `%s` with ec2 instance id: %s", k8sNodeName, instanceIds[0])

	return nil
}

// DeleteNodes terminates the ec2 instance (k8s node for EKS).
func (ec *EKSChaos) DeleteNodes(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	k8sNodeName, err := pods.GetNodeNameForPod(chaosConfig.CBPod, "default")
	if err != nil {
		return fmt.Errorf("terminate ec2 instance: %w", err)
	}

	instanceIds, err := ec.EKSSvc.GetInstancesByK8sNodeName(context.Context(), ec.EKSCred, []string{k8sNodeName})
	if err != nil {
		return fmt.Errorf("terminate ec2 instance: %w", err)
	}

	logrus.Infof("Starting to terminate the ec2 instance: %v", instanceIds)

	// TriggerConfig checks
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err = triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("terminate ec2 instance: %w", err)
	}

	// Terminating the instance
	if err = ec.EKSSess.TerminateInstances(context.Context(), instanceIds); err != nil {
		return fmt.Errorf("terminate ec2 instance: %w", err)
	}

	logrus.Infof("Successfully terminated the ec2 instance: %v", instanceIds)

	return nil
}

// DeletePods deletes a pod using kubectl delete.
func (ec *EKSChaos) DeletePods(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	// TriggerConfig checks
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err := triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("delete pods eks: %w", err)
	}

	// Deleting the pod
	_, err = kubectl.Delete("pod", chaosConfig.CBPod).InNamespace("default").Output()
	if err != nil {
		return fmt.Errorf("delete pods eks: %w", err)
	}

	return nil
}

func (ec *EKSChaos) CBServiceChaos(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	// TriggerConfig checks.
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err := triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("cb service chaos eks: %w", err)
	}

	err = ExecuteCBServiceChaos(context, chaosConfig, chaosConfig.CBPod)
	if err != nil {
		return fmt.Errorf("cb service chaos eks: %w", err)
	}

	return nil
}

func (ec *EKSChaos) CBClusterChaos(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	// TriggerConfig checks
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err := triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("cb cluster chaos eks: %w", err)
	}

	err = ExecuteCBClusterChaos(context, chaosConfig)
	if err != nil {
		return fmt.Errorf("cb cluster chaos eks: %w", err)
	}

	return nil
}
