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

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/sirupsen/logrus"

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

	// Global dummy context used buy blocking calls
	Context context.Context
)

func init() {
	Context = context.WithValue(context.Background(), "logger", logrus.WithField("module", "e2eutil"))
}

// ClusterReadyRetries specifies how many times to retry various ready checks
type ClusterReadyRetries struct {
	Size    int
	Bucket  int
	Service int
}

var (
	defaultRetries = &ClusterReadyRetries{
		Size:    constants.Retries120,
		Bucket:  constants.Retries60,
		Service: constants.Retries20,
	}

	azureRetries = &ClusterReadyRetries{
		Size:    constants.Retries120 * GetPlatformTimingMultiplier("azure"),
		Bucket:  constants.Retries60 * GetPlatformTimingMultiplier("azure"),
		Service: constants.Retries20 * GetPlatformTimingMultiplier("azure"),
	}

	systemTestRetries = &ClusterReadyRetries{
		Size:    constants.Retries120,
		Bucket:  constants.Retries120,
		Service: constants.Retries120,
	}
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
			ordinal = ordinal + int('0')
		} else {
			ordinal = ordinal - 10 + int('a')
		}
		// Append to the name
		suffix = suffix + string(rune(ordinal))
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

// newClusterFromSpecQuick creates a cluster and waits for various ready conditions.
// Returns the cluster object regardless so we can test for error conditions
func newClusterFromSpecQuick(t *testing.T, k8s *types.Cluster, namespace string, cluster *api.CouchbaseCluster, retries *ClusterReadyRetries) (*api.CouchbaseCluster, error) {
	// Create the cluster
	var err error
	if cluster, err = CreateCluster(t, k8s.CRClient, namespace, cluster); err != nil {
		t.Logf("failed to create cluster")
		return nil, err
	}

	errChan := make(chan error)
	go func() {
		// Expect the cluster to enter a failed state
		if err := WaitClusterPhaseFailed(t, k8s.CRClient, cluster, constants.Retries20); err == nil {
			errChan <- errors.New("Cluster entered failed state")
		}
	}()

	go func() {
		// Wait for the cluster to reach the correct size
		_, err := WaitUntilSizeReached(t, k8s.CRClient, cluster.Spec.TotalSize(), retries.Size, cluster)
		errChan <- err
	}()

	if err := <-errChan; err != nil {
		t.Logf("failed to wait until size reached")
		return cluster, err
	}

	// If any buckets are specified wait for these to become active
	buckets := cluster.Spec.BucketNames()
	if len(buckets) > 0 {
		err := WaitUntilBucketsExists(t, k8s.CRClient, buckets, retries.Bucket, cluster)
		if err != nil {
			t.Logf("failed to wait for bucket to exist")
			return cluster, err
		}
	}
	// Update the cluster status
	updatedCluster, err := getClusterCRD(k8s.CRClient, cluster)
	if err != nil {
		t.Logf("failed to get updated cluster spec")
		return cluster, err
	}
	return updatedCluster, nil
}

// newClusterFromSpec creates a cluster and waits for various ready conditions.
// Performs retries and garbage collection in the event of transient failure
func newClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *api.CouchbaseCluster, retries *ClusterReadyRetries) (*api.CouchbaseCluster, error) {
	var err error
	for i := 0; i < 1; i++ {
		time.Sleep(10 * time.Second)

		// Ensure we set the err variable in the main scope
		var cluster *api.CouchbaseCluster
		cluster, err = newClusterFromSpecQuick(t, k8s, namespace, clusterSpec, retries)
		if err != nil {
			t.Logf("failed to create cluster from spec")
			if cluster != nil {
				t.Logf("deleting failed cluster")
				DeleteCluster(t, k8s.CRClient, k8s.KubeClient, cluster, 5)
			}
			continue
		}

		return cluster, nil
	}
	return nil, err
}

// Creates Cluster Spec object and returns it
func CreateClusterSpec(secretName string, config map[string]map[string]string) api.ClusterSpec {
	return e2espec.CreateClusterSpec(constants.ClusterNamePrefix, secretName, config)
}

