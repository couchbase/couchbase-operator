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
}

// NewNullScheduler returns a new null scheduler.
func NewNullScheduler(pods []*corev1.Pod, cluster *couchbasev2.CouchbaseCluster) (Scheduler, error) {
	// Add existing servers to the server list, we need this for scheduling
	// pod removal
	sched := &nullSchedulerImpl{
		serverClasses: map[string]*serverList{},
	}

	for _, class := range cluster.Spec.Servers {
		sched.serverClasses[class.Name] = &serverList{}
	}

	for _, pod := range pods {
		class, ok := pod.Labels[constants.LabelNodeConf]
		if !ok {
			return nil, fmt.Errorf("%s: pod %s does not have server class label: %w", stripeErrorHeader, pod.Name, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
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
func (sched *nullSchedulerImpl) Create(class, name, group string) (string, error) {
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: pod %s server class '%s' undefined: %w", stripeErrorHeader, name, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	sched.serverClasses[class].push(name)

	return "", nil
}

// Delete removes the largest server name from the sorted list of servers.
func (sched *nullSchedulerImpl) Delete(class string) (string, error) {
	// Select the victim server group based on population
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: server group map missing server class '%s': %w", stripeErrorHeader, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	sched.serverClasses[class].sort()

	server, err := sched.serverClasses[class].pop()
	if err != nil {
		return "", fmt.Errorf("%s: %w", nullErrorHeader, err)
	}

	return server, nil
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
func (sched *nullSchedulerImpl) LogStatus(cluster string) {
}
