package cluster

import (
	"errors"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

type mirWatchdog struct {
	cluster *Cluster
}

type ManualInterventionRequired string

const (
	MIRLoginFailure              ManualInterventionRequired = "Unable to authenticate to the cluster using login credentials provided"
	MIRRebalanceRetriesExhausted ManualInterventionRequired = "Rebalance retries have been exhausted 3 times in a row"
)

type ManualInterventionRequiredList []*ManualInterventionRequired

func (mirl *ManualInterventionRequiredList) clusterConditionMessage() string {
	reasons := make([]string, len(*mirl))
	for i, condition := range *mirl {
		reasons[i] = string(*condition)
	}

	return strings.Join(reasons, "\n")
}

func newMirWatchdog(cluster *Cluster) *mirWatchdog {
	return &mirWatchdog{
		cluster: cluster,
	}
}

// run performs periodic checks on the cluster for any issues that require manual intervention by a user.
// These issues must be limited to ones that cannot be resolved by the operator.
// When manual intervention is required, this will add a condition to the cluster,
// raise a separate event for each of the reasons and set the cluster_manual_intervention metric to 1.
// When manual intervention is no longer required, this will clear the condition,
// raise a ManualInterventionResolved event and set the cluster_manual_intervention metric to 0.
func (w *mirWatchdog) run() {
	log.Info("Manual intervention required checks starting", "cluster", w.cluster.namespacedName())

	interval := 20 * time.Second

	// The interval can be configured via the mirWatchdogInterval key in the persistency grid.
	if val, err := w.cluster.state.Get(persistence.MirWatchdogInterval); err == nil {
		if parsedInterval, err := time.ParseDuration(val); err == nil {
			interval = parsedInterval
		} else {
			log.Error(err, "Invalid mirWatchdogInterval, using default", "cluster", w.cluster.namespacedName(), "interval", val)
		}
	}

	for {
		select {
		case <-w.cluster.ctx.Done():
			return
		case <-time.After(interval):
			w.checkCluster()
		}
	}
}

func (w *mirWatchdog) checkCluster() {
	// List of checks that should be ran on a predetermined interval to alert the user if manual intervention is required.
	// There should be clear lines on how to get and how to get out of each possible MIR state. Checks should be reluctant
	// to enter MIR states, and aggressive to exit them.
	// Keep in mind that by entering the MIR state, the operator will *not* reconcile the cluster until the MIR exit conidition is met.
	mirChecks := []func(*couchbasev2.ClusterCondition) *ManualInterventionRequired{
		w.checkForClusterLoginFailure,
		w.checkForConsecutiveRebalanceFailures,
	}

	mirList := ManualInterventionRequiredList{}

	// MIR checks will need the existing condition if they have different MIR state entry/exit logic.
	existingCondition := w.cluster.cluster.Status.GetCondition(couchbasev2.ClusterConditionManualInterventionRequired)
	for _, mirCheck := range mirChecks {
		if mir := mirCheck(existingCondition); mir != nil {
			mirList = append(mirList, mir)
		}
	}

	if len(mirList) > 0 {
		w.handleManualInterventionRequired(mirList, existingCondition)
	} else {
		w.handleNoManualInterventionRequired(existingCondition)
	}

	if err := w.cluster.updateCRStatus(); err != nil {
		log.Error(err, "Failed to update cluster status", "cluster", w.cluster.namespacedName())
	}
}

// handleManualInterventionRequired is called when manual intervention is required.
// It will raise an event for each of the manual intervention required reasons.
// It will then set the ManualInterventionRequired condition on the cluster and set the cluster_manual_intervention metric to 1.
func (w *mirWatchdog) handleManualInterventionRequired(mirList ManualInterventionRequiredList, existingCondition *couchbasev2.ClusterCondition) {
	for _, mirReason := range mirList {
		// If the condition doesn't exist, we will raise an event for each of the manual intervention required reasons.
		// If it does already exist, we will only raise an event for the new reasons.
		if !inMirStateForReason(existingCondition, *mirReason) {
			log.Info("Manual intervention required", "cluster", w.cluster.namespacedName(), "reason", string(*mirReason))
			w.cluster.raiseEvent(k8sutil.ManualInterventionRequiredEvent(w.cluster.cluster, string(*mirReason)))
		}
	}

	w.cluster.cluster.Status.SetManualInterventionRequiredCondition(mirList.clusterConditionMessage())
	metrics.ManualInterventionRequiredMetric.WithLabelValues(w.cluster.addOptionalLabelValues([]string{w.cluster.cluster.Namespace, w.cluster.cluster.Name})...).Set(1)
}

// handleNoManualInterventionRequired is called when no manual intervention is required.
// If the manual condition exists on the cluster, a ManualInterventionResolved event will be raised,
// before the condition is cleared and the metric is set to 0.
func (w *mirWatchdog) handleNoManualInterventionRequired(existingCondition *couchbasev2.ClusterCondition) {
	if existingCondition != nil {
		log.Info("Manual intervention resolved", "cluster", w.cluster.namespacedName())
		w.cluster.raiseEvent(k8sutil.ManualInterventionResolvedEvent(w.cluster.cluster))
		w.cluster.cluster.Status.ClearCondition(couchbasev2.ClusterConditionManualInterventionRequired)
	}

	metrics.ManualInterventionRequiredMetric.WithLabelValues(w.cluster.addOptionalLabelValues([]string{w.cluster.cluster.Namespace, w.cluster.cluster.Name})...).Set(0)
}

// checkForClusterLoginFailure checks for the entry and exit conditions for the MIR for login failure.
// Entry: The operator is not able to authenticate with the couchbase cluster.
// Exit: The operator is able to authenticate with the couchbase cluster.
func (w *mirWatchdog) checkForClusterLoginFailure(_ *couchbasev2.ClusterCondition) *ManualInterventionRequired {
	clusterInfo := couchbaseutil.PoolsInfo{}

	runningPods, _ := w.cluster.getClusterPodsByPhase()

	// If no nodes have been initialised, we can't check for login failures.
	if len(runningPods) == 0 {
		return nil
	}

	// Send a request to the cluster to check if the login credentials are valid. We might want to add additional attempts
	// here to avoid flakiness. For now, if we encounter flakiness the condiiton will be removed on subsequent runs.
	memberSet := podsToMemberSet(runningPods)
	if err := couchbaseutil.GetPools(&clusterInfo).On(w.cluster.api, memberSet); err != nil {
		var failedReqErr couchbaseutil.FailedRequestError
		if errors.As(err, &failedReqErr) && failedReqErr.StatusCode == 401 {
			return mir(MIRLoginFailure)
		} else if errors.As(err, &failedReqErr) {
			log.Info("Failed to get cluster info", "cluster", w.cluster.namespacedName(), "error", err)
		}
	}

	return nil
}

// checkForConsecutiveRebalanceFailures checks for the entry and exit conditions for the MIR for rebalance retries exhausted.
// Entry: The operator exhausts the rebalance retries on 3 consecutive reconciliation loops.
// Exit: The cluster is balanced and all nodes are active.
func (w *mirWatchdog) checkForConsecutiveRebalanceFailures(existingCondition *couchbasev2.ClusterCondition) *ManualInterventionRequired {
	// Exit
	if inMirStateForReason(existingCondition, MIRRebalanceRetriesExhausted) {
		status, err := w.cluster.GetStatus()
		if err != nil {
			log.Info("Unable to get cluster status", "cluster", w.cluster.namespacedName(), "error", err)
			return nil
		}

		if status.Balanced && areAllNodesActive(status.NodeStates) {
			return nil
		}

		return mir(MIRRebalanceRetriesExhausted)
	}

	if w.cluster.cluster.Status.GetRebalanceAttempts() >= 3 {
		return mir(MIRRebalanceRetriesExhausted)
	}

	return nil
}

func inMirStateForReason(existingCondition *couchbasev2.ClusterCondition, reason ManualInterventionRequired) bool {
	return existingCondition != nil && strings.Contains(existingCondition.Message, string(reason))
}

func mir(reason ManualInterventionRequired) *ManualInterventionRequired {
	return &reason
}
