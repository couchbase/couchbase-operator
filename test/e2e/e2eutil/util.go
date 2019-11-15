package e2eutil

import (
	"context"
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

	operator_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
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

// newClusterFromSpec creates a cluster and waits for various ready conditions.
// Performs retries and garbage collection in the event of transient failure
func newClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	// Create the cluster.
	cluster, err := CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
	if err != nil {
		return nil, err
	}

	MustWaitClusterStatusHealthy(t, k8s, cluster, 15*time.Minute)

	// Update the cluster status, this is important for the test, especially if the cluster
	// name is auto-generated.
	updatedCluster, err := getClusterCRD(k8s.CRClient, cluster)
	if err != nil {
		return cluster, err
	}
	return updatedCluster, nil
}

func MustNewClusterFromSpec(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	cluster, err := newClusterFromSpec(t, k8s, namespace, clusterSpec)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func NewClusterFromSpecAsync(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	// Create the cluster
	cluster, err := CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

func MustNewClusterFromSpecAsync(t *testing.T, k8s *types.Cluster, namespace string, clusterSpec *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	cluster, err := NewClusterFromSpecAsync(t, k8s, namespace, clusterSpec)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewClusterBasic creates a basic cluster, retrying if an error is encountered and
// performing garbage collection
func NewClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int) *couchbasev2.CouchbaseCluster {
	cluster, err := NewClusterBasic(t, k8s, namespace, size)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewTLSClusterBasic creates a new TLS enabled basic cluster, retrying if an error is encountered
func NewTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext) *couchbasev2.CouchbaseCluster {
	cluster, err := NewTLSClusterBasic(t, k8s, namespace, size, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewMutualTLSClusterBasic creates a new TLS enabled basic cluster, retrying if an error is encountered
func NewMutualTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext, policy couchbasev2.ClientCertificatePolicy) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		ClientCertificatePolicy: &policy,
		ClientCertificatePaths: []couchbasev2.ClientCertificatePath{
			{
				Path: "subject.cn",
			},
		},
	}
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewMutualTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext, policy couchbasev2.ClientCertificatePolicy) *couchbasev2.CouchbaseCluster {
	cluster, err := NewMutualTLSClusterBasic(t, k8s, namespace, size, ctx, policy)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewTLSClusterBasicNoWait creates a new TLS enabled basic cluster asynchronously
func NewTLSClusterBasicNoWait(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	clusterSpec.Name = ctx.ClusterName
	clusterSpec.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
}

// MustNotNewTLSClusterBasic ensures that a cluster is not created given the specification
func MustNotNewTLSClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext) {
	if _, err := NewTLSClusterBasicNoWait(t, k8s, namespace, size, ctx); err == nil {
		Die(t, fmt.Errorf("cluster created unexpectedly"))
	}
}

// NewTLSXdcrClusterBasic creates a new TLS and XDCR enabled basic cluster.
func NewTLSXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicXdcrCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	clusterSpec.Name = ctx.ClusterName
	// Don't use alternate addresses.
	clusterSpec.Spec.Networking.ExposeAdminConsole = false
	clusterSpec.Spec.Networking.ExposedFeatures = nil
	// Enable TLS.
	clusterSpec.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewTLSXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext) *couchbasev2.CouchbaseCluster {
	cluster, err := NewTLSXdcrClusterBasic(t, k8s, namespace, size, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// MustNewMutualTLSXDCRClusterBasic creates a new m and XDCR enabled basic cluster.
func MustNewMutualTLSXDCRClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext, policy couchbasev2.ClientCertificatePolicy) *couchbasev2.CouchbaseCluster {
	clusterSpec := e2espec.NewBasicXdcrCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	clusterSpec.Name = ctx.ClusterName
	// Don't use alternate addresses.
	clusterSpec.Spec.Networking.ExposeAdminConsole = false
	clusterSpec.Spec.Networking.ExposedFeatures = nil
	// Enable TLS.
	clusterSpec.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		ClientCertificatePolicy: &policy,
		ClientCertificatePaths: []couchbasev2.ClientCertificatePath{
			{
				Path: "subject.cn",
			},
		},
	}
	cluster, err := newClusterFromSpec(t, k8s, namespace, clusterSpec)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewXdcrClusterBasic creates a basic cluster, retrying if an error is encountered and
// performing garbage collection
func NewXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicXdcrCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	cluster, err := newClusterFromSpec(t, k8s, namespace, clusterSpec)
	if err != nil {
		Die(t, err)
	}
	MustWaitClusterStatusHealthy(t, k8s, cluster, 5*time.Minute)
	return cluster, err
}

