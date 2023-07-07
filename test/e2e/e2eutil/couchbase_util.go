package e2eutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	gocb "github.com/couchbase/gocb/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

// CreateAdminConsoleClient returns a client for interacting with the admin service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func CreateAdminConsoleClient(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (*CouchbaseClient, error) {
	return newClient(k8s.KubeClient, cluster, fmt.Sprintf("http://%s.%s.svc:8091", cluster.Name, cluster.Namespace))
}

func MustCreateAdminConsoleClient(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) *CouchbaseClient {
	client, err := CreateAdminConsoleClient(k8s, cluster)
	if err != nil {
		Die(t, err)
	}

	return client
}

// GetHostURL returns a URL for interacting with a specified service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func GetHostURL(cluster *couchbasev2.CouchbaseCluster, service couchbasev2.Service) (string, error) {
	portMap := map[couchbasev2.Service]string{
		couchbasev2.AdminService:     "8091",
		couchbasev2.QueryService:     "8093",
		couchbasev2.SearchService:    "8094",
		couchbasev2.AnalyticsService: "8095",
		couchbasev2.EventingService:  "8096",
		couchbasev2.DataService:      "11210",
	}

	port, ok := portMap[service]
	if !ok {
		return "", fmt.Errorf("unsupported service specified")
	}

	return fmt.Sprintf("%s.%s.svc:%s", cluster.Name, cluster.Namespace, port), nil
}

// GetAdminConsoleHostURL returns a URL for interacting with the Admin service of a cluster.
// Localhost ports are randomly allocated to allow for multiple clients to exist at any given time.
// If during the lifetime of the cluster a pod is deleted the client will need to be reinitialized,
// the cleanup callback must be invoked first.
func GetAdminConsoleHostURL(cluster *couchbasev2.CouchbaseCluster) (string, error) {
	return GetHostURL(cluster, couchbasev2.AdminService)
}

// newRequest is what you must use when doing stuff directly to the API.
func newRequest(path string, body []byte, result interface{}) *couchbaseutil.Request {
	return &couchbaseutil.Request{
		Path:         path,
		Body:         body,
		Result:       result,
		Authenticate: true,
	}
}

// getCouchbaseClientSDK is the canonical place to grab an SDK client to do client ops.
// The returned cleanup callback must be called somewhere, as this is a hog and will
// cause massive memory leaks.
func getCouchbaseClientSDK(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (*gocb.Cluster, func(), error) {
	opts := gocb.ClusterOptions{
		Username: string(k8s.DefaultSecret.Data["username"]),
		Password: string(k8s.DefaultSecret.Data["password"]),
	}

	host, err := gocb.Connect(fmt.Sprintf("couchbase://%s.%s", cluster.Name, cluster.Namespace), opts)
	if err != nil {
		return host, nil, err
	}

	cleanup := func() {
		host.Close(nil)
	}

	return host, cleanup, nil
}

func MustGetCouchbaseClientSDK(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (*gocb.Cluster, func()) {
	host, cleanup, err := getCouchbaseClientSDK(k8s, cluster)
	if err != nil {
		Die(t, err)
	}

	return host, cleanup
}

// PatchBucketInfo tries patching the bucket information returned directly from Couchbase server.
func PatchBucketInfo(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucketName string, patches jsonpatch.PatchSet, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		before, err := getBucket(client, bucketName)
		if err != nil {
			return err
		}

		after := *before
		if err := jsonpatch.Apply(&after, patches.Patches()); err != nil {
			return err
		}

		if reflect.DeepEqual(before, after) {
			return nil
		}

		return couchbaseutil.UpdateBucket(&after).On(client.client, client.host)
	})
}

func MustPatchBucketInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucketName string, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchBucketInfo(k8s, couchbase, bucketName, patches, timeout); err != nil {
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

// DocumentSet defines the parameters for adding documents to a Couchbase cluster.
type DocumentSet struct {
	test       *testing.T
	count      int
	kubernetes *types.Cluster
	cluster    *couchbasev2.CouchbaseCluster
	bucket     string
	scope      string
	collection string
	prefix     string
	values     map[string]interface{}
}

// NewDocumentSet creates a basic DocumentSet, specifying the bucket to insert to and the number of
// documents to insert.
func NewDocumentSet(bucket string, count int) *DocumentSet {
	return &DocumentSet{
		count:  count,
		bucket: bucket,
		prefix: RandomSuffix(),
		values: map[string]interface{}{},
	}
}

// IntoCollection defines the scope and collection that documents should be inserted into;
// if not set, DocumentSet will use the scope's default scope and collection.
func (d *DocumentSet) IntoScopeAndCollection(scope string, collection string) *DocumentSet {
	d.scope = scope
	d.collection = collection

	return d
}

// WithPrefix lets a specific key prefix be given to all the docs added (which will be appended
// with a number to create the key. If not specified, the prefix will be 5 random characters.
func (d *DocumentSet) WithPrefix(prefix string) *DocumentSet {
	d.prefix = prefix

	return d
}

func (d *DocumentSet) WithValue(key, value string) *DocumentSet {
	d.values[key] = value

	return d
}

// MustCreate creates the documents with the specified options, dying on error.
func (d *DocumentSet) MustCreate(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) {
	d.test = t
	d.kubernetes = kubernetes
	d.cluster = cluster

	if err := addDocs(d); err != nil {
		Die(d.test, err)
	}
}

// Validates the contents of the bucket from the doc set
// against the expected contents.
// This assumes that the data has been add via the addDocs function
// i.e it's the same content added d.count times.
func VerifyContents(timeout time.Duration, d *DocumentSet, expected map[string]interface{}) error {
	return retryutil.RetryFor(timeout, func() error {
		docs, err := getDocs(d)
		if err != nil {
			return err
		}

		for i := 0; i < len(docs); i++ {
			for k := range docs[i] {
				if docs[i][k] != expected[k] {
					return fmt.Errorf("found: %s, expected: %s", docs[i][k], expected[k])
				}
			}
		}
		return nil
	})
}

func getDocs(d *DocumentSet) ([]map[string]interface{}, error) {
	c, sdkCleanup, err := getCouchbaseClientSDK(d.kubernetes, d.cluster)
	if err != nil {
		return nil, err
	}

	defer sdkCleanup()

	bucket := c.Bucket(d.bucket)

	scope := bucket.DefaultScope()
	if d.scope != "" {
		scope = bucket.Scope(d.scope)
	}

	collection := bucket.DefaultCollection()
	if d.collection != "" {
		collection = scope.Collection(d.collection)
	}

	var items []map[string]interface{}

	for i := 0; i < d.count; i++ {
		id := i

		itemResult, err := collection.Get(fmt.Sprintf("%s%d", d.prefix, id), &gocb.GetOptions{
			Timeout: 2 * time.Minute,
		})
		if err != nil {
			return nil, err
		}

		var item map[string]interface{}

		err = itemResult.Content(&item)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	return items, nil
}

// addDocs uses the Couchbase Go SDK to add documents, with a DocumentSet used to specify options.
func addDocs(d *DocumentSet) error {
	c, sdkCleanup, err := getCouchbaseClientSDK(d.kubernetes, d.cluster)
	if err != nil {
		return err
	}

	defer sdkCleanup()

	bucket := c.Bucket(d.bucket)

	if err := bucket.WaitUntilReady(time.Minute*2, &gocb.WaitUntilReadyOptions{}); err != nil {
		return err
	}

	scope := bucket.DefaultScope()
	if d.scope != "" {
		scope = bucket.Scope(d.scope)
	}

	collection := bucket.DefaultCollection()
	if d.collection != "" {
		collection = scope.Collection(d.collection)
	}

	document := map[string]interface{}{
		"key1": "dummyVal1",
		"key2": "dummyVal2",
		"key3": "dummyVal3",
		"key4": "dummyVal4",
	}
	if len(d.values) > 0 {
		document = d.values
	}

	for i := 0; i < d.count; i++ {
		id := i

		if _, err := collection.Upsert(fmt.Sprintf("%s%d", d.prefix, id), document, &gocb.UpsertOptions{
			Timeout: 2 * time.Minute,
		}); err != nil {
			return err
		}
	}

	return nil
}

func CompactBucket(cluster *couchbasev2.CouchbaseCluster, bucket metav1.Object) error {
	urlBase, err := GetHostURL(cluster, couchbasev2.AdminService)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s/pools/default/buckets/%s/controller/compactBucket", urlBase, bucket.GetName())

	request, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	request.SetBasicAuth("Administrator", "password")

	client := http.Client{Timeout: time.Minute}

	response, err := client.Do(request)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %v", response.Status)
	}

	return nil
}

func MustCompactBucket(t *testing.T, cluster *couchbasev2.CouchbaseCluster, bucket metav1.Object) {
	if err := CompactBucket(cluster, bucket); err != nil {
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

	callback := func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		return couchbaseutil.AddNode(member.GetDNSName(), username, password, svcs).On(client.client, client.host)
	}
	if err := retryutil.Retry(ctx, 5*time.Second, callback); err != nil {
		return err
	}

	callback = func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return err
		}

		known := make(couchbaseutil.OTPNodeList, len(info.Nodes))

		for i, node := range info.Nodes {
			known[i] = node.OTPNode
		}

		return couchbaseutil.Rebalance(known, nil).On(client.client, client.host)
	}
	if err := retryutil.Retry(ctx, 5*time.Second, callback); err != nil {
		return err
	}

	callback = func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return err
		}

		if info.RebalanceStatus != "none" {
			return fmt.Errorf("rebalance is still running")
		}

		return nil
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
	client := MustCreateAdminConsoleClient(t, k8s, couchbase)

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
	if _, err := patchCluster(k8s, couchbase, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute); err != nil {
		return err
	}

	// Given we could be balancing out the member we are talking to using a progress channel
	// is not the best option here as it may error as the operator does things in the background
	// affecting this.  The best option is to just check for the rebalance status to complete.
	callback := func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return err
		}

		if info.RebalanceStatus != "none" {
			return fmt.Errorf("rebalance is still running")
		}

		return nil
	}
	if err := retryutil.RetryFor(timeout, callback); err != nil {
		return err
	}

	// Restore the operator back to the previous condition.
	_, err := patchCluster(k8s, couchbase, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)

	return err
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
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		data := url.Values{}

		for _, index := range indexes {
			name := couchbaseutil.CreateMemberName(couchbase.Name, index)

			member := couchbaseutil.NewPartialMember(couchbase.Namespace, couchbase.Name, name)
			data.Add("otpNode", string(member.GetOTPNode()))
		}

		request := newRequest("/controller/failOver", []byte(data.Encode()), nil)

		return client.client.Post(request, client.host)
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
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

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
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

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
func PatchCouchbaseInfo(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return err
		}

		return jsonpatch.Apply(info, patches.Patches())
	})
}

func MustPatchCouchbaseInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchCouchbaseInfo(k8s, couchbase, patches, timeout); err != nil {
		Die(t, err)
	}
}

func PatchAutoFailoverInfo(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := &couchbaseutil.AutoFailoverSettings{}
		if err := couchbaseutil.GetAutoFailoverSettings(info).On(client.client, client.host); err != nil {
			return err
		}

		return jsonpatch.Apply(info, patches.Patches())
	})
}

func MustPatchAutoFailoverInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchAutoFailoverInfo(k8s, couchbase, patches, timeout); err != nil {
		Die(t, err)
	}
}

func PatchIndexSettingInfo(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := &couchbaseutil.IndexSettings{}
		if err := couchbaseutil.GetIndexSettings(info).On(client.client, client.host); err != nil {
			return err
		}

		return jsonpatch.Apply(info, patches.Patches())
	})
}

func MustPatchIndexSettingInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchIndexSettingInfo(k8s, couchbase, patches, timeout); err != nil {
		Die(t, err)
	}
}

func PatchAutoCompactionSettings(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := &couchbaseutil.AutoCompactionSettings{}
		if err := couchbaseutil.GetAutoCompactionSettings(info).On(client.client, client.host); err != nil {
			return err
		}

		return jsonpatch.Apply(info, patches.Patches())
	})
}

func MustPatchAutoCompactionSettings(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchAutoCompactionSettings(k8s, couchbase, patches, timeout); err != nil {
		Die(t, err)
	}
}

func VerifyServices(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, value map[string]int, verifiers ...serviceVerifier) error {
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
			return err
		}

		for _, verify := range verifiers {
			if !verify(t, info, value) {
				return NewErrVerifyServices()
			}
		}

		return nil
	})
}

func MustVerifyServices(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, value map[string]int, verifiers ...serviceVerifier) {
	if err := VerifyServices(t, k8s, couchbase, timeout, value, verifiers...); err != nil {
		Die(t, err)
	}
}

