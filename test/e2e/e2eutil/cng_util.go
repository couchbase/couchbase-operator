/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/gocbcoreps"
	"github.com/couchbase/goprotostellar/genproto/admin_bucket_v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DAPITestClient struct {
	*http.Client
	dapiAddr string
	username string
	password string
}

type testDAPIHTTPRequest struct {
	Method string
	Path   string
	Body   []byte
}

type testDAPIHTTPResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func NewDAPITestClient(k8sCluster *types.Cluster, couchbaseCluster *couchbasev2.CouchbaseCluster, timeout time.Duration) *DAPITestClient {
	return &DAPITestClient{
		Client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
		dapiAddr: fmt.Sprintf("https://%s", getTestCNGClientConnectionString(couchbaseCluster, couchbaseCluster.Name, k8sutil.CNGDAPIServicePort)),
		username: string(k8sCluster.DefaultSecret.Data["username"]),
		password: string(k8sCluster.DefaultSecret.Data["password"]),
	}
}

func (c *DAPITestClient) sendTestHTTPRequest(req *testDAPIHTTPRequest) (*testDAPIHTTPResponse, error) {
	hReq, err := http.NewRequest(
		req.Method,
		fmt.Sprintf("%s%s", c.dapiAddr, req.Path),
		bytes.NewReader(req.Body))

	if err != nil {
		return nil, err
	}

	hReq.SetBasicAuth(c.username, c.password)

	hResp, err := c.Do(hReq)

	if err != nil {
		return nil, err
	}

	defer hResp.Body.Close()

	fullBody, err := io.ReadAll(hResp.Body)

	if err != nil {
		return nil, err
	}

	return &testDAPIHTTPResponse{
		StatusCode: hResp.StatusCode,
		Headers:    hResp.Header,
		Body:       fullBody,
	}, nil
}

func MustCheckCallerIdentityDAPI(t *testing.T, c *DAPITestClient) {
	resp, err := c.sendTestHTTPRequest(&testDAPIHTTPRequest{
		Method: http.MethodGet,
		Path:   "/v1/callerIdentity",
	})

	if err != nil {
		Die(t, err)
	}

	mustCheckRespStatusCodeDAPI(t, http.StatusOK, resp)

	u := unmarshalRespJSONElement(resp)["user"]

	if u != c.username {
		Die(t, fmt.Errorf("unexpected response from callerIdentity endpoint on the data api, expected %s but got %s", c.username, u))
	}
}

func MustCheckBucketExistsDAPIMgmtService(t *testing.T, c *DAPITestClient, bucketName string) {
	resp, err := c.sendTestHTTPRequest(&testDAPIHTTPRequest{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/_p/mgmt/pools/default/buckets/%s", bucketName),
	})

	if err != nil {
		Die(t, err)
	}

	mustCheckRespStatusCodeDAPI(t, http.StatusOK, resp)

	b := unmarshalRespJSONElement(resp)

	if b["name"] != bucketName {
		Die(t, fmt.Errorf("unexpected response from bucket endpoint on the data api mgmt service proxy, expected %s but got %s", bucketName, b["name"]))
	}
}

func MustCheckDAPIMgmtService(t *testing.T, c *DAPITestClient, code int) {
	resp, err := c.sendTestHTTPRequest(&testDAPIHTTPRequest{
		Method: http.MethodGet,
		Path:   "/_p/mgmt/pools/default/buckets/"})

	if code == http.StatusOK && err != nil {
		Die(t, err)
	}

	mustCheckRespStatusCodeDAPI(t, code, resp)
}

// unmarshalRespJSONElement will unmarshal a data api response body into a map[string]interface{}. If the response
// cannot be unmarshalled, an empty map will be returned.
func unmarshalRespJSONElement(resp *testDAPIHTTPResponse) map[string]interface{} {
	var data map[string]interface{}

	_ = json.Unmarshal(resp.Body, &data)

	return data
}

func MustCreateBasicBucketWithCNGClient(t *testing.T, ctx context.Context, client *gocbcoreps.RoutingClient, bucketName string) {
	ramQuota, numReplicas := uint64(100), uint32(1)

	_, err := client.BucketV1().CreateBucket(ctx, &admin_bucket_v1.CreateBucketRequest{
		BucketName:  bucketName,
		BucketType:  admin_bucket_v1.BucketType_BUCKET_TYPE_COUCHBASE,
		RamQuotaMb:  &ramQuota,
		NumReplicas: &numReplicas,
	})

	if err != nil {
		Die(t, err)
	}
}

func MustGetCNGConfigMap(t *testing.T, kubernetesCluster *types.Cluster, cluster *couchbasev2.CouchbaseCluster) {
	configMapName := k8sutil.GetCNGConfigMapName(cluster)

	err := retryutil.RetryFor(1*time.Minute, func() error {
		_, k8sErr := kubernetesCluster.KubeClient.CoreV1().ConfigMaps(kubernetesCluster.Namespace).Get(context.Background(), configMapName, metav1.GetOptions{})
		if k8sErr == nil {
			return nil
		}

		return fmt.Errorf("%s configmap not found for cluster: %s", configMapName, cluster.Name)
	})

	if err != nil {
		Die(t, err)
	}
}

func MustGetCNGClient(ctx context.Context, cluster *couchbasev2.CouchbaseCluster, clusterName string, username string, password string) (*gocbcoreps.RoutingClient, error) {
	var cngClient *gocbcoreps.RoutingClient

	dialopts := gocbcoreps.DialOptions{
		Username:           username,
		Password:           password,
		InsecureSkipVerify: true,
		PoolSize:           1,
	}

	connStr := getTestCNGClientConnectionString(cluster, clusterName, k8sutil.CNGHTTPSServicePort)

	cngConnErr := retryutil.Retry(ctx, 10*time.Minute, func() error {
		var err error
		cngClient, err = gocbcoreps.DialContext(ctx, connStr, &dialopts)
		if err != nil {
			return err
		}

		return nil
	})

	if cngConnErr != nil {
		return nil, cngConnErr
	}

	cngToCBConnErr := retryutil.Retry(ctx, 10*time.Minute, func() error {
		if cngClient.ConnectionState() == gocbcoreps.ConnStateDegraded {
			return fmt.Errorf("CNG container not ready")
		}
		return nil
	})

	if cngToCBConnErr != nil {
		return nil, cngToCBConnErr
	}

	return cngClient, nil
}

func getTestCNGClientConnectionString(cluster *couchbasev2.CouchbaseCluster, clusterName string, port int) string {
	cngSvcName := clusterName + "-cloud-native-gateway-service"
	connStr := fmt.Sprintf("%s.%s.svc.cluster.local:%d", cngSvcName, cluster.Namespace, port)

	return connStr
}

func mustCheckRespStatusCodeDAPI(t *testing.T, expectedCode int, resp *testDAPIHTTPResponse) {
	if resp != nil {
		if resp.StatusCode != expectedCode {
			Die(t, fmt.Errorf("unexpected status code from the data api %d, expected %d", resp.StatusCode, expectedCode))
		}
	} else {
		Die(t, fmt.Errorf("expected a response from the data api"))
	}
}