func MustNewXdcrClusterBasic(t *testing.T, k8s *types.Cluster, namespace string, size int) *couchbasev2.CouchbaseCluster {
	cluster, err := NewXdcrClusterBasic(t, k8s, namespace, size)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func NewClusterBasicNoWait(t *testing.T, k8s *types.Cluster, namespace string, size int) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	return CreateCluster(t, k8s.CRClient, namespace, clusterSpec)
}

// NewStatefulCluster creates a cluster with persistent block storage, retrying if an
// error is encountered and performing garbage collection
func NewStatefulCluster(t *testing.T, k8s *types.Cluster, namespace string, size int) (*couchbasev2.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewStatefulCluster(constants.ClusterNamePrefix, k8s.DefaultSecret.Name, size)
	return newClusterFromSpec(t, k8s, namespace, clusterSpec)
}

func MustNewStatefulCluster(t *testing.T, k8s *types.Cluster, namespace string, size int) *couchbasev2.CouchbaseCluster {
	cluster, err := NewStatefulCluster(t, k8s, namespace, size)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewSupportableCluster creates a cluster with two MDS groups of 'size'.  The first is
// a stateful group with data and index enabled.  The second is a stateless group with
// query enabled.
func NewSupportableCluster(t *testing.T, k8s *types.Cluster, namespace string, size int) (*couchbasev2.CouchbaseCluster, error) {
	spec := e2espec.NewSupportableCluster(size)
	return newClusterFromSpec(t, k8s, namespace, spec)
}

// MustNewSupportableCluster creates a supportable cluster as described by NewSupportableCluster
// but dies on error.
func MustNewSupportableCluster(t *testing.T, k8s *types.Cluster, namespace string, size int) *couchbasev2.CouchbaseCluster {
	cluster, err := NewSupportableCluster(t, k8s, namespace, size)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewSupportableTLSCluster creates a cluster with two MDS groups of 'size'.  The first is
// a stateful group with data and index enabled.  The second is a stateless group with
// query enabled.
func NewSupportableTLSCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext) (*couchbasev2.CouchbaseCluster, error) {
	cluster := e2espec.NewClusterCRD("", e2espec.NewSupportableClusterSpec(size))
	cluster.Name = ctx.ClusterName
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	return newClusterFromSpec(t, k8s, namespace, cluster)
}

// MustNewSupportableTLSCluster creates a supportable cluster as described by NewSupportableTLSCluster
// but dies on error.
func MustNewSupportableTLSCluster(t *testing.T, k8s *types.Cluster, namespace string, size int, ctx *TLSContext) *couchbasev2.CouchbaseCluster {
	cluster, err := NewSupportableTLSCluster(t, k8s, namespace, size, ctx)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// NewBucket creates a bucket.
func NewBucket(k8s *types.Cluster, namespace string, bucket runtime.Object) (runtime.Object, error) {
	switch t := bucket.(type) {
	case *couchbasev2.CouchbaseBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseBuckets(namespace).Create(t)
	case *couchbasev2.CouchbaseEphemeralBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).Create(t)
	case *couchbasev2.CouchbaseMemcachedBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).Create(t)
	default:
		return nil, fmt.Errorf("unsupported bucket type")
	}
}

