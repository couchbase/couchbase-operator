package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// a test consists of a name, duration of test to run, and a list of operations to run.
type sysTestDef struct {
	name     string
	duration time.Duration
	ops      []operation
}

// an operation  is a docker image run as a kubernetes job that executes a command.
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
// this information will be injected into generic commands.
type scope struct {
	nodes1  []string
	nodes2  []string
	buckets []string
}

// this function will substitute scope information into a generic command.
func supplyScope(testScope scope, testOp operation) operation {
	testOp.cmd = ConvertTemplate(testScope, testOp.cmd)
	testOp.args = ConvertTemplate(testScope, testOp.args)

	return testOp
}

func ConvertTemplate(testScope scope, templates []string) []string {
	for i, str := range templates {
		if strings.Contains(str, "{{FIRST_NODE_CLUSTER1}}") {
			hostname := testScope.nodes1[0]
			str = strings.ReplaceAll(str, "{{FIRST_NODE_CLUSTER1}}", hostname)
		}

		if strings.Contains(str, "{{FIRST_NODE_CLUSTER2}}") {
			hostname := testScope.nodes2[0]
			str = strings.ReplaceAll(str, "{{FIRST_NODE_CLUSTER2}}", hostname)
		}

		if strings.Contains(str, "{{FIRST_NODE_NO_PORT_CLUSTER1}}") {
			hostname := strings.Split(testScope.nodes1[0], ":")
			str = strings.ReplaceAll(str, "{{FIRST_NODE_NO_PORT_CLUSTER1}}", hostname[0])
		}

		if strings.Contains(str, "{{FIRST_NODE_NO_PORT_CLUSTER2}}") {
			hostname := strings.Split(testScope.nodes2[0], ":")
			str = strings.ReplaceAll(str, "{{FIRST_NODE_NO_PORT_CLUSTER2}}", hostname[0])
		}

		if strings.Contains(str, "{{SECOND_NODE_CLUSTER1}}") {
			hostname := testScope.nodes1[1]
			str = strings.ReplaceAll(str, "{{SECOND_NODE_CLUSTER1}}", hostname)
		}

		if strings.Contains(str, "{{BUCKET}}") {
			str = strings.ReplaceAll(str, "{{BUCKET}}", testScope.buckets[rand.Intn(len(testScope.buckets))])
		}

		templates[i] = str
	}

	return templates
}

// this function will create a kubernetes job from an operation.
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
							ImagePullPolicy: corev1.PullIfNotPresent,
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

// this function will be used to wait for a job to finish.
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

			checkJob, err := kubeClient.BatchV1().Jobs(namespace).Get(context.Background(), jobName, metav1.GetOptions{})
			if err != nil {
				jobInfo["status"] = "error: failed to get job status: " + err.Error()
				results <- jobInfo

				return
			}

			if checkJob.Status.Succeeded == 1 {
				fmt.Printf("succeeded %v\n", jobName)
				jobInfo["status"] = "success: " + jobName
				results <- jobInfo

				return
			}

			if checkJob.Status.Failed >= 3 {
				fmt.Printf("failed %v\n", jobName)
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

func DeleteJob(t *testing.T, f *framework.Framework, jobName string) {
	targetKube := f.ClusterSpec[0]

	fmt.Printf("deleting %v\n", jobName)

	err := targetKube.KubeClient.BatchV1().Jobs(targetKube.Namespace).Delete(context.Background(), jobName, *metav1.NewDeleteOptions(0))
	if err != nil {
		t.Fatalf("failed to delete job %v \n", err)
	}

	pods, err := targetKube.KubeClient.CoreV1().Pods(targetKube.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "job=" + jobName})
	if err != nil {
		t.Fatalf("failed to list pods for cluster: " + err.Error())
	}

	for _, pod := range pods.Items {
		_ = targetKube.KubeClient.CoreV1().Pods(targetKube.Namespace).Delete(context.Background(), pod.Name, *metav1.NewDeleteOptions(0))
	}
}

func CreateJob(t *testing.T, f *framework.Framework, jobSpec *batchv1.Job) *batchv1.Job {
	targetKube := f.ClusterSpec[0]

	job, err := targetKube.KubeClient.BatchV1().Jobs(targetKube.Namespace).Create(context.Background(), jobSpec, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create job %v", err)
	}

	fmt.Printf("created %v\n", job.Name)
	t.Logf("Created Job: %s\n", job.Name)

	return job
}

