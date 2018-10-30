package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/decoder"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type failureList []failure
type failure struct {
	testName  string
	testError error
}

func (failures *failureList) AppendFailure(name string, err error) {
	newFailure := failure{
		testName:  name,
		testError: err,
	}
	*failures = append(*failures, newFailure)
}

func (failures *failureList) PrintFailures(t *testing.T) bool {
	failureExists := false
	for i, failure := range *failures {
		t.Logf("Failure %d: %s \n Error: %v \n", i+1, failure.testName, failure.testError)
		failureExists = true
	}
	return failureExists
}

func (failures *failureList) CheckFailures(t *testing.T) {
	failureExists := failures.PrintFailures(t)
	if failureExists {
		t.Fatal("failures in test")
	}
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

	if err := ioutil.WriteFile(yamlPath, cbyaml, 0644); err != nil {
		return err
	}
	return nil
}

func RunCBOPCTL(cmd string) ([]byte, error) {
	cmdName := os.Getenv("TESTDIR") + "/build/bin/cbopctl"
	cmdArgs := []string{cmd, "-f", os.Getenv("TESTDIR") + "/test/e2e/resources/validation/temp.yaml"}
	return exec.Command(cmdName, cmdArgs...).Output()
}

func SetClusterParameter(cluster *api.CouchbaseCluster, param parameter) error {
	v := reflect.ValueOf(cluster).Elem()
	for _, subfield := range param.field {
		if index, err := strconv.Atoi(subfield); err == nil {
			v = v.Index(index)
		} else {
			v = v.FieldByName(subfield)
			if v.Kind().String() == "ptr" {
				v = reflect.Indirect(v)
			}
		}
	}

	if v.IsValid() {
		switch param.fieldType {
		case "string":
			if param.fieldIsPointer {
				v.Set(reflect.ValueOf(param.fieldValue))
			} else {
				v.SetString(param.fieldValue)
			}

		case "int":
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

		case "array":
			var newArray []string
			if err := json.Unmarshal([]byte(param.fieldValue), &newArray); err != nil {
				return err
			}
			v.Set(reflect.ValueOf(newArray))

		case "bool":
			b, err := strconv.ParseBool("true")
			if err != nil {
				return err
			}
			if param.fieldIsPointer {
				v.Set(reflect.ValueOf(&b))
			} else {
				v.Set(reflect.ValueOf(b))
			}

		case "v1.ServiceList":
			var serviceArray []api.Service
			if err := json.Unmarshal([]byte(param.fieldValue), &serviceArray); err != nil {
				return err
			}
			v.Set(reflect.ValueOf(serviceArray))

		case "apiresource.Quantity":
			resourceQtyVal, err := strconv.Atoi(param.fieldValue)
			if err != nil {
				return nil
			}
			resourceQuantity := apiresource.NewQuantity(int64(resourceQtyVal)*1024*1024*1024, apiresource.BinarySI)
			valToSet := map[corev1.ResourceName]apiresource.Quantity{
				"storage": *resourceQuantity,
			}
			v.Set(reflect.ValueOf(valToSet))

		default:
			return errors.New("Unsupported field type: " + param.fieldType)
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

func runValidationTest(t *testing.T, testDefs []testDef, kubeName, command string) {
	f := framework.Global
	failures := failureList{}
	targetKube := f.ClusterSpec[kubeName]
	os.Setenv("KUBECONFIG", targetKube.KubeConfPath)
	for _, test := range testDefs {
		t.Logf("Running test: %s", test.name)

		testCouchbase, err := YAMLToCluster("./resources/validation/validation.yaml")
		if err != nil {
			t.Logf("error: %v", err)
			failures.AppendFailure(test.name, err)
			continue
		}

		testCouchbase.Spec.AuthSecret = targetKube.DefaultSecret.Name
		testCouchbase.ObjectMeta.Namespace = f.Namespace

		// Removing previous deployment if any
		e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir, kubeName, t.Name())

		if command == "apply" || command == "delete" {
			if err := ClusterToYAML(testCouchbase, "./resources/validation/temp.yaml"); err != nil {
				t.Logf("error: %v", err)
				failures.AppendFailure(test.name, err)
				continue
			}
			cmdOut, err := RunCBOPCTL("create")
			t.Logf("Returned: %s", string(cmdOut))
			if err != nil && !test.shouldFail {
				failures.AppendFailure(test.name, err)
				continue
			}

			if !test.shouldFail {
				clusterSize := ExpectedClusterSize(testCouchbase)
				if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, constants.Retries60); err != nil {
					t.Logf("error: %v", err)
					failures.AppendFailure(test.name, err)
					continue
				}
			}

			testCouchbase, err = YAMLToCluster("./resources/validation/temp.yaml")
			if err != nil {
				t.Logf("error: %v", err)
				failures.AppendFailure(test.name, err)
				continue
			}
		}

		for _, param := range test.paramsIn {
			t.Logf("setting parameter: %+v", param)
			if err := SetClusterParameter(testCouchbase, param); err != nil {
				t.Logf("error: %v", err)
				failures.AppendFailure(test.name, err)
				continue
			}
		}

		if err := ClusterToYAML(testCouchbase, "./resources/validation/temp.yaml"); err != nil {
			t.Logf("error: %v", err)
			failures.AppendFailure(test.name, err)
			continue
		}

		cmdOut, err := RunCBOPCTL(command)
		t.Logf("Returned: %s", string(cmdOut))
		if err != nil && !test.shouldFail {
			failures.AppendFailure(test.name, err)
			continue
		}

		if test.shouldWarn {
			if !strings.Contains(string(cmdOut), test.expectedWarn) || test.expectedWarn == "" {
				t.Logf("expected warning: %+v \n returned message: %+v \n", test.expectedWarn, string(cmdOut))
				failures.AppendFailure(test.name, errors.New("incorrect warning"))
				continue
			}
		}

		for _, message := range test.expectedMessages {
			if !strings.Contains(string(cmdOut), message) || message == "" {
				t.Logf("expected message: %+v \n returned message: %+v \n", message, string(cmdOut))
				failures.AppendFailure(test.name, errors.New("incorrect message"))
				continue
			}
		}

		clusters, err := targetKube.CRClient.CouchbaseV1().CouchbaseClusters(f.Namespace).List(metav1.ListOptions{})
		if test.shouldFail {
			if command == "delete" || command == "apply" {
				if len(clusters.Items) != 1 {
					failures.AppendFailure(test.name, errors.New("cluster deletion should fail"))
					continue
				}
			}

			if command == "create" {
				if len(clusters.Items) != 0 {
					failures.AppendFailure(test.name, errors.New("cluster creation should fail"))
					continue
				}
			}
		} else {
			if command == "delete" {
				if len(clusters.Items) != 0 {
					failures.AppendFailure(test.name, errors.New("cluster deletion should work"))
					continue
				}
				if _, err := e2eutil.WaitPodsDeleted(targetKube.KubeClient, f.Namespace, constants.Retries30, metav1.ListOptions{LabelSelector: constants.CouchbaseLabel}); err != nil {
					failures.AppendFailure(test.name, err)
					continue
				}
				t.Logf("deleted couchbase cluster: \n%+v", testCouchbase)
			} else {
				if len(clusters.Items) != 1 {
					failures.AppendFailure(test.name, errors.New("only one cluster should be created"))
					continue
				}
				testCouchbase = &clusters.Items[0]
				for _, param := range test.paramsOut {
					t.Logf("verifying parameter: %+v", param)
					if err := VerifyClusterParameter(testCouchbase, param); err != nil {
						failures.AppendFailure(test.name, err)
						continue
					}
				}

				clusterSize := ExpectedClusterSize(testCouchbase)
				if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, constants.Retries60); err != nil {
					t.Logf("error: %v", err)
					failures.AppendFailure(test.name, err)
					continue
				}
				t.Logf("created couchbase cluster: \n%+v", testCouchbase)
			}
		}
	}
	// Removing deployment if any
	if !f.SkipTeardown {
		e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir, kubeName, t.Name())
	}
	failures.CheckFailures(t)
}

func TestValidationCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	supportedTimeUnits := []string{"ns", "us", "ms", "s", "m", "h"}

	testDefs := []testDef{
		{
			name:             "create default yaml",
			paramsIn:         []parameter{},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" created"},
		},
	}

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name: "Apply spec.logRetentionTime with time duration in '" + timeUnit + "'",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "LogRetentionTime"},
					fieldType:  "string",
					fieldValue: "1" + timeUnit,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" created"},
		}
		testDefs = append(testDefs, testDefCase)
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		// Spec.ExposedFeatures list validation
		{
			name: "Validate spec.exposedFeatures field values",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ExposedFeatures"},
					fieldType:  "array",
					fieldValue: `["admin", "cleint", "xdcr"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.exposedFeatures in body should be one of [admin xdcr client]"},
		},
		{
			name: "Validate spec.exposedFeatures fields uniqueness",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ExposedFeatures"},
					fieldType:  "array",
					fieldValue: `["admin", "client", "xdcr", "admin"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.exposedFeatures in body shouldn't contain duplicates"},
		},

		// Bucket validation test
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
			name: "Validate spec.bucket.enableIndexReplica with memcached bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "2", "EnableIndexReplica"},
					fieldType:  "bool",
					fieldValue: "true",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"enableReplicaIndex in spec.buckets[2] must be of type nil: \"Bucket type is memcached\""},
		},
		{
			name: "Validate spec.bucket.enableIndexReplica with ephemeral bucket",
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
			name: "Validate spec.bucket.replicas with memcached bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "2", "BucketReplicas"},
					fieldType:  "int",
					fieldValue: "1",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"replicas in spec.buckets[2] must be of type nil: \"Bucket type is memcached\""},
		},
		{
			name: "Validate spec.bucket.conflictResolution with memcached bucket",
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
			name: "Validate spec.bucket.evictionPolicy with memcached bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "2", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "valueOnly",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[2] must be of type nil: \"Bucket type is memcached\""},
		},
		{
			name: "Validate spec.bucket.ioPriority with memcached bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "2", "IoPriority"},
					fieldType:  "string",
					fieldValue: "low",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"ioPriority in spec.buckets[2] must be of type nil: \"Bucket type is memcached\""},
		},
		{
			name: "Validate spec.bucket.conflictResolution with couchbase bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "ConflictResolution"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"conflictResolution in spec.buckets[0] is required"},
		},
		{
			name: "Validate spec.bucket.evictionPolicy with couchbase bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut:  []parameter{},
			shouldFail: true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[0] is required",
				"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
		{
			name: "Validate spec.bucket.ioPriority with couchbase bucket",
			paramsIn: []parameter{

				{
					field:      []string{"Spec", "BucketSettings", "0", "IoPriority"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"ioPriority in spec.buckets[0] is required"},
		},
		{
			name: "Validate spec.bucket.conflictResolution with ephemeral bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "3", "ConflictResolution"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"conflictResolution in spec.buckets[3] is required"},
		},
		{
			name: "Validate spec.bucket.evictionPolicy with ephemeral bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "3", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut:  []parameter{},
			shouldFail: true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[3] is required",
				"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},
		{
			name: "Validate spec.bucket.ioPriority with ephemeral bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "3", "IoPriority"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"ioPriority in spec.buckets[3] is required"},
		},
		{
			name: "Validate spec.buckets.evictionPolicy as valueOnly for ephemeral bucket",
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
			name: "Validate spec.buckets.evictionPolicy as fullEviction for ephemeral bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "3", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "fullEviction",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},
		{
			name: "Validate spec.buckets.evictionPolicy as noEviction for couchbase bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "noEviction",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
		{
			name: "Validate spec.buckets.evictionPolicy as nruEviction for couchbase bucket",
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
		{
			name: "Validate spec.buckets.name for duplicate values",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketName"},
					fieldType:  "string",
					fieldValue: "default3",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets.name in body shouldn't contain duplicates"},
		},
		{
			name: "Validate spec.buckets.quota with more than declared",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "BucketMemoryQuota"},
					fieldType:  "int",
					fieldValue: "601",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[*].memoryQuota in body should be less than or equal to 600"},
		},

		// Server settings validation
		{
			name: "Validate server-settings services fields",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "indxe", "query", "search"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers.services in body should be one of [data index query search eventing analytics]"},
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
		{
			name: "Validate spec.adminConsoleServices fields uniqueness",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "index", "index", "search"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name: "Validate spec.serverSettings.services fields for uniqueness",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "index", "data"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[0].services in body shouldn't contain duplicates"},
		},

		// ServerGroups list validation
		{
			name: "Validate spec.serverGroups uniqueness",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerGroups"},
					fieldType:  "array",
					fieldValue: `["NewGroupUpdate-1", "NewGroupUpdate-1"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.serverGroups in body shouldn't contain duplicates"},
		},
		{
			name: "Validate spec.servers.serverGroups list uniqueness",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "2", "ServerGroups"},
					fieldType:  "array",
					fieldValue: `["us-east-1a", "us-east-1b", "us-east-1a"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[2].serverGroups in body shouldn't contain duplicates"},
		},

		// Admin console services field validation
		{
			name: "Validate AdminConsoleService fields",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "indxe", "query", "search"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"validation failure list:\nspec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},

		// Persistent volume claim cases
		/*
			// K8S-485, targetted for 1.2.0
			{
				name: "Validate spec.volumeClaimTemplates.Spec.storageClassName to be defined",
				paramsIn: []parameter{
					{
						field:          []string{"Spec", "VolumeClaimTemplates", "0", "Spec", "StorageClassName"},
						fieldType:      "string",
						fieldValue:     "unavailableStorageClass",
						fieldIsPointer: true,
					},
				},
				paramsOut:        []parameter{},
				shouldFail:       true,
				expectedMessages: []string{"spec.volumeClaimTemplates[0].Spec.storageClassName in body should be unique"},
			},
		*/
		{
			name: "Create PVC cluster with unavailable volume claim template defined in spec",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "VolumeClaimTemplates", "0", "ObjectMeta", "Name"},
					fieldType:  "string",
					fieldValue: "InvalidVolumeClaim",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"couchbase\" in spec.volumeClaimTemplates[*].metadata.name is required\n\"couchbase\" in spec.volumeClaimTemplates[*].metadata.name is required\n\"couchbase\" in spec.volumeClaimTemplates[*].metadata.name is required\n\"couchbase\" in spec.volumeClaimTemplates[*].metadata.name is required\n\"couchbase\" in spec.volumeClaimTemplates[*].metadata.name is required"},
		},

		// Verify for Default/Log volume mounts mutual exclusion
		{
			name: "Log volume defined on top of default volume mount in spec.servers.pod.volumeMounts.properties",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", "LogsClaim"},
					fieldType:  "string",
					fieldValue: "couchbase",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[0].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name: "default volume defined on top of log volume mounts specified in spec.servers.pod.volumeMounts.properties",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "3", "Pod", "VolumeMounts", "DefaultClaim"},
					fieldType:  "string",
					fieldValue: "couchbase-log-pv",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[3].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name: "Create with data service on top of stateless services",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "3", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "query", "search", "eventing", "analytics"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"default in spec.servers[3].pod.volumeMounts is required"},
		},

		// Verification for spec.securityContext field values
		// K8S-648, targetted for 1.2.0
		// Once fixed, need to add code to verify in TestNegValidationApply case as well
		/*
			{
				name: "Invalid type for spec.securityContext.RunAsUser",
				paramsIn: []parameter{
					{
						field:          []string{"Spec", "SecurityContext", "RunAsUser"},
						fieldType:      "string",
						fieldValue:     "1000",
						fieldIsPointer: true,
					},
				},
				paramsOut:        []parameter{},
				shouldFail:       true,
				expectedMessages: []string{"couchbaseclusters creation failed"},
			},
			{
				name: "Invalid type for spec.securityContext.RunAsNonRoot",
				paramsIn: []parameter{
					{
						field:          []string{"Spec", "SecurityContext", "RunAsNonRoot"},
						fieldType:      "string",
						fieldValue:     "true",
						fieldIsPointer: true,
					},
				},
				paramsOut:        []parameter{},
				shouldFail:       true,
				expectedMessages: []string{"couchbaseclusters creation failed"},
			},
			{
				name: "Invalid type for spec.securityContext.fsGroup",
				paramsIn: []parameter{
					{
						field:          []string{"Spec", "SecurityContext", "FsGroup"},
						fieldType:      "string",
						fieldValue:     "1000",
						fieldIsPointer: true,
					},
				},
				paramsOut:        []parameter{},
				shouldFail:       true,
				expectedMessages: []string{"couchbaseclusters creation failed"},
			},
			{
				name: "Invalid type for spec.paused",
				paramsIn: []parameter{
					{
						field:      []string{"Spec", "Paused"},
						fieldType:  "string",
						fieldValue: "false",
					},
				},
				paramsOut:        []parameter{},
				shouldFail:       true,
				expectedMessages: []string{"couchbaseclusters creation failed"},
			},
			{
				name: "Invalid type for spec.disableBucketManagement",
				paramsIn: []parameter{
					{
						field:      []string{"Spec", "DisableBucketManagement"},
						fieldType:  "string",
						fieldValue: "false",
					},
				},
				paramsOut:        []parameter{},
				shouldFail:       true,
				expectedMessages: []string{"couchbaseclusters creation failed"},
			},
		*/

		// Validation for logRetentionTime and logRetentionCount field
		{
			name: "Invalid value for spec.logRetentionTime",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "LogRetentionTime"},
					fieldType:  "string",
					fieldValue: "1",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.logRetentionTime in body should match '^\\d+(ns|us|ms|s|m|h)$'"},
		},
		{
			name: "Negative value for spec.logRetentionCount",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "LogRetentionCount"},
					fieldType:  "int",
					fieldValue: "-1",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.logRetentionCount in body should be greater than or equal to 0"},
		},

		// K8S-648, targetted for 1.2.0
		// Once fixed, need to add code to verify in TestNegValidationApply case as well
		/*
			{
				name: "Huge value for spec.logRetentionCount",
				paramsIn: []parameter{
					{
						field:      []string{"Spec", "LogRetentionCount"},
						fieldType:  "int",
						fieldValue: "9999999999999999999999",
					},
				},
				paramsOut:        []parameter{},
				shouldFail:       true,
				expectedMessages: []string{"spec.logRetentionCount in body should be greater than or equal to 0"},
			},
			{
				name: "Invalid type for spec.logRetentionTime",
				paramsIn: []parameter{
					{
						field:      []string{"Spec", "LogRetentionTime"},
						fieldType:  "int",
						fieldValue: "86400",
					},
				},
				paramsOut:        []parameter{},
				shouldFail:       true,
				expectedMessages: []string{"spec.logRetentionTime in body should match '^\\d+(ns|us|ms|s|m|h)$'"},
			},
		*/
	}

	// Cases to validate with invalidClaim name given in Pod.VolumeMounts.[Claims]
	volMountsMap := map[string]string{
		"DefaultClaim": "default",
		"DataClaim":    "data",
		"IndexClaim":   "index",
	}
	for mntField, mntName := range volMountsMap {
		testCase := testDef{
			name: "Validate spec.servers.pod.volumeMounts.properties." + mntName + " values to be defined",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", mntField},
					fieldType:  "string",
					fieldValue: "invalidClaim",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"invalidClaim\" in spec.volumeClaimTemplates[*].metadata.name is required"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with all volume mounts present in Pod.VolumeMounts but one of the Service missing in ServersSettings
	for _, serviceToSkip := range constants.StatefulCbServiceList {
		fieldValueToUse := constants.StatelessCbServiceList
		for _, statefulService := range constants.StatefulCbServiceList {
			if statefulService == serviceToSkip {
				continue
			}
			fieldValueToUse = append(fieldValueToUse, statefulService)
		}
		testCase := testDef{
			name: "Validate spec.servers.services with all persistent mount defined, but " + serviceToSkip + "service missing in config",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["` + strings.Join(fieldValueToUse, `","`) + `"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{serviceToSkip + " in spec.servers[1].services is required"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with Log PV only defined,but one of stateful service is included
	for _, statefulService := range constants.StatefulCbServiceList {
		testCase := testDef{
			name: "Validate spec.servers.services with only log volume mount defined, with " + statefulService + " service included",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "3", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["query", "search", "eventing", "` + statefulService + `"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"default in spec.servers[3].pod.volumeMounts is required"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases for defining Stateful claims without specifying Default volume mounts
	claimFieldNames := []string{"DataClaim", "IndexClaim"}
	for _, claimField := range claimFieldNames {
		testCase := testDef{
			name: "Validate by defining " + claimField + " without Default volume mount",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", claimField},
					fieldType:  "string",
					fieldValue: "couchbase",
				},
				{
					field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", "DefaultClaim"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"default in spec.servers[0].pod.volumeMounts is required"},
		}
		testDefs = append(testDefs, testCase)
	}
	// AnalyticsClaims is an array value
	testCase := testDef{
		name: "Validate by defining AnalyticsClaims without Default volume mount",
		paramsIn: []parameter{
			{
				field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", "AnalyticsClaims"},
				fieldType:  "array",
				fieldValue: `["couchbase"]`,
			},
			{
				field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", "DefaultClaim"},
				fieldType:  "string",
				fieldValue: "",
			},
		},
		paramsOut:        []parameter{},
		shouldFail:       true,
		expectedMessages: []string{"default in spec.servers[0].pod.volumeMounts is required"},
	}
	testDefs = append(testDefs, testCase)

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestValidationDefaultCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
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
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationDefaultCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
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
			expectedMessages: []string{"spec.cluster.dataServiceMemoryQuota in body should be greater than or equal to 256"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationConstraintsCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name: "more than 4 adminConsoleServices",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "index", "query", "search", "data"]`,
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
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "index", "query", "xxxxx"]`,
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
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

// cbopctl apply tests
func TestValidationApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	supportedTimeUnits := []string{"ns", "us", "ms", "s", "m", "h"}

	testDefs := []testDef{
		{
			name:             "Apply without any changes in cluster yaml",
			paramsIn:         []parameter{},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},

		{
			name: "Apply: update the bucket[0].name",
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

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name: "Apply spec.logRetentionTime with time duration in '" + timeUnit + "'",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "LogRetentionTime"},
					fieldType:  "string",
					fieldValue: "100" + timeUnit,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		}
		testDefs = append(testDefs, testDefCase)
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

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
			expectedMessages: []string{"spec.buckets.type in body should be one of [couchbase ephemeral memcached]"},
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

		// Verify for Default/Log volume mounts mutual exclusion
		{
			name: "Log volume defined on top of default volume mount in spec.servers.pod.volumeMounts.properties",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", "LogsClaim"},
					fieldType:  "string",
					fieldValue: "couchbase",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"logs in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name: "Default volume defined on top of log volume mount in spec.servers.pod.volumeMounts.properties",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "3", "Pod", "VolumeMounts", "DefaultClaim"},
					fieldType:  "string",
					fieldValue: "couchbase-log-pv",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"default in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name: "Log volume defined on top of default volume mount in spec.servers.pod.volumeMounts.properties",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "2", "Pod", "VolumeMounts", "LogsClaim"},
					fieldType:  "string",
					fieldValue: "couchbase-log-pv",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"logs in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},

		// Validation for logRetentionTime and logRetentionCount field
		{
			name: "Invalid value for spec.logRetentionTime",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "LogRetentionTime"},
					fieldType:  "string",
					fieldValue: "1",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.logRetentionTime in body should match '^\\d+(ns|us|ms|s|m|h)$'"},
		},
		{
			name: "Negative value for spec.logRetentionCount",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "LogRetentionCount"},
					fieldType:  "int",
					fieldValue: "-1",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.logRetentionCount in body should be greater than or equal to 0"},
		},
	}

	// Cases to validate with all volume mounts present in Pod.VolumeMounts but one of the Service missing in ServersSettings
	for _, serviceToSkip := range constants.StatefulCbServiceList {
		fieldValueToUse := constants.StatelessCbServiceList
		for _, statefulService := range constants.StatefulCbServiceList {
			if statefulService == serviceToSkip {
				continue
			}
			fieldValueToUse = append(fieldValueToUse, statefulService)
		}
		testCase := testDef{
			name: "Validate spec.servers.services with all persistent mount defined, but " + serviceToSkip + "service missing in config",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["` + strings.Join(fieldValueToUse, `","`) + `"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[1].services in body cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with Log PV defined along with stateful services
	for _, serviceName := range constants.StatefulCbServiceList {
		fieldValueToUse := constants.StatelessCbServiceList
		fieldValueToUse = append(fieldValueToUse, serviceName)
		testCase := testDef{
			name: "Validate spec.servers.services with only log volume mount defined, with " + serviceName + " service included",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "3", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["` + strings.Join(fieldValueToUse, `","`) + `"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[3].services in body cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate by defining Stateful volumes without defining DefaultClaim
	volMountsMap := map[string]string{
		"DataClaim":  "data",
		"IndexClaim": "index",
	}
	for mntField, mntName := range volMountsMap {
		testCase := testDef{
			name: "Validate by defining " + mntField + " without Default volume mount",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", mntField},
					fieldType:  "string",
					fieldValue: "couchbase",
				},
				{
					field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", "DefaultClaim"},
					fieldType:  "string",
					fieldValue: "",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{mntName + " in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}
	// AnalyticsClaims in an array parameter
	testCase := testDef{
		name: "Validate by defining AnalyticsClaims without Default volume mount",
		paramsIn: []parameter{
			{
				field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", "AnalyticsClaims"},
				fieldType:  "array",
				fieldValue: `["couchbase"]`,
			},
			{
				field:      []string{"Spec", "ServerSettings", "0", "Pod", "VolumeMounts", "DefaultClaim"},
				fieldType:  "string",
				fieldValue: "",
			},
		},
		paramsOut:        []parameter{},
		shouldFail:       true,
		expectedMessages: []string{"analytics in spec.servers[*].Pod.VolumeMounts cannot be updated"},
	}
	testDefs = append(testDefs, testCase)

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestValidationDefaultApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
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
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationDefaultApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
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
			expectedMessages: []string{"spec.cluster.dataServiceMemoryQuota in body should be greater than or equal to 256"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationConstraintsApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name: "more than 4 adminConsoleServices",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AdminConsoleServices"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "index", "query", "search", "data"]`,
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
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "index", "query", "xxxxx"]`,
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
			expectedWarn:     "spec.buckets.ioPriority in body should be one of [high low]",
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
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationImmutableApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		// Bucket spec updation
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
			expectedMessages: []string{"spec.buckets.conflictResolution in body should be one of [seqno lww]"},
		},
		{
			name: "Update spec.buckets.type value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "Type"},
					fieldType:  "string",
					fieldValue: "memcached",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[0].type in body cannot be updated"},
		},
		{
			name: "Update spec.buckets.conflictResolution value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "ConflictResolution"},
					fieldType:  "string",
					fieldValue: "lww",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.buckets[0].conflictResolution in body cannot be updated"},
		},
		{
			name: "Update spec.buckets.ioPriority value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "IoPriority"},
					fieldType:  "string",
					fieldValue: "low",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       false,
			shouldWarn:       true,
			expectedWarn:     "Warning: Changing the IO Priority will cause the bucket default1 to be temporarily unavailable",
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},
		{
			name: "Update spec.buckets.evictionPolicy value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "0", "EvictionPolicy"},
					fieldType:  "string",
					fieldValue: "valueOnly",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       false,
			shouldWarn:       true,
			expectedWarn:     "Warning: Changing the Eviction Policy will cause the bucket default1 to be temporarily unavailable",
			expectedMessages: []string{"couchbaseclusters \"cb-example\" applied"},
		},

		// ServerSettings service update
		{
			name: "Update spec.servers.services list",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "index", "search"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name: "Update spec.serverSettings.services values for uniqueness",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "0", "Services"},
					fieldType:  "v1.ServiceList",
					fieldValue: `["data", "data"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[0].services in body cannot be updated"},
		},

		// Cluster spec updation
		{
			name: "Update server version string",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "Version"},
					fieldType:  "string",
					fieldValue: "6.0.0-beta",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.version in body cannot be updated"},
		},
		{
			name: "Update AntiAffinity settings",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AntiAffinity"},
					fieldType:  "bool",
					fieldValue: "true",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.antiAffinity in body cannot be updated"},
		},
		{
			name: "Update AuthSecret value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "AuthSecret"},
					fieldType:  "string",
					fieldValue: "auth-secret-update",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.authSecret in body cannot be updated"},
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

		// Server groups updation
		{
			name: "Update server groups value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerGroups"},
					fieldType:  "array",
					fieldValue: `["NewGroupUpdate-1", "NewGroupUpdate-2"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.serverGroups in body cannot be updated"},
		},
		{
			name: "Update spec.servers.serverGroups list",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "2", "ServerGroups"},
					fieldType:  "array",
					fieldValue: `["us-east-1a", "us-east-1b", "us-east-1c"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servers[2].serverGroups in body cannot be updated"},
		},

		// Persistent volume spec updation
		{
			name: "Update spec.servers.pod.volumeMounts.properties.data value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", "DataClaim"},
					fieldType:  "string",
					fieldValue: "newVolumeMount",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"data in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name: "Update spec.servers.pod.volumeMounts.properties.default value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", "DefaultClaim"},
					fieldType:  "string",
					fieldValue: "newVolumeMount",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"default in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name: "Update spec.servers.pod.volumeMounts.properties.index value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", "IndexClaim"},
					fieldType:  "string",
					fieldValue: "newVolumeMount",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"index in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name: "Update spec.servers.pod.volumeMounts.properties.analytics value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", "AnalyticsClaims", "0"},
					fieldType:  "string",
					fieldValue: "newVolumeMount",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"analytics in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name: "Apply: Remove default volume claim template name",
			paramsIn: []parameter{
				{
					field:          []string{"Spec", "VolumeClaimTemplates", "0", "Spec", "StorageClassName"},
					fieldType:      "string",
					fieldValue:     "unavailableStorageClass",
					fieldIsPointer: true,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"storageClassName\" in spec.volumeClaimTemplates[*] cannot be updated"},
		},
		{
			name: "Update spec.volumeClaimTemplates.spec.resources.requests.storage value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "VolumeClaimTemplates", "0", "Spec", "Resources", "Requests"},
					fieldType:  "apiresource.Quantity",
					fieldValue: "10",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"storage\" in spec.volumeClaimTemplates[*].resources.requests cannot be updated"},
		},
		{
			name: "Update spec.volumeClaimTemplates.spec.resources.limits.storage value",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "VolumeClaimTemplates", "0", "Spec", "Resources", "Limits"},
					fieldType:  "apiresource.Quantity",
					fieldValue: "6",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"storage\" in spec.volumeClaimTemplates[*].resources.limits cannot be updated"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

//cbopctl delete tests
func TestValidationDelete(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
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
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "delete")
}

func TestNegValidationDelete(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
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
			expectedMessages: []string{"couchbaseclusters.couchbase.com \"cb-example1\" not found"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "delete")
}

/*******************************************************************
************ Test cases for RZA / Server group testing *************
********************************************************************/

// Deploy couchbase cluster over non existent server group
func TestRzaNegCreateCluster(t *testing.T) {
	testDefs := []testDef{
		{
			name: "Creating cluster with incorrect server-group in static config",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerGroups"},
					fieldType:  "array",
					fieldValue: `["InvalidGroup-1", "InvalidGroup-2"]`,
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servergroups in body is invalid"},
		},
		{
			name:             "Create cluster with server-group missing in default and class specific config",
			paramsIn:         []parameter{},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"spec.servergroups missing for "},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}