func MustNewBucket(t *testing.T, k8s *types.Cluster, namespace string, bucket runtime.Object) runtime.Object {
	object, err := NewBucket(k8s, namespace, bucket)
	if err != nil {
		Die(t, err)
	}
	return object
}

func DeleteBucket(k8s *types.Cluster, namespace string, bucket runtime.Object) error {
	switch t := bucket.(type) {
	case *couchbasev2.CouchbaseBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseBuckets(namespace).Delete(t.Name, metav1.NewDeleteOptions(0))
	case *couchbasev2.CouchbaseEphemeralBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).Delete(t.Name, metav1.NewDeleteOptions(0))
	case *couchbasev2.CouchbaseMemcachedBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).Delete(t.Name, metav1.NewDeleteOptions(0))
	default:
		return fmt.Errorf("unsupported bucket type")
	}
}

func MustDeleteBucket(t *testing.T, k8s *types.Cluster, namespace string, bucket runtime.Object) {
	if err := DeleteBucket(k8s, namespace, bucket); err != nil {
		Die(t, err)
	}
}

func GetBucketName(bucket runtime.Object) (string, error) {
	switch t := bucket.(type) {
	case *couchbasev2.CouchbaseBucket:
		return t.Name, nil
	case *couchbasev2.CouchbaseEphemeralBucket:
		return t.Name, nil
	case *couchbasev2.CouchbaseMemcachedBucket:
		return t.Name, nil
	default:
		return "", fmt.Errorf("unsupported bucket type")
	}
}

func MustGetBucketName(t *testing.T, bucket runtime.Object) string {
	name, err := GetBucketName(bucket)
	if err != nil {
		Die(t, err)
	}
	return name
}

func AddServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, newService couchbasev2.ServerConfig, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	settings := append(cl.Spec.Servers, newService)
	return PatchCluster(k8s, cl, jsonpatch.NewPatchSet().Replace("/Spec/Servers", settings), timeout)
}

func MustAddServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, newService couchbasev2.ServerConfig, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	couchbase, err := AddServices(t, k8s, cl, newService, timeout)
	if err != nil {
		Die(t, err)
	}
	return couchbase
}

func RemoveServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, removeServiceName string, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	newServiceConfig := []couchbasev2.ServerConfig{}
	for _, service := range cl.Spec.Servers {
		if service.Name != removeServiceName {
			newServiceConfig = append(newServiceConfig, service)
		}
	}
	return PatchCluster(k8s, cl, jsonpatch.NewPatchSet().Replace("/Spec/Servers", newServiceConfig), timeout)
}

func MustRemoveServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, removeServiceName string, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	couchbase, err := RemoveServices(t, k8s, cl, removeServiceName, timeout)
	if err != nil {
		Die(t, err)
	}
	return couchbase
}

func ScaleServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, servicesMap map[string]int, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	newServiceConfig := []couchbasev2.ServerConfig{}
	for _, service := range cl.Spec.Servers {
		for serviceName, size := range servicesMap {
			if serviceName == service.Name {
				service.Size = size
			}
		}
		newServiceConfig = append(newServiceConfig, service)
	}
	return PatchCluster(k8s, cl, jsonpatch.NewPatchSet().Replace("/Spec/Servers", newServiceConfig), timeout)
}

func MustScaleServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, servicesMap map[string]int, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	couchbase, err := ScaleServices(t, k8s, cl, servicesMap, timeout)
	if err != nil {
		Die(t, err)
	}
	return couchbase
}

// PatchCluster updates the specified cluster with a list of JSON patch objects, returning the updated cluster
func PatchCluster(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return cluster, retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		// Get the current cluster resource
		before, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
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
		updated, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Update(after)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Everything successful
		cluster = updated
		return true, nil
	})
}