func NodeServicesVerifier(_ *testing.T, ci *couchbaseutil.ClusterInfo, servicesMap map[string]int) bool {
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

func getEventingData(t *testing.T, cluster *couchbasev2.CouchbaseCluster, requestType, eventingJSONFunc string, timeout time.Duration) ([]byte, error) {
	hostUsername := "Administrator"
	hostPassword := "password"

	var responseData []byte

	err := retryutil.RetryFor(timeout, func() error {
		var eventingURL string

		var err error

		if eventingURL, err = GetHostURL(cluster, couchbasev2.EventingService); err != nil {
			t.Log(err)
			return err
		}

		hostURL := "http://" + eventingURL + "/api/v1/functions/test"

		request, err := http.NewRequest(requestType, hostURL, strings.NewReader(eventingJSONFunc))
		if err != nil {
			return err
		}

		request.SetBasicAuth(hostUsername, hostPassword)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		client := http.Client{Timeout: time.Minute}

		response, err := client.Do(request)
		if err != nil {
			return err
		}

		defer response.Body.Close()

		responseData, _ = io.ReadAll(response.Body)

		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("remote call failed with response: %s %s", response.Status, string(responseData))
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return responseData, nil
}

func DeployEventingFunction(t *testing.T, cluster *couchbasev2.CouchbaseCluster, eventingFuncName, srcBucketName, metaBucketName, dstBucketName, jsFunc string, timeout time.Duration) ([]byte, error) {
	eventingJSONFunc := `[{` +
		`"appname": "` + eventingFuncName + `",` +
		`"id": 0,` +
		`"depcfg":{"buckets":[{"alias":"dst_bucket","bucket_name":"` + dstBucketName + `"}],"metadata_bucket":"` + metaBucketName + `","source_bucket":"` + srcBucketName + `"},` +
		`"version":"", "handleruuid":0,` +
		`"settings": {"dcp_stream_boundary":"everything","deadline_timeout":62,"deployment_status":true,"description":"","execution_timeout":60,"log_level":"INFO","processing_status":true,"user_prefix":"eventing","worker_count":3},` +
		`"using_doc_timer": false,` +
		`"appcode": "` + jsFunc + `"` +
		`}]`

	responseData, err := getEventingData(t, cluster, "POST", eventingJSONFunc, timeout)

	return responseData, err
}

func MustDeployEventingFunction(t *testing.T, cluster *couchbasev2.CouchbaseCluster, eventingFuncName, srcBucketName, metaBucketName, dstBucketName, jsFunc string, timeout time.Duration) {
	if responseData, err := DeployEventingFunction(t, cluster, eventingFuncName, srcBucketName, metaBucketName, dstBucketName, jsFunc, timeout); err != nil {
		t.Log(string(responseData))
		Die(t, err)
	}
}

func DeleteEventingFunction(t *testing.T, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) ([]byte, error) {
	responseData, err := getEventingData(t, cluster, "DELETE", "", timeout)

	return responseData, err
}

func MustDeleteEventingFunction(t *testing.T, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if responseData, err := DeleteEventingFunction(t, cluster, timeout); err != nil {
		t.Log(string(responseData))
		Die(t, err)
	}
}

func GetEventingFunction(t *testing.T, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) ([]byte, error) {
	responseData, err := getEventingData(t, cluster, "GET", "", timeout)

	return responseData, err
}

func MustGetEventingFunction(t *testing.T, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	if responseData, err := GetEventingFunction(t, cluster, timeout); err != nil {
		t.Log(string(responseData))
		return err
	}

	return nil
}

func ExecuteQuery(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query, service string, timeout time.Duration) ([]byte, error) {
	username := string(k8s.DefaultSecret.Data["username"])
	password := string(k8s.DefaultSecret.Data["password"])

	couchbaseservice := couchbasev2.AnalyticsService

	var pretty interface{} = "true"

	if strings.Contains(service, "query") {
		couchbaseservice = couchbasev2.QueryService
		pretty = true
	}

	requestBody := map[string]interface{}{
		"statement":         query,
		"pretty":            pretty,
		"client_context_id": "",
		"timeout":           "120s",
	}

	requestBodyRaw, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	var data []byte

	callback := func() error {
		url, err := GetHostURL(cluster, couchbaseservice)
		if err != nil {
			return err
		}

		hostURL := "http://" + url + service

		request, err := http.NewRequest(http.MethodPost, hostURL, bytes.NewReader(requestBodyRaw))
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

		data, err = io.ReadAll(response.Body)
		if err != nil {
			return err
		}

		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("bad status: %v (%s)", response.Status, string(data))
		}

		return nil
	}
	if err := retryutil.RetryFor(timeout, callback); err != nil {
		return nil, err
	}

	return data, nil
}

func ExecuteAnalyticsQuery(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query string, timeout time.Duration) ([]byte, error) {
	return ExecuteQuery(k8s, cluster, query, "/analytics/service", timeout)
}

func MustExecuteAnalyticsQuery(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query string, timeout time.Duration) []byte {
	data, err := ExecuteAnalyticsQuery(k8s, cluster, query, timeout)
	if err != nil {
		Die(t, err)
	}

	return data
}

func getAnalyticsData(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query string, timeout time.Duration) (int64, error) {
	data, err := ExecuteAnalyticsQuery(k8s, cluster, query, timeout)
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

func GetDatasetItemCount(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, dataset string, timeout time.Duration) (int64, error) {
	query := "SELECT COUNT(*) AS count FROM " + dataset

	return getAnalyticsData(k8s, cluster, query, timeout)
}

func MustGetDatasetItemCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, dataset string, timeout time.Duration) int64 {
	count, err := GetDatasetItemCount(k8s, cluster, dataset, timeout)
	if err != nil {
		Die(t, err)
	}

	return count
}

func MustVerifyDatasetItemCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, dataset string, expectedItems int64, timeout time.Duration) {
	count := MustGetDatasetItemCount(t, k8s, cluster, dataset, timeout)

	if count != expectedItems {
		Die(t, fmt.Errorf("dataset item mismatch %v/%v", count, expectedItems))
	}
}

func GetDatasetCount(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) (int64, error) {
	query := "Select Count(*) AS count from Metadata.`Dataset` where DataverseName <> `Metadata`"
	return getAnalyticsData(k8s, cluster, query, timeout)
}

func MustGetDatasetCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) int64 {
	indexCount, err := GetDatasetCount(k8s, cluster, timeout)
	if err != nil {
		Die(t, err)
	}

	return indexCount
}

