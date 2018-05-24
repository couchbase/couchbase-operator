package scheduler

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// Scheduler is an abstraction for something that is able to inspect the cluster
// and make intelligent decisions about which server groups to add pods to or
// remove them from in a deterministic fashion
type Scheduler interface {
	// Create examines the cluster state and cluster specification in order to
	// schedule the creation of a new pod.  The pod parameter is mutated to
	// contain the necessary label selectors to facilitate correct placement.
	Create(client kubernetes.Interface, cluster *api.CouchbaseCluster, pod *v1.Pod) error
}

// New is a factory method to return the correct scheduler type for
// the cluster configuration
func New(cluster *api.CouchbaseCluster) Scheduler {
	if cluster.Spec.ServerGroupsEnabled() {
		return NewStripeScheduler()
	}
	return NewNullScheduler()
}
