package triggers

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	cbrestapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api"
	clusternodesapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api/cluster_nodes"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

var (
	ErrScalingNotStarted = errors.New("scaling has not started")
)

// WaitForScaling checks for the scaling to start. This is a generic trigger which includes scaling up, down or both together.
func WaitForScaling(trigger *TriggerConfig) error {
	requestClient := requestutils.NewClient()

	cbAuth, err := requestutils.GetCBClusterAuth(trigger.CBSecretName, "default")
	if err != nil {
		return fmt.Errorf("trigger for scaling: %w", err)
	}

	requestClient.SetHTTPAuth(cbAuth.Username, cbAuth.Password)

	hostname, err := requestutils.GetHTTPHostname("localhost", 8091)
	if err != nil {
		return fmt.Errorf("trigger for scaling: %w", err)
	}

	checkScalingFunc := func() error {
		var clusterTaskResults []cbrestapi.Task

		err := requestClient.Do(clusternodesapi.ClusterTasks(hostname), &clusterTaskResults, defaultRequestTimeout)
		if err != nil {
			return err
		}

		for _, clusterTaskResult := range clusterTaskResults {
			if clusterTaskResult.Type == cbrestapi.TaskTypeRebalance {
				if clusterTaskResult.Status == cbrestapi.TaskStatusRunning {
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

	err = util.RetryFunctionTillTimeout(checkScalingFunc, trigger.TriggerDuration, trigger.TriggerInterval)
	if err != nil {
		return fmt.Errorf("trigger for scaling: %w", err)
	}

	return nil
}
