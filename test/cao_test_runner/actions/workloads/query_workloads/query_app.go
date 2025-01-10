package queryworkloads

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
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
)

const (
	// Template.
	queryAppTemplateName = "queryapp"

	// Container.
	queryAppContainerImage = "docker.io/sequoiatools/queryapp:capella"
	queryAppContainerName  = "queryapp"
)

type QueryApp struct {
	Namespace            string
	FilePaths            []string
	Jobs                 []*jobs.Job
	JobDuration          time.Duration
	queryWorkloadYAMLDir *fileutils.Directory
}

type QueryAppArgs struct {
	NodeSelector map[string]string

	ServerIP        string `args:"--server_ip"`
	Username        string `args:"--username"`
	Password        string `args:"--password"`
	Port            string `args:"--port"`
	Bucket          string `args:"--bucket"`
	QueryCount      string `args:"--querycount"`
	Duration        string `args:"--duration"`
	Threads         string `args:"--threads"`
	QueryFile       string `args:"--query_file"`
	N1QL            string `args:"--n1ql"`
	QueryTimeout    string `args:"--query_timeout"`
	ScanConsistency string `args:"--scan_consistency"`
	Dataset         string `args:"--dataset"`
	BucketNames     string `args:"--bucket_names"`
	PrintDuration   string `args:"--print_duration"`
}

func ConfigQueryAppWorkload(namespace string, jobDuration time.Duration) *QueryApp {
	return &QueryApp{
		Jobs:        make([]*jobs.Job, 0),
		FilePaths:   make([]string, 0),
		JobDuration: jobDuration,
		Namespace:   namespace,
	}
}

func (qa *QueryApp) CreateJobs(config *QueryWorkloadConfig) error {
	queryAppArgs, err := populateQueryAppArgs(config)
	if err != nil {
		return fmt.Errorf("create jobs queryapp: %w", err)
	}

	for i := range queryAppArgs {
		job, err := createQueryAppJob(queryAppArgs[i])
		if err != nil {
			return err
		}

		qa.Jobs = append(qa.Jobs, job)
	}

	return nil
}

func (qa *QueryApp) MarshalJobYAMLs() error {
	for i := range qa.Jobs {
		filePath := fmt.Sprintf("%s/job-%s-%s.%s", qa.queryWorkloadYAMLDir.DirectoryPath,
			qa.Jobs[i].Metadata.Name, time.Now().Format("2006-01-02-15-04-05"), "yaml")

		qa.FilePaths = append(qa.FilePaths, filePath)

		err := jobs.MarshalJobYAML(qa.Jobs[i], filePath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (qa *QueryApp) ExecuteJobs() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("execute jobs: %w", err)
	}

	for _, filePath := range qa.FilePaths {
		err := executeJob(filepath.Join(dir, filePath), qa.Namespace)
		if err != nil {
			return err
		}
	}

	return nil
}

func (qa *QueryApp) CheckJobs() error {
	eg := errgroup.Group{}

	for _, job := range qa.Jobs {
		jobName := job.Metadata.Name

		eg.Go(func() error {
			return jobs.CheckJobCompletion(jobName, qa.Namespace, qa.JobDuration, 2*time.Second, 1)
		})
	}

	err := eg.Wait()
	if err != nil {
		return fmt.Errorf("check jobs: %w", err)
	}

	return nil
}

func (qa *QueryApp) DeleteJobs() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("delete jobs: %w", err)
	}

	for _, filePath := range qa.FilePaths {
		err := deleteJob(filepath.Join(dir, filePath), qa.Namespace)
		if err != nil {
			return err
		}
	}

	return nil
}

func createQueryAppJob(queryAppArgs *QueryAppArgs) (*jobs.Job, error) {
	queryAppJobName := "queryapp-" + queryAppArgs.Bucket

	queryAppJob := jobs.CreateBasicJob(queryAppJobName)

	queryAppJob.AddTemplateName(queryAppTemplateName)
	queryAppJob.AddRestartPolicy(v1.RestartPolicyNever)

	for key, val := range queryAppArgs.NodeSelector {
		queryAppJob.AddNodeSelector(key, val)
	}

	queryAppArgsList := []string{"-J-Xms256m", "-J-Xmx512m", "-J-cp", "/AnalyticsQueryApp/Couchbase-Java-Client-2.7.21/*", "/AnalyticsQueryApp/Query/load_queries.py"}

	argsList, err := workloads.BuildArgsList("args", queryAppArgs)
	if err != nil {
		return nil, fmt.Errorf("create queryapp job: %w", err)
	}

	queryAppArgsList = append(queryAppArgsList, argsList...)

	queryAppJob.AddContainer(queryAppContainerName, queryAppContainerImage, nil, queryAppArgsList)

	return queryAppJob, nil
}

// populateQueryAppArgs populates the QueryAppArgs for the workload which will be used to generate the K8S job.
// The jobs will be divided based on Buckets and distributed across the CB Query Pods.
func populateQueryAppArgs(config *QueryWorkloadConfig) ([]*QueryAppArgs, error) {
	cbClusterAuth, err := requestutils.GetCBClusterAuth(config.CBClusterSecret, config.Namespace)
	if err != nil {
		return nil, fmt.Errorf("populate queryapp args: %w", err)
	}

	var queryAppArgs []*QueryAppArgs

	for i := range config.BucketCount {
		cbHostname := cbpods.GetCBPodHostname(config.cbQueryPods[i%len(config.cbQueryPods)], config.Namespace)

		queryAppArgs = append(queryAppArgs, &QueryAppArgs{
			NodeSelector:    config.NodeSelector,
			ServerIP:        cbHostname,
			Username:        cbClusterAuth.Username,
			Password:        cbClusterAuth.Password,
			Port:            "8093",
			Bucket:          config.Buckets[i].Bucket,
			QueryCount:      strconv.Itoa(config.QueryConfig.NumQueries),
			Duration:        strconv.Itoa(config.QueryConfig.QueryDuration),
			Threads:         strconv.Itoa(config.QueryConfig.Threads),
			QueryFile:       config.QueryConfig.QueryFile,
			N1QL:            strconv.FormatBool(!config.QueryConfig.AnalyticsQueries),
			QueryTimeout:    strconv.Itoa(config.QueryConfig.QueryTimeout),
			ScanConsistency: config.QueryConfig.ScanConsistency,
			Dataset:         config.QueryConfig.Dataset,
			BucketNames:     fmt.Sprintf("[%s]", config.Buckets[i].Bucket),
			PrintDuration:   strconv.Itoa(config.QueryConfig.PrintDuration),
		})
	}

	return queryAppArgs, nil
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
