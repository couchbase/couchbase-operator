package dataworkloads

import (
	"errors"
	"time"
)

type DataWorkloadName string

const (
	GideonDataWorkload      DataWorkloadName = "gideon"
	SiriusDataWorkload      DataWorkloadName = "sirius"
	PillowFightDataWorkload DataWorkloadName = "pillowfight"
)

var (
	ErrInvalidDataWorkloadName = errors.New("invalid data workload name provided")
)

type DataWorkloadInterface interface {
	// CreateJobs creates the jobs.Job using DataWorkloadConfig for the Data Workload.
	CreateJobs(config *DataWorkloadConfig) error

	// MarshalJobYAMLs marshals the jobs.Job struct into YAML file and saves it.
	MarshalJobYAMLs() error

	// ExecuteJobs applies the YAMLs using kubectl to start the jobs.
	ExecuteJobs() error

	// CheckJobs checks for the completion of jobs.
	CheckJobs() error

	// DeleteJobs deletes the running / completed jobs.
	DeleteJobs() error
}

func NewDataWorkload(name DataWorkloadName, namespace, resultDir string, jobDuration time.Duration) (DataWorkloadInterface, error) {
	switch name {
	case GideonDataWorkload:
		return ConfigGideonDataWorkload(namespace, resultDir), nil
	default:
		return nil, ErrInvalidDataWorkloadName
	}
}