func ExecuteIndexQuery(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query string, timeout time.Duration) ([]byte, error) {
	return ExecuteQuery(k8s, cluster, query, "/query/service", timeout)
}

func MustExecuteIndexQuery(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query string, timeout time.Duration) []byte {
	data, err := ExecuteIndexQuery(k8s, cluster, query, timeout)
	if err != nil {
		Die(t, err)
	}

	return data
}

func getIndexData(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, query string, timeout time.Duration) (int64, error) {
	data, err := ExecuteIndexQuery(k8s, cluster, query, timeout)
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

func GetIndexCount(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) (int64, error) {
	query := "Select Count(*) AS count FROM system:indexes WHERE name = '#primary'"

	return getIndexData(k8s, cluster, query, timeout)
}

func MustGetIndexCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) int64 {
	indexCount, err := GetIndexCount(k8s, cluster, timeout)
	if err != nil {
		Die(t, err)
	}

	return indexCount
}

func GetItemCount(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) (int64, error) {
	var count int64

	callback := func() error {
		client, err := CreateAdminConsoleClient(k8s, cluster)
		if err != nil {
			return err
		}

		info := &couchbaseutil.BucketStatus{}

		request := newRequest("/pools/default/buckets/"+bucket, nil, info)

		if err := client.client.Get(request, client.host); err != nil {
			return err
		}

		count = int64(info.BasicStats.ItemCount)

		return nil
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
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
	callback := func() error {
		client, err := CreateAdminConsoleClient(k8s, cluster)
		if err != nil {
			return err
		}

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

	return retryutil.RetryFor(timeout, callback)
}

func MustCreateBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) {
	if err := CreateBucket(k8s, cluster, bucket, timeout); err != nil {
		Die(t, err)
	}
}

// PatchUserInfo tries patching the user returned directly from Couchbase server.
func PatchUserInfo(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, userName string, userAuthDomain couchbaseutil.AuthDomain, patches jsonpatch.PatchSet, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		actual := &couchbaseutil.User{}
		if err := couchbaseutil.GetUser(userName, userAuthDomain, actual).On(client.client, client.host); err != nil {
			return err
		}

		expected := &couchbaseutil.User{}
		if err := couchbaseutil.GetUser(userName, userAuthDomain, expected).On(client.client, client.host); err != nil {
			return err
		}

		if err := jsonpatch.Apply(expected, patches.Patches()); err != nil {
			return err
		}

		// loop until resources equal
		if !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("users do not match, expected %v, actial %v", expected, actual)
		}

		return nil
	})
}

func MustPatchUserInfo(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, userName string, userAuthDomain couchbaseutil.AuthDomain, patches jsonpatch.PatchSet, timeout time.Duration) {
	if err := PatchUserInfo(k8s, couchbase, userName, userAuthDomain, patches, timeout); err != nil {
		Die(t, err)
	}
}

// CheckLDAPStatus checks for successful connectivity
// between couchbase and an LDAP server.
func CheckLDAPStatus(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		client, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		status := &couchbaseutil.LDAPStatus{}
		if err := couchbaseutil.GetLDAPConnectivityStatus(status).On(client.client, client.host); err != nil {
			return err
		}

		if status.Result == couchbaseutil.LDAPStatusResultSuccess {
			return nil
		}

		if status.Reason != "" {
			return fmt.Errorf("failed to connect to LDAP server: %s", status.Reason)
		}

		return nil
	})
}

// MustCheckLDAPStatus checks ldap status success or dies.
func MustCheckLDAPStatus(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := CheckLDAPStatus(k8s, cluster, timeout); err != nil {
		Die(t, err)
	}
}

// Get LDAP settings from couchbase server.
func GetLDAPSettings(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) (*couchbaseutil.LDAPSettings, error) {
	client, err := CreateAdminConsoleClient(k8s, couchbase)
	if err != nil {
		return nil, err
	}

	settings := &couchbaseutil.LDAPSettings{}
	if err := couchbaseutil.GetLDAPSettings(settings).On(client.client, client.host); err != nil {
		return nil, err
	}

	return settings, nil
}

