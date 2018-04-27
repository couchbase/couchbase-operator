package e2e

import (
	"os"
	"testing"
	"strings"
	"time"
	"math/rand"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

)

// a test consists of a name, number of days to run, and a list of operations to run
type sysTestDef struct {
	name string
	days int
	ops []operation
}

// an operation  is a docker image run as a kubernetes job that executes a command
type operation struct {
	name string
	image string
	cmd []string
	args []string
	wait bool
	timeout int
	duration int
}

// scope will be used to hold information about the cluster configuration.
// this information will be injected into generic commands
type scope struct {
	nodes []string
	buckets []string
}

// this function will substitute scope information into a generic command
func SupplyScope(testScope scope, testOp operation) (operation) {
	for i, str := range testOp.cmd {
		if strings.Contains(str, "{{FIRST_NODE}}") {
			hostname := testScope.nodes[0]
			str = strings.Replace(str, "{{FIRST_NODE}}", hostname, -1)
		}

		if strings.Contains(str, "{{FIRST_NODE_NO_PORT}}") {
			hostname := strings.Split(testScope.nodes[0], ":")
			str = strings.Replace(str, "{{FIRST_NODE_NO_PORT}}", hostname[0], -1)
		}
		if strings.Contains(str, "{{SECOND_NODE}}") {
			hostname := testScope.nodes[1]
			str = strings.Replace(str, "{{SECOND_NODE}}", hostname, -1)
		}
		if strings.Contains(str, "{{BUCKET}}") {
			str = strings.Replace(str, "{{BUCKET}}", testScope.buckets[rand.Intn(len(testScope.buckets))], -1)
		}
		testOp.cmd[i] = str
	}
	for i, str := range testOp.args {
		if strings.Contains(str, "{{FIRST_NODE}}") {
			hostname := testScope.nodes[0]
			str = strings.Replace(str, "{{FIRST_NODE}}", hostname, -1)
		}
		if strings.Contains(str, "{{FIRST_NODE_NO_PORT}}") {
			hostname := strings.Split(testScope.nodes[0], ":")
			str = strings.Replace(str, "{{FIRST_NODE_NO_PORT}}", hostname[0], -1)
		}
		if strings.Contains(str, "{{SECOND_NODE}}") {
			hostname := testScope.nodes[1]
			str = strings.Replace(str, "{{SECOND_NODE}}", hostname, -1)
		}
		if strings.Contains(str, "{{BUCKET}}") {
			str = strings.Replace(str, "{{BUCKET}}", testScope.buckets[rand.Intn(len(testScope.buckets))], -1)
		}
		testOp.args[i] = str
	}
	return testOp
}

// this function will create a kubernetes job from an operation
func CreateJobSpec(op operation) *batchv1.Job {
	privilegedVal := true
	labels := make(map[string]string)
	labels["app"] = "couchbase"
	labels["job"] = op.name
	batchJob := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   op.name,
			Labels: labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   op.name,
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{},
					Containers: []corev1.Container{
						{
							Name:    op.name,
							Image:   op.image,
							Command: op.cmd,
							Args: op.args,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privilegedVal,
							},
							ImagePullPolicy: corev1.PullPolicy(corev1.PullIfNotPresent),
							Env:             []corev1.EnvVar{},
							VolumeMounts:    []corev1.VolumeMount{},
						},
					},
					RestartPolicy:    corev1.RestartPolicyNever,
					Volumes:          []corev1.Volume{},
					ImagePullSecrets: []corev1.LocalObjectReference{},
				},
			},
		},
	}
	return batchJob
}

