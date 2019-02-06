package e2eutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/gocbmgr"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type bucketModifier func(b *api.BucketConfig)
type bucketVerifier func(t *testing.T, b *cbmgr.Bucket) bool
type clusterVerifier func(t *testing.T, ci *cbmgr.ClusterInfo, value string) bool
type serviceVerifier func(t *testing.T, ci *cbmgr.ClusterInfo, value map[string]int) bool
type autoFailoverVerifier func(t *testing.T, ci *cbmgr.AutoFailoverSettings, value string) bool
type indexSettingVerifier func(t *testing.T, ci *cbmgr.IndexSettings, value string) bool
type bucketInfoVerifier func(t *testing.T, bs *cbmgr.Bucket, bucketKey string, bucketValue string) bool

// newClient returns a new Couchbase management client (internal not go SDK)
func newClient(t *testing.T, kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, urls []string) (*cbmgr.Couchbase, error) {
	err, username, password := GetClusterAuth(t, kubeClient, cl.Namespace, cl.Spec.AuthSecret)
	if err != nil {
		return nil, err
	}

	client := cbmgr.New(username, password)
	client.SetEndpoints(urls)
	return client, nil
}

// getFreePort probes the kernel for a randomly allocated port to use for port forwarding.
func getFreePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return "", err
	}

	return port, nil
}

// forwardPort creates a local listener that forwards connections on to the specified
// pod.  It returns a network adddress/port and a clean up function.  The port is random
// so that multiple forwards can be active for the target port.
func forwardPort(t *testing.T, k8s *types.Cluster, namespace, pod, port string) (string, func()) {
	// Allocate a free port to use
	sport, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}

	pf := &portforward.PortForwarder{
		Config:    k8s.Config,
		Client:    k8s.KubeClient,
		Namespace: namespace,
		Pod:       pod,
		Port:      sport + ":" + port,
	}
	if err := pf.ForwardPorts(); err != nil {
		t.Fatal(err)
	}

	// Analytics and eventing don't support persistent connections so we get a
	// lot of "connection reset by peer" spam on the console.
	portforward.Silent()

	return sport, func() { pf.Close() }
}

// CreateAdminConsoleClient returns a client for interacting with the admin service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func CreateAdminConsoleClient(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster) (*cbmgr.Couchbase, func()) {
	// Create a port forward and get a host connection string
	host, cleanup := GetAdminConsoleHostURL(t, k8s, cluster)

	// Return a client proxying through the port forwarder.
	client, err := newClient(t, k8s.KubeClient, cluster, []string{"http://" + host})
	if err != nil {
		t.Fatal(err)
	}

	return client, cleanup
}

// GetPod selects a random pod in the cluster.
func GetPod(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster) *corev1.Pod {
	// Grab the Couchbase service and use it to select the set of pods to connect to.
	svc, err := k8s.KubeClient.CoreV1().Services(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
	if err != nil {
		Die(t, err)
	}

	selector := labels.SelectorFromSet(svc.Spec.Selector).String()
	pods, err := k8s.KubeClient.CoreV1().Pods(cluster.Namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		Die(t, err)
		t.Fatal(err)
	}

	if len(pods.Items) == 0 {
		Die(t, fmt.Errorf("no pods found"))
	}

	return &pods.Items[rand.Int()%len(pods.Items)]
}

// GetAdminConsoleHostURL returns a URL for interacting with the admin service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func GetAdminConsoleHostURL(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster) (string, func()) {
	// Forward port to a pod to the local host.  Pick a random pod this will prevent hangs
	// if the pod we are always selecting isn't the one we need.
	port, cleanup := forwardPort(t, k8s, cluster.Namespace, GetPod(t, k8s, cluster).Name, "8091")
	return "127.0.0.1:" + port, cleanup
}

// PatchBucketInfo tries patching the bucket information returned directly from Couchbase server.
func PatchBucketInfo(t *testing.T, client *cbmgr.Couchbase, bucketName string, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, IntMax, func() (done bool, err error) {
		before, err := GetBucket(t, client, bucketName)
		if err != nil {
			return false, err
		}

		after := *before
		if err := jsonpatch.Apply(&after, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		if reflect.DeepEqual(before, after) {
			return true, nil
		}

		if err := client.EditBucket(&after); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	})
}

