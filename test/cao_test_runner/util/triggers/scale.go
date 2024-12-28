package triggers

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	cbrestapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api"
	clusternodesapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api_spec/cluster_nodes"
)

var (
	ErrScalingNotStarted = errors.New("scaling has not started")
)

// WaitForScaling checks for the scaling to start. This is a generic trigger which includes scaling up, down or both together.
func WaitForScaling(trigger *TriggerConfig) error {
	checkScalingFunc := func() error {
		podName := trigger.CBInfo.cbPodName
		clusterName := trigger.CBClusterName

		clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(podName, clusterName, "", "", trigger.CBSecretName, "default", 5*time.Second, false, false)
		if err != nil {
			return err
		}

		clusterTaskResults, err := clusterNodesAPI.PoolsDefaultTasks(trigger.PortForward)
		if err != nil {
			return fmt.Errorf("wait for scaling: %w", err)
		}

		for _, clusterTaskResult := range clusterTaskResults {
			if clusterTaskResult.Type == clusternodesapi.TaskTypeRebalance {
				if clusterTaskResult.Status == clusternodesapi.TaskStatusRunning {
					// Active Nodes - Eject Nodes = Final Number of Nodes to have.
					activeNodes := len(clusterTaskResult.NodesInfo.ActiveNodes)
					ejectNodes := len(clusterTaskResult.NodesInfo.EjectNodes)

					if trigger.CBInfo.cbPodsAfterScaling == activeNodes-ejectNodes {
						return nil
					}
				} else {
					return ErrScalingNotStarted
				}
			}
		}

		return ErrScalingNotStarted
	}

	err := util.RetryFunctionTillTimeout(checkScalingFunc, trigger.TriggerDuration, trigger.TriggerInterval)
	if err != nil {
		return fmt.Errorf("trigger for scaling: %w", err)
	}

	return nil
}
