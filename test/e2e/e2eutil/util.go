package e2eutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

var (
	BasicClusterConfig = map[string]string{
		"dataServiceMemQuota":      strconv.Itoa(constants.Mem256Mb),
		"indexServiceMemQuota":     strconv.Itoa(constants.Mem256Mb),
		"searchServiceMemQuota":    strconv.Itoa(constants.Mem256Mb),
		"eventingServiceMemQuota":  strconv.Itoa(constants.Mem256Mb),
		"analyticsServiceMemQuota": strconv.Itoa(constants.Mem1Gb),
		"indexStorageSetting":      "memory_optimized",
		"autoFailoverTimeout":      "30",
		"autoFailoverMaxCount":     "1"}

	BasicClusterConfig2 = map[string]string{
		"dataServiceMemQuota":      strconv.Itoa(constants.Mem1Gb),
		"indexServiceMemQuota":     strconv.Itoa(constants.Mem256Mb),
		"searchServiceMemQuota":    strconv.Itoa(constants.Mem256Mb),
		"eventingServiceMemQuota":  strconv.Itoa(constants.Mem256Mb),
		"analyticsServiceMemQuota": strconv.Itoa(constants.Mem1Gb),
		"indexStorageSetting":      "memory_optimized",
		"autoFailoverTimeout":      "30",
		"autoFailoverMaxCount":     "1"}

	BasicOneReplicaBucket = map[string]string{
		"bucketName":         "default",
		"bucketType":         "couchbase",
		"bucketMemoryQuota":  "100",
		"bucketReplicas":     "1",
		"ioPriority":         "high",
		"evictionPolicy":     "fullEviction",
		"conflictResolution": "seqno",
		"enableFlush":        "true",
		"enableIndexReplica": "false"}

	BasicTwoReplicaBucket = map[string]string{
		"bucketName":         "default",
		"bucketType":         "couchbase",
		"bucketMemoryQuota":  "100",
		"bucketReplicas":     "2",
		"ioPriority":         "high",
		"evictionPolicy":     "fullEviction",
		"conflictResolution": "seqno",
		"enableFlush":        "true",
		"enableIndexReplica": "false"}
)

// randomSuffix generates a 5 character random suffix to be appended to
// k8s resources to avoid namespace collisions (especially events)
func RandomSuffix() string {
	// Seed the PRNG so we get vagely random suffixes across runs
	rand.Seed(time.Now().UnixNano())

	// Generate a random 5 character suffix for the cluster name
	suffix := ""
	for i := 0; i < 5; i++ {
		// Our alphabet is 0-9 a-z, so 36 characters
		ordinal := rand.Intn(36)
		// Less than 10 places it in the 0-9 range, otherwise in
		// the a-z range
		if ordinal < 10 {
			ordinal += int('0')
		} else {
			ordinal += int('a') - 10
		}
		// Append to the name
		suffix += string(rune(ordinal))
	}
	return suffix
}

func GetClusterConfigMap(dataMem, indexMem, searchMem, eventingMem, cbasMem, autoFailoverTimeout, autoFailoverMaxCount int, autoFailoverServerGroup bool) map[string]string {
	return map[string]string{
		"dataServiceMemQuota":      strconv.Itoa(dataMem),
		"indexServiceMemQuota":     strconv.Itoa(indexMem),
		"searchServiceMemQuota":    strconv.Itoa(searchMem),
		"eventingServiceMemQuota":  strconv.Itoa(eventingMem),
		"analyticsServiceMemQuota": strconv.Itoa(cbasMem),
		"indexStorageSetting":      "memory_optimized",
		"autoFailoverTimeout":      strconv.Itoa(autoFailoverTimeout),
		"autoFailoverMaxCount":     strconv.Itoa(autoFailoverMaxCount),
		"autoFailoverServerGroup":  strconv.FormatBool(autoFailoverServerGroup),
		//"autoFailoverOnDiskIssues":        strconv.FormatBool(diskFailover),
		//"autoFailoverOnDiskIssuesTimeout": strconv.Itoa(diskFailoverTimeout),
	}
}

func GetServiceConfigMap(size int, configName string, serviceList []string) map[string]string {
	return map[string]string{
		"name":     configName,
		"size":     strconv.Itoa(size),
		"services": strings.Join(serviceList, ","),
	}
}

func GetClassSpecificServiceConfigMap(size int, configName string, serviceList, serverGroupList []string) map[string]string {
	return map[string]string{
		"name":         configName,
		"services":     strings.Join(serviceList, ","),
		"size":         strconv.Itoa(size),
		"serverGroups": strings.Join(serverGroupList, ","),
	}
}

func GetBucketConfigMap(bucketName, bucketType, ioPriority string, memQuotaInMB, replicas int, enableFlush, enableIndexReplicas bool) map[string]string {
	return map[string]string{
		"bucketName":         bucketName,
		"bucketType":         bucketType,
		"bucketMemoryQuota":  strconv.Itoa(memQuotaInMB),
		"bucketReplicas":     strconv.Itoa(replicas),
		"ioPriority":         ioPriority,
		"evictionPolicy":     "fullEviction",
		"conflictResolution": "seqno",
		"enableFlush":        strconv.FormatBool(enableFlush),
		"enableIndexReplica": strconv.FormatBool(enableIndexReplicas),
	}
}

