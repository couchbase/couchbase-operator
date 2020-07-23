package e2eutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
)

type serviceVerifier func(t *testing.T, ci *couchbaseutil.ClusterInfo, value map[string]int) bool

type CouchbaseClient struct {
	client *couchbaseutil.Client
	host   string
}

// newClient returns a new Couchbase management client (internal not go SDK).
func newClient(kubeClient kubernetes.Interface, cl *couchbasev2.CouchbaseCluster, host string) (*CouchbaseClient, error) {
	username, password, err := GetClusterAuth(kubeClient, cl.Namespace, cl.Spec.Security.AdminSecret)
	if err != nil {
		return nil, err
	}

	client := &CouchbaseClient{
		client: couchbaseutil.New(context.Background(), "", username, password),
		host:   host,
	}

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

	err = retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
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

	return sport, func() { _ = pf.Close() }, nil
}

// CreateAdminConsoleClient returns a client for interacting with the admin service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func CreateAdminConsoleClient(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (*CouchbaseClient, func(), error) {
	// Create a port forward and get a host connection string
	host, cleanup, err := GetAdminConsoleHostURL(k8s, cluster)
	if err != nil {
		return nil, nil, err
	}

	// Return a client proxying through the port forwarder.
	client, err := newClient(k8s.KubeClient, cluster, "http://"+host)
	if err != nil {
		return nil, nil, err
	}

	return client, cleanup, nil
}

func MustCreateAdminConsoleClient(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (*CouchbaseClient, func()) {
	client, cleanup, err := CreateAdminConsoleClient(k8s, cluster)
	if err != nil {
		Die(t, err)
	}

	return client, cleanup
}

// GetPod selects a random pod that may be running a specified service or set of services from the cluster.
func GetPod(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, services []couchbasev2.Service) (*corev1.Pod, error) {
	appreq, err := labels.NewRequirement(constants.LabelApp, selection.Equals, []string{constants.App})
	if err != nil {
		return nil, err
	}

	clusterreq, err := labels.NewRequirement(constants.LabelCluster, selection.Equals, []string{cluster.Name})
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector()
	selector = selector.Add(*appreq, *clusterreq)

	for _, service := range services {
		requirement, err := labels.NewRequirement(fmt.Sprintf("couchbase_service_%s", string(service)), selection.Equals, []string{"enabled"})
		if err != nil {
			return nil, err
		}

		selector = selector.Add(*requirement)
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(cluster.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods selected")
	}

	return &pods.Items[rand.Int()%len(pods.Items)], nil
}

// GetHostURL returns a URL for interacting with a specified service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func GetHostURL(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, service couchbasev2.Service) (string, func(), error) {
	// Admin is special as it's enabled everywhere and doesn't have a label selector
	services := []couchbasev2.Service{}

	if service != couchbasev2.AdminService {
		services = append(services, service)
	}

	pod, err := GetPod(k8s, cluster, services)
	if err != nil {
		return "", nil, err
	}

	portMap := map[couchbasev2.Service]string{
		couchbasev2.AdminService:     "8091",
		couchbasev2.IndexService:     "8092",
		couchbasev2.QueryService:     "8093",
		couchbasev2.SearchService:    "8094",
		couchbasev2.AnalyticsService: "8095",
		couchbasev2.EventingService:  "8096",
		couchbasev2.DataService:      "11210",
	}

	targetPort, ok := portMap[service]
	if !ok {
		return "", nil, fmt.Errorf("unsupported service specified")
	}

	port, cleanup, err := forwardPort(k8s, cluster.Namespace, pod.Name, targetPort)
	if err != nil {
		return "", nil, err
	}

	return "127.0.0.1:" + port, cleanup, nil
}

// GetAdminConsoleHostURL returns a URL for interacting with the Admin service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func GetAdminConsoleHostURL(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (string, func(), error) {
	return GetHostURL(k8s, cluster, couchbasev2.AdminService)
}

// PatchBucketInfo tries patching the bucket information returned directly from Couchbase server.
func PatchBucketInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucketName string, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		before, err := getBucket(client, bucketName)
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

		if err := couchbaseutil.UpdateBucket(&after).On(client.client, client.host); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	})
}

func MustPatchBucketInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucketName string, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchBucketInfo(t, k8s, couchbase, bucketName, patches, timeout); err != nil {
		Die(t, err)
	}
}

// Get Bucket from couchbase cluster.
func getBucket(client *CouchbaseClient, bucketName string) (*couchbaseutil.Bucket, error) {
	buckets := couchbaseutil.BucketList{}
	if err := couchbaseutil.ListBuckets(&buckets).On(client.client, client.host); err != nil {
		return nil, err
	}

	bucket, err := buckets.Get(bucketName)
	if err != nil {
		return nil, err
	}

	return bucket, nil
}

// Inserts Json docs into couchbase bucket.
func InsertJSONDocsIntoBucket(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucketName string, docStartIndex, numOfDocs int) error {
	urlBase, cleanup, err := GetHostURL(k8s, cluster, couchbasev2.AdminService)
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
			return err
		}

		body := "value=" + string(docData)
		url := "http://" + urlBase + "/pools/default/buckets/" + bucketName + "/docs/" + docKey

		request, err := http.NewRequest("POST", url, strings.NewReader(body))
		if err != nil {
			return err
		}

		request.SetBasicAuth("Administrator", "password")
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		client := http.Client{Timeout: time.Minute}

		response, err := client.Do(request)
		if err != nil {
			return err
		}

		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status code %v", response.Status)
		}
	}

	return nil
}

func MustInsertJSONDocsIntoBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucketName string, docStartIndex, numOfDocs int) {
	if err := InsertJSONDocsIntoBucket(k8s, cluster, bucketName, docStartIndex, numOfDocs); err != nil {
		Die(t, err)
	}
}

// Add a node to the cluster.
func AddNode(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, services couchbasev2.ServiceList, member couchbaseutil.Member) error {
	username, password, err := GetClusterAuth(k8s.KubeClient, couchbase.Namespace, k8s.DefaultSecret.Name)
	if err != nil {
		return err
	}

	if _, err := CreateMemberPod(k8s, couchbase, member); err != nil {
		return err
	}

	svcs, err := couchbaseutil.ServiceListFromStringArray(services.StringSlice())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	callback := func() (bool, error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		if err := couchbaseutil.AddNode(member.GetHostURLPlaintext(), username, password, svcs).On(client.client, client.host); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	}
	if err := retryutil.Retry(ctx, 5*time.Second, callback); err != nil {
		return err
	}

	callback = func() (bool, error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		known := make(couchbaseutil.OTPNodeList, len(info.Nodes))

		for i, node := range info.Nodes {
			known[i] = node.OTPNode
		}

		if err := couchbaseutil.Rebalance(known, nil).On(client.client, client.host); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	}
	if err := retryutil.Retry(ctx, 5*time.Second, callback); err != nil {
		return err
	}

	callback = func() (bool, error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return info.RebalanceStatus == "none", nil
	}

	return retryutil.Retry(ctx, time.Second, callback)
}

func MustAddNode(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, services couchbasev2.ServiceList, member couchbaseutil.Member) {
	if err := AddNode(k8s, couchbase, services, member); err != nil {
		Die(t, err)
	}
}

// EjectMember removes the given member index from the cluster.
func EjectMember(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, index int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, cleanup := MustCreateAdminConsoleClient(t, k8s, couchbase)
	defer cleanup()

	info := &couchbaseutil.ClusterInfo{}
	if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
		return err
	}

	known := make(couchbaseutil.OTPNodeList, len(info.Nodes))

	for i, node := range info.Nodes {
		known[i] = node.OTPNode
	}

	member := MemberFromSpecProps(couchbase, "", index)

	eject := couchbaseutil.OTPNodeList{
		member.GetOTPNode(),
	}

	if err := couchbaseutil.Rebalance(known, eject).On(client.client, client.host); err != nil {
		return err
	}

	// Ensure the operator doesn't start replacing the ejected node until we've registered it as having
	// been fully ejected, the two rebalance events may merge in to one otherwise.
	if _, err := PatchCluster(k8s, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute); err != nil {
		return err
	}

	// Given we could be balancing out the member we are talking to using a progress channel
	// is not the best option here as it may error as the operator does things in the background
	// affecting this.  The best option is to just check for the rebalance status to complete.
	callback := func() (bool, error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return info.RebalanceStatus == "none", nil
	}
	if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
		return err
	}

	// Restore the operator back to the previous condition.
	if _, err := PatchCluster(k8s, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute); err != nil {
		return err
	}

	return nil
}

