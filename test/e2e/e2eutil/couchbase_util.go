package e2eutil

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/gocbmgr"

	"k8s.io/client-go/kubernetes"
)

type bucketModifier func(b *api.BucketConfig)
type bucketVerifier func(t *testing.T, b *cbmgr.Bucket) bool
type clusterVerifier func(t *testing.T, ci *cbmgr.ClusterInfo, value string) bool
type serviceVerifier func(t *testing.T, ci *cbmgr.ClusterInfo, value map[string]int) bool
type autoFailoverVerifier func(t *testing.T, ci *cbmgr.AutoFailoverSettings, value string) bool
type indexSettingVerifier func(t *testing.T, ci *cbmgr.IndexSettings, value string) bool
type bucketInfoVerifier func(t *testing.T, bs *cbmgr.Bucket, bucketKey string, bucketValue string) bool

func NewClient(t *testing.T, kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, urls []string) (*cbmgr.Couchbase, error) {
	err, username, password := GetClusterAuth(t, kubeClient, cl.Namespace, cl.Spec.AuthSecret)
	if err != nil {
		return nil, err
	}

	client := cbmgr.New(username, password)
	client.SetEndpoints(urls)
	return client, nil
}

// Creates client for interacting with admin console of crd
// TODO: testing arg is not needed and will be removed, but has depends here
func CreateAdminConsoleClient(t *testing.T, apiServerHost string, kubeClient kubernetes.Interface, cl *api.CouchbaseCluster) (*cbmgr.Couchbase, error) {
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
	return retryutil.RetryOnErr(Context, 5*time.Second, tries, "verify edit bucket", "test-cluster",
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
func AddNode(t *testing.T, client *cbmgr.Couchbase, services api.ServiceList, username, password, hostname string) error {
	t.Logf("adding node: %s", hostname)

	svcs, err := cbmgr.ServiceListFromStringArray(services.StringSlice())
	if err != nil {
		return err
	}
	err = client.AddNode(hostname, username, password, svcs)
	return retryutil.RetryOnErr(Context, 5*time.Second, 36, "add node", hostname,
		func() error {
			return client.AddNode(hostname, username, password, svcs)
		})
}

// Rebalance out creates memberset with member at specified index and performs rebalance
func RebalanceOutMember(t *testing.T, client *cbmgr.Couchbase, clusterName, namespace string, memberIndex int, wait bool) error {
	outMember := MemberFromSpecProps(clusterName, namespace, "", memberIndex)
	nodesToRemove := []string{outMember.HostURL()}
	t.Logf("rebalance out: %s", outMember.Name)

	return retryutil.RetryOnErr(Context, 5*time.Second, 36, "rebalance", clusterName,
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
	apiBucket := couchbaseutil.ApiBucketToCbmgr(&bucket)

	return apiBucket, nil
}

func GetClusterInfo(t *testing.T, client *cbmgr.Couchbase, tries int) (*cbmgr.ClusterInfo, error) {
	err := retryutil.RetryOnErr(Context, 5*time.Second, tries, "get cluster info", "test-cluster",
		func() error {
			_, err := client.ClusterInfo()
			if err != nil {
				t.Logf("error getting cluster info: %v", err)
				return err
			}
			return nil
		})
	if err != nil {
		return nil, err

	}
	t.Logf("getting cluster info")
	info, err := client.ClusterInfo()
	if err != nil {
		return nil, err

	}
	return info, nil
}

func GetNodesFromCluster(t *testing.T, client *cbmgr.Couchbase, tries int) ([]cbmgr.NodeInfo, error) {
	info, err := GetClusterInfo(t, client, tries)
	if err != nil {
		return nil, err

	}
	return info.Nodes, nil
}

func FailoverNode(t *testing.T, client *cbmgr.Couchbase, tries int, nodeName string) error {
	err := retryutil.RetryOnErr(Context, 5*time.Second, tries, "failover nodes", "test-cluster",
		func() error {
			err := client.Failover(nodeName)
			if err != nil {
				t.Logf("Failover error: %v", err)
				return err
			}
			return nil
		})
	if err != nil {
		return err
	}
	t.Logf("failover success: %v", nodeName)
	return nil
}

// FailoverNodes manually fails over nodes, raises an error if the cluster is the wrong size
// or the number of down nodes is wrong
func FailoverNodes(t *testing.T, client *cbmgr.Couchbase, expectedSize, expectedDown int) {
	t.Logf("getting cluster nodes...")
	clusterNodes, err := GetNodesFromCluster(t, client, Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != expectedSize {
		t.Fatalf("expected %d nodes, got %d", expectedSize, len(clusterNodes))
	}

	nodesToFailover := []string{}
	for _, node := range clusterNodes {
		t.Logf("node status: %v", node.Status)
		if node.Status == "unhealthy" {
			nodesToFailover = append(nodesToFailover, node.HostName)
		}
	}

	t.Logf("failing over nodes: %v", nodesToFailover)
	for _, nodeName := range nodesToFailover {
		err = FailoverNode(t, client, Retries5, nodeName)
		if err != nil {
			t.Fatalf("failed to failover node: %v with error: %v", nodeName, err)
		}
	}
}

func VerifyClusterBalancedAndHealthy(t *testing.T, client *cbmgr.Couchbase, tries int) error {
	err := retryutil.RetryOnErr(Context, 5*time.Second, tries, "failover nodes", "test-cluster",
		func() error {
			clusterInfo, err := client.ClusterInfo()
			if err != nil {
				return err
			}

			if clusterInfo.Balanced != true {
				t.Logf("cluster balanced: %v", clusterInfo.Balanced)
				return NewErrVerifyClusterInfo()
			}
			for _, node := range clusterInfo.Nodes {
				if node.Status != "healthy" {
					t.Logf("node status: %v", node.Status)
					return NewErrVerifyClusterInfo()
				}
			}
			return nil
		})
	if err != nil {
		return err
	}
	return nil
}

func WaitForUnhealthyNodes(t *testing.T, client *cbmgr.Couchbase, tries int, numUnhealthy int) error {
	err := retryutil.RetryOnErr(Context, 5*time.Second, tries, "wait for unhealthy nodes", "test-cluster",
		func() error {
			unhealthy := []string{}
			clusterInfo, err := client.ClusterInfo()
			if err != nil {
				return err
			}
			for _, node := range clusterInfo.Nodes {
				if node.Status == "unhealthy" {
					unhealthy = append(unhealthy, node.HostName)
				}
			}
			if len(unhealthy) != numUnhealthy {
				t.Logf("unhealthy nodes: %v", unhealthy)
				return NewErrVerifyClusterInfo()
			}
			return nil
		})
	if err != nil {
		return err
	}
	return nil
}

func VerifyClusterInfo(t *testing.T, client *cbmgr.Couchbase, tries int, value string, verifiers ...clusterVerifier) error {
	return retryutil.RetryOnErr(Context, 5*time.Second, tries, "verify cluster info", "test-cluster",
		func() error {

			info, err := client.ClusterInfo()
			if err != nil {
				return err
			}
			for _, verify := range verifiers {
				if verify(t, info, value) == false {
					return NewErrVerifyClusterInfo()
				}
			}
			return nil
		})
}

func VerifyBucketDeleted(t *testing.T, client *cbmgr.Couchbase, tries int, bucketName string) error {
	return retryutil.RetryOnErr(Context, 5*time.Second, tries, "verify bucket deleted", "test-cluster",
		func() error {

			info, err := client.GetBuckets()
			if err != nil {
				return err
			}

			for _, bucket := range info {
				if bucket.BucketName == bucketName {
					return NewErrVerifyClusterInfo()
				}
			}

			return nil
		})
}

func VerifyBucketInfo(t *testing.T, client *cbmgr.Couchbase, tries int, bucketName string, bucketKey string, bucketValue string, verifiers ...bucketInfoVerifier) error {
	return retryutil.RetryOnErr(Context, 5*time.Second, tries, "verify cluster info", "test-cluster",
		func() error {

			info, err := client.GetBuckets()
			if err != nil {
				return err
			}
			bucketExists := false
			for _, bucket := range info {
				if bucket.BucketName == bucketName {
					bucketExists = true
					for _, verify := range verifiers {
						if verify(t, bucket, bucketKey, bucketValue) == false {
							return NewErrVerifyClusterInfo()
						}
					}
					break
				}
			}
			if !bucketExists {
				return NewErrVerifyClusterInfo()
			}

			return nil
		})
}

func BucketInfoVerifier(t *testing.T, bucket *cbmgr.Bucket, bucketKey string, bucketValue string) bool {
	verified := false
	switch {
	case bucketKey == "BucketType":
		bt := bucketValue
		t.Logf("Want BucketType: %v \n Have BucketType: %v", bt, bucket.BucketType)
		verified = bucket.BucketType == bt
	case bucketKey == "ConflictResolution":
		cr := bucketValue
		t.Logf("Want ConflictResolution: %v \n Have ConflictResolution: %v", cr, bucket.ConflictResolution)
		verified = *bucket.ConflictResolution == cr
	case bucketKey == "BucketMemoryQuota":
		bmq, _ := strconv.Atoi(bucketValue)
		t.Logf("Want BucketMemoryQuota: %v \n Have BucketMemoryQuota: %v", bmq, bucket.BucketMemoryQuota)
		verified = bucket.BucketMemoryQuota == bmq
	case bucketKey == "BucketReplicas":
		br, _ := strconv.Atoi(bucketValue)
		t.Logf("Want BucketReplicas: %v \n Have BukcetReplicas: %v", br, bucket.BucketReplicas)
		verified = bucket.BucketReplicas == br
	case bucketKey == "EnableFlush":
		ef, _ := strconv.ParseBool(bucketValue)
		t.Logf("EnableFlush: %v", bucket.EnableFlush)
		if ef == false {
			if bucket.EnableFlush == nil {
				verified = true
			}
			if bucket.EnableFlush != nil {
				verified = *bucket.EnableFlush == ef
			}
		}
		if ef == true {
			if bucket.EnableFlush != nil {
				verified = *bucket.EnableFlush == ef
			}
		}
	}
	return verified
}

func VerifyAutoFailoverInfo(t *testing.T, client *cbmgr.Couchbase, tries int, value string, verifiers ...autoFailoverVerifier) error {
	return retryutil.RetryOnErr(Context, 5*time.Second, tries, "verify autofailover info", "test-cluster",
		func() error {

			info, err := client.GetAutoFailoverSettings()
			if err != nil {
				return err
			}
			for _, verify := range verifiers {
				if verify(t, info, value) == false {
					return NewErrAutoFailoverInfo()
				}
			}
			return nil
		})
}

func VerifyIndexSettingInfo(t *testing.T, client *cbmgr.Couchbase, tries int, value string, verifiers ...indexSettingVerifier) error {
	return retryutil.RetryOnErr(Context, 5*time.Second, tries, "verify autofailover info", "test-cluster",
		func() error {

			info, err := client.GetIndexSettings()
			if err != nil {
				return err
			}
			for _, verify := range verifiers {
				if verify(t, info, value) == false {
					return NewErrIndexSettingInfo()
				}
			}
			return nil
		})
}

func VerifyServices(t *testing.T, client *cbmgr.Couchbase, tries int, value map[string]int, verifiers ...serviceVerifier) error {
	return retryutil.RetryOnErr(Context, 5*time.Second, tries, "verify service info", "test-cluster",
		func() error {

			info, err := client.ClusterInfo()
			if err != nil {
				return err
			}
			for _, verify := range verifiers {
				if verify(t, info, value) == false {
					return NewErrVerifyServices()
				}
			}
			return nil
		})
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

func DataServiceMemQuotaVerifier(t *testing.T, ci *cbmgr.ClusterInfo, value string) bool {
	valueInt, _ := strconv.ParseUint(value, 10, 64)
	dataServiceMemQuota := ci.DataMemoryQuotaMB == valueInt
	t.Logf("data service mem quota: %v", ci.DataMemoryQuotaMB)
	return dataServiceMemQuota
}

func IndexServiceMemQuotaVerifier(t *testing.T, ci *cbmgr.ClusterInfo, value string) bool {
	valueInt, _ := strconv.ParseUint(value, 10, 64)
	indexServiceMemQuota := ci.IndexMemoryQuotaMB == valueInt
	t.Logf("index service mem quota: %v", ci.IndexMemoryQuotaMB)
	return indexServiceMemQuota
}

func SearchServiceMemQuotaVerifier(t *testing.T, ci *cbmgr.ClusterInfo, value string) bool {
	valueInt, _ := strconv.ParseUint(value, 10, 64)
	searchServiceMemQuota := ci.SearchMemoryQuotaMB == valueInt
	t.Logf("search service mem quota: %v", ci.SearchMemoryQuotaMB)
	return searchServiceMemQuota
}

func NumNodesVerifier(t *testing.T, ci *cbmgr.ClusterInfo, value string) bool {
	valueInt, _ := strconv.Atoi(value)
	numNodes := len(ci.Nodes) == valueInt
	t.Logf("number of nodes: %v", len(ci.Nodes))
	return numNodes
}

func NodeServicesVerifier(t *testing.T, ci *cbmgr.ClusterInfo, servicesMap map[string]int) bool {
	clusterServices := map[string]int{
		"Data":  0,
		"N1QL":  0,
		"Index": 0,
		"FTS":   0,
	}
	for _, node := range ci.Nodes {
		for _, service := range node.Services {
			switch {
			case service == "kv":
				clusterServices["Data"] = clusterServices["Data"] + 1
			case service == "n1ql":
				clusterServices["N1QL"] = clusterServices["N1QL"] + 1
			case service == "index":
				clusterServices["Index"] = clusterServices["Index"] + 1
			case service == "fts":
				clusterServices["FTS"] = clusterServices["FTS"] + 1
			}
		}
	}
	eq := reflect.DeepEqual(clusterServices, servicesMap)
	if eq {
		return true
	} else {
		return false
	}
}

func AutoFailoverEnabledVerifier(t *testing.T, afs *cbmgr.AutoFailoverSettings, value string) bool {
	valueInt, _ := strconv.ParseBool(value)
	autoFailoverEnabled := afs.Enabled == valueInt
	t.Logf("autofailover enabled: %v", afs.Enabled)
	return autoFailoverEnabled
}

func AutoFailoverTimeoutVerifier(t *testing.T, afs *cbmgr.AutoFailoverSettings, value string) bool {
	valueInt, _ := strconv.Atoi(value)
	autoFailoverEnabled := afs.Timeout == uint64(valueInt)
	t.Logf("autofailover timeout: %v", afs.Timeout)
	return autoFailoverEnabled
}

func IndexSettingVerifier(t *testing.T, is *cbmgr.IndexSettings, value string) bool {
	indexSetting := string(is.StorageMode) == value
	t.Logf("index setting: %v", is.StorageMode)
	return indexSetting
}