// newClusterFromSpec creates a cluster and waits for various ready conditions.
// Performs retries and garbage collection in the event of transient failure
func newClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *couchbasev1.CouchbaseCluster) (*couchbasev1.CouchbaseCluster, error) {
	// Create the cluster.
	cluster, err := CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
	if err != nil {
		return nil, err
	}

	MustWaitClusterStatusHealthy(t, k8s, cluster, 15*time.Minute)

	// If any buckets are specified wait for these to become active.
	buckets := cluster.Spec.BucketNames()
	if len(buckets) > 0 {
		MustWaitUntilBucketsExists(t, k8s, cluster, buckets, 5*time.Minute)
		if err != nil {
			return cluster, err
		}
	}

	// Update the cluster status, this is important for the test, especially if the cluster
	// name is auto-generated.
	updatedCluster, err := getClusterCRD(k8s.CRClient, cluster)
	if err != nil {
		return cluster, err
	}
	return updatedCluster, nil
}

func MustNewClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *couchbasev1.CouchbaseCluster) *couchbasev1.CouchbaseCluster {
	cluster, err := newClusterFromSpec(t, k8s, namespace, clusterSpec)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func NewClusterFromSpecAsync(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *couchbasev1.CouchbaseCluster) (*couchbasev1.CouchbaseCluster, error) {
	// Create the cluster
	cluster, err := CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

func MustNewClusterFromSpecAsync(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *couchbasev1.CouchbaseCluster) *couchbasev1.CouchbaseCluster {
	cluster, err := NewClusterFromSpecAsync(t, k8s, namespace, clusterSpec)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// Creates Cluster Spec object and returns it
func CreateClusterSpec(secretName string, config map[string]map[string]string) couchbasev1.ClusterSpec {
	return e2espec.CreateClusterSpec(constants.ClusterNamePrefix, secretName, config)
}

// Creates Couchbase cluster object and returns it
func CreateClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, adminConsoleExposed bool, spec couchbasev1.ClusterSpec) (*couchbasev1.CouchbaseCluster, error) {
	crd := e2espec.CreateClusterCRD(constants.ClusterNamePrefix, adminConsoleExposed, spec)
	return newClusterFromSpec(t, k8s, namespace, crd)
}

func MustCreateClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, adminConsoleExposed bool, spec couchbasev1.ClusterSpec) *couchbasev1.CouchbaseCluster {
	cluster, err := CreateClusterFromSpec(t, k8s, namespace, adminConsoleExposed, spec)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// Creates Couchbase cluster object and returns it
func CreateClusterFromSpecSystemTest(t *testing.T, k8s *types.Cluster, namespace string, adminConsoleExposed bool, spec couchbasev1.ClusterSpec, ctx *TlsContext) (*couchbasev1.CouchbaseCluster, error) {
	crd := e2espec.CreateClusterCRD(constants.ClusterNamePrefix, adminConsoleExposed, spec)
	if ctx != nil {
		crd.Name = ctx.ClusterName
		crd.Spec.TLS = &couchbasev1.TLSPolicy{
			Static: &couchbasev1.StaticTLS{
				Member: &couchbasev1.MemberSecret{
					ServerSecret: ctx.ClusterSecretName,
				},
				OperatorSecret: ctx.OperatorSecretName,
			},
		}
	}
	return newClusterFromSpec(t, k8s, namespace, crd)
}

// Creates Couchbase cluster object and returns it
func CreateClusterFromSpecNoWait(t *testing.T, k8s *types.Cluster, namespace string, adminConsoleExposed bool, spec couchbasev1.ClusterSpec) (*couchbasev1.CouchbaseCluster, error) {
	crd := e2espec.CreateClusterCRD(constants.ClusterNamePrefix, adminConsoleExposed, spec)
	return CreateCluster(t, k8s.CRClient, namespace, crd)
}

// NewClusterBasic creates a basic cluster, retrying if an error is encountered and
// performing garbage collection
func NewClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) *couchbasev1.CouchbaseCluster {
	cluster, err := NewClusterBasic(t, k8s, namespace, size, withBucket, exposed)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewTLSClusterBasic creates a new TLS enabled basic cluster, retrying if an error is encountered
func NewTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.TLS = &couchbasev1.TLSPolicy{
		Static: &couchbasev1.StaticTLS{
			Member: &couchbasev1.MemberSecret{
				ServerSecret: ctx.ClusterSecretName,
			},
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) *couchbasev1.CouchbaseCluster {
	cluster, err := NewTLSClusterBasic(t, k8s, namespace, size, withBucket, exposed, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewTLSClusterBasicNoWait creates a new TLS enabled basic cluster asynchronously
func NewTLSClusterBasicNoWait(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.TLS = &couchbasev1.TLSPolicy{
		Static: &couchbasev1.StaticTLS{
			Member: &couchbasev1.MemberSecret{
				ServerSecret: ctx.ClusterSecretName,
			},
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
}

// MustNotNewTLSClusterBasic ensures that a cluster is not created given the specification
func MustNotNewTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) {
	if _, err := NewTLSClusterBasicNoWait(t, k8s, namespace, size, withBucket, exposed, ctx); err == nil {
		Die(t, fmt.Errorf("cluster created unexpectedly"))
	}
}

// NewTlsXdcrClusterBasic creates a new TLS and XDCR enabled basic cluster.
func NewTlsXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicXdcrCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.TLS = &couchbasev1.TLSPolicy{
		Static: &couchbasev1.StaticTLS{
			Member: &couchbasev1.MemberSecret{
				ServerSecret: ctx.ClusterSecretName,
			},
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewTlsXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) *couchbasev1.CouchbaseCluster {
	cluster, err := NewTlsXdcrClusterBasic(t, k8s, namespace, size, withBucket, exposed, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewXdcrClusterBasic creates a basic cluster, retrying if an error is encountered and
// performing garbage collection
func NewXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicXdcrCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	cluster, err := newClusterFromSpec(t, k8s, namespace, clusterSpec)
	if err != nil {
		Die(t, err)
	}
	MustWaitClusterStatusHealthy(t, k8s, cluster, 5*time.Minute)
	return cluster, err
}

func MustNewXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) *couchbasev1.CouchbaseCluster {
	cluster, err := NewXdcrClusterBasic(t, k8s, namespace, size, withBucket, exposed)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func NewClusterBasicNoWait(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	return CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
}

// NewStatefulCluster creates a cluster with persistent block storage, retrying if an
// error is encountered and performing garbage collection
func NewStatefulCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewStatefulCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewStatefulCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) *couchbasev1.CouchbaseCluster {
	cluster, err := NewStatefulCluster(t, k8s, namespace, size, withBucket, exposed)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewSupportableCluster creates a cluster with two MDS groups of 'size'.  The first is
// a stateful group with data and index enabled.  The second is a stateless group with
// query enabled.
func NewSupportableCluster(t *testing.T, k8s *types.Cluster, namespace string, size int) (*couchbasev1.CouchbaseCluster, error) {
	spec := e2espec.NewSupportableCluster(size)
	return newClusterFromSpec(t, k8s, namespace, spec)
}

// MustNewSupportableCluster creates a supportable cluster as described by NewSupportableCluster
// but dies on error.
func MustNewSupportableCluster(t *testing.T, k8s *types.Cluster, namespace string, size int) *couchbasev1.CouchbaseCluster {
	cluster, err := NewSupportableCluster(t, k8s, namespace, size)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewSupportableTLSCluster creates a cluster with two MDS groups of 'size'.  The first is
// a stateful group with data and index enabled.  The second is a stateless group with
// query enabled.
func NewSupportableTLSCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TlsContext) (*couchbasev1.CouchbaseCluster, error) {
	cluster := e2espec.NewClusterCRD("", e2espec.NewSupportableClusterSpec(size))
	cluster.Name = ctx.ClusterName
	cluster.Spec.TLS = &couchbasev1.TLSPolicy{
		Static: &couchbasev1.StaticTLS{
			Member: &couchbasev1.MemberSecret{
				ServerSecret: ctx.ClusterSecretName,
			},
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, cluster)
}

// MustNewSupportableTLSCluster creates a supportable cluster as described by NewSupportableTLSCluster
// but dies on error.
func MustNewSupportableTLSCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TlsContext) *couchbasev1.CouchbaseCluster {
	cluster, err := NewSupportableTLSCluster(t, k8s, namespace, size, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewClusterMulti creates a multi cluster, retrying if an error is encountered and
// performing garbage collection
func NewClusterMulti(t *testing.T, k8s *types.Cluster, namespace string, config map[string]map[string]string, exposed bool) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewMultiCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, config, exposed)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewClusterMulti(t *testing.T, k8s *types.Cluster, namespace string, config map[string]map[string]string, exposed bool) *couchbasev1.CouchbaseCluster {
	cluster, err := NewClusterMulti(t, k8s, namespace, config, exposed)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewClusterMultiNoWait creates a multi cluster, but doesn't wait for any events.
// Used in cases where the cluster is expected to fail
func NewClusterMultiNoWait(t *testing.T, k8s *types.Cluster, namespace string, config map[string]map[string]string) (*couchbasev1.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewMultiCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, config, false)
	return CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
}

func MustNewClusterMultiNoWait(t *testing.T, k8s *types.Cluster, namespace string, config map[string]map[string]string) *couchbasev1.CouchbaseCluster {
	cluster, err := NewClusterMultiNoWait(t, k8s, namespace, config)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func AddServices(t *testing.T, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, newService couchbasev1.ServerConfig, timeout time.Duration) (*couchbasev1.CouchbaseCluster, error) {
	settings := append(cl.Spec.ServerSettings, newService)
	return PatchCluster(t, k8s, cl, jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings", settings), timeout)
}

func MustAddServices(t *testing.T, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, newService couchbasev1.ServerConfig, timeout time.Duration) *couchbasev1.CouchbaseCluster {
	couchbase, err := AddServices(t, k8s, cl, newService, timeout)
	if err != nil {
		Die(t, err)
	}
	return couchbase
}

func RemoveServices(t *testing.T, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, removeServiceName string, timeout time.Duration) (*couchbasev1.CouchbaseCluster, error) {
	newServiceConfig := []couchbasev1.ServerConfig{}
	for _, service := range cl.Spec.ServerSettings {
		if service.Name != removeServiceName {
			newServiceConfig = append(newServiceConfig, service)
		}
	}
	return PatchCluster(t, k8s, cl, jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings", newServiceConfig), timeout)
}

func MustRemoveServices(t *testing.T, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, removeServiceName string, timeout time.Duration) *couchbasev1.CouchbaseCluster {
	couchbase, err := RemoveServices(t, k8s, cl, removeServiceName, timeout)
	if err != nil {
		Die(t, err)
	}
	return couchbase
}

func ScaleServices(t *testing.T, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, servicesMap map[string]int, timeout time.Duration) (*couchbasev1.CouchbaseCluster, error) {
	newServiceConfig := []couchbasev1.ServerConfig{}
	for _, service := range cl.Spec.ServerSettings {
		for serviceName, size := range servicesMap {
			if serviceName == service.Name {
				service.Size = size
			}
		}
		newServiceConfig = append(newServiceConfig, service)
	}
	return PatchCluster(t, k8s, cl, jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings", newServiceConfig), timeout)
}

func MustScaleServices(t *testing.T, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, servicesMap map[string]int, timeout time.Duration) *couchbasev1.CouchbaseCluster {
	couchbase, err := ScaleServices(t, k8s, cl, servicesMap, timeout)
	if err != nil {
		Die(t, err)
	}
	return couchbase
}

// PatchCluster updates the specified cluster with a list of JSON patch objects, returning the updated cluster
func PatchCluster(t *testing.T, k8s *types.Cluster, cluster *couchbasev1.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) (*couchbasev1.CouchbaseCluster, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return cluster, retryutil.Retry(ctx, 5*time.Second, IntMax, func() (done bool, err error) {
		// Get the current cluster resource
		before, err := k8s.CRClient.CouchbaseV1().CouchbaseClusters(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Apply the patch set to the cluster
		after := before.DeepCopy()
		if err := jsonpatch.Apply(after, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// If we are not modifiying e.g. just testing, then return ok
		if reflect.DeepEqual(before, after) {
			return true, nil
		}

		// Attempt to post the update, updating the cluster
		updated, err := k8s.CRClient.CouchbaseV1().CouchbaseClusters(cluster.Namespace).Update(after)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Everything successful
		cluster = updated
		return true, nil
	})
}

// MustPatchCluster patches the cluster with a list of JSON patch objects, returning the updated cluster and dying on error
func MustPatchCluster(t *testing.T, k8s *types.Cluster, cluster *couchbasev1.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev1.CouchbaseCluster {
	cluster, err := PatchCluster(t, k8s, cluster, patches, timeout)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// MustNotPatchCluster patches the cluster with a list of JSON patch objects, dying if the test succeeded.
func MustNotPatchCluster(t *testing.T, k8s *types.Cluster, cluster *couchbasev1.CouchbaseCluster, patches jsonpatch.PatchSet) {
	if _, err := PatchCluster(t, k8s, cluster, patches, 30*time.Second); err == nil {
		Die(t, fmt.Errorf("cluster patch applied unexpectedly"))
	}
}

func DestroyCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cluster *couchbasev1.CouchbaseCluster) {
	if err := DeleteCluster(t, crClient, kubeClient, cluster, 10); err != nil {
		Die(t, err)
	}
}

func CleanUpCluster(t *testing.T, k8s *types.Cluster, namespace, logDir, kubeName, testName string) {
	// Creates dir for kubename
	logDir = filepath.Join(logDir, kubeName)
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		t.Log(err)
		return
	}

	// Pulls operator pod logs
	if err := WriteLogs(k8s.KubeClient, namespace, logDir, testName); err != nil {
		t.Logf("Error: %v", err)
	}

	CleanK8Cluster(t, k8s, namespace)
}

func DeleteCbCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cbCluster *couchbasev1.CouchbaseCluster) {
	t.Logf("Attempting to delete: [%v]", cbCluster.Name)
	if err := k8sutil.DeleteCouchbaseCluster(crClient, cbCluster); err != nil {
		t.Logf("Error: %v", err)
	} else {
		t.Logf("Successfully deleted: [%v]", cbCluster.Name)
	}
	pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cbCluster.Name})
	if err != nil {
		t.Logf("Error: Failed to get pods %v", err)
	}
	killPods := []string{}
	for _, pod := range pods.Items {
		killPods = append(killPods, pod.Name)
	}
	t.Logf("Killing pods: %v", killPods)
	if err := KillMembers(kubeClient, namespace, cbCluster.Name, killPods...); err != nil {
		t.Logf("Failed to kill members: %v", err)
	}
}

func CleanK8Cluster(t *testing.T, k8s *types.Cluster, namespace string) {
	services, err := k8s.KubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err == nil {
		for _, service := range services.Items {
			_ = k8s.KubeClient.CoreV1().Services(namespace).Delete(service.Name, metav1.NewDeleteOptions(0))
		}
	}

	jobs, err := k8s.KubeClient.BatchV1().Jobs(namespace).List(metav1.ListOptions{})
	if err == nil {
		for _, job := range jobs.Items {
			_ = k8s.KubeClient.BatchV1().Jobs(namespace).Delete(job.Name, metav1.NewDeleteOptions(0))
		}
	}
	clusters, err := k8s.CRClient.CouchbaseV1().CouchbaseClusters(namespace).List(metav1.ListOptions{})
	if err == nil {
		for _, cluster := range clusters.Items {
			DeleteCbCluster(t, k8s.KubeClient, k8s.CRClient, namespace, &cluster)
		}
	}
	if err := WaitUntilPodDeleted(t, k8s.KubeClient, namespace); err != nil {
		fmt.Println("Warning: Unable to delete pods:", err)
	}

	// Ensure all existing PVCs are deleted before continuing.  In the cloud these may take a
	// while to fully disappear, and may bleed through into other tests, especially ones that
	// cover supportability.
	if err := DeleteAndWaitForPVCDeletion(k8s, namespace, 5*time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete PVCs:", err)
	}
}

func KillMembers(kubecli kubernetes.Interface, namespace string, clusterName string, names ...string) error {
	for _, name := range names {
		if err := KillMember(kubecli, namespace, clusterName, name, true); err != nil {
			return err
		}
	}
	return nil
}

// Kill member deletes Pod and optionally checks for any associated Volume to delete
func KillMember(kubecli kubernetes.Interface, namespace, clusterName, name string, removeVolumes bool) error {
	return k8sutil.DeleteCouchbasePod(kubecli, namespace, clusterName, name, metav1.NewDeleteOptions(0), removeVolumes)
}

func RemovePersistentVolumesOfPod(kubeClient kubernetes.Interface, namespace, clusterName string, memberId int) error {
	podMemberName := couchbaseutil.CreateMemberName(clusterName, memberId)
	pvcList, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: "couchbase_node=" + podMemberName})
	if err != nil {
		return errors.New("Unable to fetch persistent volume list for pod " + podMemberName + ": " + err.Error())
	}

	for _, pvc := range pvcList.Items {
		if err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, &metav1.DeleteOptions{}); err != nil {
			return errors.New("Failed to delete persistent volume claim " + pvc.Name + ": " + err.Error())
		}
	}
	return nil
}

func WriteLogs(kubeClient kubernetes.Interface, namespace, logDir, testName string) error {
	options := metav1.ListOptions{LabelSelector: constants.CouchbaseOperatorLabel}
	pods, err := kubeClient.CoreV1().Pods(namespace).List(options)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		logOpts := &v1.PodLogOptions{}
		req := kubeClient.CoreV1().Pods(namespace).GetLogs(pod.Name, logOpts)
		data, err := req.DoRaw()
		if err != nil {
			return err
		}

		if strings.Contains(testName, "/") {
			testName = strings.Split(testName, "/")[1]
		}

		logFile := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", testName, pod.Name))
		if err := ioutil.WriteFile(logFile, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func LogfWithTimestamp(t *testing.T, format string, args ...interface{}) {
	t.Log(time.Now(), fmt.Sprintf(format, args...))
}

func printContainerStatus(buf *bytes.Buffer, ss []v1.ContainerStatus) {
	for _, s := range ss {
		if s.State.Waiting != nil {
			buf.WriteString(fmt.Sprintf("%s: Waiting: message (%s) reason (%s)\n", s.Name, s.State.Waiting.Message, s.State.Waiting.Reason))
		}
		if s.State.Terminated != nil {
			buf.WriteString(fmt.Sprintf("%s: Terminated: message (%s) reason (%s)\n", s.Name, s.State.Terminated.Message, s.State.Terminated.Reason))
		}
	}
}

func ResizeClusterNoWait(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster) (*couchbasev1.CouchbaseCluster, error) {
	t.Logf("Changing Cluster Size To: %v...\n", strconv.Itoa(clusterSize))
	return PatchCluster(t, k8s, cl, jsonpatch.NewPatchSet().Replace(fmt.Sprintf("/Spec/ServerSettings/%d/Size", service), clusterSize), 30*time.Second)
}

func MustResizeClusterNoWait(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster) *couchbasev1.CouchbaseCluster {
	cluster, err := ResizeClusterNoWait(t, service, clusterSize, k8s, cl)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// ResizeCluster resizes the MDS service to the desired size and waits until the cluster is
// healthy.
func ResizeCluster(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, timeout time.Duration) (*couchbasev1.CouchbaseCluster, error) {
	cluster, err := ResizeClusterNoWait(t, service, clusterSize, k8s, cl)
	if err != nil {
		return cl, err
	}
	if err := WaitClusterStatusHealthy(t, k8s, cluster, timeout); err != nil {
		return cluster, err
	}
	return cluster, nil
}

func MustResizeCluster(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, timeout time.Duration) *couchbasev1.CouchbaseCluster {
	cluster, err := ResizeCluster(t, service, clusterSize, k8s, cl, timeout)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func KillPods(t *testing.T, kubeCli kubernetes.Interface, cl *couchbasev1.CouchbaseCluster, numToKill int) {
	pods, err := kubeCli.CoreV1().Pods(cl.Namespace).List(k8sutil.ClusterListOpt(cl.Name))
	if err != nil {
		Die(t, err)
	}

	items := len(pods.Items)
	if numToKill > items {
		Die(t, fmt.Errorf("trying to kill %d pods, but only %d exist", numToKill, items))
	}

	killPods := []string{}
	for i := 0; i < numToKill; i++ {
		killPods = append(killPods, pods.Items[i].Name)
	}
	t.Logf("Killing pods: %v", killPods)

	if err := KillMembers(kubeCli, cl.Namespace, cl.Name, killPods...); err != nil {
		Die(t, err)
	}

	for _, pod := range killPods {
		if err := WaitPodDeleted(t, kubeCli, pod, cl); err != nil {
			Die(t, err)
		}
	}
}

func KillPodForMember(kubeCli kubernetes.Interface, cl *couchbasev1.CouchbaseCluster, memberId int) error {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return KillMember(kubeCli, cl.Namespace, cl.Name, name, true)
}

func MustKillPodForMember(t *testing.T, k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, memberId int, removeVolumes bool) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	if err := KillMember(k8s.KubeClient, cl.Namespace, cl.Name, name, removeVolumes); err != nil {
		Die(t, err)
	}
}

func CreateMemberPod(k8s *types.Cluster, cl *couchbasev1.CouchbaseCluster, m *couchbaseutil.Member) (*v1.Pod, error) {
	podGetter := scheduler.NewNullPodGetter()
	scheduler, _ := scheduler.NewNullScheduler(podGetter, cl)

	for _, config := range cl.Spec.ServerSettings {
		if config.Name == m.ServerConfig {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			pod, err := k8sutil.CreateCouchbasePod(k8s.KubeClient, scheduler, cl, m, cl.Status.CurrentVersion, config, ctx)
			if err != nil {
				return nil, err
			}

			ctx, cancel = context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err = k8sutil.WaitForPod(ctx, k8s.KubeClient, cl.Namespace, pod.Name, "")
			if err != nil {
				return nil, err
			}
			return k8s.KubeClient.CoreV1().Pods(cl.Namespace).Get(pod.Name, metav1.GetOptions{})
		}
	}

	return nil, NewErrServerConfigNotFound(m.ServerConfig)
}

func DeleteCouchbaseOperator(kubeCli kubernetes.Interface, namespace string) error {
	name, err := GetOperatorName(kubeCli, namespace)
	if err != nil {
		return err
	}
	return kubeCli.CoreV1().Pods(namespace).Delete(name, metav1.NewDeleteOptions(0))
}

func MustDeleteCouchbaseOperator(t *testing.T, k8s *types.Cluster, namespace string) {
	if err := DeleteCouchbaseOperator(k8s.KubeClient, namespace); err != nil {
		Die(t, err)
	}
}

func KillOperatorAndWaitForRecovery(k8s *types.Cluster, namespace string) error {
	if err := DeleteCouchbaseOperator(k8s.KubeClient, namespace); err != nil {
		return fmt.Errorf("failed to kill couchbase operator: %v", err)
	}

	if err := WaitUntilOperatorReady(k8s.KubeClient, namespace, constants.CouchbaseOperatorLabel); err != nil {
		return fmt.Errorf("failed to recover couchbase operator: %v", err)
	}
	return nil
}

func MustKillOperatorAndWaitForRecovery(t *testing.T, k8s *types.Cluster, namespace string) {
	if err := KillOperatorAndWaitForRecovery(k8s, namespace); err != nil {
		Die(t, err)
	}
}

func GetOperatorName(kubeCli kubernetes.Interface, namespace string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var pods *v1.PodList
	selector := labels.SelectorFromSet(labels.Set(NameLabelSelector("app", "couchbase-operator")))
	outerErr := retryutil.Retry(ctx, 5*time.Second, IntMax, func() (bool, error) {
		var err error
		pods, err = kubeCli.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
	if outerErr != nil {
		return "couchbase-operator", outerErr
	}

	operatorPods := []string{}
	for _, pod := range pods.Items {
		operatorPods = append(operatorPods, pod.Name)
	}
	if len(operatorPods) == 0 {
		return "", errors.New("no pods available")
	}
	if len(operatorPods) > 1 {
		return "couchbase-operator", errors.New("too many couchbase operators")
	}
	return operatorPods[0], nil
}

func MustGetOperatorName(t *testing.T, k8s *types.Cluster, namespace string) string {
	name, err := GetOperatorName(k8s.KubeClient, namespace)
	if err != nil {
		Die(t, err)
	}
	return name
}

func GetNodeNames(kubeCli kubernetes.Interface, namespace string) (string, error) {
	selector := labels.SelectorFromSet(labels.Set(NameLabelSelector("name", "couchbase-operator")))
	pods, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return "couchbase-operator", err
	}
	operatorPods := []string{}
	for i := 0; i < len(pods.Items); i++ {
		operatorPods = append(operatorPods, pods.Items[i].Name)
	}
	if len(operatorPods) > 1 {
		return "couchbase-operator", errors.New("too many couchbase operators")
	}
	return operatorPods[0], nil
}

// getSchedulableNodes returns a list of all nodes that can be scheduled onto.
func getSchedulableNodes(k8s *types.Cluster) ([]*v1.Node, error) {
	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := []*v1.Node{}
	for index := range nodes.Items {
		schedulable := true
		for _, taint := range nodes.Items[index].Spec.Taints {
			if taint.Effect == v1.TaintEffectNoSchedule {
				schedulable = false
				break
			}
		}
		if schedulable {
			result = append(result, &nodes.Items[index])
		}
	}

	return result, nil
}

// MustNumNodes returns the number of nodes in the cluster.
func MustNumNodes(t *testing.T, k8s *types.Cluster) int {
	nodes, err := getSchedulableNodes(k8s)
	if err != nil {
		Die(t, err)
	}
	return len(nodes)
}

// memoryRequirementToFloat takes a memory requirement, scales it into MiB and
// then casts to a floating point.
func memoryRequirementToFloat(quantity resource.Quantity) float64 {
	return float64(quantity.Value() >> 20)
}

// getNodeAllocatableMemory creates a map from node name to available memory in MiB.
// This uses the per node allocatable total and deducts any pod limits or requests
// to determine what is left.
func getNodeAllocatableMemory(t *testing.T, k8s *types.Cluster) map[string]float64 {
	nodes, err := getSchedulableNodes(k8s)
	if err != nil {
		Die(t, err)
	}

	pods, err := k8s.KubeClient.CoreV1().Pods("").List(metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}

	result := map[string]float64{}
	for _, node := range nodes {
		// Begin with the allocatable amount of memory on the node.  This is fixed and
		// doesn't take in to account the running pods.
		allocatable := memoryRequirementToFloat(node.Status.Allocatable[v1.ResourceMemory])

		// Next deduct any limits and requests, the former taking precedence.
		for _, pod := range pods.Items {
			if pod.Spec.NodeName != node.Name {
				continue
			}
			for _, container := range pod.Spec.Containers {
				if quantity, ok := container.Resources.Limits[v1.ResourceMemory]; ok {
					allocatable -= memoryRequirementToFloat(quantity)
					continue
				}
				if quantity, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
					allocatable -= memoryRequirementToFloat(quantity)
					continue
				}
			}
		}

		result[node.Name] = allocatable
	}

	return result
}

// MustGetMinNodeMem returns the smallest amount of allocatable memory available on any node.
func MustGetMinNodeMem(t *testing.T, k8s *types.Cluster) float64 {
	allocatable := getNodeAllocatableMemory(t, k8s)

	result := math.Inf(+1)
	for _, value := range allocatable {
		result = math.Min(result, value)
	}

	if result == math.Inf(+1) {
		Die(t, fmt.Errorf("no minimum found"))
	}

	return result
}

// MustGetMaxNodeMem returns the largest amount of allocatable memory available on any node.
func MustGetMaxNodeMem(t *testing.T, k8s *types.Cluster) float64 {
	allocatable := getNodeAllocatableMemory(t, k8s)

	result := 0.0
	for _, value := range allocatable {
		result = math.Max(result, value)
	}

	if result == 0.0 {
		Die(t, fmt.Errorf("no maximum found"))
	}

	return result
}

// MustGetMaxScale accepts a memory figure and returns the number of pods that can be deployed
// across the cluster with that sized memory requirement.
func MustGetMaxScale(t *testing.T, k8s *types.Cluster, memory float64) int {
	allocatable := getNodeAllocatableMemory(t, k8s)

	result := 0
	for _, value := range allocatable {
		result += int(math.Floor(value / memory))
	}

	return result
}

// Construct expected name for the PersistentVolumeClaim which belongs to member
// where 'index' specifies the Nth claim generated from the specs template.
// Only specs with multiple VolumeMounts should return volumes with index > 0
func GetMemberPVC(kubeCli kubernetes.Interface, namespace, memberName string, index int, mountName couchbasev1.VolumeMountName) (*v1.PersistentVolumeClaim, error) {
	name := k8sutil.NameForPersistentVolumeClaim(memberName, index, mountName)
	return kubeCli.CoreV1().PersistentVolumeClaims(namespace).Get(name, metav1.GetOptions{})
}

func TlsCheckForCluster(t *testing.T, k8s *types.Cluster, namespace string, ctx *TlsContext) error {
	pods, err := k8s.KubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerClusterKey + "=" + ctx.ClusterName})
	if err != nil {
		return fmt.Errorf("unable to get couchbase pods: %v", err)
	}

	// TLS handshake with pods
	for _, pod := range pods.Items {
		if err := tlsCheckForPod(t, k8s, namespace, pod.GetName(), ctx); err != nil {
			return fmt.Errorf("TLS verification failed: %v", err)
		}
	}
	return nil
}

func MustCheckClusterTLS(t *testing.T, k8s *types.Cluster, namespace string, ctx *TlsContext) {
	if err := TlsCheckForCluster(t, k8s, namespace, ctx); err != nil {
		Die(t, err)
	}
}

func DeletePodsWithLabel(t *testing.T, kubeClient kubernetes.Interface, label, namespace string) error {
	t.Logf("deleting pods with label: %v", label)
	pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: label})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		err := DeletePod(t, kubeClient, pod.Name, namespace)
		if err != nil {
			return err
		}
	}
	_, err = WaitPodsDeleted(kubeClient, namespace, 30, metav1.ListOptions{LabelSelector: label})
	if err != nil {
		return err
	}
	return nil
}

func DeletePod(t *testing.T, kubeClient kubernetes.Interface, podName, namespace string) error {
	t.Logf("deleting pod: %v", podName)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := retryutil.Retry(ctx, 5*time.Second, IntMax, func() (bool, error) {
		if err := kubeClient.CoreV1().Pods(namespace).Delete(podName, metav1.NewDeleteOptions(0)); err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
	return err
}

func DeleteDaemonSetsWithLabel(t *testing.T, kubeClient kubernetes.Interface, label, namespace string) error {
	t.Logf("deleting pods with label: %v", label)
	dsList, err := kubeClient.ExtensionsV1beta1().DaemonSets(namespace).List(metav1.ListOptions{LabelSelector: label})
	if err != nil {
		return err
	}
	for _, ds := range dsList.Items {
		err := DeleteDaemonSet(t, kubeClient, ds.Name, namespace)
		if err != nil {
			return err
		}
	}
	_, err = WaitDaemonSetsDeleted(kubeClient, namespace, 30, metav1.ListOptions{LabelSelector: label})
	if err != nil {
		return err
	}
	return nil
}

func DeleteDaemonSet(t *testing.T, kubeClient kubernetes.Interface, dsName, namespace string) error {
	t.Logf("deleting daemonset: %v", dsName)
	err := kubeClient.ExtensionsV1beta1().DaemonSets(namespace).Delete(dsName, metav1.NewDeleteOptions(0))
	if err != nil {
		return err
	}
	return nil
}

func AddLabelToNodes(t *testing.T, kubeClient kubernetes.Interface, labelKey, labelValue string) error {
	t.Logf("adding label %v:%v to all nodes", labelKey, labelValue)
	k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, node := range k8sNodeList.Items {
		if err := AddLabelToNode(t, kubeClient, node, labelKey, labelValue); err != nil {
			return err
		}
	}
	return nil
}

func AddLabelToNode(t *testing.T, kubeClient kubernetes.Interface, node v1.Node, labelKey, labelValue string) error {
	t.Logf("adding label %v:%v to node %v", labelKey, labelValue, node.Name)
	currentLables := node.ObjectMeta.Labels
	currentLables[labelKey] = labelValue
	if _, err := kubeClient.CoreV1().Nodes().Update(&node); err != nil {
		return err
	}
	return nil
}

// Returns KubeConfig file path to use for testing
func GetKubeConfigToUse(kubeType, kubeName string) string {
	kubeConfPath := os.Getenv("HOME") + "/.kube/config_" + kubeType + "_" + kubeName
	// If cluster specific file doesn't exists, point to default file
	if _, err := os.Stat(kubeConfPath); os.IsNotExist(err) {
		kubeConfPath = os.Getenv("HOME") + "/.kube/config"
	}
	return kubeConfPath
}

func Die(t *testing.T, err error) {
	t.Log(err)
	t.Log(string(debug.Stack()))
	t.FailNow()
}

// MustKillCouchbaseService kills the couchbase service depending on the platform type
// TODO: Find a generic way of doing this on OpenShift
func MustKillCouchbaseService(t *testing.T, k8s *types.Cluster, namespace, member, kubernetesType string) {
	if kubernetesType == "kubernetes" {
		MustExecShellInPod(t, k8s, namespace, member, "mv /etc/service/couchbase-server /tmp/")
		return
	}

	if err := DeletePod(t, k8s.KubeClient, member, namespace); err != nil {
		Die(t, err)
	}
}

// MustDeletePodServices deletes all services in the cluster namespace that
// belong to individual pods.
func MustDeletePodServices(t *testing.T, k8s *types.Cluster, couchbase *couchbasev1.CouchbaseCluster) {
	selector := constants.CouchbaseServerPodLabelStr + couchbase.Name
	services, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		Die(t, err)
	}
	for _, service := range services.Items {
		if strings.HasSuffix(service.Name, "-exposed-ports") {
			if err := DeleteService(k8s.KubeClient, service.Namespace, service.Name, metav1.NewDeleteOptions(0)); err != nil {
				Die(t, err)
			}
		}
	}
}
