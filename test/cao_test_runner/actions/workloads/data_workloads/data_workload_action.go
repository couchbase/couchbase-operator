package dataworkloads

import (
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
	cbpodfilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cb_pod_filter"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/sirupsen/logrus"
)

type DataWorkloadConfig struct {
	Name        string        `yaml:"name" caoCli:"required"`
	SpecPath    string        `yaml:"specPath"`
	PreRunWait  time.Duration `yaml:"preRunWait"`
	PostRunWait time.Duration `yaml:"postRunWait"`
	RunDuration time.Duration `yaml:"runDuration" caoCli:"required"`

	// CBPodFilter will be used to filter the CB pod with whom we will communicate.
	CBPodFilter cbpodfilter.CBPodFilter `yaml:"cbPodFilter"`

	FilteredPods []string `yaml:"-"`

	// NodeSelector contains the labels key and value to match for k8s node selection.
	NodeSelector map[string]string `yaml:"nodeSelector"`

	// CheckJobCompletion checks for completion of job. If job completed (e.g. before RunDuration) then we can delete the job.
	CheckJobCompletion bool `yaml:"checkJobCompletion"`

	DataWorkloadName DataWorkloadName `yaml:"dataWorkloadName" caoCli:"required"`

	OpsRate    int   `yaml:"opsRate"`    // Gideon
	RangeStart int64 `yaml:"rangeStart"` // Sirius
	RangeEnd   int64 `yaml:"rangeEnd"`   // Sirius
	DocSize    int64 `yaml:"docSize"`    // Gideon, Sirius

	BucketCount int            `yaml:"bucketCount" caoCli:"required"` // Gideon, Sirius
	Buckets     []BucketConfig `yaml:"buckets"`                       // Gideon, Sirius

	Creates int `yaml:"creates"` // For Gideon: % of OpsRate e.g. 80; For Sirius: Set it to 1 to hit /create API.
	Reads   int `yaml:"reads"`   // For Gideon: % of OpsRate e.g. 50; For Sirius: Set it to 1 to hit /read API.
	Updates int `yaml:"updates"` // For Gideon: % of OpsRate e.g. 50; For Sirius: Set it to 1 to hit /update API.
	Deletes int `yaml:"deletes"` // For Gideon: % of OpsRate e.g. 10; For Sirius: Set it to 1 to hit /delete API.
	Expires int `yaml:"expires"` // For Gideon: % of OpsRate e.g. 10;
	TTL     int `yaml:"ttl"`     // For Gideon: % of OpsRate e.g. 10;
}

type BucketConfig struct {
	Bucket string        `yaml:"bucket" caoCli:"required"`
	Scopes []ScopeConfig `yaml:"scopes"`
}

type ScopeConfig struct {
	Scope       string   `yaml:"scope" caoCli:"required"`
	Collections []string `yaml:"collections"`
}

func NewDataWorkloadConfig(config interface{}) (actions.Action, error) {
	if config == nil {
		return nil, fmt.Errorf("new data workload config: %w", workloads.ErrConfigWorkload)
	}

	dataWorkloadConfig, ok := config.(*DataWorkloadConfig)
	if !ok {
		return nil, fmt.Errorf("new data workload config: %w", workloads.ErrWorkloadDecode)
	}

	if !ValidateDataWorkloadName(dataWorkloadConfig.DataWorkloadName) {
		return nil, fmt.Errorf("new data workload config: %w", ErrInvalidDataWorkloadName)
	}

	err := ValidateDataWorkloadConfig(dataWorkloadConfig)
	if err != nil {
		return nil, fmt.Errorf("new data workload config: %w", err)
	}

	return &DataWorkload{
		desc:       "run data workloads on CB cluster in K8S",
		yamlConfig: dataWorkloadConfig,
	}, nil
}

type DataWorkload struct {
	desc       string
	yamlConfig interface{}
}

func (d *DataWorkload) Describe() string {
	return d.desc
}

func (d *DataWorkload) Do(_ *context.Context, _ interface{}) error {
	workloadConfig, _ := d.yamlConfig.(*DataWorkloadConfig)

	// Introduce a wait before starting to run the workload.
	if workloadConfig.PreRunWait != 0 {
		logrus.Infof("Waiting for %d seconds before starting workload: %s", workloadConfig.PreRunWait/time.Second, workloadConfig.Name)
		time.Sleep(workloadConfig.PreRunWait)
	}

	var err error

	// Filtering the Pod to be used as the Host
	workloadConfig.FilteredPods, err = workloadConfig.CBPodFilter.FilterPods()
	if err != nil {
		return fmt.Errorf("data workload: %w", err)
	}

	logrus.Infof("filtered pod: %v", workloadConfig.FilteredPods)

	dataWorkload, err := NewDataWorkload(workloadConfig.DataWorkloadName)
	if err != nil {
		return fmt.Errorf("data workload: %w", err)
	}

	// Create the jobs
	err = dataWorkload.CreateJobs(workloadConfig)
	if err != nil {
		return fmt.Errorf("data workload: %w", err)
	}

	// Marshal the jobs into YAML files.
	err = dataWorkload.MarshalJobYAMLs()
	if err != nil {
		return fmt.Errorf("data workload: %w", err)
	}

	// Apply the YAML of the workload
	err = dataWorkload.ExecuteJobs()
	if err != nil {
		return fmt.Errorf("data workload: %w", err)
	}

	logrus.Infof("Started workload: %s", workloadConfig.Name)

	// Execute the workload for duration = workloadConfig.RunDuration.
	if workloadConfig.RunDuration != 0 {
		logrus.Infof("Running `%s` for %d minute(s)", workloadConfig.Name, workloadConfig.RunDuration/time.Minute)

		// If workloadConfig.CheckJobCompletion is true, then the workload (job) is removed as soon as it is completed.
		// Else, the workload (job) will run for workloadConfig.RunDuration duration even if it has not been completed.
		if workloadConfig.CheckJobCompletion {
			// TODO: get the name of the job and then check if it has been completed or not.
			// checkJobCompletion()
			// will have to implement something like error group having context with deadline. Since this function shall not be blocking
			logrus.Error("workloadConfig.CheckJobCompletion to be implemented")
		} else {
			time.Sleep(workloadConfig.RunDuration)
		}
	} else {
		logrus.Infof("Running `%s`", workloadConfig.Name)
		return nil
	}

	// After the workload duration is over, we delete the workload
	err = dataWorkload.DeleteJobs()
	if err != nil {
		return fmt.Errorf("data workload: %w", err)
	}

	logrus.Infof("Deleted workload: %s", workloadConfig.Name)

	// Introduce a wait after the workload gets completed.
	if workloadConfig.PostRunWait != 0 {
		logrus.Infof("Waiting for %d seconds after completing workload: %s", workloadConfig.PostRunWait/time.Second, workloadConfig.Name)
		time.Sleep(workloadConfig.PostRunWait)
	}

	return nil
}

func (d *DataWorkload) Config() interface{} {
	return d.yamlConfig
}

func (d *DataWorkload) Checks(_ *context.Context, _ interface{}, _ string) error {
	return nil
}