// Requires fetching of couchbase server.
func MustGetLDAPSettings(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	if _, err := GetLDAPSettings(k8s, couchbase); err != nil {
		Die(t, err)
	}
}

// VerifyLDAPConfigured checks that most basic LDAP configuration has been applied to Couchbase Server.
// Does not check connectivity... see CheckLDAPStatus.
func VerifyLDAPConfigured(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) error {
	settings, err := GetLDAPSettings(k8s, couchbase)
	if err != nil {
		return err
	}

	// Host must be specified
	if len(settings.Hosts) == 0 {
		return fmt.Errorf("ldap settings does not specify any hosts")
	}

	// Bind DN is used by most sensible people... or tests at least
	if settings.BindDN == "" {
		return fmt.Errorf("ldap settings missing bind distinguished name")
	}

	return nil
}

// MustVerifyLDAPConfigured requires LDAP configuration to exist.
func MustVerifyLDAPConfigured(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	if err := VerifyLDAPConfigured(k8s, couchbase); err != nil {
		Die(t, err)
	}
}

// CheckLDAPSettingsRemoved waits for LDAP settings to be cleared from Couchbase cluster.
func CheckLDAPSettingsRemoved(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		// check is successful when required hostname is removed
		settings, err := GetLDAPSettings(k8s, couchbase)
		if err != nil {
			return err
		}

		if len(settings.Hosts) != 0 {
			return fmt.Errorf("ldap server is still configured")
		}

		return nil
	})
}

// MustCheckLDAPSettingsPersisted requires that LDAP settings remain for the specified duration.
func MustCheckLDAPSettingsPersisted(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	err := CheckLDAPSettingsRemoved(k8s, couchbase, timeout)
	if err == nil {
		Die(t, fmt.Errorf("ldap settings were removed"))
	}
}

// CheckN2N checks that all nodes are in the requested encryption state.
func CheckN2N(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, enabled bool, encryptionLevel couchbasev2.NodeToNodeEncryptionType, timeout time.Duration) error {
	callback := func() error {
		client, err := CreateAdminConsoleClient(k8s, cluster)
		if err != nil {
			return err
		}

		clusterInfo := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(clusterInfo).On(client.client, client.host); err != nil {
			return err
		}

		securityInfo := &couchbaseutil.SecuritySettings{}
		if err := couchbaseutil.GetSecuritySettings(securityInfo).On(client.client, client.host); err != nil {
			return err
		}

		for _, node := range clusterInfo.Nodes {
			if !enabled && node.NodeEncryption {
				return fmt.Errorf("node to node encryption unexpectedly enabled")
			}

			if enabled {
				if !node.NodeEncryption {
					return fmt.Errorf("node to node encryption unexpectedly disabled")
				}

				apiLevel := couchbaseutil.ClusterEncryptionAll

				if encryptionLevel == couchbasev2.NodeToNodeControlPlaneOnly {
					apiLevel = couchbaseutil.ClusterEncryptionControl
				}

				if apiLevel != securityInfo.ClusterEncryptionLevel {
					return fmt.Errorf("node to node encryption unexpectedly in the wrong mode, expected %v, got %v", apiLevel, securityInfo.ClusterEncryptionLevel)
				}
			}
		}

		return nil
	}

	return retryutil.RetryFor(timeout, callback)
}

func MustCheckN2NEnabled(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, encryptionLevel couchbasev2.NodeToNodeEncryptionType, timeout time.Duration) {
	if err := CheckN2N(k8s, cluster, true, encryptionLevel, timeout); err != nil {
		Die(t, err)
	}
}

