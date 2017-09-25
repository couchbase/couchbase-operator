/*
	Job package contains defintions of jobs that can be
	dispatched against a pod.
*/
package job

import (
	"fmt"
	"github.com/couchbaselabs/couchbase-operator/pkg/spec"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
)

const (
	couchbaseCliImageName = "couchbase/couchbase-cli"
	couchbaseCliExecPath  = "/couchbase-cli/couchbase-cli"
)

// JobRunner is the interface which all jobs implement
// to provide a common interface to the scheduler
type JobRunner interface {
	Run(ctx KubeContext) error
	GetType() JobType
	GetTarget() string
}

// node init job initializes a node
type NodeInitJob struct {
	Cluster    string
	Username   string
	Password   string
	IndexPath  string
	DataPath   string
	name       JobType
	target     string
	requires   []string
	cliVersion string
}

// Creates new InitJob according to params provided in
// the cluster spec
func NewNodeInitJob(podTarget string, cluster *spec.CouchbaseCluster) *NodeInitJob {

	settings := cluster.Spec.ClusterSettings

	return &NodeInitJob{
		Cluster:    podTarget,
		Username:   settings.AdminUsername,
		Password:   settings.AdminPassword,
		IndexPath:  "/opt/couchbase/var/lib/couchbase/data",
		DataPath:   "/opt/couchbase/var/lib/couchbase/data",
		name:       NodeInit,
		target:     podTarget,
		requires:   []string{},
		cliVersion: "latest",
	}
}

// Generates batchJob config with container information
func (j *NodeInitJob) config() *batchv1.Job {

	container := &v1.Container{
		Name:  j.name.Str(),
		Image: cliVersionedImage(j.cliVersion),
		Command: []string{couchbaseCliExecPath,
			j.name.Str(),
			"-c", j.Cluster,
			"-u", j.Username,
			"-p", j.Password,
			"--node-init-data-path", j.IndexPath,
			"--node-init-index-path", j.DataPath},
		ImagePullPolicy: v1.PullNever,
	}

	return createJobSpec(container)
}

func (j *NodeInitJob) Run(ctx KubeContext) error {
	job := j.config()
	return runJob(ctx, job)
}

func (j *NodeInitJob) GetType() JobType {
	return j.name
}
func (j *NodeInitJob) GetTarget() string {
	return j.target
}

// node init job initializes a node
type ClusterInitJob struct {
	Cluster               string
	Username              string
	Password              string
	Services              string
	DataServiceMemQuota   int /* uint32? */
	IndexServiceMemQuota  int
	SearchServiceMemQuota int
	IndexStorageSetting   string
	Port                  int
	name                  JobType
	target                string
	requires              []string
	cliVersion            string
}

func NewClusterInitJob(podTarget string, cluster *spec.CouchbaseCluster) *ClusterInitJob {

	settings := cluster.Spec.ClusterSettings

	return &ClusterInitJob{
		Cluster:               podTarget,
		Username:              settings.AdminUsername,
		Password:              settings.AdminPassword,
		Services:              settings.Services,
		DataServiceMemQuota:   settings.DataServiceMemQuota,
		IndexServiceMemQuota:  settings.IndexServiceMemQuota,
		SearchServiceMemQuota: settings.SearchServiceMemQuota,
		IndexStorageSetting:   settings.IndexStorageSetting,
		Port:                  8091,
		name:                  ClusterInit,
		target:                podTarget,
		requires:              []string{},
		cliVersion:            "latest",
	}
}

func (j *ClusterInitJob) config() *batchv1.Job {

	container := &v1.Container{
		Name:  j.name.Str(),
		Image: cliVersionedImage(j.cliVersion),
		Command: []string{couchbaseCliExecPath,
			j.name.Str(),
			"-c", j.Cluster,
			"--cluster-username", j.Username,
			"--cluster-password", j.Password,
			"--cluster-port", strconv.Itoa(j.Port),
			"--services", j.Services,
			"--cluster-ramsize", strconv.Itoa(j.DataServiceMemQuota),
			"--cluster-index-ramsize", strconv.Itoa(j.IndexServiceMemQuota),
			"--cluster-fts-ramsize", strconv.Itoa(j.SearchServiceMemQuota),
			"--index-storage-setting", j.IndexStorageSetting},
		ImagePullPolicy: v1.PullNever,
	}

	return createJobSpec(container)
}

// run kubernete batch job
func (j *ClusterInitJob) Run(ctx KubeContext) error {
	job := j.config()
	return runJob(ctx, job)
}

func (j *ClusterInitJob) GetType() JobType {
	return j.name
}

func (j *ClusterInitJob) GetTarget() string {
	return j.target
}

// helper method to get cli docker image
func cliVersionedImage(v string) string {
	return fmt.Sprintf("%s:%s", couchbaseCliImageName, v)
}

// creates the batchJobSpec for container
func createJobSpec(container *v1.Container) *batchv1.Job {

	name := container.Name
	labels := make(map[string]string)
	labels["op"] = name

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: batchv1.JobSpec{
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: v1.PodSpec{
					Containers:    []v1.Container{*container},
					RestartPolicy: v1.RestartPolicyNever,
				},
			},
		},
	}

}

// helper method for starting jobs
func runJob(ctx KubeContext, job *batchv1.Job) error {
	_, err := ctx.KubeCli.
		BatchV1().
		Jobs(ctx.Namespace).
		Create(job)
	return err
}
