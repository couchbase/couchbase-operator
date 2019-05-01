package scheduler

import (
	"io"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
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
	Create(pod *v1.Pod) error

	// Delete selects a server name to delete from a specific server class.
	Delete(class string) (string, error)

	// Upgrade removes a node from the scheduler as it's an upgrade target.
	Upgrade(class, name string) error

	// LogStatus writes out the status to a writer.
	LogStatus(io.Writer) error
}

// New is a factory method to return the correct scheduler type for
// the cluster configuration
func New(client kubernetes.Interface, cluster *couchbasev2.CouchbaseCluster) (Scheduler, error) {
	podGetter := NewK8SPodGetter(client, cluster)

	// At present we only support a scheduler which evenly stripes servers
	// across server groups on a per server class basis
	if cluster.Spec.ServerGroupsEnabled() {
		return NewStripeScheduler(podGetter, cluster)
	}

	// The default does virtually nothing
	return NewNullScheduler(podGetter, cluster)
}
