package e2eutil

import (
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbaselabs/gocbmgr"

	"k8s.io/client-go/kubernetes"
)

type bucketModifier func(b *api.BucketConfig)
type bucketVerifier func(t *testing.T, b *cbmgr.Bucket) bool

func NewClient(t *testing.T, kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, urls []string) (*cbmgr.Couchbase, error) {
	err, username, password := GetClusterAuth(t, kubeClient, cl.Namespace, cl.Spec.AuthSecret)
	if err != nil {
		return nil, err
	}

	return cbmgr.New(urls, username, password), nil
}

// Creates client for interacting with admin console of crd
// TODO: testing arg is not needed and will be removed, but has depends here
func CreateAdminConsoleClient(t *testing.T, kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, apiServerHost string) (*cbmgr.Couchbase, error) {
	if cl.Spec.ExposeAdminConsole == false {
		return nil, NewErrConsoleNotExposed()
	}
	consoleURL, err := AdminConsoleURL(apiServerHost, cl.Status.AdminConsolePort)
	if err != nil {
		return nil, err
	}
	return NewClient(t, kubeClient, cl, []string{consoleURL})
}

func EditBucket(t *testing.T, client *cbmgr.Couchbase, bucket *cbmgr.Bucket) error {
	t.Logf("editing bucket: %s", bucket.BucketName)
	return client.EditBucket(bucket)
}

// Edit bucket to make sure change occurred via list of verification methods.
// This is done within a retry loop in case the operator reconciles bucket
// changes before verifiers run
func EditBucketAndVerify(t *testing.T, client *cbmgr.Couchbase, bucket *cbmgr.Bucket, tries int, verifiers ...bucketVerifier) error {
	return retryutil.RetryOnErr(5*time.Second, tries, "verify edit bucket", "test-cluster",
		func() error {

			err := EditBucket(t, client, bucket)
			if err != nil {
				return err
			}
			newBucket, err := GetBucket(t, client, bucket.BucketName)
			if err != nil {
				return err
			}
			for _, verify := range verifiers {
				if verify(t, newBucket) == false {
					return NewErrVerifyEditBucket(bucket.BucketName)
				}
			}
			return nil
		})
}

// Get Bucket from couchbase cluster
func GetBucket(t *testing.T, client *cbmgr.Couchbase, bucketName string) (*cbmgr.Bucket, error) {
	t.Logf("get bucket: %s", bucketName)
	buckets, err := client.GetBuckets()
	if err == nil {
		for _, b := range buckets {
			if b.BucketName == bucketName {
				return b, nil
			}
		}
	}
	return nil, NewErrGetClusterBucket(bucketName)
}

// Add a node to the cluster
func AddNode(t *testing.T, client *cbmgr.Couchbase, services []string, username, password, hostname string) error {
	t.Logf("adding node: %s", hostname)

	svcs, err := cbmgr.ServiceListFromStringArray(services)
	if err != nil {
		return err
	}
	err = client.AddNode(hostname, username, password, svcs)
	return retryutil.RetryOnErr(5*time.Second, 36, "add node", hostname,
		func() error {
			return client.AddNode(hostname, username, password, svcs)
		})
}

// Rebalance out creates memberset with member at specified index and performs rebalance
func RebalanceOutMember(t *testing.T, client *cbmgr.Couchbase, clusterName, namespace string, memberIndex int, wait bool) error {
	outMember := MemberFromSpecProps(clusterName, namespace, "", memberIndex)
	nodesToRemove := []string{outMember.HostURL()}
	t.Logf("rebalance out: %s", outMember.Name)

	return retryutil.RetryOnErr(5*time.Second, 36, "rebalance", clusterName,
		func() error {
			status, err := client.Rebalance(nodesToRemove)
			if wait && status != nil {
				return status.Wait()
			}
			return err
		})
}

func MemberFromSpecProps(name, namespace, serverConfig string, memberIndex int) *couchbaseutil.Member {
	return &couchbaseutil.Member{
		Name:         couchbaseutil.CreateMemberName(name, memberIndex),
		Namespace:    namespace,
		ServerConfig: serverConfig,
		SecureClient: false,
	}
}

// Converts cluster spec bucket to cbmgr api type with
// the option of modifying the spec prior to translation
func SpecToApiBucket(bucketName string, cl *api.CouchbaseCluster, modifiers ...bucketModifier) (*cbmgr.Bucket, error) {
	var bucket api.BucketConfig
	if b := cl.Spec.GetBucketByName(bucketName); b != nil {
		bucket = *b
	} else {
		return nil, NewErrGetBucketSpec(bucketName)
	}
	for _, f := range modifiers {
		f(&bucket)
	}
	apiBucket, err := couchbaseutil.ApiBucketToCbmgr(&bucket)
	if err != nil {
		return nil, err
	}

	return apiBucket, nil
}

// Verifies flush is disabled for an api bucket
func FlushDisabledVerifier(t *testing.T, b *cbmgr.Bucket) bool {
	// flush can be 'nil' as rest api doesn't specify flush info when disabled
	flushDisabled := b.EnableFlush == nil || *b.EnableFlush == false
	t.Logf("disabled bucket flush: %v", flushDisabled)
	return flushDisabled
}

func ThreeReplicaVerifier(t *testing.T, b *cbmgr.Bucket) bool {
	threeReplicas := b.BucketReplicas == 3
	t.Logf("bucket replicas: %v", b.BucketReplicas)
	return threeReplicas
}

func DefaultIoPriorityVerifier(t *testing.T, b *cbmgr.Bucket) bool {
	defaultIoPriority := b.IoPriority == "low"
	t.Logf("io priority: %v", b.IoPriority)
	return defaultIoPriority
}