func MustEjectMember(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, index int, timeout time.Duration) {
	if err := EjectMember(t, k8s, couchbase, index, timeout); err != nil {
		Die(t, err)
	}
}

func MemberFromSpecProps(couchbase *couchbasev2.CouchbaseCluster, serverConfig string, memberIndex int) couchbaseutil.Member {
	name := couchbaseutil.CreateMemberName(couchbase.Name, memberIndex)

	return couchbaseutil.NewMember(couchbase.Namespace, couchbase.Name, name, "", serverConfig, false)
}

func FailoverNodes(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, indexes []int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		data := url.Values{}

		for _, index := range indexes {
			name := couchbaseutil.CreateMemberName(couchbase.Name, index)

			member := couchbaseutil.NewPartialMember(couchbase.Namespace, couchbase.Name, name)
			data.Add("otpNode", string(member.GetOTPNode()))
		}

		request := &couchbaseutil.Request{
			Path: "/controller/failOver",
			Body: []byte(data.Encode()),
		}

		if err := client.client.Post(request, client.host); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	})
}

func MustFailoverNode(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, index int, timeout time.Duration) {
	if err := FailoverNodes(k8s, couchbase, []int{index}, timeout); err != nil {
		Die(t, err)
	}
}

func MustFailoverNodes(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, indexes []int, timeout time.Duration) {
	if err := FailoverNodes(k8s, couchbase, indexes, timeout); err != nil {
		Die(t, err)
	}
}

func VerifyClusterBalancedAndHealthy(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		defer cleanup()

		clusterInfo := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(clusterInfo).On(client.client, client.host); err != nil {
			return err
		}

		if !clusterInfo.Balanced {
			return NewErrVerifyClusterInfo()
		}

		for _, node := range clusterInfo.Nodes {
			if node.Status != "healthy" {
				return NewErrVerifyClusterInfo()
			}
		}

		return nil
	})
}

func MustVerifyClusterBalancedAndHealthy(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := VerifyClusterBalancedAndHealthy(k8s, couchbase, timeout); err != nil {
		Die(t, err)
	}
}

func WaitForUnhealthyNodes(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, numUnhealthy int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		defer cleanup()

		clusterInfo := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(clusterInfo).On(client.client, client.host); err != nil {
			return err
		}

		unhealthy := couchbaseutil.HostNameList{}

		for _, node := range clusterInfo.Nodes {
			if node.Status == "unhealthy" {
				unhealthy = append(unhealthy, node.HostName)
			}
		}

		if len(unhealthy) != numUnhealthy {
			return NewErrVerifyClusterInfo()
		}

		return nil
	})
}

func MustWaitForUnhealthyNodes(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, numUnhealthy int, timeout time.Duration) {
	if err := WaitForUnhealthyNodes(k8s, couchbase, numUnhealthy, timeout); err != nil {
		Die(t, err)
	}
}

// PatchCouchbaseInfo tries patching the cluster information returned directly from Couchbase server.
func PatchCouchbaseInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return false, err
		}

		if err := jsonpatch.Apply(info, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	})
}

func MustPatchCouchbaseInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchCouchbaseInfo(t, k8s, couchbase, patches, timeout); err != nil {
		Die(t, err)
	}
}

func PatchAutoFailoverInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		info := &couchbaseutil.AutoFailoverSettings{}
		if err := couchbaseutil.GetAutoFailoverSettings(info).On(client.client, client.host); err != nil {
			return false, err
		}

		if err := jsonpatch.Apply(info, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	})
}

func MustPatchAutoFailoverInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchAutoFailoverInfo(t, k8s, couchbase, patches, timeout); err != nil {
		Die(t, err)
	}
}

func PatchIndexSettingInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		info := &couchbaseutil.IndexSettings{}
		if err := couchbaseutil.GetIndexSettings(info).On(client.client, client.host); err != nil {
			return false, err
		}

		if err := jsonpatch.Apply(info, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	})
}

func MustPatchIndexSettingInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchIndexSettingInfo(t, k8s, couchbase, patches, timeout); err != nil {
		Die(t, err)
	}
}

func PatchAutoCompactionSettings(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		info := &couchbaseutil.AutoCompactionSettings{}
		if err := couchbaseutil.GetAutoCompactionSettings(info).On(client.client, client.host); err != nil {
			return false, err
		}

		if err := jsonpatch.Apply(info, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	})
}

func MustPatchAutoCompactionSettings(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchAutoCompactionSettings(k8s, couchbase, patches, timeout); err != nil {
		Die(t, err)
	}
}

func VerifyServices(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, value map[string]int, verifiers ...serviceVerifier) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		for _, verify := range verifiers {
			if !verify(t, info, value) {
				return false, retryutil.RetryOkError(NewErrVerifyServices())
			}
		}

		return true, nil
	})
}

func MustVerifyServices(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, value map[string]int, verifiers ...serviceVerifier) {
	if err := VerifyServices(t, k8s, couchbase, timeout, value, verifiers...); err != nil {
		Die(t, err)
	}
}

func NodeServicesVerifier(t *testing.T, ci *couchbaseutil.ClusterInfo, servicesMap map[string]int) bool {
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
				clusterServices["Data"]++
			case service == "n1ql":
				clusterServices["N1QL"]++
			case service == "index":
				clusterServices["Index"]++
			case service == "fts":
				clusterServices["FTS"]++
			}
		}
	}

	return reflect.DeepEqual(clusterServices, servicesMap)
}

func MustDeployEventingFunction(t *testing.T, targetKube *types.Cluster, testCouchbase *couchbasev2.CouchbaseCluster, eventingFuncName, srcBucketName, metaBucketName, dstBucketName, jsFunc string, timeout time.Duration) {
	if responseData, err := DeployEventingFunction(t, targetKube, testCouchbase, eventingFuncName, srcBucketName, metaBucketName, dstBucketName, jsFunc, timeout); err != nil {
		t.Log(string(responseData))
		Die(t, err)
	}
}

func DeployEventingFunction(t *testing.T, targetKube *types.Cluster, cluster *couchbasev2.CouchbaseCluster, eventingFuncName, srcBucketName, metaBucketName, dstBucketName, jsFunc string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	requestType := "POST"
	hostUsername := "Administrator"
	hostPassword := "password"

	var responseData []byte

	eventingJSONFunc := `[{` +
		`"appname": "` + eventingFuncName + `",` +
		`"id": 0,` +
		`"depcfg":{"buckets":[{"alias":"dst_bucket","bucket_name":"` + dstBucketName + `"}],"metadata_bucket":"` + metaBucketName + `","source_bucket":"` + srcBucketName + `"},` +
		`"version":"", "handleruuid":0,` +
		`"settings": {"dcp_stream_boundary":"everything","deadline_timeout":62,"deployment_status":true,"description":"","execution_timeout":60,"log_level":"INFO","processing_status":true,"user_prefix":"eventing","worker_count":3},` +
		`"using_doc_timer": false,` +
		`"appcode": "` + jsFunc + `"` +
		`}]`

	err := retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
		var eventingURL string

		var cleanup func()

		var err error

		if eventingURL, cleanup, err = GetHostURL(targetKube, cluster, couchbasev2.EventingService); err != nil {
			t.Log(err)
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		hostURL := "http://" + eventingURL + "/api/v1/functions?name=" + eventingFuncName

		request, err := http.NewRequest(requestType, hostURL, strings.NewReader(eventingJSONFunc))
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		request.SetBasicAuth(hostUsername, hostPassword)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		client := http.Client{Timeout: time.Minute}

		response, err := client.Do(request)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer response.Body.Close()

		responseData, _ = ioutil.ReadAll(response.Body)

		if response.StatusCode != http.StatusOK {
			return false, retryutil.RetryOkError(fmt.Errorf("remote call failed with response: %s %s", response.Status, string(responseData)))
		}

		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return responseData, nil
}

func ExecuteAnalyticsQuery(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query string, timeout time.Duration) ([]byte, error) {
	username := string(k8s.DefaultSecret.Data["username"])
	password := string(k8s.DefaultSecret.Data["password"])

	requestBody := map[string]string{
		"statement":         query,
		"pretty":            "true",
		"client_context_id": "",
		"timeout":           "120s",
	}

	requestBodyRaw, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var data []byte

	callback := func() error {
		url, cleanup, err := GetHostURL(k8s, cluster, couchbasev2.AnalyticsService)
		if err != nil {
			return err
		}

		defer cleanup()

		hostURL := "http://" + url + "/analytics/service"

		request, err := http.NewRequest("POST", hostURL, bytes.NewReader(requestBodyRaw))
		if err != nil {
			return err
		}

		request.SetBasicAuth(username, password)
		request.Header.Set("Content-Type", "application/json")

		client := http.Client{Timeout: time.Minute}

		response, err := client.Do(request)
		if err != nil {
			return err
		}

		defer response.Body.Close()

		data, err = ioutil.ReadAll(response.Body)
		if err != nil {
			return err
		}

		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("bad status: %v (%s)", response.Status, string(data))
		}

		return nil
	}
	if err := retryutil.RetryOnErr(ctx, 10*time.Second, callback); err != nil {
		return nil, err
	}

	return data, nil
}