// this function will be used to wait for a job to finish
func MonitorJob(jobName string, namespace string, kubeClient kubernetes.Interface, duration int, timeout int, results chan<- map[string]string) {
	jobInfo := make(map[string]string)
	jobInfo["jobName"] = jobName
	opDuration := time.Duration(duration * 60) * time.Second
	opTimeout := time.Duration(timeout * 60) * time.Second
	to := time.After(opTimeout)
	dur := time.After(opDuration)
	for {
		select {
		case <-to:
			jobInfo["status"] = "timeout after " + opTimeout.String() + " seconds"
			results <- jobInfo
			return
		case <-dur:
			jobInfo["status"] = "success: " + jobName
			results <- jobInfo
			return
		default:
			checkJob, err := kubeClient.BatchV1().Jobs(namespace).Get(jobName, metav1.GetOptions{})
			if err != nil {
				jobInfo["status"] = "error: failed to get job status: " + err.Error()
				results <- jobInfo
				return
			}

			if checkJob.Status.Succeeded == 1 {
				jobInfo["status"] = "success: " + jobName
				results <- jobInfo
				return
			}

			if checkJob.Status.Failed >= 3 {
				jobInfo["status"] = "error: job failed: " + jobName
				results <- jobInfo
				return
			}
			time.Sleep(3 * time.Second)
		}
	}
}

func CheckJob(t *testing.T, jobStatus map[string]string) {
	if !strings.Contains(jobStatus["status"], "success") {
		t.Fatalf("job failed: %v, %v", jobStatus["jobName"], jobStatus["status"])
	}
}

func DeleteJob(t *testing.T, f *framework.Framework, result map[string]string) {
	err := f.KubeClient.BatchV1().Jobs(f.Namespace).Delete(result["jobName"], metav1.NewDeleteOptions(0))
	if err != nil {
		t.Fatalf("failed to delete job %v \n", err)
	}
}

func CreateJob(t *testing.T, f *framework.Framework, jobSpec *batchv1.Job) (*batchv1.Job) {
	job, err := f.KubeClient.BatchV1().Jobs(f.Namespace).Create(jobSpec)
	if err != nil {
		t.Fatalf("failed to create job %v", err)
	}
	t.Logf("Created Job: %s\n", job.Name)
	return job
}

func NewBucket(name string) (map[string]string){
	return map[string]string{
		"bucketName":         name,
		"bucketType":         "couchbase",
		"bucketMemoryQuota":  "100",
		"bucketReplicas":     "1",
		"ioPriority":         "high",
		"evictionPolicy":     "fullEviction",
		"conflictResolution": "seqno",
		"enableFlush":        "true",
		"enableIndexReplica": "false",
	}
}

