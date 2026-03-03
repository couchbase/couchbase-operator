/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package scheduler

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	corev1 "k8s.io/api/core/v1"
)

const (
	nullErrorHeader = "null scheduler"
)

// nullSchedulerImpl does no scheduling except enforcing ordering of
// server deletion.
type nullSchedulerImpl struct {
	// A plain list of servers per server class
	serverClasses serverGroups

	// removableServerClasses is for tracking our internal state of ordered removal
	// of pods by server classes
	removableServerClasses serverClassServerRemovalMap
}

// NewNullScheduler returns a new null scheduler.
func NewNullScheduler(pods []*corev1.Pod, cluster *couchbasev2.CouchbaseCluster) (Scheduler, error) {
	// Add existing servers to the server list, we need this for scheduling
	// pod removal
	sched := &nullSchedulerImpl{
		serverClasses:          map[string]*serverList{},
		removableServerClasses: map[string]*serverRemovalQueue{},
	}

	for _, class := range cluster.Spec.Servers {
		sched.serverClasses[class.Name] = &serverList{}
	}

	for _, pod := range pods {
		class, ok := pod.Labels[constants.LabelNodeConf]
		if !ok {
			return nil, fmt.Errorf("%s: pod %s does not have server class label: %w", nullErrorHeader, pod.Name, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		// Class deleted, ignore the pod
		if _, ok := sched.serverClasses[class]; !ok {
			continue
		}

		sched.serverClasses[class].push(pod.Name)
	}

	return sched, nil
}

// Create does nothing.
func (sched *nullSchedulerImpl) Create(class, name, _ string) (string, error) {
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: pod %s server class '%s' undefined: %w", nullErrorHeader, name, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	sched.serverClasses[class].push(name)

	return "", nil
}

// Delete removes the server(pod) depending on the serverRemovalQueue in a scale down scenario.
func (sched *nullSchedulerImpl) Delete(class string) (string, error) {
	// Select the victim server group based on population
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: no server list present for server class '%s': %w", nullErrorHeader, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	var podName string

	srq, ok := sched.removableServerClasses[class]
	if !ok {
		// Select the victim server deterministically based on alphabetical order
		sched.serverClasses[class].sort()

		server, err := sched.serverClasses[class].pop()
		if err != nil {
			return "", fmt.Errorf("%s: no server list found for server class '%s': %w", nullErrorHeader, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		podName = server
	} else {
		// if present, dequeue and delete
		podName = srq.dequeue()

		err := sched.serverClasses[class].del(podName)
		if err != nil {
			return "", fmt.Errorf("%s: no server named %s present for server class %s: %w", nullErrorHeader, podName, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}
	}

	return podName, nil
}

// Upgrade removes a node from the scheduler as it's an upgrade target.
func (sched *nullSchedulerImpl) Upgrade(class, name string) error {
	if err := sched.serverClasses[class].del(name); err != nil {
		return nil
	}

	return nil
}

// Reschedule looks at the current state, and if it doesn't match that
// requested when the scheduler was initialized, return a set of mutually
// exclusive moves to get us back into a conforming state.
func (sched *nullSchedulerImpl) Reschedule() ([]Move, error) {
	return nil, nil
}

// LogStatus returns nothing.
func (sched *nullSchedulerImpl) LogStatus(_ string) {
}

// EnQueueRemovals sets the list server names per serve class name for in-order removal.
func (sched *nullSchedulerImpl) EnQueueRemovals(class string, servers []string) {
	if _, ok := sched.removableServerClasses[class]; !ok {
		sched.removableServerClasses[class] = &serverRemovalQueue{
			servers: []string{},
		}
		sched.removableServerClasses[class].enqueueAll(servers)
	} else {
		sched.removableServerClasses[class].enqueueAll(servers)
	}
}