func GetRetriesForPlatform(platformType string) *ClusterReadyRetries {
	if platformType == "azure" {
		return azureRetries
	} else {
		return defaultRetries
	}
}

func GetPlatformTimingMultiplier(platformType string) int {
	if platformType == "azure" {
		return 5
	} else {
		return 1
	}
}

// Creates Couchbase cluster object and returns it
func CreateClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, adminConsoleExposed bool, spec api.ClusterSpec, platformType string) (*api.CouchbaseCluster, error) {
	crd := e2espec.CreateClusterCRD(constants.ClusterNamePrefix, adminConsoleExposed, spec)
	return newClusterFromSpec(t, k8s, namespace, crd, GetRetriesForPlatform(platformType))
}

func MustCreateClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, adminConsoleExposed bool, spec api.ClusterSpec, platformType string) *api.CouchbaseCluster {
	cluster, err := CreateClusterFromSpec(t, k8s, namespace, adminConsoleExposed, spec, platformType)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// Creates Couchbase cluster object and returns it
func CreateClusterFromSpecSystemTest(t *testing.T, k8s *types.Cluster, namespace string, adminConsoleExposed bool, spec api.ClusterSpec, ctx *TlsContext) (*api.CouchbaseCluster, error) {
	crd := e2espec.CreateClusterCRD(constants.ClusterNamePrefix, adminConsoleExposed, spec)
	if ctx != nil {
		crd.Name = ctx.ClusterName
		crd.Spec.TLS = &api.TLSPolicy{
			Static: &api.StaticTLS{
				Member: &api.MemberSecret{
					ServerSecret: ctx.ClusterSecretName,
				},
				OperatorSecret: ctx.OperatorSecretName,
			},
		}
	}
	return newClusterFromSpec(t, k8s, namespace, crd, systemTestRetries)
}

// Creates Couchbase cluster object and returns it
func CreateClusterFromSpecNoWait(t *testing.T, k8s *types.Cluster, namespace string, adminConsoleExposed bool, spec api.ClusterSpec) (*api.CouchbaseCluster, error) {
	crd := e2espec.CreateClusterCRD(constants.ClusterNamePrefix, adminConsoleExposed, spec)
	return CreateCluster(t, k8s.CRClient, namespace, crd)
}

// NewClusterBasicQuick attempts to create a basic cluster only once.  The returned
// cluster object may still be valid upon error
func NewClusterBasicQuick(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, retries *ClusterReadyRetries) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster("test-couchbase-", k8s.DefaultSecret.Name, size, withBucket, exposed)
	return newClusterFromSpecQuick(t, k8s, namespace, clusterSpec, retries)
}

// NewClusterMultiQuick attempts to create a multi cluster only once.  The returned
// cluster object may still be valid upon error
func NewClusterMultiQuick(t *testing.T, k8s *types.Cluster, namespace string, config map[string]map[string]string, exposed bool, retries *ClusterReadyRetries) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewMultiCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, config, exposed)
	return newClusterFromSpecQuick(t, k8s, namespace, clusterSpec, retries)
}

// NewClusterBasic creates a basic cluster, retrying if an error is encountered and
// performing garbage collection
func NewClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec, defaultRetries)
}

func MustNewClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) *api.CouchbaseCluster {
	cluster, err := NewClusterBasic(t, k8s, namespace, size, withBucket, exposed)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewTLSClusterBasic creates a new TLS enabled basic cluster, retrying if an error is encountered
func NewTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.TLS = &api.TLSPolicy{
		Static: &api.StaticTLS{
			Member: &api.MemberSecret{
				ServerSecret: ctx.ClusterSecretName,
			},
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, clusterSpec, defaultRetries)
}

func MustNewTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) *api.CouchbaseCluster {
	cluster, err := NewTLSClusterBasic(t, k8s, namespace, size, withBucket, exposed, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewTLSClusterBasicNoWait creates a new TLS enabled basic cluster asynchronously
func NewTLSClusterBasicNoWait(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.TLS = &api.TLSPolicy{
		Static: &api.StaticTLS{
			Member: &api.MemberSecret{
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
func NewTlsXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicXdcrCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.TLS = &api.TLSPolicy{
		Static: &api.StaticTLS{
			Member: &api.MemberSecret{
				ServerSecret: ctx.ClusterSecretName,
			},
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, clusterSpec, defaultRetries)
}

func MustNewTlsXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool, ctx *TlsContext) *api.CouchbaseCluster {
	cluster, err := NewTlsXdcrClusterBasic(t, k8s, namespace, size, withBucket, exposed, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewXdcrClusterBasic creates a basic cluster, retrying if an error is encountered and
// performing garbage collection
func NewXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicXdcrCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec, defaultRetries)
}

func MustNewXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) *api.CouchbaseCluster {
	cluster, err := NewXdcrClusterBasic(t, k8s, namespace, size, withBucket, exposed)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func NewClusterBasicNoWait(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	return CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
}

// NewStatefulCluster creates a cluster with persistent block storage, retrying if an
// error is encountered and performing garbage collection
func NewStatefulCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewStatefulCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size, withBucket, exposed)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec, defaultRetries)
}

func MustNewStatefulCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, withBucket bool, exposed bool) *api.CouchbaseCluster {
	cluster, err := NewStatefulCluster(t, k8s, namespace, size, withBucket, exposed)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewSupportableCluster creates a cluster with two MDS groups of 'size'.  The first is
// a stateful group with data and index enabled.  The second is a stateless group with
// query enabled.
func NewSupportableCluster(t *testing.T, k8s *types.Cluster, namespace string, size int) (*api.CouchbaseCluster, error) {
	spec := e2espec.NewSupportableCluster(size)
	return newClusterFromSpec(t, k8s, namespace, spec, defaultRetries)
}

// MustNewSupportableCluster creates a supportable cluster as described by NewSupportableCluster
// but dies on error.
func MustNewSupportableCluster(t *testing.T, k8s *types.Cluster, namespace string, size int) *api.CouchbaseCluster {
	cluster, err := NewSupportableCluster(t, k8s, namespace, size)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewSupportableTLSCluster creates a cluster with two MDS groups of 'size'.  The first is
// a stateful group with data and index enabled.  The second is a stateless group with
// query enabled.
func NewSupportableTLSCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TlsContext) (*api.CouchbaseCluster, error) {
	cluster := e2espec.NewClusterCRD("", e2espec.NewSupportableClusterSpec(size))
	cluster.Name = ctx.ClusterName
	cluster.Spec.TLS = &api.TLSPolicy{
		Static: &api.StaticTLS{
			Member: &api.MemberSecret{
				ServerSecret: ctx.ClusterSecretName,
			},
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, cluster, defaultRetries)
}

// MustNewSupportableTLSCluster creates a supportable cluster as described by NewSupportableTLSCluster
// but dies on error.
func MustNewSupportableTLSCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TlsContext) *api.CouchbaseCluster {
	cluster, err := NewSupportableTLSCluster(t, k8s, namespace, size, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewClusterMulti creates a multi cluster, retrying if an error is encountered and
// performing garbage collection
func NewClusterMulti(t *testing.T, k8s *types.Cluster, namespace string, config map[string]map[string]string, exposed bool) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewMultiCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, config, exposed)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec, defaultRetries)
}

func MustNewClusterMulti(t *testing.T, k8s *types.Cluster, namespace string, config map[string]map[string]string, exposed bool) *api.CouchbaseCluster {
	cluster, err := NewClusterMulti(t, k8s, namespace, config, exposed)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewClusterMultiNoWait creates a multi cluster, but doesn't wait for any events.
// Used in cases where the cluster is expected to fail
func NewClusterMultiNoWait(t *testing.T, k8s *types.Cluster, namespace string, config map[string]map[string]string) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewMultiCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, config, false)
	return CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
}

