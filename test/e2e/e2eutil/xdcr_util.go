package e2eutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
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

func GenerateHttpRequest(requestType, hostUrl, hostUsername, hostPassword string, reqParams *strings.Reader) ([]byte, error) {
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

// PopulateBucket selects a random pod from the cluster and then uses cbworkloadgen
// to create a defined number of documents.  The prefix is randomized so subsequent
// runs do not collide.
func PopulateBucket(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, bucket string, items int) error {
	pod, err := GetPod(k8s, cluster)
	if err != nil {
		return err
	}

	cmd := []string{
		"/opt/couchbase/bin/cbworkloadgen",
		"-n", "127.0.0.1:8091",
		"-u", constants.CbClusterUsername,
		"-p", constants.CbClusterPassword,
		"-b", bucket,
		"-j",
		"-i", strconv.Itoa(items),
		"--prefix", RandomSuffix(),
	}
	stdout, stderr, err := ExecCommandInPod(k8s, cluster.Namespace, pod.Name, cmd...)
	if err != nil {
		t.Logf("Error: %v", err)
		t.Logf("Command: %s", cmd)
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
		return err
	}

	return nil
}

func MustPopulateBucket(t *testing.T, k8s *types.Cluster, couchbase *api.CouchbaseCluster, bucket string, items int) {
	if err := PopulateBucket(t, k8s, couchbase, bucket, items); err != nil {
		Die(t, err)
	}
}

// VerifyDocCountInBucket polls the Couchbase API for the named bucket and checks whther the
// document count matches the expected number of items.
func VerifyDocCountInBucket(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, bucket string, items int, timeout time.Duration) error {
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

func MustVerifyDocCountInBucket(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, bucket string, items int, timeout time.Duration) {
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
func CreateDestClusterReference(t *testing.T, host string, k8s *types.Cluster, cluster *couchbasev1.CouchbaseCluster, username, password string) {
	// List the pods on the remote cluster and pick one
	selector := labels.SelectorFromSet(k8sutil.LabelsForCluster(cluster.Name)).String()
	pods, err := k8s.KubeClient.CoreV1().Pods(cluster.Namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal(err)
	}
	if len(pods.Items) == 0 {
		t.Fatal("no pods listed")
	}
	pod := pods.Items[0]

	// Make the request
	values := url.Values{}
	values.Add("uuid", cluster.Status.ClusterID)
	values.Add("name", cluster.Name)
	values.Add("hostname", pod.Status.HostIP+":"+strconv.Itoa(int(cluster.Status.ExposedPorts[pod.Name].AdminServicePort)))
	values.Add("username", username)
	values.Add("password", password)

	if _, err := GenerateHttpRequest("POST", "http://"+host+"/pools/default/remoteClusters", username, password, strings.NewReader(values.Encode())); err != nil {
		t.Fatal(err)
	}
}

func GetXdcrClusterReferences(hostUrl, hostUsername, hostPassword string) (xdcrClusterRefList []xdcrRemoteClusterReference, err error) {
	// Get all XDCR cluster reference
	hostUrl = "http://" + hostUrl + "/pools/default/remoteClusters"
	responseData, err := GenerateHttpRequest("POST", hostUrl, hostUsername, hostPassword, nil)
	if err != nil {
		return xdcrClusterRefList, err
	}
	json.Unmarshal(responseData, &xdcrClusterRefList)
	return xdcrClusterRefList, err
}

func DeleteXdcrClusterReferences(hostUrl, hostUsername, hostPassword string, xdcrClusterRef xdcrRemoteClusterReference) error {
	// Stop replication of default bucket
	hostUrl = "http://" + xdcrClusterRef.Hostname + "/controller/cancelXDCR/" + xdcrClusterRef.Uri + "%2Fdeafult%2Fdefault"
	GenerateHttpRequest("DELETE", hostUrl, hostUsername, hostPassword, nil)

	// Delete XDCR reference
	hostUrl = "http://" + xdcrClusterRef.Hostname + xdcrClusterRef.Uri
	_, err := GenerateHttpRequest("DELETE", hostUrl, hostUsername, hostPassword, nil)
	return err
}

func CreateXdcrBucketReplication(hostUrl, hostUsername, hostPassword, remoteClusterName, fromBucketName, destBucketName, versionType string) ([]byte, error) {
	// curl -v -X POST -u Administrator:password http://192.168.99.100:32589/controller/createReplication -d fromBucket=default
	//  -d toCluster=test-couchbase-zcrxp -d toBucket=default  -d replicationType=continuous
	fromBucketName = "fromBucket=" + fromBucketName
	remoteClusterName = "toCluster=" + remoteClusterName
	destBucketName = "toBucket=" + destBucketName
	replicationType := "replicationType=continuous"
	versionType = "type=" + versionType

	hostUrl = "http://" + hostUrl + "/controller/createReplication"
	reqParamList := []string{fromBucketName, remoteClusterName, destBucketName, replicationType, versionType}
	reqParams := strings.NewReader(strings.Join(reqParamList, "&"))

	return GenerateHttpRequest("POST", hostUrl, hostUsername, hostPassword, reqParams)
}