func MustCheckN2NDisabled(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, encryptionLevel couchbasev2.NodeToNodeEncryptionType, timeout time.Duration) {
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
	callback := func() error {
		c, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(context.Background(), cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if c.Status.CurrentVersion != version {
			return fmt.Errorf("expected %s, got %s", version, c.Status.CurrentVersion)
		}

		return nil
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}

// MustCheckStatusVersionFor checks that the status version is as we expect
// for a set amount of time, in case it is reverted by something.
func MustCheckStatusVersionFor(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, version string, period time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), period)
	defer cancel()

	for {
		c, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(context.Background(), cluster.Name, metav1.GetOptions{})
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

// MustVerifyDataServerSettingsMemcachedThreadCounts checks memcached's reader, writer, auxIo, nonIo thread counts.
// Due to some (yet more) whackiness of Couchbase's API design, 0 means unset.
func MustVerifyDataServerSettingsMemcachedThreadCounts(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, readerThreads, writerThreads, nonIOThreads, auxIOThreads *int, timeout time.Duration) {
	callback := func() error {
		client, err := CreateAdminConsoleClient(k8s, cluster)
		if err != nil {
			return err
		}

		current := couchbaseutil.MemcachedGlobals{}
		if err := couchbaseutil.GetMemcachedGlobalSettings(&current).On(client.client, client.host); err != nil {
			return err
		}

		if !reflect.DeepEqual(current.NumReaderThreads, readerThreads) {
			return fmt.Errorf("expected %s readers, got %s", IntPointerToString(readerThreads), IntPointerToString(current.NumReaderThreads))
		}

		if !reflect.DeepEqual(current.NumWriterThreads, writerThreads) {
			return fmt.Errorf("expected %s writers, got %s", IntPointerToString(writerThreads), IntPointerToString(current.NumWriterThreads))
		}

		if !reflect.DeepEqual(current.NumNonIOThreads, nonIOThreads) {
			return fmt.Errorf("expected %s non IO threads, got %s", IntPointerToString(nonIOThreads), IntPointerToString(current.NumNonIOThreads))
		}

		if !reflect.DeepEqual(current.NumAuxIOThreads, auxIOThreads) {
			return fmt.Errorf("expected %s aux IO threads, got %s", IntPointerToString(auxIOThreads), IntPointerToString(current.NumAuxIOThreads))
		}

		return nil
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}

func IntPointerToString(a *int) string {
	if a == nil {
		return ""
	}

	return fmt.Sprintf("%d", *a)
}

// GetCouchbaseMetric gets the specified metric from Couchbase Server, using the specified labels, and returns them in a MetricsResponse.
func GetCouchbaseMetric(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, metric string, labels map[string]string, _ time.Duration) (*couchbaseutil.MetricsResponse, error) {
	client := MustCreateAdminConsoleClient(t, k8s, cluster)

	result := &couchbaseutil.MetricsResponse{}

	path := fmt.Sprintf("/pools/default/stats/range/%s?", metric)

	for k, v := range labels {
		path = fmt.Sprintf("%s%s=%s&", path, k, v)
	}

	request := newRequest(path, nil, result)

	if err := client.client.Get(request, client.host); err != nil {
		return result, err
	}

	return result, nil
}

// MustCreateSecondaryIndex creates Secondary Index against default collection.
func MustCreateSecondaryIndex(t *testing.T, queryManager *gocb.QueryIndexManager, bucketName string) {
	if err := queryManager.CreateIndex(bucketName, "cnd_gsi", []string{"key1"}, &gocb.CreateQueryIndexOptions{Timeout: 3 * time.Minute}); err != nil {
		Die(t, err)
	}
}

// MustCreateSecondaryIndex creates Secondary Index against default collection.
func MustDropSecondaryIndex(t *testing.T, queryManager *gocb.QueryIndexManager, bucketName string) {
	if err := queryManager.DropIndex(bucketName, "cnd_gsi", &gocb.DropQueryIndexOptions{Timeout: 3 * time.Minute}); err != nil {
		Die(t, err)
	}
}

// MustExecuteN1qlQuery runs the specified query against default collection.
func MustExecuteN1qlQuery(t *testing.T, host *gocb.Cluster, query string) *gocb.QueryResult {
	queryResult, err := host.Query(query, &gocb.QueryOptions{Timeout: 3 * time.Minute})
	if err != nil {
		Die(t, err)
	}

	return queryResult
}

// MustGetAllIndexes gets all the indexes built against the specified bucket.
func MustGetAllIndexes(t *testing.T, queryManager *gocb.QueryIndexManager, bucketName string) []gocb.QueryIndex {
	indexes, err := queryManager.GetAllIndexes(bucketName, &gocb.GetAllQueryIndexesOptions{Timeout: 5 * time.Minute})
	if err != nil {
		Die(t, err)
	}

	return indexes
}

func NewFTSIndex(bucketName string) gocb.SearchIndex {
	// define basic params of the Index.
	searchIndex := gocb.SearchIndex{Name: "cnd-test", Type: "fulltext-index", SourceName: bucketName, SourceType: "couchbase"}

	searchIndex.Params = map[string]interface{}{
		"mapping": map[string]interface{}{
			"default_analyzer": "keyword",
		},
	}

	searchIndex.PlanParams = map[string]interface{}{
		"maxPartitionsPerPIndex": 1024,
		"indexPartitions":        1,
	}

	return searchIndex
}

func NewFTSIndexWithCollections(bucketName, scopeName, collectionName string) gocb.SearchIndex {
	ftsType := fmt.Sprintf("%s.%s", scopeName, collectionName)

	// define basic params of the Index.
	searchIndex := gocb.SearchIndex{Name: "cnd-test", Type: "fulltext-index", SourceName: bucketName, SourceType: "couchbase"}

	searchIndex.Params = map[string]interface{}{
		"doc_config": map[string]interface{}{
			"mode": "scope.collection.type_field",
		},
		"mapping": map[string]interface{}{
			"default_analyzer": "keyword",
			// Disable Indexing of default scopes and collections.
			"default_mapping": map[string]interface{}{
				"enabled": false,
			},
			"types": map[string]interface{}{
				// Enble Indexing against custom scopes and collections.
				ftsType: map[string]interface{}{
					"enabled":          true,
					"default_analyzer": "keyword",
				},
			},
		},
	}

	searchIndex.PlanParams = map[string]interface{}{
		"maxPartitionsPerPIndex": 1024,
		"indexPartitions":        1,
	}

	return searchIndex
}

// executeFTSOPs creates FTS Index and execute a n1ql against that index.
func executeFTSOps(searchIndex gocb.SearchIndex, searchManager *gocb.SearchIndexManager, host *gocb.Cluster, query string) error {
	// Create FTS Index.
	if err := searchManager.UpsertIndex(searchIndex, &gocb.UpsertSearchIndexOptions{Timeout: 10 * time.Minute}); err != nil {
		return err
	}

	// wait for FTS to process all docs.
	time.Sleep(2 * time.Minute)

	// Allow Query to be executed against FTS Index.
	if err := searchManager.AllowQuerying("cnd-test", &gocb.AllowQueryingSearchIndexOptions{Timeout: 5 * time.Minute}); err != nil {
		return err
	}

	// Run N1QL Query against FTS Index.
	_, err := host.Query(query, &gocb.QueryOptions{Timeout: 3 * time.Minute})

	return err
}

func MustExecuteFTSOps(t *testing.T, searchIndex gocb.SearchIndex, searchManager *gocb.SearchIndexManager, host *gocb.Cluster, query string) {
	if err := executeFTSOps(searchIndex, searchManager, host, query); err != nil {
		Die(t, err)
	}
}

func NewViewsDesignDoc() gocb.DesignDocument {
	// designDoc is Views Design Doc which return doc ID.
	designDoc := gocb.DesignDocument{
		Name: "landmarks",
		Views: map[string]gocb.View{
			"key1": {
				Map: "function (doc, meta) {emit(doc.id, null);}",
			},
		},
	}

	return designDoc
}

// upsertViewsDesignDocs upserts defined Designed doc.
func upsertViewsDesignDocs(designDoc gocb.DesignDocument, viewManager *gocb.ViewIndexManager) error {
	if err := viewManager.UpsertDesignDocument(designDoc, gocb.DesignDocumentNamespaceDevelopment, &gocb.UpsertDesignDocumentOptions{Timeout: 3 * time.Minute}); err != nil {
		return err
	}

	dDoc, err := viewManager.GetDesignDocument(designDoc.Name, gocb.DesignDocumentNamespaceDevelopment, &gocb.GetDesignDocumentOptions{Timeout: 3 * time.Minute})
	if err != nil {
		return err
	}

	if dDoc.Name != designDoc.Name {
		return fmt.Errorf("upserted dDoc and Received dDoc don't match")
	}

	return nil
}

func MustUpsertViewsDesignDocs(t *testing.T, designDoc gocb.DesignDocument, viewManager *gocb.ViewIndexManager) {
	if err := upsertViewsDesignDocs(designDoc, viewManager); err != nil {
		Die(t, err)
	}
}

// dropViewsDesignDocs drops defined Designed doc.
func dropViewsDesignDocs(designDoc gocb.DesignDocument, viewManager *gocb.ViewIndexManager) error {
	if err := viewManager.DropDesignDocument(designDoc.Name, gocb.DesignDocumentNamespaceDevelopment, &gocb.DropDesignDocumentOptions{Timeout: 3 * time.Minute}); err != nil {
		return err
	}

	// wait for ddocs to disappear
	time.Sleep(30 * time.Second)

	if _, err := viewManager.GetDesignDocument(designDoc.Name, gocb.DesignDocumentNamespaceDevelopment, &gocb.GetDesignDocumentOptions{Timeout: 3 * time.Minute}); err == nil {
		return fmt.Errorf("design Documents still present")
	}

	return nil
}

func MustDropViewsDesignDocs(t *testing.T, designDoc gocb.DesignDocument, viewManager *gocb.ViewIndexManager) {
	if err := dropViewsDesignDocs(designDoc, viewManager); err != nil {
		Die(t, err)
	}
}