// MustPatchCluster patches the cluster with a list of JSON patch objects, returning the updated cluster and dying on error
func MustPatchCluster(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	cluster, err := PatchCluster(k8s, cluster, patches, timeout)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// MustNotPatchCluster patches the cluster with a list of JSON patch objects, dying if the test succeeded.
func MustNotPatchCluster(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet) {
	if _, err := PatchCluster(k8s, cluster, patches, 30*time.Second); err == nil {
		Die(t, fmt.Errorf("cluster patch applied unexpectedly"))
	}
}

func PatchBucket(k8s *types.Cluster, bucket runtime.Object, patches jsonpatch.PatchSet, timeout time.Duration) (runtime.Object, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return bucket, retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		// Get the current bucket resource
		switch t := bucket.(type) {
		case *couchbasev2.CouchbaseBucket:
			before, err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(t.Namespace).Get(t.Name, metav1.GetOptions{})
			if err != nil {
				return false, retryutil.RetryOkError(err)
			}

			// Apply the patch set to the bucket
			after := before.DeepCopy()
			if err := jsonpatch.Apply(after, patches.Patches()); err != nil {
				return false, retryutil.RetryOkError(err)
			}

			// If we are not modifiying e.g. just testing, then return ok
			if reflect.DeepEqual(before, after) {
				return true, nil
			}

			// Attempt to post the update, updating the bucket
			updated, err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(t.Namespace).Update(after)
			if err != nil {
				return false, retryutil.RetryOkError(err)
			}

			bucket = updated
		default:
			return false, fmt.Errorf("unsupported type")
		}

		// Everything successful
		return true, nil
	})
}

func MustPatchBucket(t *testing.T, k8s *types.Cluster, bucket runtime.Object, patches jsonpatch.PatchSet, timeout time.Duration) runtime.Object {
	bucket, err := PatchBucket(k8s, bucket, patches, timeout)
	if err != nil {
		Die(t, err)
	}
	return bucket
}

func PatchReplication(k8s *types.Cluster, replication *couchbasev2.CouchbaseReplication, patches jsonpatch.PatchSet, timeout time.Duration) (*couchbasev2.CouchbaseReplication, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return replication, retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		before, err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(replication.Namespace).Get(replication.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Apply the patch set
		after := before.DeepCopy()
		if err := jsonpatch.Apply(after, patches.Patches()); err != nil {
			return err
		}

		// If we are not modifiying e.g. just testing, then return ok
		if reflect.DeepEqual(before, after) {
			return nil
		}

		// Attempt to post the update
		updated, err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(replication.Namespace).Update(after)
		if err != nil {
			return err
		}

		replication = updated

		// Everything successful
		return nil
	})
}