// runs a system test based on a sysTestDef
func runSysTest(t *testing.T, f *framework.Framework, testDef sysTestDef) {
	t.Logf("Creating New Couchbase Cluster...\n")

	// cluster configuration, 10 buckets, 4 nodes, all services
	clusterConfig := map[string]string{
		"dataServiceMemQuota":   "3000",
		"indexServiceMemQuota":  "1000",
		"searchServiceMemQuota": "1000",
		"indexStorageSetting":   "plasma",
		"autoFailoverTimeout":   "120",
	}
	serviceConfig1 := map[string]string{
		"size":      "4",
		"name":      "test_config_1",
		"services":  "data,n1ql,index,fts",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data",
	}
	bucketConfig1 := NewBucket("default")
	bucketConfig2 := NewBucket("CUSTOMER")
	bucketConfig3 := NewBucket("DISTRICT")
	bucketConfig4 := NewBucket("HISTORY")
	bucketConfig5 := NewBucket("WAREHOUSE")
	bucketConfig6 := NewBucket("ITEM")
	bucketConfig7 := NewBucket("NEW_ORDER")
	bucketConfig8 := NewBucket("ORDERS")
	bucketConfig9 := NewBucket("ORDER_LINE")
	bucketConfig10 := NewBucket("STOCK")
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
		"bucket2":  bucketConfig2,
		"bucket3":  bucketConfig3,
		"bucket4":  bucketConfig4,
		"bucket5":  bucketConfig5,
		"bucket6":  bucketConfig6,
		"bucket7":  bucketConfig7,
		"bucket8":  bucketConfig8,
		"bucket9":  bucketConfig9,
		"bucket10":  bucketConfig10,
	}

	// create cluster
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Logf("cluster: %+v", testCouchbase)
		t.Fatalf("failed to create cluster %+v", err)
	}
	if !f.SkipTeardown {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir)
	}

	// create connection to couchbase nodes
	t.Logf("creating couchbase client")
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// pause the operator for the test duration
	t.Logf("Pausing operator...")
	testCouchbase, err = e2eutil.UpdateClusterSpec("Paused", "true", f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	// make sure cluster is healthy before proceeding
	err = e2eutil.WaitForClusterStatus(t, f.CRClient, "ControlPaused", "true", testCouchbase, 300)
	if err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	// get info from cluster to add to the scope
	t.Logf("grabbing cluster info")
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	t.Logf("grabbing bucket info")
	bucketInfo, err := client.GetBuckets()
	if err != nil {
		t.Fatalf("failed to get bucket info %v", err)
	}
	t.Logf("bucket info: %v", bucketInfo)

	t.Logf("populating scope")
	testScope := scope{
		nodes: []string{},
		buckets: []string{},
	}

	for _, n := range clusterInfo.Nodes {
		testScope.nodes = append(testScope.nodes, n.HostName)
	}

	for _, b := range bucketInfo {
		testScope.buckets = append(testScope.buckets, b.BucketName)
	}
	t.Logf("scope: %v", testScope)

	// apply scope to generic commands for all operations
	t.Logf("supplying scope to test ops")
	for i, op := range testDef.ops {
		op = SupplyScope(testScope, op)
		testDef.ops[i] = op
	}
	t.Logf("test ops: %v", testDef.ops)

	//create rbac user, required for some operation to succeed
	rbacOp := operation{
		name: "create-user",
		image: "sequoiatools/couchbase-cli:v5.0.1",
		cmd: []string{"couchbase-cli", "user-manage", "-c", "{{FIRST_NODE}}", "-u", "Administrator", "-p", "password", "--set", "--rbac-username", "default_user", "--rbac-password", "password", "--roles", "admin", "--auth-domain", "local"},
		timeout: 2,
	}
	rbacOp = SupplyScope(testScope, rbacOp)
	jobSpec := CreateJobSpec(rbacOp)
	job := CreateJob(t, f, jobSpec)
	// wait for job to succeed
	singleResults := make(chan map[string]string, 1)
	go MonitorJob(job.Name, f.Namespace, f.KubeClient, rbacOp.duration, rbacOp.timeout, singleResults)
	jobStatus := <- singleResults
	CheckJob(t, jobStatus)
	DeleteJob(t, f, jobStatus)
	time.Sleep(3 * time.Second)

	rbacOp = operation{
		name: "create-user",
		image: "sequoiatools/couchbase-cli:v5.0.1",
		cmd: []string{"couchbase-cli", "user-manage", "-c", "{{FIRST_NODE}}", "-u", "Administrator", "-p", "password", "--set", "--rbac-username", "default", "--rbac-password", "password", "--roles", "admin", "--auth-domain", "local"},
		timeout: 2,
	}
	rbacOp = SupplyScope(testScope, rbacOp)
	jobSpec = CreateJobSpec(rbacOp)
	job = CreateJob(t, f, jobSpec)
	// wait for job to succeed
	singleResults = make(chan map[string]string, 1)
	go MonitorJob(job.Name, f.Namespace, f.KubeClient, rbacOp.duration, rbacOp.timeout, singleResults)
	jobStatus = <- singleResults
	CheckJob(t, jobStatus)
	DeleteJob(t, f, jobStatus)
	time.Sleep(3 * time.Second)

	// run the system test
	to := time.After(time.Duration(testDef.days * 24 * 60 * 60) * time.Second)
	cycles := 1
	outerLoop:
	for {
		// timeout after duration, otherwise run all the jobs
		results := make(chan map[string]string, len(testDef.ops))
		jobList := map[string]*batchv1.Job{}
		i := 0
		t.Logf("Starting cycle %v\n", cycles)
		innerLoop:
		for {
			select {
			// test timeout
			case <-to:
				break outerLoop
			// receive message from MonitorJob goroutines
			case result := <-results:
				t.Logf("got results from %v \n", result["jobName"])
				CheckJob(t, result)
				DeleteJob(t, f, result)
				delete(jobList, result["jobName"])
				time.Sleep(3 * time.Second)
			//launch next job if any left
			default:
				if len(jobList) == 0 && i == len(testDef.ops) {
					break innerLoop
				}
				if i < len(testDef.ops) {
					op := testDef.ops[i]
					jobSpec := CreateJobSpec(op)
					job := CreateJob(t, f, jobSpec)
					t.Logf("launched job %v::%v::%v \n", job.Name, cycles, i)
					// if wait, wait for success before launching next job
					if op.wait {
						singleResults := make(chan map[string]string, 1)
						go MonitorJob(job.Name, f.Namespace, f.KubeClient, op.duration, op.timeout, singleResults)
						jobStatus := <-singleResults
						CheckJob(t, jobStatus)
						DeleteJob(t, f, jobStatus)
					} else {
						go MonitorJob(job.Name, f.Namespace, f.KubeClient, op.duration, op.timeout, results)
						jobList[job.Name] = job
					}
					i = i + 1
				}

				time.Sleep(3 * time.Second)
			}
		}
		// make sure cluster is healthy before running next cycle
		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size4, e2eutil.Retries10)
		if err != nil {
			t.Fatalf("failed to wait for cluster to be healthy %v", err)
		}
		cycles = cycles + 1
		time.Sleep(1 * time.Second)
	}
}

