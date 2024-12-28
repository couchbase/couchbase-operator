package triggers

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	cbrestapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api"
	clusternodesapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api_spec/cluster_nodes"
)

var (
	ErrDeltaRecoveryWarmupNotStarted  = errors.New("delta recovery upgrade warmup not started")
	ErrCBPodNotUpgraded               = errors.New("cb pod not upgraded")
	ErrCBPodNotFound                  = errors.New("cb pod not found")
	ErrDeltaRecoveryUpgradeNotStarted = errors.New("delta recovery upgrade not started")
	ErrSwapRebUpgradeNotStarted       = errors.New("swap rebalance upgrade not started")
	ErrSwapRebInPodNotFound           = errors.New("swap rebalance added pod not found")
)

// WaitForDeltaRecoveryUpgradeWarmup checks for the Delta Recovery warmup to get started. Delta Recovery warmup happens after
// the CB node has been failed over and is brought back (with upgraded version).
func WaitForDeltaRecoveryUpgradeWarmup(trigger *TriggerConfig) error {
	checkDeltaUpgradeWarmup := func() error {
		checkCBPodVersion := false

		podName := trigger.CBInfo.cbPodName
		clusterName := trigger.CBClusterName

		clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(podName, clusterName, "", "", trigger.CBSecretName, "default", 5*time.Second, false, false)
		if err != nil {
			return err
		}

		poolsDefault, err := clusterNodesAPI.PoolsDefault(trigger.PortForward)
		if err != nil {
			return fmt.Errorf("wait for scaling: %w", err)
		}

		for _, poolsNode := range poolsDefault.Nodes {
			// Making sure we have the right cb pod
			if strings.Contains(poolsNode.Hostname, trigger.CBInfo.cbPodName) {
				// Checking the version of cb pod
				if strings.Contains(poolsNode.Version, trigger.CBInfo.cbPodVersion) {
					// During Delta Recovery Warmup the cb node is inactiveAdded and recovery type is delta.
					if poolsNode.ClusterMembership == "active" && poolsNode.RecoveryType == "delta" && poolsNode.Status == "warmup" {
						checkCBPodVersion = true
						break
					} else {
						return fmt.Errorf("cb pod %s: %w", trigger.CBInfo.cbPodName, ErrDeltaRecoveryWarmupNotStarted)
					}
				}

				return fmt.Errorf("cb pod `%s` version=%s: %w", trigger.CBInfo.cbPodName, poolsNode.Version, ErrDeltaRecoveryWarmupNotStarted)
			}
		}

		// If the required cbPod is in the delta nodes, we just confirm the version of the cbPod
		if checkCBPodVersion {
			poolsDefault, err := clusterNodesAPI.PoolsDefault(trigger.PortForward)
			if err != nil {
				return fmt.Errorf("wait for scaling: %w", err)
			}

			for _, poolsNode := range poolsDefault.Nodes {
				if strings.Contains(poolsNode.Hostname, trigger.CBInfo.cbPodName) {
					// Checking the version of cb pod
					if strings.Contains(poolsNode.Version, trigger.CBInfo.cbPodVersion) {
						return nil
					}

					return fmt.Errorf("cb pod `%s` version=%s: %w", trigger.CBInfo.cbPodName, poolsNode.Version, ErrCBPodNotUpgraded)
				}
			}
		}

		return fmt.Errorf("cb pod %s: %w", trigger.CBInfo.cbPodName, ErrCBPodNotFound)
	}

	err := util.RetryFunctionTillTimeout(checkDeltaUpgradeWarmup, trigger.TriggerDuration, trigger.TriggerInterval)
	if err != nil {
		return fmt.Errorf("trigger for delta recovery warmup: %w", err)
	}

	return nil
}

