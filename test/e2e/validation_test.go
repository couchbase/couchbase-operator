package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/decoder"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type testDef struct {
	name             string
	paramsIn         []parameter
	paramsOut        []parameter
	shouldFail       bool
	shouldWarn       bool
	expectedWarn     string
	expectedMessages []string
}

type parameter struct {
	field          []string
	fieldType      string
	fieldValue     string
	fieldIsPointer bool
}

type failure struct {
	testName  string
	testError error
}

func YAMLToCluster(yamlPath string) (*api.CouchbaseCluster, error) {
	raw, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	cluster, err := decoder.DecodeCouchbaseCluster(raw)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

func ClusterToYAML(cluster *api.CouchbaseCluster, yamlPath string) error {
	cbyaml, err := decoder.EncodeCouchbaseCluster(cluster)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(yamlPath, cbyaml, 0644)
	if err != nil {
		return err
	}
	return nil
}

func RunCBOPCTL(cmd string) ([]byte, error) {
	cmdName := os.Getenv("TESTDIR") + "/build/bin/cbopctl"
	cmdArgs := []string{cmd, "-f", os.Getenv("TESTDIR") + "/test/e2e/resources/temp.yaml"}
	return exec.Command(cmdName, cmdArgs...).Output()
}

func SetClusterParameter(cluster *api.CouchbaseCluster, param parameter) error {
	v := reflect.ValueOf(cluster).Elem()
	for _, subfield := range param.field {
		if index, err := strconv.Atoi(subfield); err == nil {
			v = v.Index(index)
		} else {
			v = v.FieldByName(subfield)
		}
	}

	if v.IsValid() {
		if param.fieldType == "string" {
			if param.fieldIsPointer {
				v.Set(reflect.ValueOf(&param.fieldValue))
			} else {
				v.SetString(param.fieldValue)
			}
		}

		if param.fieldType == "int" {
			val, err := strconv.Atoi(param.fieldValue)
			if err != nil {
				return err
			}
			switch v.Kind() {
			case reflect.Int,
				reflect.Int8,
				reflect.Int16,
				reflect.Int32,
				reflect.Int64:
				setVal := int64(val)
				if param.fieldIsPointer {
					v.Set(reflect.ValueOf(setVal))
				} else {
					v.SetInt(setVal)
				}

			case reflect.Uint,
				reflect.Uint8,
				reflect.Uint16,
				reflect.Uint32,
				reflect.Uint64:
				setVal := uint64(val)
				if param.fieldIsPointer {
					v.Set(reflect.ValueOf(setVal))
				} else {
					v.SetUint(setVal)
				}
			}
		}

		if param.fieldType == "array" {
			var newArray []string
			err := json.Unmarshal([]byte(param.fieldValue), &newArray)
			if err != nil {
				return err
			}
			v.Set(reflect.ValueOf(newArray))
		}

		if param.fieldType == "bool" {
			b, err := strconv.ParseBool("true")
			if err != nil {
				return err
			}
			if param.fieldIsPointer {
				v.Set(reflect.ValueOf(&b))
			} else {
				v.Set(reflect.ValueOf(b))
			}
		}
	}
	return nil
}

func VerifyClusterParameter(cluster *api.CouchbaseCluster, param parameter) error {
	v := reflect.ValueOf(cluster)
	v = reflect.Indirect(v)
	for _, subfield := range param.field {
		if index, err := strconv.Atoi(subfield); err == nil {
			v = v.Index(index)
		} else {
			v = v.FieldByName(subfield)
		}
	}

	if param.fieldType == "int" {
		compareVal, _ := strconv.Atoi(param.fieldValue)
		switch v.Kind() {
		case reflect.Int,
			reflect.Int8,
			reflect.Int16,
			reflect.Int32,
			reflect.Int64:
			if int(v.Int()) != int(compareVal) {
				return fmt.Errorf("%+v != %+v", int(v.Int()), compareVal)
			}
		case reflect.Uint,
			reflect.Uint8,
			reflect.Uint16,
			reflect.Uint32,
			reflect.Uint64:
			if uint(v.Uint()) != uint(compareVal) {
				return fmt.Errorf("%+v != %+v", uint(v.Uint()), compareVal)
			}
		}
	}

	if param.fieldType == "string" {
		clusterVal := string(v.String())
		compareVal := param.fieldValue
		if clusterVal != compareVal {
			return fmt.Errorf("%+v != %+v", clusterVal, compareVal)
		}
	}

	return nil
}

func ExpectedClusterSize(cluster *api.CouchbaseCluster) int {
	clusterSize := 0
	for _, server := range cluster.Spec.ServerSettings {
		clusterSize = clusterSize + server.Size
	}
	return clusterSize
}

func runValidationTest(t *testing.T, f *framework.Framework, testDefs []testDef, targetKubeName, command string) []failure {
	failures := []failure{}
	targetKube := f.ClusterSpec[targetKubeName]
	for _, test := range testDefs {
		t.Logf("Running test: %s", test.name)

		testCouchbase, err := YAMLToCluster("./resources/validation.yaml")
		if err != nil {
			t.Logf("error: %v", err)
			failures = append(failures, failure{testName: test.name, testError: err})
			e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
			continue
		}

		t.Logf("setting secret: %+v", targetKube.DefaultSecret)
		testCouchbase.Spec.AuthSecret = targetKube.DefaultSecret.Name

		ns := os.Getenv("KUBENAMESPACE")
		if ns != "" {
			t.Logf("setting namespace: %s", ns)
			testCouchbase.ObjectMeta.Namespace = ns
		}

		if command == "apply" || command == "delete" {
			err = ClusterToYAML(testCouchbase, "./resources/temp.yaml")
			if err != nil {
				t.Logf("error: %v", err)
				failures = append(failures, failure{testName: test.name, testError: err})
				e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
				continue
			}
			cmdOut, err := RunCBOPCTL("create")
			t.Logf("Returned: %s", string(cmdOut))
			if err != nil && !test.shouldFail {
				failures = append(failures, failure{testName: test.name, testError: err})
				e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
				continue
			}

			if !test.shouldFail {
				clusterSize := ExpectedClusterSize(testCouchbase)
				err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries20)
				if err != nil {
					t.Logf("error: %v", err)
					failures = append(failures, failure{testName: test.name, testError: err})
					e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					continue
				}
			}

			testCouchbase, err = YAMLToCluster("./resources/temp.yaml")
			if err != nil {
				t.Logf("error: %v", err)
				failures = append(failures, failure{testName: test.name, testError: err})
				e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
				continue
			}
		}

		for _, param := range test.paramsIn {
			t.Logf("setting parameter: %+v", param)
			err = SetClusterParameter(testCouchbase, param)
			if err != nil {
				t.Logf("error: %v", err)
				failures = append(failures, failure{testName: test.name, testError: err})
				e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
				continue
			}
		}

		err = ClusterToYAML(testCouchbase, "./resources/temp.yaml")
		if err != nil {
			t.Logf("error: %v", err)
			failures = append(failures, failure{testName: test.name, testError: err})
			e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
			continue
		}

		cmdOut, err := RunCBOPCTL(command)
		t.Logf("Returned: %s", string(cmdOut))
		if err != nil && !test.shouldFail {
			failures = append(failures, failure{testName: test.name, testError: err})
			e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
			continue
		}

		clusters, err := targetKube.CRClient.CouchbaseV1().CouchbaseClusters(f.Namespace).List(metav1.ListOptions{})

		if test.shouldWarn {
			if !strings.Contains(string(cmdOut), test.expectedWarn) || test.expectedWarn == "" {
				t.Logf("expected warning: %+v \n returned message: %+v \n", test.expectedWarn, string(cmdOut))
				failures = append(failures, failure{testName: test.name, testError: errors.New("incorrect warning")})
				e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
				continue
			}
		}

		for _, message := range test.expectedMessages {
			if !strings.Contains(string(cmdOut), message) || message == "" {
				t.Logf("expected message: %+v \n returned message: %+v \n", message, string(cmdOut))
				failures = append(failures, failure{testName: test.name, testError: errors.New("incorrect message")})
				e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
				continue
			}
		}
		if test.shouldFail {
			if command == "delete" || command == "apply" {
				if len(clusters.Items) != 1 {
					failures = append(failures, failure{testName: test.name, testError: errors.New("cluster deletion should fail")})
					e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					continue
				}
			}

			if command == "create" {
				if len(clusters.Items) != 0 {
					failures = append(failures, failure{testName: test.name, testError: errors.New("cluster creation should fail")})
					e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					continue
				}
			}
		} else {
			if command == "delete" {
				if len(clusters.Items) != 0 {
					failures = append(failures, failure{testName: test.name, testError: errors.New("cluster deletion should work")})
					e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					continue
				}
				_, err = e2eutil.WaitPodsDeleted(targetKube.KubeClient, ns, e2eutil.Retries30, metav1.ListOptions{LabelSelector: "app=couchbase"})
				if err != nil {
					failures = append(failures, failure{testName: test.name, testError: err})
					e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					continue
				}
				t.Logf("deleted couchbase cluster: \n%+v", testCouchbase)
			} else {
				if len(clusters.Items) != 1 {
					failures = append(failures, failure{testName: test.name, testError: errors.New("only one cluster should be created")})
					e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					continue
				}
				testCouchbase = &clusters.Items[0]
				for _, param := range test.paramsOut {
					t.Logf("verifying parameter: %+v", param)
					err = VerifyClusterParameter(testCouchbase, param)
					if err != nil {
						failures = append(failures, failure{testName: test.name, testError: err})
						e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
						continue
					}
				}

				clusterSize := ExpectedClusterSize(testCouchbase)
				err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
				if err != nil {
					t.Logf("error: %v", err)
					failures = append(failures, failure{testName: test.name, testError: err})
					e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					continue
				}
				t.Logf("created couchbase cluster: \n%+v", testCouchbase)
			}
		}
		e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
		time.Sleep(10 * time.Second) //should add a wait for number of couchbase pods to be 0
	}
	return failures
}

