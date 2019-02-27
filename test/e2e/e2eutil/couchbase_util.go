package e2eutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

type serviceVerifier func(t *testing.T, ci *cbmgr.ClusterInfo, value map[string]int) bool

// newClient returns a new Couchbase management client (internal not go SDK)
func newClient(kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, urls []string) (*cbmgr.Couchbase, error) {
	err, username, password := GetClusterAuth(kubeClient, cl.Namespace, cl.Spec.AuthSecret)
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
func forwardPort(k8s *types.Cluster, namespace, pod, port string) (string, func(), error) {
	// Allocate a free port to use
	sport, err := getFreePort()
	if err != nil {
		return "", nil, err
	}

	pf := &portforward.PortForwarder{
		Config:    k8s.Config,
		Client:    k8s.KubeClient,
		Namespace: namespace,
		Pod:       pod,
		Port:      sport + ":" + port,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err = retryutil.Retry(ctx, 5*time.Second, IntMax, func() (bool, error) {
		if err := pf.ForwardPorts(); err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
	if err != nil {
		return "", nil, err
	}

	// Analytics and eventing don't support persistent connections so we get a
	// lot of "connection reset by peer" spam on the console.
	portforward.Silent()

	return sport, func() { pf.Close() }, nil
}

// CreateAdminConsoleClient returns a client for interacting with the admin service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func CreateAdminConsoleClient(k8s *types.Cluster, cluster *api.CouchbaseCluster) (*cbmgr.Couchbase, func(), error) {
	// Create a port forward and get a host connection string
	host, cleanup, err := GetAdminConsoleHostURL(k8s, cluster)
	if err != nil {
		return nil, nil, err
	}

	// Return a client proxying through the port forwarder.
	client, err := newClient(k8s.KubeClient, cluster, []string{"http://" + host})
	if err != nil {
		return nil, nil, err
	}

	return client, cleanup, nil
}

func MustCreateAdminConsoleClient(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster) (*cbmgr.Couchbase, func()) {
	client, cleanup, err := CreateAdminConsoleClient(k8s, cluster)
	if err != nil {
		Die(t, err)
	}
	return client, cleanup
}

// GetPod selects a random pod in the cluster.
func GetPod(k8s *types.Cluster, cluster *api.CouchbaseCluster) (*corev1.Pod, error) {
	// Grab the Couchbase service and use it to select the set of pods to connect to.
	svc, err := k8s.KubeClient.CoreV1().Services(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	selector := labels.SelectorFromSet(svc.Spec.Selector).String()
	pods, err := k8s.KubeClient.CoreV1().Pods(cluster.Namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, err
	}

	return &pods.Items[rand.Int()%len(pods.Items)], nil
}

// GetAdminConsoleHostURL returns a URL for interacting with the admin service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func GetAdminConsoleHostURL(k8s *types.Cluster, cluster *api.CouchbaseCluster) (string, func(), error) {
	// Forward port to a pod to the local host.  Pick a random pod this will prevent hangs
	// if the pod we are always selecting isn't the one we need.
	pod, err := GetPod(k8s, cluster)
	if err != nil {
		return "", nil, err
	}
	port, cleanup, err := forwardPort(k8s, cluster.Namespace, pod.Name, "8091")
	if err != nil {
		return "", nil, err
	}
	return "127.0.0.1:" + port, cleanup, nil
}

func MustGetAdminConsoleHostURL(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster) (string, func()) {
	url, cleanup, err := GetAdminConsoleHostURL(k8s, cluster)
	if err != nil {
		Die(t, err)
	}
	return url, cleanup
}

// PatchBucketInfo tries patching the bucket information returned directly from Couchbase server.
func PatchBucketInfo(t *testing.T, client *cbmgr.Couchbase, bucketName string, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, IntMax, func() (done bool, err error) {
		before, err := getBucket(t, client, bucketName)
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

// Get Bucket from couchbase cluster
func getBucket(t *testing.T, client *cbmgr.Couchbase, bucketName string) (*cbmgr.Bucket, error) {
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
func InsertJsonDocsIntoBucket(k8s *types.Cluster, cluster *api.CouchbaseCluster, bucketName string, docStartIndex, numOfDocs int) error {
	client, cleanup, err := CreateAdminConsoleClient(k8s, cluster)
	if err != nil {
		return err
	}
	defer cleanup()

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

func MustInsertJsonDocsIntoBucket(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, bucketName string, docStartIndex, numOfDocs int) {
	if err := InsertJsonDocsIntoBucket(k8s, cluster, bucketName, docStartIndex, numOfDocs); err != nil {
		Die(t, err)
	}
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

// EjectMember removes the given member index from the cluster,
func EjectMember(t *testing.T, k8s *types.Cluster, couchbase *api.CouchbaseCluster, index int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, cleanup := MustCreateAdminConsoleClient(t, k8s, couchbase)
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
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
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

	client := http.Client{Timeout: time.Minute}
	response, err := client.Do(request)
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

	client := http.Client{Timeout: time.Minute}
	response, err := client.Do(request)
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