func TestFeaturesAll(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDef := sysTestDef{
		name: "simple",
		days: 2,
		ops: []operation{
			// Load data
			{
				name: "pillowfight-htp-1",
				image: "sequoiatools/pillowfight:v5.0.1",
				cmd: []string{"cbc-pillowfight", "-U", "couchbase://{{FIRST_NODE}}/default?select_bucket=true", "-I", "1000", "-B", "100", "-c", "100", "-t", "4", "-u", "default_user", "-P", "password"},
				wait: true,
				timeout: 10,
				duration: 11,
			},

			{
				name: "pillowfight-1",
				image: "sequoiatools/pillowfight:v5.0.1",
				cmd: []string{"cbc-pillowfight", "-U", "couchbase://{{FIRST_NODE}}/default?select_bucket=true", "-I", "10000", "-B", "1000", "-c", "10", "-t", "1", "-u", "default_user", "-P", "password"},
				wait: true,
				timeout: 10,
			},
			// create index
			{
				name: "cbq-create-index-1",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='create primary index on default'"},
				wait: true,
				timeout: 10,
			},

			// Rebalance
			//{
			//	name: "rebalance",
			//	image: "sequoiatools/couchbase-cli:v5.0.1",
			//	cmd: []string{"couchbase-cli", "rebalance", "-c", "couchbase://{{FIRST_NODE}}", "-u", "Administrator", "-p", "password"},
			//	wait: true,
			//	timeout: 10,
			//	duration: 11,
			//},
			// TPCC workload
			{
				name: "tpcc-indexing-1",
				image: "sequoiatools/tpcc",
				cmd: []string{"./run.sh", "{{FIRST_NODE_NO_PORT}}:8093", "util/cbcrindex.sql"},
				wait: true,
				timeout: 10,
				duration: 11,
			},

			{
				name: "tpcc-indexing-2",
				image: "sequoiatools/tpcc",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"python tpcc.py --warehouses 1 --client 3 --no-execute n1ql --query-url {{FIRST_NODE_NO_PORT}}:8093"},
				wait: true,
				timeout: 30,
				duration: 31,
			},

			{
				name: "tpcc-indexing-3",
				image: "sequoiatools/tpcc",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"python tpcc.py --no-load --warehouses 1 --duration 10 --client 3 --query-url {{FIRST_NODE_NO_PORT}}:8093 n1ql"},
				wait: true,
				timeout: 15,
				duration: 16,
			},

			// continuous load
			/*
			{
				name: "gideon-1",
				image: "sequoiatools/gideon",
				cmd: []string{},
				args: []string{"kv --ops 100 --create 10 --get 90 --expire 100 --ttl 660 --hosts {{FIRST_NODE}} --bucket default --sizes 16000"},
				wait: true,
				duration: 10,
				timeout: 11,
			},

			{
				name: "gideon-2",
				image: "sequoiatools/gideon",
				cmd: []string{},
				args: []string{"kv --ops 2000 --create 15 --get 80 --delete 5 --hosts {{FIRST_NODE}} --bucket default --sizes 512 128 1024 2048"},
				wait: true,
				duration: 10,
				timeout: 11,

			},
			// continuous delete
			{
				name: "attack-query-1",
				image: "sequoiatools/cbdozer",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./cbdozer -method POST -duration 10 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT}}:8093/query/service -body 'delete from default where rating > 0 limit 100'"},
				wait: true,
				duration: 5,
				timeout: 7,
			},
			// continuous query
			{
				name: "attack-query-2",
				image: "sequoiatools/cbdozer",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./cbdozer -method POST -duration 10 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT}}:8093/query/service -body 'select * from default where rating > 100 limit 100 offset 50'"},
				wait: true,
				duration: 5,
				timeout: 7,
			},
			*/
			// continuous query
			{
				name: "attack-query-2",
				image: "sequoiatools/cbdozer",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./cbdozer -method POST -duration 10 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT}}:8093/query/service -body 'select * from default limit 100 offset 50'"},
				wait: true,
				duration: 20,
				timeout: 21,
			},

			{
				name: "rebalance-out-1",
				image: "sequoiatools/couchbase-cli:v5.0.1",
				cmd: []string{"couchbase-cli", "rebalance", "-c", "couchbase://{{FIRST_NODE}}","--server-remove", "{{SECOND_NODE}}", "-u", "Administrator", "-p", "password"},
				wait: true,
				timeout: 30,
				duration: 31,
			},

			/*
			{
				name: "cbq-create-index-1",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='create primary index on default'"},
				wait: true,
				timeout: 10,
			},

			{
				name: "cbq-create-index-2",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='create index default_rating on `default`(rating)'"},
				wait: true,
				timeout: 10,
			},

			{
				name: "cbq-query-1",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='select rating from default'"},
				wait: true,
				timeout: 10,
			},

			{
				name: "cbq-delete-1",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='delete from default where rating < 300'"},
				wait: true,
				timeout: 10,
			},

			{
				name: "cbq-delete-2",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='delete from default where rating > 700'"},
				wait: true,
				timeout: 10,
			},

			{
				name: "cbq-delete-3",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='delete from default where rating > 300 and rating < 700'"},
				wait: true,
				timeout: 10,
			},

			{
				name: "cbq-delete-index-1",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='drop primary index on default'"},
				wait: true,
				timeout: 10,
			},

			{
				name: "cbq-delete-index-2",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='drop index default.default_rating'"},
				wait: true,
				timeout: 10,
			},
			*/

			{
				name: "add-node-1",
				image: "sequoiatools/couchbase-cli:v5.0.1",
				cmd: []string{"couchbase-cli", "server-add", "-c", "couchbase://{{FIRST_NODE}}","--server-add", "{{SECOND_NODE}}", "-u", "Administrator", "-p", "password", "--server-add-username", "Administrator", "--server-add-password", "password", "--services", "data,index,query,fts"},
				wait: true,
				timeout: 30,
				duration: 31,
			},

			{
				name: "rebalance",
				image: "sequoiatools/couchbase-cli:v5.0.1",
				cmd: []string{"couchbase-cli", "rebalance", "-c", "couchbase://{{FIRST_NODE}}", "-u", "Administrator", "-p", "password"},
				wait: true,
				timeout: 30,
				duration: 31,
			},

			{
				name: "cbq-delete-1",
				image: "sequoiatools/cbq",
				cmd: []string{"/bin/bash", "-c", "--"},
				args: []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT}}:8093 -u=Administrator -p=password -script='delete from default where 1=1'"},
				wait: true,
				timeout: 10,
				duration: 11,
			},


		},
	}
	runSysTest(t, f, testDef)
}