func MustPatchReplication(t *testing.T, k8s *types.Cluster, replication *couchbasev2.CouchbaseReplication, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseReplication {
	replication, err := PatchReplication(k8s, replication, patches, timeout)
	if err != nil {
		Die(t, err)
	}
	return replication
}

func DestroyCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cluster *couchbasev2.CouchbaseCluster) {
	if err := DeleteCluster(t, crClient, kubeClient, cluster); err != nil {
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

	CleanK8Cluster(k8s, namespace)
}

func DeleteCbCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cbCluster *couchbasev2.CouchbaseCluster) {
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

func CleanK8Cluster(k8s *types.Cluster, namespace string) {
	if err := k8s.KubeClient.BatchV1().Jobs(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete jobs: ", err)
	}
	if err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete couchbaseclusters: ", err)
	}
	if err := WaitUntilPodDeleted(k8s.KubeClient, namespace); err != nil {
		fmt.Println("Warning: Unable to delete pods:", err)
	}

	// Ensure all existing PVCs are deleted before continuing.  In the cloud these may take a
	// while to fully disappear, and may bleed through into other tests, especially ones that
	// cover supportability.
	if err := DeleteAndWaitForPVCDeletion(k8s, namespace, 5*time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete PVCs:", err)
	}
	// Should be garbage collected
	services, err := k8s.KubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err == nil {
		for _, service := range services.Items {
			_ = k8s.KubeClient.CoreV1().Services(namespace).Delete(service.Name, metav1.NewDeleteOptions(0))
		}
	}

	if err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete couchbasebuckets: ", err)
	} else if err := WaitForBucketDeletion(k8s, namespace, time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete couchbasebuckets: ", err)
	}
	if err := k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete couchbaseephemeralbuckets: ", err)
	} else if err := WaitForEphemeralBucketDeletion(k8s, namespace, time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete couchbaseephemeralbuckets: ", err)
	}
	if err := k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete couchbasememcachedbuckets: ", err)
	} else if err := WaitForMemcachedBucketDeletion(k8s, namespace, time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete couchbasememcachedbuckets: ", err)
	}
	if err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete couchbasereplications: ", err)
	} else if err := WaitForReplicationDeletion(k8s, namespace, time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete couchbasereplications: ", err)
	}
	if err := k8s.CRClient.CouchbaseV2().CouchbaseUsers(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete couchbaseusers: ", err)
	} else if err := WaitForAllUserDeletion(k8s, namespace, time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete couchbaseusers: ", err)
	}
	if err := k8s.CRClient.CouchbaseV2().CouchbaseRoles(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete couchbaseroles: ", err)
	} else if err := WaitForRoleDeletion(k8s, namespace, time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete couchbaseroles: ", err)
	}
	if err := k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(namespace).DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{}); err != nil {
		fmt.Println("Warning: Unable to delete couchbaserolebindings: ", err)
	} else if err := WaitForRoleBindingDeletion(k8s, namespace, time.Minute); err != nil {
		fmt.Println("Warning: Unable to delete couchbaserolebindings: ", err)
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
	if err := kubecli.CoreV1().Pods(namespace).Delete(name, metav1.NewDeleteOptions(0)); err != nil {
		return err
	}

	if removeVolumes {
		if err := kubecli.CoreV1().PersistentVolumeClaims(namespace).DeleteCollection(metav1.NewDeleteOptions(0), k8sutil.NodeListOpt(name, clusterName)); err != nil {
			return err
		}
	}

	return nil
}

