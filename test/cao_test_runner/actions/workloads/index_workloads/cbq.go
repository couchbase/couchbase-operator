package indexworkloads

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	cbpods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cb_pods"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/jobs"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
)

const (
	// Template.
	cbqTemplateName = "cbq"

	// Container.
	cbqContainerImage = "docker.io/sequoiatools/cbq"
	cbqContainerName  = "cbq"
)

type CBQ struct {
	Namespace          string
	FilePaths          []string
	Jobs               []*jobs.Job
	JobDuration        time.Duration
	idxWorkloadYAMLDir *fileutils.Directory
}

type CBQArgs struct {
	NodeSelector map[string]string

	Engine   string   `args:"--engine"`
	User     string   `args:"--user"`
	Password string   `args:"--password"`
	Scripts  []string `args:"--s"`

	Bucket string
}

func ConfigCBQIndexWorkload(namespace, resultDir string, jobDuration time.Duration) *CBQ {
	return &CBQ{
		Jobs:               make([]*jobs.Job, 0),
		FilePaths:          make([]string, 0),
		JobDuration:        jobDuration,
		Namespace:          namespace,
		idxWorkloadYAMLDir: fileutils.NewDirectory(filepath.Join(resultDir, "/index_workloads"), os.ModePerm),
	}
}

func (cbq *CBQ) CreateJobs(config *IndexWorkloadConfig) error {
	cbqStructs, err := populateCBQArgs(config)
	if err != nil {
		return fmt.Errorf("create jobs cbq: %w", err)
	}

	for i := range cbqStructs {
		job, err := createJob(cbqStructs[i])
		if err != nil {
			return err
		}

		cbq.Jobs = append(cbq.Jobs, job)
	}

	return nil
}

func (cbq *CBQ) MarshalJobYAMLs() error {
	for i := range cbq.Jobs {
		filePath := fmt.Sprintf("%s/job-%s-%s.%s", cbq.idxWorkloadYAMLDir.DirectoryPath,
			cbq.Jobs[i].Metadata.Name, time.Now().Format("2006-01-02-15-04-05"), "yaml")

		cbq.FilePaths = append(cbq.FilePaths, filePath)

		err := jobs.MarshalJobYAML(cbq.Jobs[i], filePath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cbq *CBQ) ExecuteJobs() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("execute jobs: %w", err)
	}

	for _, filePath := range cbq.FilePaths {
		err := executeJob(filepath.Join(dir, filePath), cbq.Namespace)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cbq *CBQ) CheckJobs() error {
	eg := errgroup.Group{}

	for _, job := range cbq.Jobs {
		jobName := job.Metadata.Name

		eg.Go(func() error {
			return jobs.CheckJobCompletion(jobName, cbq.Namespace, cbq.JobDuration, 2*time.Second, 1)
		})
	}

	err := eg.Wait()
	if err != nil {
		return fmt.Errorf("check jobs: %w", err)
	}

	return nil
}

func (cbq *CBQ) DeleteJobs() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("delete jobs: %w", err)
	}

	for _, filePath := range cbq.FilePaths {
		err := deleteJob(filepath.Join(dir, filePath), cbq.Namespace)
		if err != nil {
			return err
		}
	}

	return nil
}

func createJob(cbqArgs *CBQArgs) (*jobs.Job, error) {
	// Based on the number of queries and NumQueryPods we divide the load
	// Based on the buckets we divide the load.
	cbqJobName := "cbq-" + cbqArgs.Bucket

	cbqJob := jobs.CreateBasicJob(cbqJobName)

	cbqJob.AddTemplateName(cbqTemplateName)
	cbqJob.AddRestartPolicy(v1.RestartPolicyNever)

	for key, val := range cbqArgs.NodeSelector {
		cbqJob.AddNodeSelector(key, val)
	}

	cbqArgsList := []string{"./cbq"}

	argsList, err := workloads.BuildArgsList("args", cbqArgs)
	if err != nil {
		return nil, fmt.Errorf("create cbq job: %w", err)
	}

	cbqArgsList = append(cbqArgsList, argsList...)

	cbqJob.AddContainer(cbqContainerName, cbqContainerImage, []string{"/bin/bash", "-c", "--"}, []string{strings.TrimSpace(strings.Join(cbqArgsList, " "))})

	return cbqJob, nil
}

func executeJob(filePath, namespace string) error {
	err := kubectl.ApplyFiles(filePath).InNamespace(namespace).ExecWithoutOutputCapture()
	if err != nil {
		return fmt.Errorf("execute job %s: %w", filepath.Base(filePath), err)
	}

	return nil
}

func deleteJob(filePath, namespace string) error {
	err := kubectl.DeleteFromFiles(filePath).InNamespace(namespace).ExecWithoutOutputCapture()
	if err != nil {
		return fmt.Errorf("delete job %s: %w", filepath.Base(filePath), err)
	}

	return nil
}

// populateCBQArgs populates the CBQArgs for the workload which will be used to generate the K8S job.
// The jobs will be divided based on Buckets and distributed across the CB Query Pods.
func populateCBQArgs(config *IndexWorkloadConfig) ([]*CBQArgs, error) {
	cbClusterAuth, err := requestutils.GetCBClusterAuth("cb-example-auth", "default")
	if err != nil {
		return nil, fmt.Errorf("populate cbq args: %w", err)
	}

	var cbqArgs []*CBQArgs

	for i := range config.BucketCount {
		cbHostname := cbpods.GetCBPodHostname(config.cbQueryPods[i%len(config.cbQueryPods)], config.Namespace)

		cbqArgs = append(cbqArgs, &CBQArgs{
			NodeSelector: config.NodeSelector,
			Engine:       cbHostname,
			User:         cbClusterAuth.Username,
			Password:     cbClusterAuth.Password,
			Scripts:      config.queries[config.Buckets[i].Bucket],
			Bucket:       config.Buckets[i].Bucket,
		})
	}

	return cbqArgs, nil
}
