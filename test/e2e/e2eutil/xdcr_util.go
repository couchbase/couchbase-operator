package e2eutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type cbClusterInfo struct {
	Uuid string `json:"uuid"`
}

type xdcrRemoteClusterReference struct {
	Name     string `json:"name"`
	Uri      string `json:"uri"`
	Hostname string `json:"hostname"`
	Username string `json:"username"`
}

func GenerateHttpRequest(requestType, hostUrl, hostUsername, hostPassword string, reqParams io.Reader) ([]byte, error) {
	var request *http.Request
	var err error

	if reqParams == nil {
		request, err = http.NewRequest(requestType, hostUrl, nil)
	} else {
		request, err = http.NewRequest(requestType, hostUrl, reqParams)
	}
	if err != nil {
		return nil, errors.New("Http request failed: " + err.Error())
	}

	request.SetBasicAuth(hostUsername, hostPassword)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, errors.New("Failed to " + err.Error())
	}
	defer response.Body.Close()
	responseBody := response.Body
	responseData, _ := ioutil.ReadAll(responseBody)
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("Remote call failed with response: " + response.Status + ", " + string(responseData))
	}
	return responseData, nil
}

func FlushBucket(hostUrl, bucketName, hostUsername, hostPassword string) ([]byte, error) {
	//curl -X POST -u [admin]:[password] [localhost]:8091/pools/default/buckets/[bucket-name]/controller/doFlush
	hostUrl = "http://" + hostUrl + "/pools/default/buckets/" + bucketName + "/controller/doFlush"
	return GenerateHttpRequest("POST", hostUrl, hostUsername, hostPassword, nil)
}

// PopulateBucket selects a random pod from the cluster and then uses the API
// to create a defined number of documents.  The prefix is randomized so subsequent
// runs do not collide.  Documents are inserted one at a time, so we can keep a count
// of exactly how many were successfully committed.
func PopulateBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, items int) error {
	document := RandomSuffix()
	for i := 0; i < items; i++ {
		callback := func() (bool, error) {
			host, cleanup, err := GetAdminConsoleHostURL(k8s, cluster)
			if err != nil {
				return false, retryutil.RetryOkError(err)
			}
			defer cleanup()

			// Note: I tried using cbworkloadgen, however it does die half way through, so say you
			// want to add 10 docs, and it does 7, if you retry you end up with 17, which is not
			// what we want from a test stability perspective!
			uri := "http://" + host + "/pools/default/buckets/" + bucket + "/docs/" + document + strconv.Itoa(i)
			values := url.Values{}
			values.Add(`flags`, `24`)
			values.Add(`value`, `{"key":"value"}`)

			if _, err := GenerateHttpRequest("POST", uri, "Administrator", "password", strings.NewReader(values.Encode())); err != nil {
				return false, retryutil.RetryOkError(err)
			}
			return true, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		if err := retryutil.Retry(ctx, 5*time.Second, IntMax, callback); err != nil {
			return err
		}
	}

	return nil
}

func MustPopulateBucket(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucket string, items int) {
	if err := PopulateBucket(t, k8s, couchbase, bucket, items); err != nil {
		Die(t, err)
	}
}

// VerifyDocCountInBucket polls the Couchbase API for the named bucket and checks whether the
// document count matches the expected number of items.
func VerifyDocCountInBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, items int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 10*time.Second, IntMax, func() (bool, error) {
		client, cleanup := MustCreateAdminConsoleClient(t, k8s, cluster)
		defer cleanup()

		info, err := client.GetBucketStatus(bucket)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		if info.BasicStats.ItemCount != items {
			return false, retryutil.RetryOkError(fmt.Errorf("document count %d, expected %d", info.BasicStats.ItemCount, items))
		}

		return true, nil
	})
}

func MustVerifyDocCountInBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, items int, timeout time.Duration) {
	if err := VerifyDocCountInBucket(t, k8s, cluster, bucket, items, timeout); err != nil {
		Die(t, err)
	}
}

func GetRemoteUuid(hostUrl, cbUsername, cbPassword string) (uuid string, err error) {
	// curl -u [admin]:[password] http://[localhost]:8091/pools
	hostUrl = "http://" + hostUrl + "/pools"
	responseData, err := GenerateHttpRequest("GET", hostUrl, cbUsername, cbPassword, nil)
	if err != nil {
		return
	}
	var cbClusterInfo cbClusterInfo
	if err = json.Unmarshal(responseData, &cbClusterInfo); err != nil {
		return
	}
	uuid = cbClusterInfo.Uuid
	return
}