func RemovePersistentVolumesOfPod(kubeClient kubernetes.Interface, namespace, clusterName string, memberID int) error {
	podMemberName := couchbaseutil.CreateMemberName(clusterName, memberID)
	pvcList, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: "couchbase_node=" + podMemberName})
	if err != nil {
		return fmt.Errorf("unable to fetch persistent volume list for pod %s: %v", podMemberName, err)
	}

	for _, pvc := range pvcList.Items {
		if err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, &metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("failed to delete persistent volume claim %s: %v", pvc.Name, err)
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

func ResizeClusterNoWait(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	t.Logf("Changing Cluster Size To: %v...\n", strconv.Itoa(clusterSize))
	return PatchCluster(k8s, cl, jsonpatch.NewPatchSet().Replace(fmt.Sprintf("/Spec/Servers/%d/Size", service), clusterSize), 30*time.Second)
}

func MustResizeClusterNoWait(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	cluster, err := ResizeClusterNoWait(t, service, clusterSize, k8s, cl)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

// ResizeCluster resizes the MDS service to the desired size and waits until the cluster is
// healthy.
func ResizeCluster(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	cluster, err := ResizeClusterNoWait(t, service, clusterSize, k8s, cl)
	if err != nil {
		return cl, err
	}
	if err := WaitClusterStatusHealthy(t, k8s, cluster, timeout); err != nil {
		return cluster, err
	}
	return cluster, nil
}

func MustResizeCluster(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	cluster, err := ResizeCluster(t, service, clusterSize, k8s, cl, timeout)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func KillPods(t *testing.T, kubeCli kubernetes.Interface, cl *couchbasev2.CouchbaseCluster, numToKill int) {
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

func KillPodForMember(kubeCli kubernetes.Interface, cl *couchbasev2.CouchbaseCluster, memberID int) error {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return KillMember(kubeCli, cl.Namespace, cl.Name, name, true)
}

func MustKillPodForMember(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, memberID int, removeVolumes bool) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	if err := KillMember(k8s.KubeClient, cl.Namespace, cl.Name, name, removeVolumes); err != nil {
		Die(t, err)
	}
}

func CreateMemberPod(k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, m *couchbaseutil.Member) (*v1.Pod, error) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: m.Name,
			Labels: map[string]string{
				operator_constants.LabelApp:      "couchbase",
				operator_constants.LabelCluster:  cl.Name,
				operator_constants.LabelNode:     m.Name,
				operator_constants.LabelNodeConf: m.ServerConfig,
			},
			OwnerReferences: []metav1.OwnerReference{
				cl.AsOwner(),
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				k8sutil.CouchbaseContainer(cl.Spec.Image),
			},
			Hostname:  m.Name,
			Subdomain: cl.Name,
		},
	}

	for _, config := range cl.Spec.Servers {
		if config.Name == m.ServerConfig {
			p, err := k8s.KubeClient.CoreV1().Pods(cl.Namespace).Create(pod)
			if err != nil {
				return nil, err
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err = k8sutil.WaitForPod(ctx, k8s.KubeClient, cl.Namespace, pod.Name, "")
			if err != nil {
				return nil, err
			}
			return p, nil
		}
	}

	return nil, NewErrServerConfigNotFound(m.ServerConfig)
}

func deleteCouchbaseOperator(kubeCli kubernetes.Interface, namespace string) error {
	name, err := GetOperatorName(kubeCli, namespace)
	if err != nil {
		return err
	}
	return kubeCli.CoreV1().Pods(namespace).Delete(name, metav1.NewDeleteOptions(0))
}

func MustDeleteCouchbaseOperator(t *testing.T, k8s *types.Cluster, namespace string) {
	if err := deleteCouchbaseOperator(k8s.KubeClient, namespace); err != nil {
		Die(t, err)
	}
}

func KillOperatorAndWaitForRecovery(k8s *types.Cluster, namespace string) error {
	if err := deleteCouchbaseOperator(k8s.KubeClient, namespace); err != nil {
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

// MustDeleteOperatorDeployment shuts down the operator and waits for it to be garbage collected
// once all the dependant pods are cleaned up.  This allows us to explicitly make alterations
// while the operator is not running and see what happens on a restart without introducing race
// conditions.
func MustDeleteOperatorDeployment(t *testing.T, k8s *types.Cluster, namespace string, deployment *appsv1.Deployment, timeout time.Duration) {
	if err := k8s.KubeClient.AppsV1().Deployments(namespace).Delete(deployment.Name, metav1.NewDeleteOptions(0)); err != nil {
		Die(t, err)
	}

	callback := func() error {
		_, err := k8s.KubeClient.AppsV1().Deployments(namespace).Get(deployment.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("deployment still exists")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}
}

// MustCreateOperatorDeployment is the partner of MustDeleteOperatorDeployment which is used to
// restart the operator synchronously, potentially after modifying resources.
func MustCreateOperatorDeployment(t *testing.T, k8s *types.Cluster, namespace string, deployment *appsv1.Deployment) {
	if _, err := k8s.KubeClient.AppsV1().Deployments(namespace).Create(deployment); err != nil {
		Die(t, err)
	}
}

func GetOperatorName(kubeCli kubernetes.Interface, namespace string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var pods *v1.PodList
	selector := labels.SelectorFromSet(labels.Set(NameLabelSelector("app", "couchbase-operator")))
	outerErr := retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
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
		return "", fmt.Errorf("no pods available")
	}
	if len(operatorPods) > 1 {
		return "couchbase-operator", fmt.Errorf("too many couchbase operators")
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
		return "couchbase-operator", fmt.Errorf("too many couchbase operators")
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
		result[node.Name] = allocatable
	}

	// Next deduct any requests.
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if quantity, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
				result[pod.Spec.NodeName] -= memoryRequirementToFloat(quantity)
				continue
			}
		}
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
func GetMemberPVC(kubeCli kubernetes.Interface, namespace, memberName string, index int, mountName couchbasev2.VolumeMountName) (*v1.PersistentVolumeClaim, error) {
	name := k8sutil.NameForPersistentVolumeClaim(memberName, index, mountName)
	return kubeCli.CoreV1().PersistentVolumeClaims(namespace).Get(name, metav1.GetOptions{})
}

func TLSCheckForCluster(t *testing.T, k8s *types.Cluster, namespace string, ctx *TLSContext) error {
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

func MustCheckClusterTLS(t *testing.T, k8s *types.Cluster, namespace string, ctx *TLSContext) {
	if err := TLSCheckForCluster(t, k8s, namespace, ctx); err != nil {
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
		err := deletePod(t, kubeClient, pod.Name, namespace)
		if err != nil {
			return err
		}
	}
	_, err = WaitPodsDeleted(kubeClient, namespace, metav1.ListOptions{LabelSelector: label})
	if err != nil {
		return err
	}
	return nil
}

func deletePod(t *testing.T, kubeClient kubernetes.Interface, podName, namespace string) error {
	t.Logf("deleting pod: %v", podName)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
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
	_, err = WaitDaemonSetsDeleted(kubeClient, namespace, metav1.ListOptions{LabelSelector: label})
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

	if err := deletePod(t, k8s.KubeClient, member, namespace); err != nil {
		Die(t, err)
	}
}

// MustDeletePodServices deletes all services in the cluster namespace that
// belong to individual pods.
func MustDeletePodServices(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	selector := constants.CouchbaseServerPodLabelStr + couchbase.Name
	services, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		Die(t, err)
	}
	for _, service := range services.Items {
		if _, ok := service.Spec.Selector[operator_constants.LabelNode]; ok {
			if err := DeleteService(k8s.KubeClient, service.Namespace, service.Name, metav1.NewDeleteOptions(0)); err != nil {
				Die(t, err)
			}
		}
	}
}

// GenerateWorkload creates workload on a cluster with the cbc-pillowfight utility.  It
// inserts JSON documents continuously until deleted by the returned cleanup function.
func GenerateWorkload(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, image, bucket string) (func(), error) {
	podName := couchbase.Name + "-workloadgen"
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "couchbase-server",
					Image: image,
					Command: []string{
						"/opt/couchbase/bin/cbc-pillowfight",
					},
					Args: []string{
						"-U",
						fmt.Sprintf("couchbase://%s-srv.%s.svc/%s", couchbase.Name, couchbase.Namespace, bucket),
						"-u",
						string(k8s.DefaultSecret.Data["username"]),
						"-P",
						string(k8s.DefaultSecret.Data["password"]),
						// No pre-population, dive straight in.
						"-n",
						// JSON documents.
						"-J",
						// A small batch size does rudimentary rate limiting as minikube is likely
						// to get pwned.
						"-B", "1",
						// A large (ish) number of items so we generate lots of writes and flushes,
						// even a little compaction, but not too much or rebalances will take forever.
						"-I", "100000",
						// Run continuously.
						"-c", "-1",
					},
				},
			},
		},
	}
	if _, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).Create(pod); err != nil {
		return nil, err
	}

	cleanup := func() {
		_ = k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).Delete(podName, metav1.NewDeleteOptions(0))
	}
	return cleanup, nil
}

func MustGenerateWorkload(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, image, bucket string) func() {
	cleanup, err := GenerateWorkload(k8s, couchbase, image, bucket)
	if err != nil {
		Die(t, err)
	}
	return cleanup
}

// GetUUID returns the UUID of the cluster
func GetUUID(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) (string, error) {
	uuid := ""

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	callback := func() error {
		c, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(couchbase.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if c.Status.ClusterID == "" {
			return fmt.Errorf("cluster ID is not set")
		}

		uuid = c.Status.ClusterID
		return nil
	}

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		return "", err
	}

	return uuid, nil
}

func MustGetUUID(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) string {
	uuid, err := GetUUID(k8s, couchbase, timeout)
	if err != nil {
		Die(t, err)
	}
	return uuid
}
