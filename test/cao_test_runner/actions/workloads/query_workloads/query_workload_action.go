package queryworkloads

import (
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
	cbpodfilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cb_pod_filter"
	"github.com/sirupsen/logrus"
)

type QueryWorkloadConfig struct {
	Name            string `yaml:"name" caoCli:"required"`
	Namespace       string `yaml:"namespace" caoCli:"required"`
	CBClusterSecret string `yaml:"cbClusterSecret" caoCli:"required"`

	RunDuration time.Duration `yaml:"runDuration" caoCli:"required"`
	PreRunWait  time.Duration `yaml:"preRunWait"`
	PostRunWait time.Duration `yaml:"postRunWait"`

	// CheckJobCompletion checks for completion of job. If job completed (before RunDuration) then we can delete the job.
	CheckJobCompletion bool `yaml:"checkJobCompletion"`

	QueryWorkloadName QueryWorkloadName `yaml:"queryWorkloadName" caoCli:"required"`

	NumQueryPods int `yaml:"numQueryPods" caoCli:"required"`

	// NodeSelector contains the labels key and value to match for k8s node selection.
	NodeSelector map[string]string `yaml:"nodeSelector"`

	BucketCount int `yaml:"bucketCount" caoCli:"required"`
	// Buckets contains the bucket(s) configuration for the workload.
	Buckets []*workloads.BucketConfig `yaml:"buckets"`

	QueryConfig QueryConfig `yaml:"queryConfig"`

	// Internal Parameters
	cbQueryPods []string `yaml:"-"`
}

type QueryConfig struct {
	NumQueries       int    `yaml:"numQueries" caoCli:"required"`    // QueryApp
	QueryDuration    int    `yaml:"queryDuration" caoCli:"required"` // QueryApp
	Threads          int    `yaml:"threads"`                         // QueryApp
	QueryFile        string `yaml:"queryFile"`                       // QueryApp
	AnalyticsQueries bool   `yaml:"analyticsQueries"`                // QueryApp
	QueryTimeout     int    `yaml:"queryTimeout"`                    // QueryApp
	ScanConsistency  string `yaml:"scanConsistency"`                 // QueryApp
	Dataset          string `yaml:"dataset"`                         // QueryApp
	PrintDuration    int    `yaml:"printDuration"`                   // QueryApp
}

func NewQueryWorkloadConfig(config interface{}) (actions.Action, error) {
	if config == nil {
		return nil, fmt.Errorf("new query workload config: %w", workloads.ErrConfigWorkload)
	}

	queryWorkloadConfig, ok := config.(*QueryWorkloadConfig)
	if !ok {
		return nil, fmt.Errorf("new query workload config: %w", workloads.ErrWorkloadDecode)
	}

	if !ValidateQueryWorkloadName(queryWorkloadConfig.QueryWorkloadName) {
		return nil, fmt.Errorf("new query workload config: %w", ErrInvalidQueryWorkloadName)
	}

	err := ValidateQueryWorkloadConfig(queryWorkloadConfig)
	if err != nil {
		return nil, fmt.Errorf("new query workload config: %w", err)
	}

	return &QueryWorkload{
		desc:       "run query workloads on CB cluster in K8S",
		yamlConfig: queryWorkloadConfig,
	}, nil
}

type QueryWorkload struct {
	desc       string
	yamlConfig interface{}
}

func (d *QueryWorkload) Describe() string {
	return d.desc
}

func (d *QueryWorkload) Do(_ *context.Context, _ interface{}) error {
	queryConfig, _ := d.yamlConfig.(*QueryWorkloadConfig)

	// Introduce a wait before starting to run the workload.
	if queryConfig.PreRunWait != 0 {
		logrus.Infof("Waiting for %s before starting workload: %s", queryConfig.PreRunWait.String(), queryConfig.Name)
		time.Sleep(queryConfig.PreRunWait)
	}

	var err error

	logrus.Infof("Starting workload: %s", queryConfig.Name)

	// Filtering the CB pod with Query service.
	queryPodFilter := cbpodfilter.CBPodFilter{
		FilterType:        "services",
		SelectionStrategy: "sorted",
		Services:          []string{"query"},
		ServicesNotStrict: true,
		Count:             queryConfig.NumQueryPods,
	}

	queryConfig.cbQueryPods, err = queryPodFilter.FilterPods()
	if err != nil {
		return fmt.Errorf("query workload: %w", err)
	}

	queryWorkload, err := NewQueryWorkload(queryConfig.QueryWorkloadName, queryConfig.Namespace, queryConfig.RunDuration)
	if err != nil {
		return fmt.Errorf("query workload: %w", err)
	}

	// Create the jobs
	err = queryWorkload.CreateJobs(queryConfig)
	if err != nil {
		return fmt.Errorf("query workload: %w", err)
	}

	// Marshal the jobs into YAML files.
	err = queryWorkload.MarshalJobYAMLs()
	if err != nil {
		return fmt.Errorf("query workload: %w", err)
	}

	// Apply the YAML of the workload
	err = queryWorkload.ExecuteJobs()
	if err != nil {
		return fmt.Errorf("query workload: %w", err)
	}

	// Execute the workload for duration = queryConfig.RunDuration.
	if queryConfig.RunDuration != 0 {
		logrus.Infof("Running `%s` for %s", queryConfig.Name, queryConfig.RunDuration.String())

		// If queryConfig.CheckJobCompletion is true, then the workload (job) is removed as soon as it is completed.
		// Else, the workload (job) will run for queryConfig.RunDuration duration even if it has not been completed.
		if queryConfig.CheckJobCompletion {
			err := queryWorkload.CheckJobs()
			if err != nil {
				return fmt.Errorf("query workload: %w", err)
			}

			logrus.Infof("Job `%s` completed", queryConfig.Name)
		} else {
			time.Sleep(queryConfig.RunDuration)
		}
	} else {
		logrus.Infof("Running `%s`", queryConfig.Name)

		if queryConfig.CheckJobCompletion {
			err := queryWorkload.CheckJobs()
			if err != nil {
				return fmt.Errorf("query workload: %w", err)
			}

			logrus.Infof("Job `%s` completed", queryConfig.Name)
		}

		return nil
	}

	// After the workload duration is over, we delete the workload
	err = queryWorkload.DeleteJobs()
	if err != nil {
		return fmt.Errorf("query workload: %w", err)
	}

	logrus.Infof("Deleted workload: %s", queryConfig.Name)

	// Introduce a wait after the workload gets completed.
	if queryConfig.PostRunWait != 0 {
		logrus.Infof("Waiting for %s after completing workload: %s", queryConfig.PostRunWait.String(), queryConfig.Name)
		time.Sleep(queryConfig.PostRunWait)
	}

	return nil
}

func (d *QueryWorkload) Config() interface{} {
	return d.yamlConfig
}

func (d *QueryWorkload) Checks(_ *context.Context, _ interface{}, _ string) error {
	return nil
}
