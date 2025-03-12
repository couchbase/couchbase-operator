package queryworkloads

import (
	"errors"
	"time"
)

type QueryWorkloadName string

const (
	QueryAppWorkload QueryWorkloadName = "queryApp"
)

var (
	ErrInvalidQueryWorkloadName = errors.New("invalid query workload name provided")
)

type QueryWorkloadInterface interface {
	// CreateJobs creates the jobs.Job using QueryWorkloadConfig for the Query Workload.
	CreateJobs(config *QueryWorkloadConfig) error

	// MarshalJobYAMLs marshals the jobs.Job struct into YAML file and saves it.
	MarshalJobYAMLs() error

	// ExecuteJobs applies the YAMLs using kubectl to start the jobs.
	ExecuteJobs() error

	// CheckJobs checks for the completion of jobs.
	CheckJobs() error

	// DeleteJobs deletes the running / completed jobs.
	DeleteJobs() error
}

func NewQueryWorkload(name QueryWorkloadName, namespace, resultDir string, jobDuration time.Duration) (QueryWorkloadInterface, error) {
	switch name {
	case QueryAppWorkload:
		return ConfigQueryAppWorkload(namespace, resultDir, jobDuration), nil
	default:
		return nil, ErrInvalidQueryWorkloadName
	}
}