func checkFailures(t *testing.T, failures []failure) {
	fail := false
	for i, failure := range failures {
		t.Logf("Failure %d: %s \n Error: %v \n", i+1, failure.testName, failure.testError)
		fail = true
	}
	if fail {
		t.Fatal("failures in test")
	}
}

func TestValidationCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name:             "create default yaml",
			paramsIn:         []parameter{},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" created"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "create")
	checkFailures(t, failures)
}

func TestNegValidationCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "create invalid bucket name",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketName"},
					fieldType:  "string",
					fieldValue: "`invalid!bucket!name`",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets.name in body should match '^[a-zA-Z0-9._\\-%]*$'"},
		},

		{
			name: "Validate server-settings services fields",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Services"},
					fieldType:  "array",
					fieldValue: "[\"data\", \"indxe\", \"query\", \"search\"]",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers.services in body should be one of [data index query search eventing analytics]"},
		},

		{
			name: "Validate AdminConsoleService fields",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "array",
					fieldValue: "[\"data\", \"indxe\", \"query\", \"search\"]",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"validation failure list:\nspec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},

		{
			name: "create invalid bucket type",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketType"},
					fieldType:  "string",
					fieldValue: "invalid-bucket-type",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets.type in body should be one of [couchbase ephemeral memcached]"},
		},

		{
			name: "create invalid bucket name and bucket type",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketName"},
					fieldType:  "string",
					fieldValue: "`invalid!bucket!name`",
				},

				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketType"},
					fieldType:  "string",
					fieldValue: "invalid-bucket-type",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets.type in body should be one of [couchbase ephemeral memcached]", "spec.buckets.name in body should match '^[a-zA-Z0-9._\\-%]*$'"},
		},

		{
			name: "create invalid server name",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Name"},
					fieldType:  "string",
					fieldValue: "data_only",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers.name in body shouldn't contain duplicates"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "create")
	checkFailures(t, failures)
}

func TestValidationDefaultCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "create:default:Spec.BaseImage",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BaseImage"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut: []parameter{
				{
					field:      []string{"Spec", "BaseImage"},
					fieldType:  "string",
					fieldValue: "couchbase/server",
				},
			},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" created"},
		},

		{
			name: "create:default:Spec.ClusterSettings.IndexServiceMemQuota",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "IndexServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "0",
				},
			},
			paramsOut: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "IndexServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "256",
				},
			},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" created"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "create")
	checkFailures(t, failures)
}

func TestNegValidationDefaultCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "create:default:Spec.ClusterSettings.DataServiceMemQuota",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "DataServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "0",
				},
			},
			paramsOut: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "DataServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "256",
				},
			},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[*].memoryQuota in body should be less than or equal to 256"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "create")
	checkFailures(t, failures)
}

func TestNegValidationConstraintsCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "more than 4 adminConsoleServices",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "array",
					fieldValue: "[\"data\", \"index\", \"query\", \"search\", \"data\"]",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},

		{
			name: "invalid adminConsoleService",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "array",
					fieldValue: "[\"data\", \"index\", \"query\", \"xxxxx\"]",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},

		{
			name: "ephemeral bucket with index replicas enabled",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "3", "EnableIndexReplica"},
					fieldType:  "bool",
					fieldValue: "true",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"enableReplicaIndex in spec.buckets[3] must be of type nil: \"Bucket type is ephemeral\""},
		},

		{
			name: "memcached bucket with seqno conflict resolution",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "2", "ConflictResolution"},
					fieldType:  "string",
					fieldValue: "seqno",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"conflictResolution in spec.buckets[2] must be of type nil: \"Bucket type is memcached\""},
		},

		{
			name: "couchbase bucket with invalid io priority",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "IoPriority"},
					fieldType:  "string",
					fieldValue: "lighow",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets.ioPriority in body should be one of [high low]"},
		},

		{
			name: "couchbase bucket with invalid conflict resolution",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "ConflictResolution"},
					fieldType:  "string",
					fieldValue: "selwwno",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets.conflictResolution in body should be one of [seqno lww]"},
		},

		{
			name: "ephemeral bucket with invalid eviction policy",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "3", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "valueOnly",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},

		{
			name: "couchbase bucket with invalid eviction policy",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "nruEviction",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "create")
	checkFailures(t, failures)
}

