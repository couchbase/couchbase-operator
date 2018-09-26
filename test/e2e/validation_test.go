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

	err = ioutil.WriteFile(yamlPath, cbyaml, 0644)
	if err != nil {
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
			err := json.Unmarshal([]byte(param.fieldValue), &newArray)
			if err != nil {
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
			err := json.Unmarshal([]byte(param.fieldValue), &serviceArray)
			if err != nil {
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

func runValidationTest(t *testing.T, testDefs []testDef, targetKubeName, command string) {
	f := framework.Global
	failures := failureList{}
	targetKube := f.ClusterSpec[targetKubeName]
	os.Setenv("KUBECONFIG", e2eutil.GetKubeConfigToUse(f.KubeType, targetKubeName))
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
		e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir, targetKubeName, t.Name())

		if command == "apply" || command == "delete" {
			err = ClusterToYAML(testCouchbase, "./resources/validation/temp.yaml")
			if err != nil {
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
				err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries30)
				if err != nil {
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
			err = SetClusterParameter(testCouchbase, param)
			if err != nil {
				t.Logf("error: %v", err)
				failures.AppendFailure(test.name, err)
				continue
			}
		}

		err = ClusterToYAML(testCouchbase, "./resources/validation/temp.yaml")
		if err != nil {
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
				_, err = e2eutil.WaitPodsDeleted(targetKube.KubeClient, f.Namespace, e2eutil.Retries30, metav1.ListOptions{LabelSelector: "app=couchbase"})
				if err != nil {
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
					err = VerifyClusterParameter(testCouchbase, param)
					if err != nil {
						failures.AppendFailure(test.name, err)
						continue
					}
				}

				clusterSize := ExpectedClusterSize(testCouchbase)
				err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries30)
				if err != nil {
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
		e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir, targetKubeName, t.Name())
	}
	failures.CheckFailures(t)
}

func TestValidationCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:             "create default yaml",
			paramsIn:         []parameter{},
			paramsOut:        []parameter{},
			shouldFail:       false,
			expectedMessages: []string{"couchbaseclusters \"cb-example\" created"},
		},
	}
	kubeName := "NewCluster1"
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
					fieldValue: "[\"admin\", \"cleint\", \"xdcr\"]",
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
					fieldValue: "[\"admin\", \"client\", \"xdcr\", \"admin\"]",
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
			name: "Validate spec.bucket.bucketReplicas with memcached bucket",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "BucketSettings", "2", "BucketReplicas"},
					fieldType:  "int",
					fieldValue: "1",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"bucketReplicas in spec.buckets[2] must be of type nil: \"Bucket type is memcached\""},
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
					fieldValue: "[\"data\", \"indxe\", \"query\", \"search\"]",
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
					fieldValue: "[\"data\", \"index\", \"index\", \"search\"]",
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
					fieldValue: "[\"data\", \"index\", \"data\"]",
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
					fieldValue: "[\"NewGroupUpdate-1\", \"NewGroupUpdate-1\"]",
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
					fieldValue: "[\"us-east-1a\", \"us-east-1b\", \"us-east-1a\"]",
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
					fieldValue: "[\"data\", \"indxe\", \"query\", \"search\"]",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"validation failure list:\nspec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},

		// Persistent volume claim cases
		// this test is not valid for 1.0.0
		//{
		//	name: "Validate spec.volumeClaimTemplates.Spec.storageClassName to be defined",
		//	paramsIn: []parameter{
		//		{
		//			field:          []string{"Spec", "VolumeClaimTemplates", "0", "Spec", "StorageClassName"},
		//			fieldType:      "string",
		//			fieldValue:     "unavailableStorageClass",
		//			fieldIsPointer: true,
		//		},
		//	},
		//	paramsOut:        []parameter{},
		//	shouldFail:       true,
		//	expectedMessages: []string{"spec.volumeClaimTemplates[0].Spec.storageClassName in body should be unique"},
		//},
		{
			name: "Create PVC cluster with unavailable default volume claim in pod spec",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", "DefaultClaim"},
					fieldType:  "string",
					fieldValue: "InvalidDefaultClaim",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"InvalidDefaultClaim\" in spec.volumeClaimTemplates[*].metadata.name is required"},
		},
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
		{
			name: "Validate spec.servers.pod.volumeMounts.properties.data values to be defined",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", "DataClaim"},
					fieldType:  "string",
					fieldValue: "invalidClaim",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"invalidClaim\" in spec.volumeClaimTemplates[*].metadata.name is required"},
		},
		{
			name: "Validate spec.servers.pod.volumeMounts.properties.default values to be defined",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", "DefaultClaim"},
					fieldType:  "string",
					fieldValue: "invalidClaim",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"invalidClaim\" in spec.volumeClaimTemplates[*].metadata.name is required"},
		},
		{
			name: "Validate spec.servers.pod.volumeMounts.properties.index values to be defined",
			paramsIn: []parameter{
				{
					field:      []string{"Spec", "ServerSettings", "1", "Pod", "VolumeMounts", "IndexClaim"},
					fieldType:  "string",
					fieldValue: "invalidClaim",
				},
			},
			paramsOut:        []parameter{},
			shouldFail:       true,
			expectedMessages: []string{"\"invalidClaim\" in spec.volumeClaimTemplates[*].metadata.name is required"},
		},
	}
	kubeName := "NewCluster1"
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
	kubeName := "NewCluster1"
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
			expectedMessages: []string{"spec.buckets[*].memoryQuota in body should be less than or equal to 256"},
		},
	}
	kubeName := "NewCluster1"
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
					fieldType:  "v1.ServiceList",
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
	kubeName := "NewCluster1"
	runValidationTest(t, testDefs, kubeName, "create")
}

// cbopctl apply tests

func TestValidationApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
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
	kubeName := "NewCluster1"
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
	kubeName := "NewCluster1"
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
	kubeName := "NewCluster1"
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
			expectedMessages: []string{"spec.buckets[*].memoryQuota in body should be less than or equal to 256"},
		},
	}
	kubeName := "NewCluster1"
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
					fieldType:  "v1.ServiceList",
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
	kubeName := "NewCluster1"
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
			expectedMessages: []string{"spec.buckets[0].conflictResolution in body cannot be updated"},
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
					fieldValue: "[\"data\", \"index\", \"search\"]",
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
					fieldValue: "[\"data\", \"data\"]",
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
					fieldValue: "[\"NewGroupUpdate-1\", \"NewGroupUpdate-2\"]",
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
					fieldValue: "[\"us-east-1a\", \"us-east-1b\", \"us-east-1c\"]",
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
	kubeName := "NewCluster1"
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
	kubeName := "NewCluster1"
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
	kubeName := "NewCluster1"
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
					fieldValue: "[\"InvalidGroup-1\", \"InvalidGroup-2\"]",
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
	targetKubeName := "NewCluster1"
	runValidationTest(t, testDefs, targetKubeName, "create")
}
