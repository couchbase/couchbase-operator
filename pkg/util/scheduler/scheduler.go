package scheduler

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	corev1 "k8s.io/api/core/v1"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("scheduler")

// Move is the movement of a member as a result of a rescheduling operation.
type Move struct {
	// Name is the name of the member.
	Name string

	// From is the source server group.
	From string

	// To is the destination server group.
	To string
}

// Scheduler is an abstraction for something that is able to inspect the cluster
// and make intelligent decisions about which server groups to add pods to or
// remove them from in a deterministic fashion.
type Scheduler interface {
	// Create examines the cluster state and cluster specification in order to
	// schedule the creation of a new pod.  The pod parameter is mutated to
	// contain the necessary label selectors to facilitate correct placement.
	Create(class, name, group string) (string, error)

	// Delete selects a server name to delete from a specific server class.
	Delete(class string) (string, error)

	// Upgrade removes a node from the scheduler as it's an upgrade target.
	Upgrade(class, name string) error

	// Reschedule looks at the current state, and if it doesn't match that
	// requested when the scheduler was initialized, return a set of mutually
	// exclusive moves to get us back into a conforming state.
	Reschedule() ([]Move, error)

	// RescheduleUnschedulableOnly performs a simplified reschedule that only moves
	// pods from unschedulable server groups (removed from cluster spec) to valid
	// server groups. This is used in unstable cluster mode where full rebalancing
	// is not desired.
	RescheduleUnschedulableOnly() ([]Move, error)

	// LogStatus writes out the status to a writer.
	LogStatus(cluster string)

	// EnQueueRemovals enqueues the group of servers(cb nodes/pods) for prioritised
	// removal ready for Delete() action
	EnQueueRemovals(class string, servers []string)

	// AvoidGroups is used to avoid certain groups when scheduling. Avoidance is best
	// effort and may not be possible in all cases.
	AvoidGroups(groups ...string)
}

// New is a factory method to return the correct scheduler type for
// the cluster configuration.
func New(pods []*corev1.Pod, cluster *couchbasev2.CouchbaseCluster) (Scheduler, error) {
	// At present we only support a scheduler which evenly stripes servers
	// across server groups on a per server class basis
	if cluster.Spec.ServerGroupsEnabled() {
		return NewStripeScheduler(pods, cluster)
	}

	// The default does virtually nothing
	return NewNullScheduler(pods, cluster)
}