// runs a system test based on a sysTestDef.
func runSysTest(t *testing.T, f *framework.Framework, testDef sysTestDef) {
	t.Logf("Creating New Couchbase Cluster...\n")

	targetKube := f.ClusterSpec[0]
	clusterSize := 4
	pvcName := "couchbase"
	leaveJobsRunning := false

	// cluster configuration, 4 nodes, all services
	clusterTemplate := &couchbasev2.CouchbaseCluster{
		Spec: couchbasev2.ClusterSpec{
			Image:        f.CouchbaseServerImage,
			AntiAffinity: true,
			ClusterSettings: couchbasev2.ClusterConfig{
				DataServiceMemQuota:   e2espec.NewResourceQuantityMi(2048),
				IndexServiceMemQuota:  e2espec.NewResourceQuantityMi(2048),
				SearchServiceMemQuota: e2espec.NewResourceQuantityMi(2048),
				IndexStorageSetting:   couchbasev2.CouchbaseClusterIndexStorageSettingStandard,
				AutoFailoverTimeout:   e2espec.NewDurationS(120),
			},
			Security: couchbasev2.CouchbaseClusterSecuritySpec{
				AdminSecret: targetKube.DefaultSecret.Name,
			},
			Servers: []couchbasev2.ServerConfig{
				{
					Name: "test",
					Size: clusterSize,
					Services: couchbasev2.ServiceList{
						couchbasev2.DataService,
						couchbasev2.IndexService,
						couchbasev2.QueryService,
						couchbasev2.SearchService,
						couchbasev2.EventingService,
						couchbasev2.AnalyticsService,
					},
					VolumeMounts: &couchbasev2.VolumeMounts{
						DefaultClaim: pvcName,
						DataClaim:    pvcName + "-data",
						IndexClaim:   pvcName + "-index",
						AnalyticsClaims: []string{
							pvcName,
							pvcName,
						},
					},
				},
			},
			Networking: couchbasev2.CouchbaseClusterNetworkingSpec{
				AdminConsoleServices: couchbasev2.ServiceList{
					couchbasev2.DataService,
				},
				ExposedFeatures: couchbasev2.ExposedFeatureList{
					couchbasev2.FeatureXDCR,
				},
			},
			VolumeClaimTemplates: []couchbasev2.PersistentVolumeClaimTemplate{
				createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 1),
				createPersistentVolumeClaimSpec(f.StorageClassName, pvcName+"-data", 4),
				createPersistentVolumeClaimSpec(f.StorageClassName, pvcName+"-index", 4),
			},
		},
	}

	bucketTemplate := &couchbasev2.CouchbaseBucket{
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(100),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	// Create the buckets.  These will be shared by both clusters.
	bucketNames := []string{
		"default",
		"CUSTOMER",
		"DISTRICT",
		"HISTORY",
		"WAREHOUSE",
		"ITEM",
		"NEW_ORDER",
		"ORDERS",
		"ORDER_LINE",
		"STOCK",
	}
	for _, name := range bucketNames {
		bucket := bucketTemplate.DeepCopy()
		bucket.Name = name
		e2eutil.MustNewBucket(t, targetKube, bucket)
	}

	// Create the first cluster.
	ctx1, err := e2eutil.InitClusterTLS(targetKube, &e2eutil.TLSOpts{})
	if err != nil {
		t.Fatal(err)
	}

	testCouchbase1 := clusterTemplate.DeepCopy()
	testCouchbase1.Name = ctx1.ClusterName
	testCouchbase1.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx1.ClusterSecretName,
			OperatorSecret: ctx1.OperatorSecretName,
		},
	}

	testCouchbase1 = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase1)

	// Create the second cluster.
	ctx2, err := e2eutil.InitClusterTLS(targetKube, &e2eutil.TLSOpts{})
	if err != nil {
		t.Fatal(err)
	}

	testCouchbase2 := clusterTemplate.DeepCopy()
	testCouchbase2.Name = ctx2.ClusterName
	testCouchbase2.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx2.ClusterSecretName,
			OperatorSecret: ctx2.OperatorSecretName,
		},
	}

	testCouchbase2 = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase2)

	if err := e2eutil.TLSCheckForCluster(t, targetKube, ctx1, time.Minute); err != nil {
		t.Fatal("TLS check for cluster failed: ", err)
	}

	if err := e2eutil.TLSCheckForCluster(t, targetKube, ctx2, time.Minute); err != nil {
		t.Fatal("TLS check for cluster failed: ", err)
	}

	// create connection to couchbase nodes
	t.Logf("creating couchbase client")

	// pause the operator for the test duration
	t.Logf("Pausing operator...")

	testCouchbase1 = e2eutil.MustPatchCluster(t, targetKube, testCouchbase1, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	testCouchbase2 = e2eutil.MustPatchCluster(t, targetKube, testCouchbase2, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)

	// make sure cluster is healthy before proceeding
	testCouchbase1 = e2eutil.MustPatchCluster(t, targetKube, testCouchbase1, jsonpatch.NewPatchSet().Test("/spec/paused", true), time.Minute)
	testCouchbase2 = e2eutil.MustPatchCluster(t, targetKube, testCouchbase2, jsonpatch.NewPatchSet().Test("/spec/paused", true), time.Minute)

	t.Logf("grabbing bucket info")

	t.Logf("populating scope")

	testScope := scope{
		nodes1:  []string{},
		nodes2:  []string{},
		buckets: []string{},
	}

	suffix1 := "." + testCouchbase1.Name + "." + targetKube.Namespace + ".svc"
	suffix2 := "." + testCouchbase2.Name + "." + targetKube.Namespace + ".svc"

	for i := 0; i < clusterSize; i++ {
		testScope.nodes1 = append(testScope.nodes1, couchbaseutil.CreateMemberName(testCouchbase1.Name, i)+suffix1)
		testScope.nodes2 = append(testScope.nodes2, couchbaseutil.CreateMemberName(testCouchbase2.Name, i)+suffix2)
	}

	testScope.buckets = bucketNames

	t.Logf("scope: %v", testScope)

	// apply scope to generic commands for all operations
	t.Logf("supplying scope to test ops")

	for i, op := range testDef.ops {
		op = supplyScope(testScope, op)
		testDef.ops[i] = op
	}

	t.Logf("test ops: %v", testDef.ops)

	// create rbac user, required for some operation to succeed
	rbacOp := operation{
		name:    "create-user-1",
		image:   "sequoiatools/couchbase-cli:v5.0.1",
		cmd:     []string{"couchbase-cli", "user-manage", "-c", "{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--set", "--rbac-username", "default_user", "--rbac-password", "password", "--roles", "admin", "--auth-domain", "local"},
		timeout: 2,
		wait:    true,
	}
	rbacOp = supplyScope(testScope, rbacOp)
	jobSpec := CreateJobSpec(rbacOp)
	job := CreateJob(t, f, jobSpec)
	// wait for job to succeed
	singleResults := make(chan map[string]string, 1)

	go MonitorJob(job.Name, targetKube.Namespace, targetKube.KubeClient, rbacOp.duration, rbacOp.timeout, singleResults)

	jobStatus := <-singleResults

	CheckJob(t, jobStatus)
	DeleteJob(t, f, jobStatus["jobName"])
	time.Sleep(3 * time.Second)

	rbacOp = operation{
		name:    "create-user-2",
		image:   "sequoiatools/couchbase-cli:v5.0.1",
		cmd:     []string{"couchbase-cli", "user-manage", "-c", "{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--set", "--rbac-username", "default", "--rbac-password", "password", "--roles", "admin", "--auth-domain", "local"},
		timeout: 2,
		wait:    true,
	}
	rbacOp = supplyScope(testScope, rbacOp)
	jobSpec = CreateJobSpec(rbacOp)
	job = CreateJob(t, f, jobSpec)
	// wait for job to succeed
	singleResults = make(chan map[string]string, 1)

	go MonitorJob(job.Name, targetKube.Namespace, targetKube.KubeClient, rbacOp.duration, rbacOp.timeout, singleResults)

	jobStatus = <-singleResults

	CheckJob(t, jobStatus)
	DeleteJob(t, f, jobStatus["jobName"])
	time.Sleep(3 * time.Second)

	// run the system test
	cycles := 1

outerLoop:
	for {
		// timeout after duration, otherwise run all the jobs
		results := make(chan map[string]string, len(testDef.ops))
		jobList := map[string]*batchv1.Job{}
		i := 0

		fmt.Printf("Starting cycle %v\n", cycles)
		t.Logf("Starting cycle %v\n", cycles)
	innerLoop:
		for {
			select {
			// test timeout
			case <-time.After(testDef.duration):
				break outerLoop
			// receive message from MonitorJob goroutines
			case result := <-results:
				fmt.Printf("results from %v \n", result["jobName"])
				CheckJob(t, result)

				if !leaveJobsRunning {
					DeleteJob(t, f, result["jobName"])
				}

				delete(jobList, result["jobName"])
				time.Sleep(3 * time.Second)
			// launch next job if any left
			default:
				if len(jobList) == 0 && i == len(testDef.ops) {
					break innerLoop
				}

				if i < len(testDef.ops) {
					op := testDef.ops[i]
					jobSpec := CreateJobSpec(op)
					job := CreateJob(t, f, jobSpec)

					fmt.Printf("launched: job=%v cycle=%v index:%v lastIndex:%v \n", job.Name, cycles, i, len(testDef.ops))
					// if wait, wait for success before launching next job
					if op.wait {
						singleResults := make(chan map[string]string, 1)

						go MonitorJob(job.Name, targetKube.Namespace, targetKube.KubeClient, op.duration, op.timeout, singleResults)

						jobStatus := <-singleResults
						CheckJob(t, jobStatus)

						if !leaveJobsRunning {
							DeleteJob(t, f, jobStatus["jobName"])
						}
					} else {
						go MonitorJob(job.Name, targetKube.Namespace, targetKube.KubeClient, op.duration, op.timeout, results)

						jobList[job.Name] = job
					}

					i++
				}

				time.Sleep(3 * time.Second)
			}
		}

		// make sure cluster is healthy before running next cycle
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase1, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase2, 2*time.Minute)

		cycles++
		time.Sleep(1 * time.Second)
	}
}