func UpdateClusterSpec(field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {
	updateFunc := func(cl *api.CouchbaseCluster) {}
	switch {
	case field == "Paused":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.Paused, _ = strconv.ParseBool(value) }
	case field == "ExposedFeatures":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ExposedFeatures = strings.Split(value, ",") }
	case field == "ServerGroups":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerGroups = strings.Split(value, ",") }
	case field == "AntiAffinity":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.AntiAffinity, _ = strconv.ParseBool(value) }
	case field == "Version":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.Version = value }
	}
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func MustUpdateClusterSpec(t *testing.T, field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) *api.CouchbaseCluster {
	cluster, err := UpdateClusterSpec(field, value, crClient, cl, maxRetries)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func MustNotUpdateClusterSpec(t *testing.T, field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster) {
	if _, err := UpdateClusterSpec(field, value, crClient, cl, 1); err == nil {
		Die(t, fmt.Errorf("cluster spec update succeeded unexpectedly"))
	}
}

func UpdateClusterSettings(field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {

	updateFunc := func(cl *api.CouchbaseCluster) {}

	switch {
	case field == "DataServiceMemQuota":
		updateFunc = func(cl *api.CouchbaseCluster) {
			cl.Spec.ClusterSettings.DataServiceMemQuota, _ = strconv.ParseUint(value, 10, 64)
		}
	case field == "IndexServiceMemQuota":
		updateFunc = func(cl *api.CouchbaseCluster) {
			cl.Spec.ClusterSettings.IndexServiceMemQuota, _ = strconv.ParseUint(value, 10, 64)
		}
	case field == "SearchServiceMemQuota":
		updateFunc = func(cl *api.CouchbaseCluster) {
			cl.Spec.ClusterSettings.SearchServiceMemQuota, _ = strconv.ParseUint(value, 10, 64)
		}
	case field == "IndexStorageSetting":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ClusterSettings.IndexStorageSetting = value }
	case field == "AutoFailoverTimeout":
		updateFunc = func(cl *api.CouchbaseCluster) {
			newTimeout, _ := strconv.Atoi(value)
			cl.Spec.ClusterSettings.AutoFailoverTimeout = uint64(newTimeout)
		}
	}
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func UpdateServiceSpec(service int, field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {
	updateFunc := func(cl *api.CouchbaseCluster) {}
	switch {
	case field == "Size":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[service].Size = ConvertToInt(value) }
	case field == "Name":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[service].Name = value }
	case field == "Services":
		updateFunc = func(cl *api.CouchbaseCluster) {
			cl.Spec.ServerSettings[service].Services = api.NewServiceList(strings.Split(value, ","))
		}
	}
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

// convert interfaced value to int, note this can downcast i64
func ConvertToInt(value interface{}) int {
	switch value.(type) {
	case int, int64:
		return value.(int)
	case string:
		strInt, _ := strconv.Atoi(value.(string))
		return strInt
	}
	return 0
}

func AddServices(crClient versioned.Interface, cl *api.CouchbaseCluster, newService api.ServerConfig, maxRetries int) (*api.CouchbaseCluster, error) {
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings = append(cl.Spec.ServerSettings, newService) }
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func RemoveServices(crClient versioned.Interface, cl *api.CouchbaseCluster, removeServiceName string, maxRetries int) (*api.CouchbaseCluster, error) {
	newServiceConfig := []api.ServerConfig{}
	for _, service := range cl.Spec.ServerSettings {
		if service.Name != removeServiceName {
			newServiceConfig = append(newServiceConfig, service)
		}
	}
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings = newServiceConfig }
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func ScaleServices(crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int, servicesMap map[string]int) (*api.CouchbaseCluster, error) {
	newServiceConfig := []api.ServerConfig{}
	for _, service := range cl.Spec.ServerSettings {
		for serviceName, size := range servicesMap {
			if serviceName == service.Name {
				service.Size = size
			}
		}
		newServiceConfig = append(newServiceConfig, service)
	}
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings = newServiceConfig }
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

// PatchCluster updates the specified cluster with a list of JSON patch objects, returning the updated cluster
func PatchCluster(t *testing.T, client versioned.Interface, cluster *api.CouchbaseCluster, patches jsonpatch.PatchSet, retries int) (*api.CouchbaseCluster, error) {
	return cluster, retryutil.Retry(Context, 5*time.Second, retries, func() (done bool, err error) {
		// Get the current cluster resource
		before, err := client.CouchbaseV1().CouchbaseClusters(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
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
		updated, err := client.CouchbaseV1().CouchbaseClusters(cluster.Namespace).Update(after)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Everything successful
		cluster = updated
		return true, nil
	})
}

// MustPatchCluster patches the cluster with a list of JSON patch objects, returning the updated cluster and dying on error
func MustPatchCluster(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, patches jsonpatch.PatchSet, retries int) *api.CouchbaseCluster {
	cluster, err := PatchCluster(t, k8s.CRClient, cluster, patches, retries)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// MustNotPatchCluster patches the cluster with a list of JSON patch objects, dying if the test succeeded.
func MustNotPatchCluster(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, patches jsonpatch.PatchSet) {
	if _, err := PatchCluster(t, k8s.CRClient, cluster, patches, 1); err == nil {
		Die(t, fmt.Errorf("cluster patch applied unexpectedly"))
	}
}

func UpdateBucketSpec(bucketName string, field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {
	bucketIndex := 0
	for i, bucket := range cl.Spec.BucketSettings {
		if bucketName == bucket.BucketName {
			bucketIndex = i
			break
		}
	}
	updateFunc := func(cl *api.CouchbaseCluster) {}
	switch {
	case field == "BucketName":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].BucketName = value }
	case field == "BucketType":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].BucketType = value }
	case field == "BucketMemoryQuota":
		updateFunc = func(cl *api.CouchbaseCluster) {
			cl.Spec.BucketSettings[bucketIndex].BucketMemoryQuota, _ = strconv.Atoi(value)
		}
	case field == "BucketReplicas":
		updateFunc = func(cl *api.CouchbaseCluster) {
			replicas, _ := strconv.Atoi(value)
			cl.Spec.BucketSettings[bucketIndex].BucketReplicas = replicas
		}
	case field == "IoPriority":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].IoPriority = value }
	case field == "EvictionPolicy":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].EvictionPolicy = value }
	case field == "ConflictResolution":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].ConflictResolution = value }
	case field == "EnableFlush":
		updateFunc = func(cl *api.CouchbaseCluster) {
			flush, _ := strconv.ParseBool(value)
			cl.Spec.BucketSettings[bucketIndex].EnableFlush = flush
		}
	case field == "EnableIndexReplica":
		updateFunc = func(cl *api.CouchbaseCluster) {
			enableReplicas, _ := strconv.ParseBool(value)
			cl.Spec.BucketSettings[bucketIndex].EnableIndexReplica = enableReplicas
		}
	}
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func DestroyCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cluster *api.CouchbaseCluster) {
	err1 := DeleteCluster(t, crClient, kubeClient, cluster, 10)
	if err1 != nil {
		t.Fatal(err1)
	}
}

func CleanUpCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, logDir, kubeName, testName string) {
	// Creates dir for kubename
	logDir = filepath.Join(logDir, kubeName)
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		t.Log(err)
		return
	}

	// Pulls operator pod logs
	if err := WriteLogs(kubeClient, namespace, logDir, testName); err != nil {
		t.Logf("Error: %v", err)
	}

	CleanK8Cluster(t, kubeClient, crClient, namespace)
}

func DeleteCbCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cbCluster *api.CouchbaseCluster) {
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
	KillMembers(kubeClient, namespace, cbCluster.Name, killPods...)
}

func CleanK8Cluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string) {
	services, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	for _, service := range services.Items {
		kubeClient.CoreV1().Services(namespace).Delete(service.Name, metav1.NewDeleteOptions(0))
	}

	jobs, err := kubeClient.BatchV1().Jobs(namespace).List(metav1.ListOptions{})
	for _, job := range jobs.Items {
		kubeClient.BatchV1().Jobs(namespace).Delete(job.Name, metav1.NewDeleteOptions(0))
	}
	clusters, err := crClient.CouchbaseV1().CouchbaseClusters(namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Logf("Error: %v", err)
	}
	for _, cluster := range clusters.Items {
		DeleteCbCluster(t, kubeClient, crClient, namespace, &cluster)
	}
	WaitUntilPodDeleted(t, kubeClient, namespace)

	// Remove all PVC for the CB clusters
	for _, cluster := range clusters.Items {
		pvcSelectorLabel := constants.CouchbaseServerClusterKey + "=" + cluster.Name
		pvcList, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: pvcSelectorLabel})
		if err != nil {
			t.Logf("Failed to list pvcs for %s: %v", err, cluster.Name)
		} else {
			for _, pvc := range pvcList.Items {
				kubeClient.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, metav1.NewDeleteOptions(0))
			}
		}
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