// cbopctl apply tests

func TestValidationApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name:             "apply:",
			paramsIn:         []parameter{},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},

		{
			name: "apply:",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketName"},
					fieldType:  "string",
					fieldValue: "newNameForBucket",
				},
			},
			paramsOut: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketName"},
					fieldType:  "string",
					fieldValue: "newNameForBucket",
				},
			},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "apply")
	checkFailures(t, failures)
}

func TestNegValidationApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "apply invalid changes to bucket name",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketName"},
					fieldType:  "string",
					fieldValue: "`invalid!bucket!name`",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets.name in body should match '^[a-zA-Z0-9._\\-%]*$'"},
		},

		{
			name: "apply invalid changes to bucket type",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketType"},
					fieldType:  "string",
					fieldValue: "invalid-bucket-type",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[0].type in body cannot be updated"},
		},

		{
			name: "apply invalid changes to server name",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Name"},
					fieldType:  "string",
					fieldValue: "data_only",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[0].services in body cannot be updated"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "apply")
	checkFailures(t, failures)
}

func TestValidationDefaultApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "apply defaults to base image",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BaseImage"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut: []parameter{
				{
					field:      []string{"Spec", "BaseImage"},
					fieldType:  "string",
					fieldValue: "couchbase/server",
				},
			},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},

		{
			name: "apply defaults to indexServiceMemQuota",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "IndexServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "0",
				},
			},
			paramsOut: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "IndexServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "256",
				},
			},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},

		{
			name: "apply defaults to searchServiceMemQuota",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "SearchServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "0",
				},
			},
			paramsOut: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "SearchServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "256",
				},
			},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},

		{
			name: "apply defaults to autofailover timeout",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "AutoFailoverTimeout"},
					fieldType:  "int",
					fieldValue: "0",
				},
			},
			paramsOut: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "AutoFailoverTimeout"},
					fieldType:  "int",
					fieldValue: "120",
				},
			},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "apply")
	checkFailures(t, failures)
}

func TestNegValidationDefaultApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "create:default:Spec.ClusterSettings.DataServiceMemQuota",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "DataServiceMemQuota"},
					fieldType:  "int",
					fieldValue: "0",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[*].memoryQuota in body should be less than or equal to 256"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "create")
	checkFailures(t, failures)
}

func TestNegValidationConstraintsApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "more than 4 adminConsoleServices",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "array",
					fieldValue: "[\"data\", \"index\", \"query\", \"search\", \"data\"]",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},

		{
			name: "invalid adminConsoleService",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "array",
					fieldValue: "[\"data\", \"index\", \"query\", \"xxxxx\"]",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},

		{
			name: "ephemeral bucket with index replicas enabled",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "3", "EnableIndexReplica"},
					fieldType:  "bool",
					fieldValue: "true",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"enableReplicaIndex in spec.buckets[3] must be of type nil: \"Bucket type is ephemeral\""},
		},

		{
			name: "couchbase bucket with invalid io priority",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "IoPriority"},
					fieldType:  "string",
					fieldValue: "lighow",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets.ioPriority in body should be one of [high low]"},
			shouldWarn:       true,
			expectedWarn:     "Changing the IO Priority will cause the bucket default1 to be temporarily unavailable",
		},

		{
			name: "ephemeral bucket with invalid eviction policy",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "3", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "valueOnly",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
			shouldWarn:       true,
			expectedWarn:     "Changing the Eviction Policy will cause the bucket default4 to be temporarily unavailable",
		},

		{
			name: "couchbase bucket with invalid eviction policy",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "nruEviction",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
			shouldWarn:       true,
			expectedWarn:     "Changing the Eviction Policy will cause the bucket default1 to be temporarily unavailable",
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "apply")
	checkFailures(t, failures)
}

func TestNegValidationImmutableApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "ephemeral bucket with seqno conflict resolution",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "4", "ConflictResolution"},
					fieldType:  "string",
					fieldValue: "seqno",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[4].conflictResolution in body cannot be updated"},
		},

		{
			name: "memcached bucket with seqno conflict resolution",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "2", "ConflictResolution"},
					fieldType:  "string",
					fieldValue: "seqno",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[2].conflictResolution in body cannot be updated"},
		},

		{
			name: "couchbase bucket with invalid conflict resolution",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "ConflictResolution"},
					fieldType:  "string",
					fieldValue: "selwwno",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[0].conflictResolution in body cannot be updated"},
		},
		{
			name: "change index storage mode",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ClusterSettings", "IndexStorageSetting"},
					fieldType:  "string",
					fieldValue: "plasma",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.cluster.indexStorageSetting in body cannot be updated"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "apply")
	checkFailures(t, failures)
}

//cbopctl delete tests

func TestValidationDelete(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "delete after modifying",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketName"},
					fieldType:  "string",
					fieldValue: "newBucketName",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" deleted"},
		},

		{
			name:             "delete",
			paramsIn:         []parameter{},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" deleted"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "delete")
	checkFailures(t, failures)
}

func TestNegValidationDelete(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testDefs := []testDef{
		{
			name: "delete",
			paramsIn: []parameter{
				{
					field:      []string{"ObjectMeta", "Name"},
					fieldType:  "string",
					fieldValue: "cb-example1",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"couchbaseclusters.couchbase.database.couchbase.com \"cb-example1\" not found"},
		},
	}
	kubeName := "BasicCluster"
	failures := runValidationTest(t, f, testDefs, kubeName, "delete")
	checkFailures(t, failures)
}