func MustPatchBucketInfo(t *testing.T, client *cbmgr.Couchbase, bucketName string, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchBucketInfo(t, client, bucketName, patches, timeout); err != nil {
		Die(t, err)
	}
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

// Inserts Json docs into couchbase bucket
func InsertJsonDocsIntoBucket(client *cbmgr.Couchbase, bucketName string, docStartIndex, numOfDocs int) error {
	numOfDocs += docStartIndex
	for docIndex := docStartIndex; docIndex < numOfDocs; docIndex++ {
		docKey := "doc" + strconv.Itoa(docIndex)
		docMap := map[string]string{}
		docMap["key1"] = "dummyVal 1"
		docMap["key2"] = "dummyVal 2"
		docMap["key3"] = "dummyVal 3"
		docMap["key4"] = "dummyVal 4"

		// Convert map data to byte array
		docData, err := json.Marshal(docMap)
		if err != nil {
			return errors.New("Failed to unmarshal map into bytes: " + err.Error())
		}
		docData = append([]byte("value="), docData...)

		// Get bucket Obj
		bucketObj, err := client.GetBucket(bucketName)
		if err != nil {
			return errors.New("Failed to retrieve couchbase bucket " + bucketName + ": " + err.Error())
		}

		// Inserts document using client
		if err := client.InsertDoc(bucketObj, docKey, docData); err != nil {
			return errors.New("Failed to insert doc " + docKey + ": " + err.Error())
		}
	}
	return nil
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
			err := client.Rebalance(nodesToRemove)
			if err != nil {
				return err
			}

			if wait {
				progress := client.NewRebalanceProgress()

			RebalanceWaitLoop:
				for {
					select {
					case _, ok := <-progress.Status():
						// Channel closed, rebalance complete.
						if !ok {
							break RebalanceWaitLoop
						}
					case err := <-progress.Error():
						return err
					}
				}
			}

			return nil
		})
}

func MustRebalanceOutMember(t *testing.T, client *cbmgr.Couchbase, clusterName, namespace string, memberIndex int, wait bool) {
	if err := RebalanceOutMember(t, client, clusterName, namespace, memberIndex, wait); err != nil {
		Die(t, err)
	}
}

// EjectMember removes the given member index from the cluster,
func EjectMember(t *testing.T, k8s *types.Cluster, couchbase *api.CouchbaseCluster, index int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, cleanup := CreateAdminConsoleClient(t, k8s, couchbase)
	defer cleanup()

	member := MemberFromSpecProps(couchbase.Name, couchbase.Namespace, "", index)
	err := client.Rebalance([]string{member.HostURL()})
	if err != nil {
		return err
	}

	// Given we could be balancing out the member we are talking to using a progress channel
	// is not the best option here as it may error as the operator does things in the background
	// affecting this.  The best option is to just check for the rebalance status to complete.
	return retryutil.Retry(ctx, time.Second, IntMax, func() (bool, error) {
		client, cleanup := CreateAdminConsoleClient(t, k8s, couchbase)
		defer cleanup()

		info, err := client.ClusterInfo()
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return info.RebalanceStatus == "none", nil
	})
}

