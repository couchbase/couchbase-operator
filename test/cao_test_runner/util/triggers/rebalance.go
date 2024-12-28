package triggers

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	cbrestapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api"
	clusternodesapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api_spec/cluster_nodes"
	"github.com/sirupsen/logrus"
)

const (
	RebalanceSubTypeRebalance = "rebalance"
	RebalanceSubTypeFailover  = "failover"
)

var (
	ErrRebalanceNotStarted     = errors.New("rebalance not started")
	ErrDeltaRecoveryNotStarted = errors.New("delta recovery rebalance not started")
	ErrSwapRebNotStarted       = errors.New("swap rebalance not started")
)

// WaitForRebalance checks for latest rebalance to trigger. This trigger checks for any rebalance.
// To check for specific rebalance use other triggers.
func WaitForRebalance(trigger *TriggerConfig) error {
	checkRebalanceFunc := func() error {
		podName := trigger.CBInfo.cbPodName
		clusterName := trigger.CBClusterName

		clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(podName, clusterName, "", "", trigger.CBSecretName, "default", 5*time.Second, false, false)
		if err != nil {
			return err
		}

		clusterTasks, err := clusterNodesAPI.PoolsDefaultTasks(trigger.PortForward)
		if err != nil {
			return fmt.Errorf("wait for rebalance: %w", err)
		}

		logrus.Debugf("tasks: %v\n", clusterTasks)

		for _, task := range clusterTasks {
			if task.Type == clusternodesapi.TaskTypeRebalance {
				if task.Status == clusternodesapi.TaskStatusRunning {
					return nil
				} else {
					return ErrRebalanceNotStarted
				}
			}
		}

		return ErrRebalanceNotStarted
	}

	err := util.RetryFunctionTillTimeout(checkRebalanceFunc, trigger.TriggerDuration, trigger.TriggerInterval)
	if err != nil {
		return fmt.Errorf("trigger for rebalance: %w", err)
	}

	return nil
}

// WaitForDeltaRecoveryRebalance checks for latest delta recovery rebalance to trigger.
func WaitForDeltaRecoveryRebalance(trigger *TriggerConfig) error {
	checkDeltaRecovery := func() error {
		podName := trigger.CBInfo.cbPodName
		clusterName := trigger.CBClusterName

		clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(podName, clusterName, "", "", trigger.CBSecretName, "default", 5*time.Second, false, false)
		if err != nil {
			return err
		}

		clusterTasks, err := clusterNodesAPI.PoolsDefaultTasks(trigger.PortForward)
		if err != nil {
			return fmt.Errorf("wait for delta recovery rebalance: %w", err)
		}

		logrus.Debugf("tasks: %v\n", clusterTasks)

		for _, task := range clusterTasks {
			if task.Type == clusternodesapi.TaskTypeRebalance && task.Subtype == RebalanceSubTypeRebalance {
				if task.Status == clusternodesapi.TaskStatusRunning {
					// Rebalance started, we check if the required cb pod is in delta_nodes
					for _, cbPod := range task.NodesInfo.DeltaNodes {
						if strings.Contains(cbPod, trigger.CBInfo.cbPodName) {
							return nil
						}
					}
				} else {
					return ErrDeltaRecoveryNotStarted
				}
			}
		}

		return ErrDeltaRecoveryNotStarted
	}

	err := util.RetryFunctionTillTimeout(checkDeltaRecovery, trigger.TriggerDuration, trigger.TriggerInterval)
	if err != nil {
		return fmt.Errorf("trigger for delta recovery rebalance: %w", err)
	}

	return nil
}

// WaitForSwapRebalanceOut checks if the Swap Rebalance has started for the CB pod which is getting ejected.
func WaitForSwapRebalanceOut(trigger *TriggerConfig) error {
	checkSwapRebalance := func() error {
		podName := trigger.CBInfo.cbPodName
		clusterName := trigger.CBClusterName

		clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(podName, clusterName, "", "", trigger.CBSecretName, "default", 5*time.Second, false, false)
		if err != nil {
			return err
		}

		clusterTasks, err := clusterNodesAPI.PoolsDefaultTasks(trigger.PortForward)
		if err != nil {
			return fmt.Errorf("wait for delta recovery rebalance: %w", err)
		}

		for _, task := range clusterTasks {
			if task.Type == clusternodesapi.TaskTypeRebalance {
				if task.Status == clusternodesapi.TaskStatusRunning {
					// Check if the eject nodes is the one we want
					for _, ejectCBPod := range task.NodesInfo.EjectNodes {
						if strings.Contains(ejectCBPod, trigger.CBInfo.cbPodName) {
							return nil
						}
					}
				} else {
					return ErrSwapRebNotStarted
				}
			}
		}

		return ErrSwapRebNotStarted
	}

	err := util.RetryFunctionTillTimeout(checkSwapRebalance, trigger.TriggerDuration, trigger.TriggerInterval)
	if err != nil {
		return fmt.Errorf("trigger for swap rebalance out: %w", err)
	}

	return nil
}
