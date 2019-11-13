package scheduler

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
)

const (
	nullErrorHeader = "null scheduler"
)

// nullSchedulerImpl does no scheduling except enforcing ordering of
// server deletion
type nullSchedulerImpl struct {
	// A plain list of servers per server class
	serverClasses map[string]*serverList
}

// NewNullScheduler returns a new null scheduler
func NewNullScheduler(podGetter PodGetter, cluster *couchbasev2.CouchbaseCluster) (Scheduler, error) {
	// Add existing servers to the server list, we need this for scheduling
	// pod removal
	sched := &nullSchedulerImpl{
		serverClasses: map[string]*serverList{},
	}
	for _, class := range cluster.Spec.Servers {
		sched.serverClasses[class.Name] = &serverList{}
	}

	for _, pod := range podGetter.Get() {
		class, ok := pod.Labels[constants.LabelNodeConf]
		if !ok {
			return nil, fmt.Errorf("%s: pod %s does not have server class label", stripeErrorHeader, pod.Name)
		}
		// Class deleted, ignore the pod
		if _, ok := sched.serverClasses[class]; !ok {
			continue
		}
		sched.serverClasses[class].push(pod.Name)
	}

	return sched, nil
}

// Create does nothing
func (sched *nullSchedulerImpl) Create(class, name, group string) (string, error) {
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: pod %s server class '%s' undefined", stripeErrorHeader, name, class)
	}

	sched.serverClasses[class].push(name)

	return "", nil
}

// Delete removes the largest server name from the sorted list of servers
func (sched *nullSchedulerImpl) Delete(class string) (string, error) {
	// Select the victim server group based on population
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: server group map missing server class '%s'", stripeErrorHeader, class)
	}

	sched.serverClasses[class].sort()
	server, err := sched.serverClasses[class].pop()
	if err != nil {
		return "", fmt.Errorf("%s: %v", nullErrorHeader, err)
	}

	return server, nil
}

// Upgrade removes a node from the scheduler as it's an upgrade target.
func (sched *nullSchedulerImpl) Upgrade(class, name string) error {
	return sched.serverClasses[class].del(name)
}

// LogStatus returns nothing
func (sched *nullSchedulerImpl) LogStatus(cluster string) {
}