// CreateDestClusterReference polls the destination cluster to discover the node port of a pod and uses that to
// initialize connection on the source cluster.
func CreateDestClusterReference(k8sSrc, k8sDst *types.Cluster, src, dst *couchbasev2.CouchbaseCluster, username, password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	callback := func() (bool, error) {
		// List the pods on the remote cluster and pick one
		selector := labels.SelectorFromSet(k8sutil.LabelsForCluster(dst.Name)).String()
		pods, err := k8sDst.KubeClient.CoreV1().Pods(dst.Namespace).List(metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		if len(pods.Items) == 0 {
			return false, retryutil.RetryOkError(fmt.Errorf("no pods listed"))
		}
		pod := pods.Items[0]

		host, cleanup, err := GetAdminConsoleHostURL(k8sSrc, src)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		defer cleanup()

		cluster, err := k8sDst.CRClient.CouchbaseV2().CouchbaseClusters(dst.Namespace).Get(dst.Name, metav1.GetOptions{})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		ports, ok := cluster.Status.ExposedPorts[pod.Name]
		if !ok {
			return false, retryutil.RetryOkError(fmt.Errorf("admin service port not exposed"))
		}

		// Make the request
		values := url.Values{}
		values.Add("uuid", cluster.Status.ClusterID)
		values.Add("name", cluster.Name)
		values.Add("hostname", pod.Status.HostIP+":"+strconv.Itoa(int(ports.AdminServicePort)))
		values.Add("username", username)
		values.Add("password", password)

		if _, err := GenerateHttpRequest("POST", "http://"+host+"/pools/default/remoteClusters", username, password, strings.NewReader(values.Encode())); err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	}

	return retryutil.Retry(ctx, 5*time.Second, IntMax, callback)
}

func MustCreateDestClusterReference(t *testing.T, k8sSrc, k8sDst *types.Cluster, src, dst *couchbasev2.CouchbaseCluster, username, password string) {
	if err := CreateDestClusterReference(k8sSrc, k8sDst, src, dst, username, password); err != nil {
		Die(t, err)
	}
}

func GetXdcrClusterReferences(hostUrl, hostUsername, hostPassword string) (xdcrClusterRefList []xdcrRemoteClusterReference, err error) {
	// Get all XDCR cluster reference
	hostUrl = "http://" + hostUrl + "/pools/default/remoteClusters"
	var responseData []byte
	responseData, err = GenerateHttpRequest("POST", hostUrl, hostUsername, hostPassword, nil)
	if err != nil {
		return
	}
	if err = json.Unmarshal(responseData, &xdcrClusterRefList); err != nil {
		return
	}
	return
}

func DeleteXdcrClusterReferences(hostUsername, hostPassword string, xdcrClusterRef xdcrRemoteClusterReference) error {
	// Stop replication of default bucket
	hostUrl := "http://" + xdcrClusterRef.Hostname + "/controller/cancelXDCR/" + xdcrClusterRef.Uri + "%2Fdeafult%2Fdefault"
	if _, err := GenerateHttpRequest("DELETE", hostUrl, hostUsername, hostPassword, nil); err != nil {
		return err
	}

	// Delete XDCR reference
	hostUrl = "http://" + xdcrClusterRef.Hostname + xdcrClusterRef.Uri
	_, err := GenerateHttpRequest("DELETE", hostUrl, hostUsername, hostPassword, nil)
	return err
}

func CreateXdcrBucketReplication(k8s *types.Cluster, src, dst *couchbasev2.CouchbaseCluster, username, password, srcbucket, dstBucket string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	values := url.Values{}
	values.Add("fromBucket", srcbucket)
	values.Add("toCluster", dst.Name)
	values.Add("toBucket", dstBucket)
	values.Add("versionType", "xmem")
	values.Add("replicationType", "continuous")

	callback := func() (bool, error) {
		host, cleanup, err := GetAdminConsoleHostURL(k8s, src)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		defer cleanup()

		if _, err := GenerateHttpRequest("POST", "http://"+host+"/controller/createReplication", username, password, strings.NewReader(values.Encode())); err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	}

	return retryutil.Retry(ctx, 5*time.Second, IntMax, callback)
}

func MustCreateXdcrBucketReplication(t *testing.T, k8s *types.Cluster, src, dst *couchbasev2.CouchbaseCluster, username, password, srcbucket, dstBucket string) {
	if err := CreateXdcrBucketReplication(k8s, src, dst, username, password, srcbucket, dstBucket); err != nil {
		Die(t, err)
	}
}
