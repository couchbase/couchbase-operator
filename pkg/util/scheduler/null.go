package scheduler

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// nullSchedulerImpl does nothing, used in test where scheduling is unnecessary
type nullSchedulerImpl struct {
}

// NewNullScheduler returns a new null scheduler
func NewNullScheduler() Scheduler {
	return &nullSchedulerImpl{}
}

// Create does nothing
func (sched *nullSchedulerImpl) Create(client kubernetes.Interface, cluster *api.CouchbaseCluster, pod *v1.Pod) error {
	return nil
}
