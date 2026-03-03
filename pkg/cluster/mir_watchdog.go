/*
Copyright 2025-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
)

type mirWatchdog struct {
	cluster *Cluster
	ctx     context.Context
}

// MirWatchdogContext is a context for the MIR watchdog goroutine.
type MirWatchdogContext struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func StartMirWatchdog(cluster *Cluster, interval time.Duration) *MirWatchdogContext {
	mwc := MirWatchdogContext{}
	mwc.ctx, mwc.cancel = context.WithCancel(context.Background())
	go newMirWatchdog(cluster, mwc.ctx).run(mwc.ctx, interval)
	return &mwc
}

func (mwc *MirWatchdogContext) isRunning() bool {
	mwc.mu.Lock()
	defer mwc.mu.Unlock()
	return mwc.ctx != nil && mwc.cancel != nil
}

func (mwc *MirWatchdogContext) Stop() {
	mwc.mu.Lock()
	defer mwc.mu.Unlock()
	if mwc.cancel != nil {
		mwc.cancel()
	}
	mwc.ctx = nil
	mwc.cancel = nil
}

type ManualInterventionRequired string

const (
	MIRLoginFailure              ManualInterventionRequired = "Unable to authenticate to the cluster using login credentials provided"
	MIRRebalanceRetriesExhausted ManualInterventionRequired = "Rebalance retries have been exhausted 3 times in a row"
	MIRManualActionDownNodes     ManualInterventionRequired = "There are down nodes that cannot be recovered"
	MIRTLSExpired                ManualInterventionRequired = "TLS certificate has expired"
)

type ManualInterventionRequiredList []*ManualInterventionRequired

func (mirl *ManualInterventionRequiredList) clusterConditionMessage() string {
	reasons := make([]string, len(*mirl))
	for i, condition := range *mirl {
		reasons[i] = string(*condition)
	}

	return strings.Join(reasons, "\n")
}

func newMirWatchdog(cluster *Cluster, ctx context.Context) *mirWatchdog {
	return &mirWatchdog{
		cluster: cluster,
		ctx:     ctx,
	}
}

// run performs periodic checks on the cluster for any issues that require manual intervention by a user.
// These issues must be limited to ones that cannot be resolved by the operator.
// When manual intervention is required, this will add a condition to the cluster,
// raise a separate event for each of the reasons and set the cluster_manual_intervention metric to 1.
// When manual intervention is no longer required, this will clear the condition,
// raise a ManualInterventionResolved event and set the cluster_manual_intervention metric to 0.
func (w *mirWatchdog) run(ctx context.Context, interval time.Duration) {
	log.Info("Manual intervention required checks started", "cluster", w.cluster.namespacedName())

	for {
		select {
		case <-ctx.Done():
			log.Info("Manual intervention required checks stopped", "cluster", w.cluster.namespacedName())
			return
		case <-time.After(interval):
			w.checkCluster()
		}
	}
}

func (w *mirWatchdog) checkCluster() {
	// Check if the context is cancelled before proceeding to avoid race conditions
	// where the watchdog modifies conditions/raises events after being told to stop.
	select {
	case <-w.ctx.Done():
		return
	default:
	}

	// List of checks that should be ran on a predetermined interval to alert the user if manual intervention is required.
	// There should be clear lines on how to get and how to get out of each possible MIR state. Checks should be reluctant
	// to enter MIR states, and aggressive to exit them.
	// Keep in mind that by entering the MIR state, the operator will *not* reconcile the cluster until the MIR exit conidition is met.
	mirChecks := []func(*couchbasev2.ClusterCondition) *ManualInterventionRequired{
		w.checkForClusterLoginFailure,
		w.checkForConsecutiveRebalanceFailures,
		w.checkForManualActionDownNodes,
		w.checkForTLSExpiration,
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

	// Try to update the cluster status for up to 10 seconds in case of transient failures.
	if err := retryutil.RetryFor(10*time.Second, w.cluster.updateCRStatus); err != nil {
		log.Error(err, "MirWatchdog failed to update cluster status", "cluster", w.cluster.namespacedName())
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
			return mir(MIRRebalanceRetriesExhausted)
		}

		if status.Balanced && areAllNodesActive(status.NodeStates) {
			return nil
		}

		return mir(MIRRebalanceRetriesExhausted)
	}

	// Entry
	if w.cluster.cluster.Status.GetRebalanceAttempts() >= 3 {
		status, err := w.cluster.GetStatus()
		if err != nil {
			return nil
		}

		if !status.Balanced || !areAllNodesActive(status.NodeStates) {
			return mir(MIRRebalanceRetriesExhausted)
		}
	}

	return nil
}

// checkForManualActionDownNodes checks whether there are down nodes that cannot be recovered by the operator.
// Entry: There are down nodes that cannot be recovered.
// Exit: There are no unrecoverable down nodes.
func (w *mirWatchdog) checkForManualActionDownNodes(existingCondition *couchbasev2.ClusterCondition) *ManualInterventionRequired {
	if w.cluster.cluster.GetRecoveryPolicy() != couchbasev2.PrioritizeDataIntegrity {
		return nil
	}

	downNodes := w.cluster.getDownNodes()
	if len(downNodes) == 0 {
		return nil
	}

	for _, downMember := range downNodes {
		m, ok := w.cluster.members[downMember]

		if !ok {
			continue
		}

		r, _ := w.cluster.hasMemberRecoveryTimeElapsed(downMember)
		if !r {
			continue
		}

		recoverable, _ := w.cluster.checkPodRecoverability(m, false)
		if !recoverable {
			return mir(MIRManualActionDownNodes)
		}
	}

	return nil
}

// checkForTLSExpiration checks for tls expiration on CA, server, and client certs.
// Entry: There are TLS certificates that have expired and will not be rotated by the operator.
// Exit: There are no expired TLS certificates or the operator will rotate any that are expired.
func (w *mirWatchdog) checkForTLSExpiration(existingCondition *couchbasev2.ClusterCondition) *ManualInterventionRequired {
	if !w.cluster.cluster.IsTLSEnabled() {
		return nil
	}

	clientTLS := w.cluster.api.GetTLS()
	if clientTLS == nil {
		return nil
	}

	// If we are in the MIR state for TLS expiration, we should refresh the tls cache as reconcile will be blocked ontil the MIR is resolved.
	if inMirStateForReason(existingCondition, MIRTLSExpired) {
		if err := w.cluster.initTLSCache(); err != nil {
			log.Error(err, "MirWatchdog failed to refresh tls cache", "cluster", w.cluster.namespacedName())
		}
	}

	// For all TLS certs, we should enter the MIR condition if they are expired and the operator is not going to rotate them.
	// We can leave the MIR state once we know they will be rotated by the operator on one of the subsequent reconcile loops.
	if clientTLS.ClientAuth != nil && w.cluster.checkCertExpiration(clientTLS.ClientAuth.Cert) && reflect.DeepEqual(clientTLS.ClientAuth.Cert, w.cluster.tlsCache.clientCert) {
		return mir(MIRTLSExpired)
	}

	if w.cluster.checkCertExpiration(clientTLS.CACert) && (!w.cluster.cluster.Spec.Networking.TLS.AllowPlainTextCertReload || reflect.DeepEqual(clientTLS.CACert, w.cluster.tlsCache.serverCA)) {
		return mir(MIRTLSExpired)
	}

	for _, ca := range clientTLS.RootCAs {
		if w.cluster.checkCertExpiration(ca) && (!w.cluster.cluster.Spec.Networking.TLS.AllowPlainTextCertReload || reflect.DeepEqual(clientTLS.RootCAs, &w.cluster.tlsCache.rootCAs)) {
			return mir(MIRTLSExpired)
		}
	}

	return nil
}

func inMirStateForReason(existingCondition *couchbasev2.ClusterCondition, reason ManualInterventionRequired) bool {
	return existingCondition != nil && strings.Contains(existingCondition.Message, string(reason))
}

func mir(reason ManualInterventionRequired) *ManualInterventionRequired {
	return &reason
}
