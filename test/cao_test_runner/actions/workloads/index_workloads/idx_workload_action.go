package indexworkloads

import (
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
	cbpodfilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cb_pod_filter"
	"github.com/sirupsen/logrus"
)

type IndexWorkloadConfig struct {
	Name            string `yaml:"name" caoCli:"required"`
	SpecPath        string `yaml:"specPath"`
	Namespace       string `yaml:"namespace" caoCli:"required"`
	CBClusterSecret string `yaml:"cbClusterSecret" caoCli:"required"`

	RunDuration time.Duration `yaml:"runDuration" caoCli:"required"`
	PreRunWait  time.Duration `yaml:"preRunWait"`
	PostRunWait time.Duration `yaml:"postRunWait"`

	// CheckJobCompletion checks for completion of job. If job completed (before RunDuration) then we can delete the job.
	CheckJobCompletion bool `yaml:"checkJobCompletion"`

	IndexWorkloadName IndexWorkloadName `yaml:"indexWorkloadName" caoCli:"required"`

	NumQueryPods int `yaml:"numQueryPods" caoCli:"required"`

	// NodeSelector contains the labels key and value to match for k8s node selection.
	NodeSelector map[string]string `yaml:"nodeSelector"`

	BucketCount int `yaml:"bucketCount" caoCli:"required"`
	// Buckets contains the bucket(s) configuration for the workload.
	Buckets []*workloads.BucketConfig `yaml:"buckets"`

	IndexConfig IndexConfig `yaml:"indexConfig"`

	// Internal Parameters
	queries     map[string][]string `yaml:"-"` // Maps the queries for each bucket. map[bucketName][]{index queries}
	cbQueryPods []string            `yaml:"-"`
}

type IndexConfig struct {
	NumIndexes    int  `yaml:"numIndexes"`    // CBQ; per Bucket per Scope per Collection
	PrimaryIndex  bool `yaml:"primaryIndex"`  // CBQ
	IndexReplicas int  `yaml:"indexReplicas"` // CBQ
}

func NewIndexWorkloadConfig(config interface{}) (actions.Action, error) {
	if config == nil {
		return nil, fmt.Errorf("new index workload config: %w", workloads.ErrConfigWorkload)
	}

	indexWorkloadConfig, ok := config.(*IndexWorkloadConfig)
	if !ok {
		return nil, fmt.Errorf("new index workload config: %w", workloads.ErrWorkloadDecode)
	}

	if !ValidateIndexWorkloadName(indexWorkloadConfig.IndexWorkloadName) {
		return nil, fmt.Errorf("new index workload config: %w", ErrInvalidIndexWorkloadName)
	}

	err := ValidateIndexWorkloadConfig(indexWorkloadConfig)
	if err != nil {
		return nil, fmt.Errorf("new index workload config: %w", err)
	}

	return &IndexWorkload{
		desc:       "run index workloads on CB cluster in K8S",
		yamlConfig: indexWorkloadConfig,
	}, nil
}

type IndexWorkload struct {
	desc       string
	yamlConfig interface{}
}

func (d *IndexWorkload) Describe() string {
	return d.desc
}

func (d *IndexWorkload) Do(_ *context.Context, _ interface{}) error {
	workloadConfig, _ := d.yamlConfig.(*IndexWorkloadConfig)

	// Introduce a wait before starting to run the workload.
	if workloadConfig.PreRunWait != 0 {
		logrus.Infof("Waiting for %s before starting workload: %s", workloadConfig.PreRunWait.String(), workloadConfig.Name)
		time.Sleep(workloadConfig.PreRunWait)
	}

	var err error

	logrus.Infof("Starting workload: %s", workloadConfig.Name)

	// Filtering the CB pod with Query service.
	queryPodFilter := cbpodfilter.CBPodFilter{
		FilterType:        "services",
		SelectionStrategy: "sorted",
		Services:          []string{"query"},
		ServicesNotStrict: true,
		Count:             workloadConfig.NumQueryPods,
	}

	workloadConfig.cbQueryPods, err = queryPodFilter.FilterPods()
	if err != nil {
		return fmt.Errorf("index workload: %w", err)
	}

	indexWorkload, err := NewIndexWorkload(workloadConfig.IndexWorkloadName, workloadConfig.Namespace, workloadConfig.RunDuration)
	if err != nil {
		return fmt.Errorf("index workload: %w", err)
	}

	if workloadConfig.IndexConfig.PrimaryIndex {
		workloadConfig.queries = generateQueryForPrimaryIdx(workloadConfig)
	} else {
		workloadConfig.queries = generateQueryForIdx(workloadConfig)
	}

	// Create the jobs
	err = indexWorkload.CreateJobs(workloadConfig)
	if err != nil {
		return fmt.Errorf("index workload: %w", err)
	}

	// Marshal the jobs into YAML files.
	err = indexWorkload.MarshalJobYAMLs()
	if err != nil {
		return fmt.Errorf("index workload: %w", err)
	}

	// Apply the YAML of the workload
	err = indexWorkload.ExecuteJobs()
	if err != nil {
		return fmt.Errorf("index workload: %w", err)
	}

	// Execute the workload for duration = workloadConfig.RunDuration.
	if workloadConfig.RunDuration != 0 {
		logrus.Infof("Running `%s` for %s", workloadConfig.Name, workloadConfig.RunDuration.String())

		// If workloadConfig.CheckJobCompletion is true, then the workload (job) is removed as soon as it is completed.
		// Else, the workload (job) will run for workloadConfig.RunDuration duration even if it has not been completed.
		if workloadConfig.CheckJobCompletion {
			err := indexWorkload.CheckJobs()
			if err != nil {
				return fmt.Errorf("index workload: %w", err)
			}

			logrus.Infof("Job `%s` completed", workloadConfig.Name)
		} else {
			time.Sleep(workloadConfig.RunDuration)
		}
	} else {
		logrus.Infof("Running `%s`", workloadConfig.Name)

		if workloadConfig.CheckJobCompletion {
			err := indexWorkload.CheckJobs()
			if err != nil {
				return fmt.Errorf("index workload: %w", err)
			}

			logrus.Infof("Job `%s` completed", workloadConfig.Name)
		}

		return nil
	}

	// After the workload duration is over, we delete the workload
	err = indexWorkload.DeleteJobs()
	if err != nil {
		return fmt.Errorf("index workload: %w", err)
	}

	logrus.Infof("Deleted workload: %s", workloadConfig.Name)

	// Introduce a wait after the workload gets completed.
	if workloadConfig.PostRunWait != 0 {
		logrus.Infof("Waiting for %s after completing workload: %s", workloadConfig.PostRunWait.String(), workloadConfig.Name)
		time.Sleep(workloadConfig.PostRunWait)
	}

	return nil
}

func (d *IndexWorkload) Config() interface{} {
	return d.yamlConfig
}

func (d *IndexWorkload) Checks(_ *context.Context, _ interface{}, _ string) error {
	return nil
}