func TestFeaturesAll(t *testing.T) {
	f := framework.Global
	testDef := sysTestDef{
		name:     "simple",
		duration: framework.GetDuration(f.SuiteYmlData.Timeout),
		ops: []operation{
			// load data to default cluster 1
			{
				name:     "pillowfight-cluster1-1",
				image:    "sequoiatools/pillowfight:v5.0.1",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"cbc-pillowfight -U couchbase://{{FIRST_NODE_NO_PORT_CLUSTER1}}/default?select_bucket=true -I 10000 -B 1000 -c 10 -t 1 -u default_user -P password"},
				wait:     false,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "pillowfight-htp-cluster1-1",
				image:    "sequoiatools/pillowfight:v5.0.1",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"cbc-pillowfight -U couchbase://{{FIRST_NODE_NO_PORT_CLUSTER1}}/default?select_bucket=true -I 1000 -B 100 -c 100 -t 4 -u default_user -P password"},
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
			{
				name:     "rebalance-cluster1-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "rebalance", "-c", "couchbase://{{FIRST_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password"},
				wait:     true,
				timeout:  10,
				duration: 11,
			},

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
			// deploy eventing function
			{
				name:     "eventing-deploy-cluster1-1",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER1}} 8091 bucket_op_function_integration.json Administrator password create_and_deploy"},
				wait:     true,
				timeout:  2,
				duration: 1,
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
				duration: 5,
				timeout:  7,
			},

			// FTS indexes
			{
				name:     "fts-index-cluster1-1",
				image:    "appropriate/curl",
				cmd:      []string{"/bin/sh", "-c", "--"},
				args:     []string{"curl -X PUT -u Administrator:password -H Content-Type:application/json http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8094/api/index/st_index_scorch_1 -d '{\"type\": \"fulltext-index\",\"sourceType\": \"couchbase\",\"sourceName\": \"default\",\"params\": {\"store\": {\"indexType\": \"scorch\"}}}'"},
				wait:     true,
				duration: 1,
				timeout:  2,
			},
			// FTS query
			{
				name:     "fts-query-cluster1-1",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer fts -method POST -duration -1 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8094/api/index/st_index_scorch_1/query -query 5F"},
				wait:     true,
				duration: 5,
				timeout:  7,
			},

			// rebalance out node from cluster 1
			{
				name:     "rebalance-out-cluster1-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "rebalance", "-c", "couchbase://{{FIRST_NODE_CLUSTER1}}", "--server-remove", "{{SECOND_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password"},
				wait:     true,
				timeout:  240,
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

			// undeploy eventing function
			{
				name:     "eventing-undeploy-cluster1-1",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER1}} 8096 bucket_op_function_integration.json Administrator password undeploy true"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},
			// redeploy eventing function
			{
				name:     "eventing-deploy-cluster1-2",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER1}} 8091 bucket_op_function_integration.json Administrator password create_and_deploy"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			// FTS indexes
			{
				name:     "fts-index-cluster1-2",
				image:    "appropriate/curl",
				cmd:      []string{"/bin/sh", "-c", "--"},
				args:     []string{"curl -X PUT -u Administrator:password -H Content-Type:application/json http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8094/api/index/st_index_upside_down_1 -d '({\"type\": \"fulltext-index\",\"sourceType\": \"couchbase\",\"sourceName\": \"default\",\"params\": {\"store\": {\"indexType\": \"upside_down\"}}})'"},
				wait:     true,
				duration: 1,
				timeout:  2,
			},
			// FTS query
			{
				name:     "fts-query-cluster1-2",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer fts -method POST -duration -1 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8094/api/index/st_index_upside_down_1/query -query 5F"},
				wait:     true,
				duration: 1,
				timeout:  2,
			},

			// add back node to cluster 1
			{
				name:     "add-node-cluster1-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"couchbase-cli", "server-add", "-c", "couchbase://{{FIRST_NODE_CLUSTER1}}", "--server-add", "{{SECOND_NODE_CLUSTER1}}", "-u", "Administrator", "-p", "password", "--server-add-username", "Administrator", "--server-add-password", "password", "--services", "data,index,query,fts,eventing,analytics"},
				wait:     true,
				timeout:  240,
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
			// FTS query
			{
				name:     "fts-query-cluster1-3",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer fts -method POST -duration -1 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8094/api/index/st_index_upside_down_1/query -query 5F"},
				wait:     true,
				duration: 10,
				timeout:  11,
			},

			// undeploy eventing function
			{
				name:     "eventing-undeploy-cluster1-2",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER1}} 8096 bucket_op_function_integration.json Administrator password undeploy true"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},
			// redeploy eventing function
			{
				name:     "eventing-deploy-cluster1-3",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER1}} 8091 bucket_op_function_integration.json Administrator password create_and_deploy"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			// FTS query
			{
				name:     "fts-query-cluster1-4",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer fts -method POST -duration -1 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER1}}:8094/api/index/st_index_upside_down_1/query -query 5F"},
				wait:     true,
				duration: 10,
				timeout:  11,
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
			{
				name:     "fts-index-cluster2-1",
				image:    "appropriate/curl",
				cmd:      []string{"/bin/sh", "-c", "--"},
				args:     []string{"curl -X PUT -u Administrator:password -H Content-Type:application/json http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8094/api/index/st_index_scorch_1 -d '({\"type\": \"fulltext-index\",\"sourceType\": \"couchbase\",\"sourceName\": \"default\",\"params\": {\"store\": {\"indexType\": \"scorch\"}}})'"},
				wait:     true,
				duration: 1,
				timeout:  2,
			},
			// FTS indexes
			{
				name:     "fts-index-cluster2-2",
				image:    "appropriate/curl",
				cmd:      []string{"/bin/sh", "-c", "--"},
				args:     []string{"curl -X PUT -u Administrator:password -H Content-Type:application/json http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8094/api/index/st_index_upside_down_1 -d '({\"type\": \"fulltext-index\",\"sourceType\": \"couchbase\",\"sourceName\": \"default\",\"params\": {\"store\": {\"indexType\": \"upside_down\"}}})'"},
				wait:     true,
				duration: 1,
				timeout:  2,
			},
			// FTS query
			{
				name:     "fts-query-cluster2-1",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer fts -method POST -duration -1 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER2}}:8094/api/index/st_index_upside_down_1/query -query 5F"},
				wait:     false,
				duration: 10,
				timeout:  11,
			},
			// FTS query
			{
				name:     "fts-query-cluster2-2",
				image:    "sequoiatools/cbdozer",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./cbdozer fts -method POST -duration -1 -rate 10 -url http://Administrator:password@{{FIRST_NODE_NO_PORT_CLUSTER2}}:8094/api/index/st_index_upside_down_1/query -query 5F"},
				wait:     false,
				duration: 10,
				timeout:  11,
			},

			// deploy eventing function
			{
				name:     "eventing-deploy-cluster2-1",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER2}} 8091 bucket_op_function_integration.json Administrator password create_and_deploy"},
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

			// undeploy eventing function
			{
				name:     "eventing-undeploy-cluster1-3",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER1}} 8096 bucket_op_function_integration.json Administrator password undeploy true"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			// delete eventing function
			{
				name:     "eventing-delete-cluster1-1",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER1}} 8096 bucket_op_function_integration.json Administrator password delete"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			{
				name:     "cbq-delete-cluster1-1",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='delete from default where rating <= 300'"},
				wait:     false,
				timeout:  11,
				duration: 10,
			},

			{
				name:     "cbq-delete-cluster1-2",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='delete from default where rating >= 700'"},
				wait:     false,
				timeout:  11,
				duration: 10,
			},

			{
				name:     "cbq-delete-cluster1-3",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='delete from default where rating >= 300 and rating <= 700'"},
				wait:     false,
				timeout:  11,
				duration: 10,
			},

			{
				name:     "cbq-delete-cluster1-4",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER1}}:8093 -u=Administrator -p=password -script='delete from default where 1=1'"},
				wait:     false,
				timeout:  31,
				duration: 30,
			},

			{
				name:     "flush-bucket-cluster1-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"yes | couchbase-cli bucket-flush -c couchbase://{{FIRST_NODE_CLUSTER1}} -u Administrator -p password --bucket NEW_ORDER"},
				wait:     true,
				timeout:  11,
				duration: 10,
			},

			{
				name:     "flush-bucket-cluster1-2",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"yes | couchbase-cli bucket-flush -c couchbase://{{FIRST_NODE_CLUSTER1}} -u Administrator -p password --bucket DISTRICT"},
				wait:     true,
				timeout:  11,
				duration: 10,
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

			// undeploy eventing function
			{
				name:     "eventing-undeploy-cluster2-1",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER2}} 8096 bucket_op_function_integration.json Administrator password undeploy true"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

			// delete eventing function
			{
				name:     "eventing-delete-cluster2-1",
				image:    "sequoiatools/eventing",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"python /eventing.py {{FIRST_NODE_NO_PORT_CLUSTER2}} 8096 bucket_op_function_integration.json Administrator password delete"},
				wait:     true,
				timeout:  2,
				duration: 1,
			},

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
				name:     "cbq-delete-cluster2-4",
				image:    "sequoiatools/cbq",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"./shell/cbq/cbq -e=http://{{FIRST_NODE_NO_PORT_CLUSTER2}}:8093 -u=Administrator -p=password -script='delete from default where 1=1'"},
				wait:     false,
				timeout:  31,
				duration: 30,
			},

			{
				name:     "flush-bucket-cluster2-1",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"yes | couchbase-cli bucket-flush -c couchbase://{{FIRST_NODE_CLUSTER2}} -u Administrator -p password --bucket NEW_ORDER"},
				wait:     true,
				timeout:  11,
				duration: 10,
			},

			{
				name:     "flush-bucket-cluster2-2",
				image:    "sequoiatools/couchbase-cli:v5.0.1",
				cmd:      []string{"/bin/bash", "-c", "--"},
				args:     []string{"yes | couchbase-cli bucket-flush -c couchbase://{{FIRST_NODE_CLUSTER2}} -u Administrator -p password --bucket DISTRICT"},
				wait:     true,
				timeout:  11,
				duration: 10,
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
