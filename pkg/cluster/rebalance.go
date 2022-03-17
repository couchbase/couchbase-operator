package cluster

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
)

const (
	// DefaultRetryPeriod is the default amount of time to wait for an API
	// operation to report success.
	DefaultRetryPeriod = 30 * time.Second

	// ExtendedRetryPeriod is an extended amount of time to wait for slow
	// API operations to report success.
	ExtendedRetryPeriod = 3 * time.Minute
)

// RebalanceProgressEntry is the type communicated to clients periodically
// over a channel.
type RebalanceStatusEntry struct {
	// Status is the status of a rebalance.
	Status couchbaseutil.RebalanceStatus

	// Progress is how far the rebalance has progressed, only valid when
	// the status is RebalanceStatusRunning.
	Progress float64
}

// RebalanceProgress is a type used to monitor rebalance status.
type RebalanceProgress interface {
	// Status is a channel that is periodically updated with the rebalance
	// status and progress.  This channel is closed once the rebalance task
	// is no longer running or an error was detected.
	Status() <-chan *RebalanceStatusEntry

	// Error is used to check the error status when the Status channel has
	// been closed.
	Error() error

	// Cancel allows the client to stop the go routine before a rebalance has
	// completed.
	Cancel()
}

// rebalanceProgressImpl implements the RebalanceProgress interface.
type rebalanceProgressImpl struct {
	// statusChan is the main channel for communicating status to the client.
	statusChan chan *RebalanceStatusEntry

	// err is used to return any error encountered
	err error

	// context is a context used to cancel the rebalance progress
	context context.Context

	// cancel is used to cancel the rebalance progress
	cancel context.CancelFunc
}

// NewRebalanceProgress creates a new RebalanceProgress object and starts a go routine to
// periodically poll for updates.
func (c *Cluster) NewRebalanceProgress(ms couchbaseutil.MemberSet) RebalanceProgress {
	progress := &rebalanceProgressImpl{
		statusChan: make(chan *RebalanceStatusEntry),
	}

	progress.context, progress.cancel = context.WithCancel(context.Background())

	go func() {
	RoutineRunloop:
		for {
			tasks := &couchbaseutil.TaskList{}
			if err := couchbaseutil.ListTasks(tasks).On(c.api, ms); err != nil {
				progress.err = err

				close(progress.statusChan)

				break RoutineRunloop
			}

			task, err := tasks.GetTask(couchbaseutil.TaskTypeRebalance)
			if err != nil {
				progress.err = err

				close(progress.statusChan)

				break RoutineRunloop
			}

			status := getRebalanceStatus(task)

			// If the task is no longer running then terminate the routine.
			if status == couchbaseutil.RebalanceStatusNotRunning {
				close(progress.statusChan)

				break RoutineRunloop
			}

			// Otherwise return the status to the client
			progress.statusChan <- &RebalanceStatusEntry{
				Status:   status,
				Progress: task.Progress,
			}

			// Wait for a period of time or for the client to close the
			// progress.  Do this in the loop tail to maintain compatibility
			// with the old code.
			select {
			case <-time.After(4 * time.Second):
			case <-progress.context.Done():
				break RoutineRunloop
			}
		}
	}()

	return progress
}

// Status returns the RebalanceProgress status channel.
func (r *rebalanceProgressImpl) Status() <-chan *RebalanceStatusEntry {
	return r.statusChan
}

// Error returns the RebalanceProgress error channel.
func (r *rebalanceProgressImpl) Error() error {
	return r.err
}

// Cancel terminates the RebalanceProgress routine.
func (r *rebalanceProgressImpl) Cancel() {
	r.cancel()
}

// getRebalanceStatus transforms a task status into our simplified rebalance status.
func getRebalanceStatus(task *couchbaseutil.Task) couchbaseutil.RebalanceStatus {
	// We treat stale or timed out tasks as unknown, no status
	// is treated as not running.
	status := couchbaseutil.RebalanceStatus(task.Status)

	switch {
	case task.Stale || task.Timeout:
		status = couchbaseutil.RebalanceStatusUnknown
	case status == couchbaseutil.RebalanceStatusNone:
		status = couchbaseutil.RebalanceStatusNotRunning
	}

	return status
}

// witnessRebalance waits until we can start streaming progress from the rebalance task.
func (c *Cluster) witnessRebalance(ms couchbaseutil.MemberSet) (RebalanceProgress, *RebalanceStatusEntry, error) {
	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

WitnessLoop:
	for {
		progress := c.NewRebalanceProgress(ms)
		status, ok := <-progress.Status()
		if ok {
			return progress, status, nil
		}

		select {
		case <-tick.C:
		case <-ctx.Done():
			break WitnessLoop
		}
	}

	return nil, nil, errors.NewStackTracedError(errors.ErrRebalanceIncomplete)
}

