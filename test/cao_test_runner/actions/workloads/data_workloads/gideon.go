package dataworkloads

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	cbpods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cb_pods"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/jobs"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
	v1 "k8s.io/api/core/v1"
)

const (
	// Template.
	gideonTemplateName = "gideon_latest"

	// Container.
	gideonContainerImage = "docker.io/sequoiatools/gideon_latest"
	gideonContainerName  = "gideon-latest"
)

type Gideon struct {
	Jobs                []*jobs.Job
	FilePaths           []string
	Namespace           string
	dataWorkloadYAMLDir *fileutils.Directory
}

type GideonArgs struct {
	NodeSelector map[string]string

	Ops      string `args:"--ops"`
	Create   string `args:"--create"`
	Get      string `args:"--get"`
	Sizes    string `args:"--sizes"`
	Expire   string `args:"--expire"`
	TTL      string `args:"--ttl"`
	Hosts    string `args:"--hosts"`
	User     string `args:"--user"`
	Password string `args:"--password"`
	Bucket   string `args:"--bucket"`
}

func ConfigGideonDataWorkload(namespace, resultDir string) *Gideon {
	return &Gideon{
		Jobs:                make([]*jobs.Job, 0),
		FilePaths:           make([]string, 0),
		Namespace:           namespace,
		dataWorkloadYAMLDir: fileutils.NewDirectory(filepath.Join(resultDir, "/data_workloads"), os.ModePerm),
	}
}

func (gideon *Gideon) CreateJobs(config *DataWorkloadConfig) error {
	gideonStructs, err := populateGideonArgs(config)
	if err != nil {
		return fmt.Errorf("create jobs gideon: %w", err)
	}

	for i := range gideonStructs {
		job, err := createJob(gideonStructs[i])
		if err != nil {
			return err
		}

		gideon.Jobs = append(gideon.Jobs, job)
	}

	return nil
}

func (gideon *Gideon) MarshalJobYAMLs() error {
	for i := range gideon.Jobs {
		filePath := fmt.Sprintf("%s/job-%s-%s.%s", gideon.dataWorkloadYAMLDir.DirectoryPath,
			gideon.Jobs[i].Metadata.Name, time.Now().Format("2006-01-02-15-04-05"), "yaml")

		gideon.FilePaths = append(gideon.FilePaths, filePath)

		err := jobs.MarshalJobYAML(gideon.Jobs[i], filePath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (gideon *Gideon) ExecuteJobs() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("execute jobs: %w", err)
	}

	for _, filePath := range gideon.FilePaths {
		err := executeJob(filepath.Join(dir, filePath), gideon.Namespace)
		if err != nil {
			return err
		}
	}

	return nil
}

func (gideon *Gideon) CheckJobs() error {
	panic("gideon jobs don't support check jobs")
}

func (gideon *Gideon) DeleteJobs() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("delete jobs: %w", err)
	}

	for _, filePath := range gideon.FilePaths {
		err := deleteJob(filepath.Join(dir, filePath), gideon.Namespace)
		if err != nil {
			return err
		}
	}

	return nil
}

func createJob(gideonArgs *GideonArgs) (*jobs.Job, error) {
	gideonJobName := "gideon-" + gideonArgs.Bucket

	gideonJob := jobs.CreateBasicJob(gideonJobName)

	gideonJob.AddTemplateName(gideonTemplateName)
	gideonJob.AddRestartPolicy(v1.RestartPolicyNever)

	for key, val := range gideonArgs.NodeSelector {
		gideonJob.AddNodeSelector(key, val)
	}

	gideonArgsList := []string{"kv"}

	argsList, err := workloads.BuildArgsList("args", gideonArgs)
	if err != nil {
		return nil, fmt.Errorf("create gideon job: %w", err)
	}

	gideonArgsList = append(gideonArgsList, argsList...)

	gideonJob.AddContainer(gideonContainerName, gideonContainerImage, nil, gideonArgsList)

	return gideonJob, nil
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

func populateGideonArgs(config *DataWorkloadConfig) ([]*GideonArgs, error) {
	cbClusterAuth, err := requestutils.GetCBClusterAuth(config.CBClusterSecret, config.Namespace)
	if err != nil {
		return nil, fmt.Errorf("populate gideon args: %w", err)
	}

	var gideonArgs []*GideonArgs

	for i, bucketConfig := range config.Buckets {
		gideonArgs = append(gideonArgs, &GideonArgs{
			NodeSelector: config.NodeSelector,
			Ops:          strconv.Itoa(config.OpsRate),
			Create:       strconv.Itoa(config.Creates),
			Get:          strconv.Itoa(config.Reads),
			Sizes:        strconv.FormatInt(config.DocSize, 10),
			Expire:       strconv.Itoa(config.Expires),
			TTL:          strconv.Itoa(config.TTL),
			Hosts:        cbpods.GetCBPodHostname(config.cbDataPods[i%len(config.cbDataPods)], config.Namespace),
			User:         cbClusterAuth.Username,
			Password:     cbClusterAuth.Password,
			Bucket:       bucketConfig.Bucket,
		})
	}

	return gideonArgs, nil
}
