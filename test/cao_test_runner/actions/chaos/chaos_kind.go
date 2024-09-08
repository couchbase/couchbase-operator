package chaos

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
	shellutil "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/shell"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
)

type KindChaos struct {
}

// NewKindChaos returns an instance of KindChaos to perform chaos actions on Kind cluster.
func NewKindChaos() (*KindChaos, error) {
	return &KindChaos{}, nil
}

// RestartNodes restarts the kind node (the docker container for the node).
func (k *KindChaos) RestartNodes(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	k8sNodeName, err := pods.GetNodeNameForPod(chaosConfig.CBPod, "default")
	if err != nil {
		return fmt.Errorf("restart kind node: %w", err)
	}

	// TriggerConfig checks
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err = triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("restart kind node: %w", err)
	}

	err = shellutil.RunWithoutOutputCapture("docker", "restart", k8sNodeName)
	if err != nil {
		return fmt.Errorf("restart kind node: %w", err)
	}

	return nil
}

// DeleteNodes forcefully deletes the kind node (the docker container for the node).
func (k *KindChaos) DeleteNodes(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	k8sNodeName, err := pods.GetNodeNameForPod(chaosConfig.CBPod, "default")
	if err != nil {
		return fmt.Errorf("delete kind node: %w", err)
	}

	// TriggerConfig checks
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err = triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("delete kind node: %w", err)
	}

	err = shellutil.RunWithoutOutputCapture("docker", "rm", "--force", k8sNodeName)
	if err != nil {
		return fmt.Errorf("delete kind node: %w", err)
	}

	return nil
}

// DeletePods deletes a pod using kubectl delete.
func (k *KindChaos) DeletePods(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	// TriggerConfig checks
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err := triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("delete pods kind: %w", err)
	}

	_, err = kubectl.Delete("pod", chaosConfig.CBPod).InNamespace("default").Output()
	if err != nil {
		return fmt.Errorf("delete pods kind: %w", err)
	}

	return nil
}

func (k *KindChaos) CBServiceChaos(context *context.Context, chaosConfig *CBPodChaosConfig) error {
	// TriggerConfig checks
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err := triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("cb service chaos kind: %w", err)
	}

	err = ExecuteCBServiceChaos(context, chaosConfig, chaosConfig.CBPod)
	if err != nil {
		return fmt.Errorf("cb service chaos kind: %w", err)
	}

	return nil
}