// WaitForDeltaRecoveryUpgrade checks if the Delta Recovery upgrade has started for the CB pod.
func WaitForDeltaRecoveryUpgrade(trigger *TriggerConfig) error {
	checkDeltaUpgrade := func() error {
		checkCBPodVersion := false

		podName := trigger.CBInfo.cbPodName
		clusterName := trigger.CBClusterName

		clusterNodesAPI, err := cbrestapi.NewClusterNodesAPI(podName, clusterName, "", "", trigger.CBSecretName, "default", 5*time.Second, false, false)
		if err != nil {
			return err
		}

		clusterTasks, err := clusterNodesAPI.PoolsDefaultTasks(trigger.PortForward)
		if err != nil {
			return fmt.Errorf("wait for scaling: %w", err)
		}

		for _, task := range clusterTasks {
			// Finding the task rebalance from the list of tasks and checking if the rebalance is running
			if task.Type == clusternodesapi.TaskTypeRebalance && task.Status == clusternodesapi.TaskStatusRunning {
				// Checking if the delta nodes is the one we want
				for _, deltaCBPod := range task.NodesInfo.DeltaNodes {
					if strings.Contains(deltaCBPod, trigger.CBInfo.cbPodName) {
						checkCBPodVersion = true
						break
					}
				}
			}

			if checkCBPodVersion {
				break
			}
		}

		// If the required cbPod is in the delta nodes, we just confirm the version of the cbPod
		if checkCBPodVersion {
			poolsDefault, err := clusterNodesAPI.PoolsDefault(trigger.PortForward)
			if err != nil {
				return fmt.Errorf("wait for scaling: %w", err)
			}

			for _, poolsNode := range poolsDefault.Nodes {
				if strings.Contains(poolsNode.Hostname, trigger.CBInfo.cbPodName) {
					// Checking the version of cb pod
					if strings.Contains(poolsNode.Version, trigger.CBInfo.cbPodVersion) {
						return nil
					}

					return fmt.Errorf("cb pod `%s` version=%s: %w", trigger.CBInfo.cbPodName, poolsNode.Version, ErrCBPodNotUpgraded)
				}
			}
		}

		return ErrDeltaRecoveryUpgradeNotStarted
	}

	err := util.RetryFunctionTillTimeout(checkDeltaUpgrade, trigger.TriggerDuration, trigger.TriggerInterval)
	if err != nil {
		return fmt.Errorf("trigger for delta recovery upgrade: %w", err)
	}

	return nil
}

// WaitForSwapRebalanceIn checks if the Swap Rebalance has started for the CB pod which is getting ejected and
// updates TriggerConfig.CBInfo CBSwapRebPodName with the name of the CB pod which is being added to CB cluster.
func WaitForSwapRebalanceIn(trigger *TriggerConfig) error {
	checkSwapRebalance := func() error {
		var reqRebalanceTask clusternodesapi.Task

		getSwapInPod := false

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

		// First checking if Swap Rebalance upgrade has started.
		for _, task := range clusterTasks {
			if task.Type == clusternodesapi.TaskTypeRebalance {
				if task.Status == clusternodesapi.TaskStatusRunning {
					// Check if the eject nodes is the one we want
					for _, ejectCBPod := range task.NodesInfo.EjectNodes {
						if strings.Contains(ejectCBPod, trigger.CBInfo.cbPodName) {
							getSwapInPod = true
							reqRebalanceTask = task

							break
						}
					}
				} else {
					return ErrSwapRebUpgradeNotStarted
				}
			}

			if getSwapInPod {
				break
			}
		}

		// Once we have found that the cbPod is being swap rebalanced out. We find the CB pod which is being swap rebalanced in.
		if getSwapInPod {
			keepNodes := reqRebalanceTask.NodesInfo.KeepNodes

			slices.Sort(keepNodes)

			// swapInPod is the latest pod with updated count in the name.
			swapInPod := keepNodes[len(keepNodes)-1]

			poolsDefault, err := clusterNodesAPI.PoolsDefault(trigger.PortForward)
			if err != nil {
				return fmt.Errorf("wait for rebalance: %w", err)
			}

			for _, poolsNode := range poolsDefault.Nodes {
				if strings.Contains(poolsNode.Hostname, swapInPod) {
					// Checking the version of cb pod
					if strings.Contains(poolsNode.Version, trigger.CBInfo.cbPodVersion) {
						return nil
					}

					return fmt.Errorf("swapped in pod `%s` current version=%s: %w", swapInPod, poolsNode.Version, ErrCBPodNotUpgraded)
				}
			}

			return ErrSwapRebInPodNotFound
		}

		return ErrSwapRebUpgradeNotStarted
	}

	err := util.RetryFunctionTillTimeout(checkSwapRebalance, trigger.TriggerDuration, trigger.TriggerInterval)
	if err != nil {
		return fmt.Errorf("trigger for swap rebalance in: %w", err)
	}

	return nil
}
