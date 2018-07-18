package e2e

import (
	"fmt"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// a test consists of a name, number of days to run, and a list of operations to run
type sysTestDef struct {
	name string
	days int
	ops  []operation
}

// an operation  is a docker image run as a kubernetes job that executes a command
type operation struct {
	name     string
	image    string
	cmd      []string
	args     []string
	wait     bool
	timeout  int
	duration int
}

// scope will be used to hold information about the cluster configuration.
// this information will be injected into generic commands
type scope struct {
	nodes1  []string
	nodes2  []string
	buckets []string
}

// this function will substitute scope information into a generic command
func SupplyScope(testScope scope, testOp operation) operation {
	testOp.cmd = ConvertTemplate(testScope, testOp.cmd)
	testOp.args = ConvertTemplate(testScope, testOp.args)
	return testOp
}

func ConvertTemplate(testScope scope, templates []string) []string {
	for i, str := range templates {
		if strings.Contains(str, "{{FIRST_NODE_CLUSTER1}}") {
			hostname := testScope.nodes1[0]
			str = strings.Replace(str, "{{FIRST_NODE_CLUSTER1}}", hostname, -1)
		}
		if strings.Contains(str, "{{FIRST_NODE_CLUSTER2}}") {
			hostname := testScope.nodes2[0]
			str = strings.Replace(str, "{{FIRST_NODE_CLUSTER2}}", hostname, -1)
		}
		if strings.Contains(str, "{{FIRST_NODE_NO_PORT_CLUSTER1}}") {
			hostname := strings.Split(testScope.nodes1[0], ":")
			str = strings.Replace(str, "{{FIRST_NODE_NO_PORT_CLUSTER1}}", hostname[0], -1)
		}
		if strings.Contains(str, "{{FIRST_NODE_NO_PORT_CLUSTER2}}") {
			hostname := strings.Split(testScope.nodes2[0], ":")
			str = strings.Replace(str, "{{FIRST_NODE_NO_PORT_CLUSTER2}}", hostname[0], -1)
		}
		if strings.Contains(str, "{{SECOND_NODE_CLUSTER1}}") {
			hostname := testScope.nodes1[1]
			str = strings.Replace(str, "{{SECOND_NODE_CLUSTER1}}", hostname, -1)
		}
		if strings.Contains(str, "{{BUCKET}}") {
			str = strings.Replace(str, "{{BUCKET}}", testScope.buckets[rand.Intn(len(testScope.buckets))], -1)
		}
		templates[i] = str
	}
	return templates
}

// this function will create a kubernetes job from an operation
func CreateJobSpec(op operation) *batchv1.Job {
	privilegedVal := true
	labels := make(map[string]string)
	labels["app"] = "couchbase"
	labels["job"] = op.name
	labels["type"] = "job"
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
							Args:    op.args,
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
	opDuration := time.Duration(duration*60) * time.Second
	opTimeout := time.Duration(timeout*60) * time.Second
	to := time.After(opTimeout)
	dur := time.After(opDuration)
	for {
		select {
		case <-to:
			fmt.Printf("timeout %v\n", jobName)
			jobInfo["status"] = "timeout after " + opTimeout.String() + " seconds"
			results <- jobInfo
			return
		case <-dur:
			fmt.Printf("duration up %v\n", jobName)
			jobInfo["status"] = "success: " + jobName
			results <- jobInfo
			return
		default:
			fmt.Printf("default %v\n", jobName)
			checkJob, err := kubeClient.BatchV1().Jobs(namespace).Get(jobName, metav1.GetOptions{})
			if err != nil {
				jobInfo["status"] = "error: failed to get job status: " + err.Error()
				results <- jobInfo
				return
			}
			fmt.Printf("bbb %v\n", checkJob.Status.Succeeded)
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

func DeleteJob(t *testing.T, f *framework.Framework, kubeName string, result map[string]string) {
	targetKube := f.ClusterSpec[kubeName]
	fmt.Printf("deleting %v\n", result["jobName"])
	err := targetKube.KubeClient.BatchV1().Jobs(f.Namespace).Delete(result["jobName"], metav1.NewDeleteOptions(0))
	if err != nil {
		t.Fatalf("failed to delete job %v \n", err)
	}
	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "job=" + result["jobName"]})
	if err != nil {
		t.Fatalf("failed to list pods for cluster: " + err.Error())
	}
	for _, pod := range pods.Items {
		targetKube.KubeClient.CoreV1().Pods(f.Namespace).Delete(pod.Name, metav1.NewDeleteOptions(0))
	}
}

func CreateJob(t *testing.T, f *framework.Framework, kubeName string, jobSpec *batchv1.Job) *batchv1.Job {
	targetKube := f.ClusterSpec[kubeName]
	job, err := targetKube.KubeClient.BatchV1().Jobs(f.Namespace).Create(jobSpec)
	if err != nil {
		t.Fatalf("failed to create job %v", err)
	}
	t.Logf("Created Job: %s\n", job.Name)
	return job
}

func NewBucket(name string) map[string]string {
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
	//kubeName := "BasicCluster"
	kubeName := "AuxillaryCluster1"
	targetKube := f.ClusterSpec[kubeName]

	// cluster configuration, 10 buckets, 4 nodes, all services
	clusterConfig := map[string]string{
		"dataServiceMemQuota":   "2000",
		"indexServiceMemQuota":  "800",
		"searchServiceMemQuota": "800",
		"indexStorageSetting":   "plasma",
		"autoFailoverTimeout":   "120",
	}
	serviceConfig1 := map[string]string{
		"size":     "4",
		"name":     "test_config_1",
		"services": "data,query,index",
	}
	otherConfig1 := map[string]string{
		"antiAffinity": "off",
	}
	exposedFeaturesConfig := map[string]string{
		"featureNames": "xdcr",
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
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"bucket1":         bucketConfig1,
		"bucket2":         bucketConfig2,
		"bucket3":         bucketConfig3,
		"bucket4":         bucketConfig4,
		"bucket5":         bucketConfig5,
		"bucket6":         bucketConfig6,
		"bucket7":         bucketConfig7,
		"bucket8":         bucketConfig8,
		"bucket9":         bucketConfig9,
		"bucket10":        bucketConfig10,
		"other1":          otherConfig1,
		"exposedFeatures": exposedFeaturesConfig,
	}

	// create cluster
	testCouchbase1, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Logf("cluster: %+v", testCouchbase1)
		t.Fatalf("failed to create cluster %+v", err)
	}

	testCouchbase2, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Logf("cluster: %+v", testCouchbase2)
		t.Fatalf("failed to create cluster %+v", err)
	}

	if !f.SkipTeardown {
		defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
	}

	// check tls
	//err = e2eutil.TlsCheckForCluster(t, targetKube.KubeClient, targetKube.Config, f.Namespace)
	//if err != nil {
	//	t.Fatal("TLS check for cluster failed: ", err)
	//}

	// create connection to couchbase nodes
	t.Logf("creating couchbase client")
	consoleURL1, err := e2eutil.AdminConsoleURL(f.ApiServerHost(kubeName), testCouchbase1.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client1, err := e2eutil.NewClient(t, targetKube.KubeClient, testCouchbase1, []string{consoleURL1})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}
	consoleURL2, err := e2eutil.AdminConsoleURL(f.ApiServerHost(kubeName), testCouchbase2.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client2, err := e2eutil.NewClient(t, targetKube.KubeClient, testCouchbase2, []string{consoleURL2})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// pause the operator for the test duration
	t.Logf("Pausing operator...")
	testCouchbase1, err = e2eutil.UpdateClusterSpec("Paused", "true", targetKube.CRClient, testCouchbase1, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}
	testCouchbase2, err = e2eutil.UpdateClusterSpec("Paused", "true", targetKube.CRClient, testCouchbase2, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	// make sure cluster is healthy before proceeding
	err = e2eutil.WaitForClusterStatus(t, targetKube.CRClient, "ControlPaused", "true", testCouchbase1, 300)
	if err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}
	err = e2eutil.WaitForClusterStatus(t, targetKube.CRClient, "ControlPaused", "true", testCouchbase2, 300)
	if err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	// get info from cluster to add to the scope
	t.Logf("grabbing cluster info")
	clusterInfo1, err := e2eutil.GetClusterInfo(t, client1, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo1)
	clusterInfo2, err := e2eutil.GetClusterInfo(t, client2, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo2)

	t.Logf("grabbing bucket info")
	bucketInfo1, err := client1.GetBuckets()
	if err != nil {
		t.Fatalf("failed to get bucket info %v", err)
	}
	t.Logf("bucket info: %v", bucketInfo1)
	bucketInfo2, err := client2.GetBuckets()
	if err != nil {
		t.Fatalf("failed to get bucket info %v", err)
	}
	t.Logf("bucket info: %v", bucketInfo2)

	t.Logf("populating scope")
	testScope := scope{
		nodes1:  []string{},
		nodes2:  []string{},
		buckets: []string{},
	}

	for _, n := range clusterInfo1.Nodes {
		testScope.nodes1 = append(testScope.nodes1, n.HostName)
	}
	for _, n := range clusterInfo2.Nodes {
		testScope.nodes2 = append(testScope.nodes2, n.HostName)
	}
	for _, b := range bucketInfo1 {
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
		name:    "create-user-1",
		image:   "sequoiatools/couchbase-cli:v5.0.1",
		cmd:     []string{"couchbase-cli", "user-manage", "-c", "{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--set", "--rbac-username", "default_user", "--rbac-password", "password", "--roles", "admin", "--auth-domain", "local"},
		timeout: 2,
		wait:    true,
	}
	rbacOp = SupplyScope(testScope, rbacOp)
	jobSpec := CreateJobSpec(rbacOp)
	job := CreateJob(t, f, kubeName, jobSpec)
	// wait for job to succeed
	singleResults := make(chan map[string]string, 1)
	go MonitorJob(job.Name, f.Namespace, targetKube.KubeClient, rbacOp.duration, rbacOp.timeout, singleResults)
	jobStatus := <-singleResults
	CheckJob(t, jobStatus)
	DeleteJob(t, f, kubeName, jobStatus)
	time.Sleep(3 * time.Second)

	rbacOp = operation{
		name:    "create-user-2",
		image:   "sequoiatools/couchbase-cli:v5.0.1",
		cmd:     []string{"couchbase-cli", "user-manage", "-c", "{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--set", "--rbac-username", "default", "--rbac-password", "password", "--roles", "admin", "--auth-domain", "local"},
		timeout: 2,
		wait:    true,
	}
	rbacOp = SupplyScope(testScope, rbacOp)
	jobSpec = CreateJobSpec(rbacOp)
	job = CreateJob(t, f, kubeName, jobSpec)
	// wait for job to succeed
	singleResults = make(chan map[string]string, 1)
	go MonitorJob(job.Name, f.Namespace, targetKube.KubeClient, rbacOp.duration, rbacOp.timeout, singleResults)
	jobStatus = <-singleResults
	CheckJob(t, jobStatus)
	DeleteJob(t, f, kubeName, jobStatus)
	time.Sleep(3 * time.Second)

	// run the system test
	to := time.After(time.Duration(testDef.days*24*60*60) * time.Second)
	cycles := 1
outerLoop:
	for {
		// timeout after duration, otherwise run all the jobs
		results := make(chan map[string]string, len(testDef.ops))
		jobList := map[string]*batchv1.Job{}
		i := 0
		fmt.Printf("Starting cycle %v\n", cycles)
	innerLoop:
		for {
			select {
			// test timeout
			case <-to:
				break outerLoop
			// receive message from MonitorJob goroutines
			case result := <-results:
				fmt.Printf("got results from %v \n", result["jobName"])
				CheckJob(t, result)
				DeleteJob(t, f, kubeName, result)
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
					job := CreateJob(t, f, kubeName, jobSpec)
					fmt.Printf("launched job %v::%v::%v \n", job.Name, cycles, i)
					// if wait, wait for success before launching next job
					if op.wait {
						singleResults := make(chan map[string]string, 1)
						go MonitorJob(job.Name, f.Namespace, targetKube.KubeClient, op.duration, op.timeout, singleResults)
						jobStatus := <-singleResults
						CheckJob(t, jobStatus)
						DeleteJob(t, f, kubeName, jobStatus)
					} else {
						go MonitorJob(job.Name, f.Namespace, targetKube.KubeClient, op.duration, op.timeout, results)
						jobList[job.Name] = job
					}
					i = i + 1
				}

				time.Sleep(3 * time.Second)
			}
		}
		// make sure cluster is healthy before running next cycle
		err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase1.Name, f.Namespace, e2eutil.Size4, e2eutil.Retries10)
		if err != nil {
			t.Fatalf("failed to wait for cluster to be healthy %v", err)
		}
		err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase2.Name, f.Namespace, e2eutil.Size4, e2eutil.Retries10)
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
		days: f.Duration,
		ops: []operation{
			// load data to default cluster 1

			{
				name:     "pillowfight-cluster1-1",
				image:    "sequoiatools/pillowfight:v5.0.1",
				cmd:      []string{"cbc-pillowfight", "-U", "couchbase://{{FIRST_NODE_CLUSTER1}}/default?select_bucket=true", "-I", "10000", "-B", "1000", "-c", "10", "-t", "1", "-u", "default_user", "-P", "password"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "pillowfight-htp-cluster1-1",
				image:    "sequoiatools/pillowfight:v5.0.1",
				cmd:      []string{"cbc-pillowfight", "-U", "couchbase://{{FIRST_NODE_CLUSTER1}}/default?select_bucket=true", "-I", "1000", "-B", "100", "-c", "100", "-t", "4", "-u", "default_user", "-P", "password"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},
			// create index cluster 1
			{
				name:     "cbq-create-index-cluster1-1",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='create index default_rating on `default`(rating)'"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-create-index-cluster1-2",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='create primary index on default'"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			// rebalance cluster 1
			//{
			//	name: "rebalance-cluster1-1",
			//	image: "sequoiatools/couchbase-cli:v5.0.1",
			//	cmd: []string{"couchbase-cli", "rebalance", "-c", "couchbase://{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password"},
			//	wait: true,
			//	timeout: 10,
			//	duration: 11,
			//},
			// tpcc workload cluster 1
			{
				name:     "tpcc-indexing-cluster1-1",
				image:    "sequoiatools/tpcc",
				cmd:      []string{"./run.sh", "{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093", "util/cbcrindex.sql"},
				wait:     true,
				timeout:  10,
				duration: 11,
			},

			{
				name:     "tpcc-indexing-custer1-2",
				image:    "sequoiatools/tpcc",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python tpcc.py --warehouses 1 --client 3 --no-execute n1ql --query-url {{FIRST_NODE_NO_PORT_CLUSTER1}}:8093"},
				wait:     true,
				timeout:  31,
				duration: 30,
			},

			{
				name:     "tpcc-indexing-cluster1-3",
				image:    "sequoiatools/tpcc",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python tpcc.py --no-load --warehouses 1 --duration 10 --client 3 --query-url {{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 n1ql"},
				wait:     true,
				timeout:  15,
				duration: 16,
			},

			// continuous load cluster 1
			{
				name:     "gideon-cluster1-1",
				image:    "sequoiatools/gideon",
				cmd:      []string{},
				args:     []string{"kv --ops 100 --create 10 --get 70 --expire 20 --ttl 15 --hosts {{FIRST_NODE_CLUSTER1}} --bucket default --sizes 16000"},
				wait:     true,
				duration: 5,
				timeout:  7,
			},

			{
				name:     "gideon-cluster1-2",
				image:    "sequoiatools/gideon",
				cmd:      []string{},
				args:     []string{"kv --ops 100 --create 15 --get 80 --delete 5 --hosts {{FIRST_NODE_CLUSTER1}} --bucket default"},
				wait:     true,
				duration: 5,
				timeout:  7,
			},
			// continuous delete cluster 1
			{
				name:     "attack-query-cluster1-1",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'delete from default where rating > 0 limit 100'"},
				wait:     false,
				duration: 5,
				timeout:  7,
			},
			// continuous query cluster 1
			{
				name:     "attack-query-cluster1-2",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'select * from default where rating > 100 limit 100 offset 50'"},
				wait:     false,
				duration: 5,
				timeout:  7,
			},
			{
				name:     "attack-query-cluster1-3",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 15 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'select * from default limit 100 offset 50'"},
				wait:     true,
				duration: 20,
				timeout:  21,
			},

			// rebalance out node from cluster 1
			{
				name:     "rebalance-out-cluster1-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "rebalance", "-c", "couchbase://{{FIRST_NODE_CLUSTER1}}", "--server-remove", "{{SECOND_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password"},
				wait:     true,
				timeout:  120,
				duration: 121,
			},

			// continuous load
			{
				name:     "gideon-cluster1-3",
				image:    "sequoiatools/gideon",
				cmd:      []string{},
				args:     []string{"kv --ops 100 --create 10 --get 90 --expire 100 --ttl 660 --hosts {{FIRST_NODE_CLUSTER1}} --bucket default --sizes 16000"},
				wait:     true,
				timeout:  11,
				duration: 10,
			},

			{
				name:     "gideon-cluster1-4",
				image:    "sequoiatools/gideon",
				cmd:      []string{},
				args:     []string{"kv --ops 2000 --create 15 --get 80 --delete 5 --hosts {{FIRST_NODE_CLUSTER1}} --bucket default --sizes 512 128 1024 2048"},
				wait:     true,
				timeout:  11,
				duration: 10,
			},
			// continuous delete
			{
				name:     "attack-query-cluster1-4",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'delete from default where rating > 0 limit 100'"},
				wait:     false,
				timeout:  11,
				duration: 10,
			},
			// continuous query
			{
				name:     "attack-query-cluster1-5",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'select * from default where rating > 100 limit 100 offset 50'"},
				wait:     false,
				timeout:  11,
				duration: 10,
			},
			{
				name:     "attack-query-cluster1-6",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 15 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'select * from default limit 100 offset 50'"},
				wait:     true,
				timeout:  16,
				duration: 15,
			},

			// add back node to cluster 1
			{
				name:     "add-node-cluster1-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "server-add", "-c", "couchbase://{{FIRST_NODE_CLUSTER1}}", "--server-add", "{{SECOND_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--server-add-username", "Administrator", "--server-add-password", "password", "--services", "data,index,query,fts"},
				wait:     true,
				timeout:  120,
				duration: 121,
			},

			{
				name:     "rebalance-cluster1-2",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "rebalance", "-c", "couchbase://{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password"},
				wait:     true,
				timeout:  120,
				duration: 121,
			},

			// continuous load
			{
				name:     "gideon-cluster1-5",
				image:    "sequoiatools/gideon",
				cmd:      []string{},
				args:     []string{"kv --ops 100 --create 10 --get 90 --expire 100 --ttl 660 --hosts {{FIRST_NODE_CLUSTER1}} --bucket default --sizes 16000"},
				wait:     true,
				duration: 10,
				timeout:  11,
			},

			{
				name:     "gideon-cluster1-6",
				image:    "sequoiatools/gideon",
				cmd:      []string{},
				args:     []string{"kv --ops 2000 --create 15 --get 80 --delete 5 --hosts {{FIRST_NODE_CLUSTER1}} --bucket default --sizes 512 128 1024 2048"},
				wait:     true,
				duration: 10,
				timeout:  11,
			},
			// continuous delete
			{
				name:     "attack-query-cluster1-7",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'delete from default where rating > 0 limit 100'"},
				wait:     false,
				duration: 5,
				timeout:  7,
			},
			// continuous query
			{
				name:     "attack-query-cluster1-8",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'select * from default where rating > 100 limit 100 offset 50'"},
				wait:     false,
				duration: 10,
				timeout:  11,
			},
			// continuous query
			{
				name:     "attack-query-cluster1-9",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 15 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'select * from default limit 100 offset 50'"},
				wait:     true,
				duration: 15,
				timeout:  16,
			},

			// setup xdcr
			{
				name:     "xdcr-setup-cluster1-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "xdcr-setup", "-c", "{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--create", "--xdcr-cluster-name", "RemoteCluster", "--xdcr-hostname", "{{FIRST_NODE_CLUSTER2}}", "--xdcr-username", "Administrator", "--xdcr-password", "password"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "xdcr-replicate-cluster1-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "xdcr-replicate", "-c", "{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--create", "--xdcr-cluster-name", "RemoteCluster", "--xdcr-from-bucket", "default", "--xdcr-to-bucket", "default"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			// create indexes in remote cluster

			{
				name:     "cbq-create-index-cluster2-1",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='create index default_rating on `default`(rating)'"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},
			{
				name:     "cbq-create-index-cluster2-2",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='create primary index on default'"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			// continuous load
			{
				name:     "gideon-cluster1-7",
				image:    "sequoiatools/gideon",
				cmd:      []string{},
				args:     []string{"kv --ops 100 --create 10 --get 90 --expire 100 --ttl 660 --hosts {{FIRST_NODE_CLUSTER1}} --bucket default --sizes 16000"},
				wait:     true,
				duration: 10,
				timeout:  11,
			},

			{
				name:     "gideon-cluster1-8",
				image:    "sequoiatools/gideon",
				cmd:      []string{},
				args:     []string{"kv --ops 2000 --create 15 --get 80 --delete 5 --hosts {{FIRST_NODE_CLUSTER1}} --bucket default --sizes 512 128 1024 2048"},
				wait:     true,
				duration: 10,
				timeout:  11,
			},
			// continuous delete
			{
				name:     "attack-query-cluster1-10",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'delete from default where rating > 0 limit 100'"},
				wait:     false,
				duration: 10,
				timeout:  11,
			},
			// continuous query
			{
				name:     "attack-query-cluster1-11",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'select * from default where rating > 100 limit 100 offset 50'"},
				wait:     false,
				duration: 10,
				timeout:  11,
			},
			// continuous query
			{
				name:     "attack-query-cluster1-12",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 15 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093/query/service -body 'select * from default limit 100 offset 50'"},
				wait:     false,
				duration: 15,
				timeout:  16,
			},

			// continuous delete
			{
				name:     "attack-query-cluster2-1",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093/query/service -body 'delete from default where rating > 0 limit 100'"},
				wait:     false,
				duration: 10,
				timeout:  11,
			},
			// continuous query
			{
				name:     "attack-query-cluster2-2",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 10 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093/query/service -body 'select * from default where rating > 100 limit 100 offset 50'"},
				wait:     false,
				duration: 10,
				timeout:  11,
			},
			// continuous query
			{
				name:     "attack-query-cluster2-3",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer -method POST -duration 20 -rate 5 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093/query/service -body 'select * from default limit 100 offset 50'"},
				wait:     true,
				duration: 20,
				timeout:  21,
			},

			// stop xdcr
			{
				name:     "xdcr-replicate-cluster1-2",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"couchbase-cli xdcr-replicate -c {{FIRST_NODE_CLUSTER1}} -u Administrator -p password --delete --xdcr-cluster-name RemoteCluster --xdcr-from-bucket default --xdcr-to-bucket default --xdcr-replicator $(couchbase-cli xdcr-replicate -c {{FIRST_NODE_CLUSTER1}} -u Administrator -p password --list | sed -n 's/stream id: \\(.*\\)/\\1/p')"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "xdcr-setup-cluster1-2",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "xdcr-setup", "-c", "{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--delete", "--xdcr-cluster-name", "RemoteCluster"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			// delete phase
			// delete cluster 1
			{
				name:     "cbq-delete-cluster1-1",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='delete from default where rating <= 300'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-cluster1-2",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='delete from default where rating >= 700'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-cluster1-3",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='delete from default where rating >= 300 and rating <= 700'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-cluster1-4",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='delete from default where 1=1'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-index-cluster1-1",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='drop primary index on default'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-index-cluster1-2",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='drop index default.default_rating'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},
			// delete cluster 2
			{
				name:     "cbq-delete-cluster2-1",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='delete from default where rating <= 300'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-cluster2-2",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='delete from default where rating >= 700'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-cluster2-3",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='delete from default where rating >= 300 and rating <= 700'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-cluster2-1",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='delete from default where 1=1'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-index-cluster2-1",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='drop primary index on default'"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-index-cluster2-2",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='drop index default.default_rating'"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},
		},
	}
	runSysTest(t, f, testDef)
}