func isMasterNode(nodeMap map[string]string) bool {
	_, isK8SMaster := nodeMap["node-role.kubernetes.io/master"]
	_, isOpenshiftMaster := nodeMap["openshift-infra"]
	if isK8SMaster || isOpenshiftMaster {
		return true
	}
	return false
}

func ResizeClusterNoWait(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *api.CouchbaseCluster) (*api.CouchbaseCluster, error) {
	t.Logf("Changing Cluster Size To: %v...\n", strconv.Itoa(clusterSize))
	cluster, err := UpdateServiceSpec(service, "Size", strconv.Itoa(clusterSize), k8s.CRClient, cl, 10)
	return cluster, err
}

func MustResizeClusterNoWait(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *api.CouchbaseCluster) *api.CouchbaseCluster {
	cluster, err := ResizeClusterNoWait(t, service, clusterSize, k8s, cl)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func ResizeCluster(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *api.CouchbaseCluster, retries int) (*api.CouchbaseCluster, error) {
	cluster, err := ResizeClusterNoWait(t, service, clusterSize, k8s, cl)
	if err != nil {
		return cl, err
	}
	t.Logf("Waiting For Cluster Size To Be: %v...\n", strconv.Itoa(clusterSize))
	names, err := WaitUntilSizeReached(t, k8s.CRClient, clusterSize, retries, cl)
	if err != nil {
		return cluster, err
	}
	t.Logf("Resize Success: %v...\n", names)
	return cluster, nil
}

func MustResizeCluster(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *api.CouchbaseCluster, retries int) *api.CouchbaseCluster {
	cluster, err := ResizeCluster(t, service, clusterSize, k8s, cl, retries)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func KillPods(t *testing.T, kubeCli kubernetes.Interface, cl *api.CouchbaseCluster, numToKill int) {
	pods, err := kubeCli.CoreV1().Pods(cl.Namespace).List(k8sutil.ClusterListOpt(cl.Name))
	if err != nil {
		t.Fatalf("Error getting pods in cluster: %v", err)
	}

	items := len(pods.Items)
	if numToKill > items {
		t.Fatalf("Trying to kill %d pods, but only %d exist", numToKill, items)
	}

	killPods := []string{}
	for i := 0; i < numToKill; i++ {
		killPods = append(killPods, pods.Items[i].Name)
	}
	t.Logf("Killing pods: %v", killPods)

	KillMembers(kubeCli, cl.Namespace, cl.Name, killPods...)

	for _, pod := range killPods {
		WaitPodDeleted(t, kubeCli, pod, cl)
	}
}

func KillPodForMember(kubeCli kubernetes.Interface, cl *api.CouchbaseCluster, memberId int) error {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return KillMember(kubeCli, cl.Namespace, cl.Name, name, true)
}

func MustKillPodForMember(t *testing.T, k8s *types.Cluster, cl *api.CouchbaseCluster, memberId int, removeVolumes bool) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	if err := KillMember(k8s.KubeClient, cl.Namespace, cl.Name, name, removeVolumes); err != nil {
		Die(t, err)
	}
}

func CreateMemberPod(kubeCli kubernetes.Interface, m *couchbaseutil.Member, cl *api.CouchbaseCluster, clusterName, namespace string) (*v1.Pod, error) {
	podGetter := scheduler.NewNullPodGetter()
	scheduler, _ := scheduler.NewNullScheduler(podGetter, cl)

	for _, config := range cl.Spec.ServerSettings {
		if config.Name == m.ServerConfig {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(60)*time.Second)
			defer cancel()
			pod, err := k8sutil.CreateCouchbasePod(kubeCli, scheduler, cl, m, cl.Status.CurrentVersion, config, ctx)
			if err != nil {
				return nil, err
			}

			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(60)*time.Second)
			defer cancel()
			err = k8sutil.WaitForPod(ctx, kubeCli, namespace, pod.Name, "")
			if err != nil {
				return nil, err
			}
			return kubeCli.Core().Pods(namespace).Get(pod.Name, metav1.GetOptions{})
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

func KillOperatorAndWaitForRecovery(t *testing.T, kubeClient kubernetes.Interface, namespace string) error {
	t.Logf("Killing operator...")
	if err := DeleteCouchbaseOperator(kubeClient, namespace); err != nil {
		return errors.New("Failed to kill couchbase operator: " + err.Error())
	}

	t.Logf("Waiting for operator to recover...")
	if err := WaitUntilOperatorReady(kubeClient, namespace, constants.CouchbaseOperatorLabel); err != nil {
		return errors.New("Failed to recover couchbase operator: " + err.Error())
	}
	t.Logf("Operator recovered...")
	return nil
}

func GetOperatorName(kubeCli kubernetes.Interface, namespace string) (string, error) {
	selector := labels.SelectorFromSet(labels.Set(NameLabelSelector("app", "couchbase-operator")))
	pods, err := kubeCli.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
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

func NumK8Nodes(kubeCli kubernetes.Interface) (int, error) {
	nodeList, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return -1, err
	}
	return len(nodeList.Items), nil
}

// MustNumNodes returns the number of nodes in the cluster.
func MustNumNodes(t *testing.T, k8s *types.Cluster) int {
	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}
	return len(nodes.Items)
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
	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}

	pods, err := k8s.KubeClient.CoreV1().Pods("").List(metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}

	result := map[string]float64{}
	for _, node := range nodes.Items {
		// Master nodes aren't allowed to have normal pods scheduled on them.
		if node.Name != "minikube" && isMasterNode(node.Labels) {
			continue
		}

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
func GetMemberPVC(kubeCli kubernetes.Interface, namespace, claimName, memberName string, index int, mountName api.VolumeMountName) (*v1.PersistentVolumeClaim, error) {
	name := k8sutil.NameForPersistentVolumeClaim(claimName, memberName, index, mountName)
	return kubeCli.CoreV1().PersistentVolumeClaims(namespace).Get(name, metav1.GetOptions{})
}

func TlsCheckForCluster(t *testing.T, k8s *types.Cluster, namespace string, ctx *TlsContext) error {
	pods, err := k8s.KubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerClusterKey + "=" + ctx.ClusterName})
	if err != nil {
		return fmt.Errorf("Unable to get couchbase pods: %v", err)
	}

	// TLS handshake with pods
	for _, pod := range pods.Items {
		if err := TlsCheckForPod(t, k8s, namespace, pod.GetName(), ctx); err != nil {
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
	_, err = WaitPodsDeleted(kubeClient, namespace, constants.Retries30, metav1.ListOptions{LabelSelector: label})
	if err != nil {
		return err
	}
	return nil
}

func DeletePod(t *testing.T, kubeClient kubernetes.Interface, podName, namespace string) error {
	t.Logf("deleting pod: %v", podName)
	if err := kubeClient.CoreV1().Pods(namespace).Delete(podName, metav1.NewDeleteOptions(0)); err != nil {
		return err
	}
	return nil
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
	_, err = WaitDaemonSetsDeleted(kubeClient, namespace, constants.Retries30, metav1.ListOptions{LabelSelector: label})
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
