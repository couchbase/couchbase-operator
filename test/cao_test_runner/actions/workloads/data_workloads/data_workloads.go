package dataworkloads

import (
	"errors"
)

type DataWorkloadName string

const (
	GideonDataWorkload      DataWorkloadName = "gideon"
	SiriusDataWorkload      DataWorkloadName = "sirius"
	PillowFightDataWorkload DataWorkloadName = "pillowfight"
)

const (
	// YAML File Directory.
	dataWorkloadYAMLDir = "./tmp/data_workloads/"
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

	// DeleteJobs deletes the running / completed jobs.
	DeleteJobs() error
}

func NewDataWorkload(name DataWorkloadName) (DataWorkloadInterface, error) {
	switch name {
	case GideonDataWorkload:
		return ConfigGideonDataWorkload(), nil
	default:
		return nil, ErrInvalidDataWorkloadName
	}
}

// ValidateDataWorkloadName validates the DataWorkloadName for the supported Data Workloads.
func ValidateDataWorkloadName(name DataWorkloadName) bool {
	switch name {
	case GideonDataWorkload, SiriusDataWorkload, PillowFightDataWorkload:
		return true
	default:
		return false
	}
}