func MustExecuteAnalyticsQuery(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query string, timeout time.Duration) []byte {
	data, err := ExecuteAnalyticsQuery(k8s, cluster, query, timeout)
	if err != nil {
		Die(t, err)
	}

	return data
}

func GetDatasetItemCount(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, dataset string, timeout time.Duration) (int64, error) {
	data, err := ExecuteAnalyticsQuery(k8s, cluster, "SELECT COUNT(*) AS count FROM "+dataset, timeout)
	if err != nil {
		return 0, err
	}

	result := struct {
		Results []map[string]int64 `json:"results"`
	}{}

	if err := json.Unmarshal(data, &result); err != nil {
		return 0, err
	}

	return result.Results[0]["count"], nil
}

func MustGetDatasetItemCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, dataset string, timeout time.Duration) int64 {
	count, err := GetDatasetItemCount(k8s, cluster, dataset, timeout)
	if err != nil {
		Die(t, err)
	}

	return count
}

func GetItemCount(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var count int64

	callback := func() error {
		client, cleanup, err := CreateAdminConsoleClient(k8s, cluster)
		if err != nil {
			return err
		}

		defer cleanup()

		info := &couchbaseutil.BucketStatus{}

		request := &couchbaseutil.Request{
			Path:   "/pools/default/buckets/" + bucket,
			Result: info,
		}

		if err := client.client.Get(request, client.host); err != nil {
			return err
		}

		count = int64(info.BasicStats.ItemCount)

		return nil
	}

	if err := retryutil.RetryOnErr(ctx, 10*time.Second, callback); err != nil {
		return 0, err
	}

	return count, nil
}

func MustGetItemCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) int64 {
	count, err := GetItemCount(k8s, cluster, bucket, timeout)
	if err != nil {
		Die(t, err)
	}

	return count
}

func CreateBucket(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		client, cleanup, err := CreateAdminConsoleClient(k8s, cluster)
		if err != nil {
			return err
		}

		defer cleanup()

		b := &couchbaseutil.Bucket{
			BucketName:         bucket,
			BucketType:         "couchbase",
			BucketMemoryQuota:  100,
			IoPriority:         couchbaseutil.IoPriorityTypeHigh,
			EvictionPolicy:     "fullEviction",
			ConflictResolution: "seqno",
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbaseutil.CompressionModePassive,
		}

		return couchbaseutil.CreateBucket(b).On(client.client, client.host)
	}

	return retryutil.RetryOnErr(ctx, 10*time.Second, callback)
}

func MustCreateBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) {
	if err := CreateBucket(k8s, cluster, bucket, timeout); err != nil {
		Die(t, err)
	}
}

// PatchUserInfo tries patching the user returned directly from Couchbase server.
func PatchUserInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, userName string, userAuthDomain couchbaseutil.AuthDomain, patches jsonpatch.PatchSet, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		actual := &couchbaseutil.User{}
		if err := couchbaseutil.GetUser(userName, userAuthDomain, actual).On(client.client, client.host); err != nil {
			return false, err
		}

		expected := &couchbaseutil.User{}
		if err := couchbaseutil.GetUser(userName, userAuthDomain, expected).On(client.client, client.host); err != nil {
			return false, err
		}

		if err := jsonpatch.Apply(expected, patches.Patches()); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// loop until resources equal
		return reflect.DeepEqual(actual, expected), nil
	})
}

func MustPatchUserInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, userName string, userAuthDomain couchbaseutil.AuthDomain, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchUserInfo(t, k8s, couchbase, userName, userAuthDomain, patches, timeout); err != nil {
		Die(t, err)
	}
}

// CheckLDAPStatus checks for successful connectivity
// between couchbase and an LDAP server.
func CheckLDAPStatus(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 5*time.Second, func() (done bool, err error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		defer cleanup()

		status := &couchbaseutil.LDAPStatus{}
		if err := couchbaseutil.GetLDAPConnectivityStatus(status).On(client.client, client.host); err != nil {
			return false, err
		}

		if status.Result == couchbaseutil.LDAPStatusResultSuccess {
			return true, nil
		} else if status.Reason != "" {
			err = fmt.Errorf("failed to connect to LDAP server: %s", status.Reason)
			return false, retryutil.RetryOkError(err)
		}

		return false, nil
	})
}

// MustCheckLDAPStatus checks ldap status success or dies.
func MustCheckLDAPStatus(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := CheckLDAPStatus(k8s, cluster, timeout); err != nil {
		Die(t, err)
	}
}

// CheckN2N checks that all nodes are in the requested encryption state.
func CheckN2N(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, enabled bool, encryptionLevel string, timeout time.Duration) error {
	callback := func() error {
		client, cleanup, err := CreateAdminConsoleClient(k8s, cluster)
		if err != nil {
			return err
		}

		defer cleanup()

		clusterInfo := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(clusterInfo).On(client.client, client.host); err != nil {
			return err
		}

		securityInfo := &couchbaseutil.SecuritySettings{}
		if err := couchbaseutil.GetSecuritySettings(securityInfo).On(client.client, client.host); err != nil {
			return err
		}

		for _, node := range clusterInfo.Nodes {
			if node.NodeEncryption != enabled || string(securityInfo.ClusterEncryptionLevel) != encryptionLevel {
				return fmt.Errorf("node %s encryption %v, expected %v", node.HostName, node.NodeEncryption, enabled)
			}
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, callback)
}

func MustCheckN2NEnabled(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, encryptionLevel string, timeout time.Duration) {
	if err := CheckN2N(k8s, cluster, true, encryptionLevel, timeout); err != nil {
		Die(t, err)
	}
}

func MustCheckN2NDisabled(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, encryptionLevel string, timeout time.Duration) {
	if err := CheckN2N(k8s, cluster, false, encryptionLevel, timeout); err != nil {
		Die(t, err)
	}
}

func ClusterListOpt(cluster *couchbasev2.CouchbaseCluster) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(k8sutil.LabelsForCluster(cluster)).String(),
	}
}

func NodeListOpt(cluster *couchbasev2.CouchbaseCluster, memberName string) metav1.ListOptions {
	l := k8sutil.LabelsForCluster(cluster)
	l[constants.LabelNode] = memberName

	return metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(l).String(),
	}
}

// MustCheckStatusVersion checks that the status version is as we expect.
func MustCheckStatusVersion(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, version string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		c, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if c.Status.CurrentVersion != version {
			return fmt.Errorf("expected %s, got %s", version, c.Status.CurrentVersion)
		}

		return nil
	}

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}
}

// MustCheckStatusVersionFor checks that the status version is as we expect
// for a set amount of time, in case it is reverted by something.
func MustCheckStatusVersionFor(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, version string, period time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), period)
	defer cancel()

	for {
		c, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
		if err != nil {
			Die(t, err)
		}

		if c.Status.CurrentVersion != version {
			Die(t, fmt.Errorf("expected %s, got %s", version, c.Status.CurrentVersion))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// MustGetCouchbaseVersion extracts the semantic version from the image tag.
func MustGetCouchbaseVersion(t *testing.T, image string) string {
	version, err := k8sutil.CouchbaseVersion(image)
	if err != nil {
		Die(t, err)
	}

	return version
}
