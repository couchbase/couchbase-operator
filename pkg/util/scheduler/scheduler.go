package scheduler

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	corev1 "k8s.io/api/core/v1"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("scheduler")

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

	// LogStatus writes out the status to a writer.
	LogStatus(cluster string)
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