func (c *Cluster) rebalance(ms couchbaseutil.MemberSet, eject couchbaseutil.OTPNodeList) error {
	// Notify that we are starting a rebalance, the actual client operation
	// is blocking so we need to report now or kubernetes will be out of sync
	c.cluster.Status.SetUnbalancedCondition()

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.RebalanceStartedEvent(c.cluster))

	// The rebalance API is crap, rather than accepting a list of host names to eject
	// it requires a list of nodes that it already knows about as well.  On top of that
	// the names need translating into erlang rather than the hostnames that all the
	// clients and uses know and use.
	info := &couchbaseutil.ClusterInfo{}
	if err := couchbaseutil.GetPoolsDefault(info).On(c.api, ms); err != nil {
		return err
	}

	known := make(couchbaseutil.OTPNodeList, len(info.Nodes))

	for i, node := range info.Nodes {
		known[i] = node.OTPNode
	}

	if err := couchbaseutil.Rebalance(known, eject).On(c.api, ms); err != nil {
		c.raiseEvent(k8sutil.RebalanceIncompleteEvent(c.cluster))
		return err
	}

	// Ensure we see the rebalance happen, if we don't do this then we can delete
	// pods prematurely.
	progress, status, _ := c.witnessRebalance(ms)

	// Stream out rebalance status if we have observed the rebalance starting.
	if status != nil {
		for {
			switch status.Status {
			case couchbaseutil.RebalanceStatusUnknown:
				log.Info("Rebalancing", "cluster", c.namespacedName(), "progress", "unknown")
			case couchbaseutil.RebalanceStatusRunning:
				log.Info("Rebalancing", "cluster", c.namespacedName(), "progress", status.Progress)
			}

			var ok bool

			status, ok = <-progress.Status()
			if !ok {
				if err := progress.Error(); err != nil {
					return err
				}

				break
			}
		}
	}

	// Verify the rebalance occurred as we expected it to.  Even if we did not witness
	// the rebalance, the cluster status may indicate that all expected members are balanced
	// in, and there are no nodes we don't expect.  It may have happened too quickly to
	// be observed!
	if err := c.verifyRebalance(ms); err != nil {
		c.raiseEvent(k8sutil.RebalanceIncompleteEvent(c.cluster))
		return err
	}

	// Notify if we've removed some nodes (deterministically sorted)
	ejectedOTPNodeStringSlice := eject.StringSlice()
	sort.Strings(ejectedOTPNodeStringSlice)

	for _, otpNode := range ejectedOTPNodeStringSlice {
		// TODO: this feels dirty!!  If everything was a Member, then this would
		// be trivial...
		hostname := strings.Split(otpNode, "@")[1]
		memberName := strings.Split(hostname, ".")[0]
		c.raiseEvent(k8sutil.MemberRemoveEvent(memberName, c.cluster))
	}

	// Report the cluster is balanced
	log.Info("Rebalance completed successfully", "cluster", c.namespacedName())
	c.raiseEvent(k8sutil.RebalanceCompletedEvent(c.cluster))
	c.cluster.Status.SetBalancedCondition()

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) verifyRebalance(members couchbaseutil.MemberSet) error {
	// Error checking... perfom this a few times as there is a race between when
	// we check and when Server reports rebalance is complete.
	retryFunc := func() error {
		status, err := c.GetStatus()
		if err != nil {
			return err
		}

		// Cluster reports as balanced.
		if !status.Balanced {
			return errors.NewStackTracedError(errors.ErrRebalanceIncomplete)
		}

		// All Operator members should be active Couchbase nodes.
		for name := range members {
			state, ok := status.NodeStates[name]
			if !ok {
				return fmt.Errorf("%w: node %s not found in cluster", errors.NewStackTracedError(errors.ErrRebalanceIncomplete), name)
			}

			if state != NodeStateActive {
				return fmt.Errorf("%w: node %s state %v, expected Active", errors.NewStackTracedError(errors.ErrRebalanceIncomplete), name, state)
			}
		}

		// All Couchbase nodes should be Operator members.
		for name := range status.NodeStates {
			if _, ok := members[name]; !ok {
				return fmt.Errorf("%w: node %s unexpectedly clustered", errors.NewStackTracedError(errors.ErrRebalanceIncomplete), name)
			}
		}

		return nil
	}

	return retryutil.RetryFor(10*time.Second, retryFunc)
}

// Check that cluster is actively rebalancing with status 'running'.
func (c *Cluster) IsRebalanceActive(ms couchbaseutil.MemberSet) (bool, error) {
	tasks := &couchbaseutil.TaskList{}
	if err := couchbaseutil.ListTasks(tasks).On(c.api, ms); err != nil {
		return false, err
	}

	task, err := tasks.GetTask(couchbaseutil.TaskTypeRebalance)
	if err != nil {
		return false, err
	}

	return getRebalanceStatus(task) == couchbaseutil.RebalanceStatusRunning, nil
}