func MustEjectMember(t *testing.T, k8s *types.Cluster, couchbase *api.CouchbaseCluster, index int, timeout time.Duration) {
	if err := EjectMember(t, k8s, couchbase, index, timeout); err != nil {
		Die(t, err)
	}
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
func FailoverNodes(t *testing.T, client *cbmgr.Couchbase, cbClusterSize int, memberIdsToFailover []int) {
	clusterNodes, err := GetNodesFromCluster(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("Failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != cbClusterSize {
		t.Fatalf("Expected %d nodes, got %d nodes", cbClusterSize, len(clusterNodes))
	}

	nodesToFailover := []string{}
	for _, node := range clusterNodes {
		t.Logf("Node %s: %v", node.HostName, node.Status)
		if node.Status == "unhealthy" {
			nodesToFailover = append(nodesToFailover, node.HostName)
		}
	}

	t.Logf("Failing over nodes: %v", nodesToFailover)
	for _, nodeName := range nodesToFailover {
		if err := FailoverNode(t, client, constants.Retries5, nodeName); err != nil {
			t.Fatalf("Failed to failover node '%s': %v", nodeName, err)
		}
	}
}

func VerifyClusterBalancedAndHealthy(t *testing.T, client *cbmgr.Couchbase, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := retryutil.RetryOnErr(ctx, 5*time.Second, IntMax, "failover nodes", "test-cluster",
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

func MustVerifyClusterBalancedAndHealthy(t *testing.T, client *cbmgr.Couchbase, timeout time.Duration) {
	if err := VerifyClusterBalancedAndHealthy(t, client, timeout); err != nil {
		Die(t, err)
	}
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

// PatchCouchbaseInfo tries patching the cluster information returned directly from Couchbase server.
func PatchCouchbaseInfo(t *testing.T, client *cbmgr.Couchbase, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, IntMax, func() (done bool, err error) {
		info, err := client.ClusterInfo()
		if err != nil {
			return false, err
		}
		if err := jsonpatch.Apply(info, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
}

func MustPatchCouchbaseInfo(t *testing.T, client *cbmgr.Couchbase, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchCouchbaseInfo(t, client, patches, timeout); err != nil {
		Die(t, err)
	}
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

func PatchAutoFailoverInfo(t *testing.T, client *cbmgr.Couchbase, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, IntMax, func() (done bool, err error) {
		info, err := client.GetAutoFailoverSettings()
		if err != nil {
			return false, err
		}
		if err := jsonpatch.Apply(info, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
}

func MustPatchAutoFailoverInfo(t *testing.T, client *cbmgr.Couchbase, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchAutoFailoverInfo(t, client, patches, timeout); err != nil {
		Die(t, err)
	}
}

func PatchIndexSettingInfo(t *testing.T, client *cbmgr.Couchbase, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, IntMax, func() (done bool, err error) {
		info, err := client.GetIndexSettings()
		if err != nil {
			return false, err
		}
		if err := jsonpatch.Apply(info, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
}

func MustPatchIndexSettingInfo(t *testing.T, client *cbmgr.Couchbase, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchIndexSettingInfo(t, client, patches, timeout); err != nil {
		Die(t, err)
	}
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

func DeployEventingFunction(hostUrl, eventingPort, eventingFuncName, srcBucketName, metaBucketName, dstBucketName, jsFunc string) ([]byte, error) {
	hostUrl = "http://" + hostUrl + ":" + eventingPort + "/api/v1/functions?name=" + eventingFuncName
	requestType := "POST"
	hostUsername := "Administrator"
	hostPassword := "password"
	var responseData []byte

	eventingJsonFunc := `[{` +
		`"appname": "` + eventingFuncName + `",` +
		`"id": 0,` +
		`"depcfg":{"buckets":[{"alias":"dst_bucket","bucket_name":"` + dstBucketName + `"}],"metadata_bucket":"` + metaBucketName + `","source_bucket":"` + srcBucketName + `"},` +
		`"version":"", "handleruuid":0,` +
		`"settings": {"dcp_stream_boundary":"everything","deadline_timeout":62,"deployment_status":true,"description":"","execution_timeout":60,"log_level":"INFO","processing_status":true,"user_prefix":"eventing","worker_count":3},` +
		`"using_doc_timer": false,` +
		`"appcode": "` + jsFunc + `"` +
		`}]`

	request, err := http.NewRequest(requestType, hostUrl, strings.NewReader(eventingJsonFunc))
	if err != nil {
		return responseData, errors.New("Http request failed: " + err.Error())
	}

	request.SetBasicAuth(hostUsername, hostPassword)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return responseData, errors.New("Failed to " + err.Error())
	}
	defer response.Body.Close()

	responseData, _ = ioutil.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return responseData, errors.New("Remote call failed with response: " + response.Status + ", " + string(responseData))
	}
	return responseData, nil
}

func ExecuteAnalyticsQuery(hostIp, hostPort, queryStr string) (responseData []byte, err error) {
	hostUrl := "http://" + hostIp + ":" + hostPort + "/analytics/service"
	username := string(e2espec.BasicSecretData["username"])
	password := string(e2espec.BasicSecretData["password"])
	data := []byte(queryStr)

	request, err := http.NewRequest("POST", hostUrl, bytes.NewReader(data))
	if err != nil {
		err = errors.New("Failed to create http request: " + err.Error())
		return
	}

	request.SetBasicAuth(username, password)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		err = errors.New("Http POST failed: " + err.Error())
		return
	}
	defer response.Body.Close()

	responseBody := response.Body
	responseData, _ = ioutil.ReadAll(responseBody)

	if response.StatusCode != http.StatusOK {
		err = errors.New("Remote call failed with response: " + response.Status)
	}
	return
}

func VerifyDocCountInAnalyticsDataset(hostName, hostPort, datasetName, userName, password string, reqNumOfDocs, maxRetries int) error {
	currDocCount := 0
	query := "select count(*) as count from " + datasetName

	type queryResult struct {
		Results []map[string]int
	}

	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		response, err := ExecuteAnalyticsQuery(hostName, hostPort, query)
		if err != nil {
			return errors.New(err.Error() + "-" + string(response))
		}
		queryRes := queryResult{}
		json.Unmarshal(response, &queryRes)
		currDocCount = queryRes.Results[0]["count"]

		if currDocCount == reqNumOfDocs {
			return nil
		}
		time.Sleep(time.Second * 10)
	}
	return errors.New("Mismatch in doc " + datasetName + " count. Current docs " + strconv.Itoa(currDocCount) + ", expecting " + strconv.Itoa(reqNumOfDocs))
}
