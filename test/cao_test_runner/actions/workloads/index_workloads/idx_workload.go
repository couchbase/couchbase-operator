package indexworkloads

import (
	"errors"
	"time"
)

type IndexWorkloadName string

const (
	RestAPIIndexWorkload  IndexWorkloadName = "restAPI"
	CBQIndexWorkload      IndexWorkloadName = "cbq"
	QueryAppIndexWorkload IndexWorkloadName = "queryApp"
)

var (
	ErrInvalidIndexWorkloadName = errors.New("invalid index workload name provided")
)

type IndexWorkloadInterface interface {
	// CreateJobs creates the jobs.Job using IndexWorkloadConfig for the Index Workload.
	CreateJobs(config *IndexWorkloadConfig) error

	// MarshalJobYAMLs marshals the jobs.Job struct into YAML file and saves it.
	MarshalJobYAMLs() error

	// ExecuteJobs applies the YAMLs using kubectl to start the jobs.
	ExecuteJobs() error

	// CheckJobs checks for the completion of jobs.
	CheckJobs() error

	// DeleteJobs deletes the running / completed jobs.
	DeleteJobs() error
}

func NewIndexWorkload(name IndexWorkloadName, namespace string, jobDuration time.Duration) (IndexWorkloadInterface, error) {
	switch name {
	case CBQIndexWorkload:
		return ConfigCBQIndexWorkload(namespace, jobDuration), nil
	default:
		return nil, ErrInvalidIndexWorkloadName
	}
}
