package job

import (
	"k8s.io/client-go/kubernetes"
)

type JobType string

const (
	NodeInit     JobType = "node-init"
	ClusterInit  JobType = "cluster-init"
	BucketCreate JobType = "bucket-create"
)

type JobPhase string

const (
	Pending   JobPhase = "pending"
	Completed JobPhase = "completed"
	Failed    JobPhase = "failed"
)

type JobStatus struct {
	// Type of job being run
	Type JobType

	// Phase job is currently
	Phase JobPhase
}

func (j JobType) Str() string {
	return string(j)
}

// provides context when running jobs
// regarding which cli to use and
// the namespace in which they should be run
type KubeContext struct {
	KubeCli   kubernetes.Interface
	Namespace string
}
