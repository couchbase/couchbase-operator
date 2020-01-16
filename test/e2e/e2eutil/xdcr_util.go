package e2eutil

import (
	"context"
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
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GenerateHTTPRequest(requestType, hostURL, hostUsername, hostPassword string, reqParams io.Reader) ([]byte, error) {
	var request *http.Request
	var err error

	request, err = http.NewRequest(requestType, hostURL, reqParams)
	if err != nil {
		return nil, err
	}

	request.SetBasicAuth(hostUsername, hostPassword)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody := response.Body
	responseData, _ := ioutil.ReadAll(responseBody)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote call failed with response: %s %s", response.Status, string(responseData))
	}
	return responseData, nil
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

			if _, err := GenerateHTTPRequest("POST", uri, "Administrator", "password", strings.NewReader(values.Encode())); err != nil {
				return false, retryutil.RetryOkError(err)
			}
			return true, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		if err := retryutil.Retry(ctx, 5*time.Second, callback); err != nil {
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

	return retryutil.Retry(ctx, 10*time.Second, func() (bool, error) {
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

// getRemoteUUID returns the UUID of the remote cluster, or if it is not populated polls until
// it is populated.
func getRemoteUUID(kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (string, error) {
	if cluster.Status.ClusterID != "" {
		return cluster.Status.ClusterID, nil
	}

	var uuid string
	callback := func() error {
		cluster, err := kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if cluster.Status.ClusterID == "" {
			return fmt.Errorf("remote cluster UUID not populated")
		}
		uuid = cluster.Status.ClusterID
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := retryutil.RetryOnErr(ctx, 5*time.Second, callback); err != nil {
		return "", err
	}

	return uuid, nil
}

// getRemoteUUIDAndHost returns the remote hostname, based on IP and node port, and the cluster UUID.
// Used for generic XDCR testing.
func getRemoteUUIDAndHost(kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (string, string, error) {
	// List the pods on the remote cluster and pick one
	svc, err := kubernetes.KubeClient.CoreV1().Services(cluster.Namespace).Get(cluster.Name+"-ui", metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}

	nodePort := -1
	for _, port := range svc.Spec.Ports {
		if port.Port == 8091 {
			nodePort = int(port.NodePort)
			break
		}
	}
	if nodePort == -1 {
		return "", "", fmt.Errorf("admin service port not exposed")
	}

	nodes, err := kubernetes.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return "", "", err
	}

	ip := ""
	for _, address := range nodes.Items[0].Status.Addresses {
		if address.Type == "InternalIP" {
			ip = address.Address
			break
		}
	}
	if ip == "" {
		return "", "", fmt.Errorf("unable to determine node IP address")
	}

	uuid, err := getRemoteUUID(kubernetes, cluster)
	if err != nil {
		return "", "", err
	}

	return uuid, fmt.Sprintf("%s:%d", ip, nodePort), nil
}

// getRemoteUUIDAndHostTLS returns the remote hostname, based on DNS, and the cluster UUID.
// Used for generic XDCR testing.
func getRemoteUUIDAndHostTLS(kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (string, string, error) {
	var err error
	cluster, err = kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(cluster.Name, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}

	uuid, err := getRemoteUUID(kubernetes, cluster)
	if err != nil {
		return "", "", err
	}

	// Use an SRV lookup.
	return uuid, fmt.Sprintf("%s-srv.%s", cluster.Name, cluster.Namespace), nil
}

// EstablishXDCRReplication creates a remote cluster in the source, and a replication from the source bucket to the destination
// bucket.  If the function was successful (did not return an error) then the client is responsible for defered secret cleanup.
func EstablishXDCRReplication(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, srcBucket, dstBucket string) (replication *couchbasev2.CouchbaseReplication, cleanup func(), err error) {
	// Create the remote cluster secret.
	xdcrSecret := fmt.Sprintf("%s-auth", target.Name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: xdcrSecret,
		},
		Data: dstK8s.DefaultSecret.Data,
	}
	if _, err = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Create(secret); err != nil {
		return
	}

	// Define the cleanup to remove the secret and automatically perform cleanup on error so the client doesn't need to worry.
	cleanup = func() {
		_ = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Delete(xdcrSecret, metav1.NewDeleteOptions(0))
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()

	// Create the replication.
	replication = &couchbasev2.CouchbaseReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-replication",
		},
		Spec: couchbasev2.CouchbaseReplicationSpec{
			Bucket:       srcBucket,
			RemoteBucket: dstBucket,
		},
	}
	if replication, err = srcK8s.CRClient.CouchbaseV2().CouchbaseReplications(source.Namespace).Create(replication); err != nil {
		return
	}

	// Create the XDCR remote cluster.
	clusterName := "remote"

	uuid, host, err := getRemoteUUIDAndHost(dstK8s, target)
	if err != nil {
		return
	}

	xdcr := couchbasev2.XDCR{
		Managed: true,
		RemoteClusters: []couchbasev2.RemoteCluster{
			{
				Name:                 clusterName,
				UUID:                 uuid,
				Hostname:             host,
				AuthenticationSecret: &xdcrSecret,
			},
		},
	}
	if _, err = PatchCluster(srcK8s, source, jsonpatch.NewPatchSet().Replace("/Spec/XDCR", xdcr), time.Minute); err != nil {
		return
	}

	// Wait for the operator to successfully connect before continuing.
	name := fmt.Sprintf("%s/%s/%s", clusterName, srcBucket, dstBucket)
	if err = WaitForClusterEvent(srcK8s.KubeClient, source, k8sutil.ReplicationAddedEvent(source, name), 5*time.Minute); err != nil {
		return
	}

	return
}

// EstablishXDCRReplicationTLS creates a remote cluster in the source, and a replication from the source bucket to the destination
// bucket.  If the function was successful (did not return an error) then the client is responsible for defered secret cleanup.
func EstablishXDCRReplicationTLS(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, srcBucket, dstBucket string, ctx *TLSContext) (cleanup func(), err error) {
	// Create the remote cluster secret.
	xdcrSecret := fmt.Sprintf("%s-auth", target.Name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: xdcrSecret,
		},
		Data: dstK8s.DefaultSecret.Data,
	}
	if _, err = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Create(secret); err != nil {
		return
	}

	// Define the cleanup to remove the secret and automatically perform cleanup on error so the client doesn't need to worry.
	cleanup = func() {
		_ = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Delete(xdcrSecret, metav1.NewDeleteOptions(0))
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()

	// Define the TLS secret.
	tlsSecret := fmt.Sprintf("%s-xdcr-tls", target.Name)
	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: tlsSecret,
		},
		Data: map[string][]byte{
			couchbasev2.RemoteClusterTLSCA: ctx.CA.Certificate,
		},
	}
	if target.Spec.Networking.TLS.ClientCertificatePolicy != nil {
		secret.Data[couchbasev2.RemoteClusterTLSCertificate] = ctx.ClientCert
		secret.Data[couchbasev2.RemoteClusterTLSKey] = ctx.ClientKey
	}

	if _, err = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Create(secret); err != nil {
		return
	}

	cleanup = func() {
		_ = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Delete(xdcrSecret, metav1.NewDeleteOptions(0))
		_ = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Delete(tlsSecret, metav1.NewDeleteOptions(0))
	}

	// Create the replication.
	replication := &couchbasev2.CouchbaseReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-replication",
		},
		Spec: couchbasev2.CouchbaseReplicationSpec{
			Bucket:       srcBucket,
			RemoteBucket: dstBucket,
		},
	}
	if _, err = srcK8s.CRClient.CouchbaseV2().CouchbaseReplications(source.Namespace).Create(replication); err != nil {
		return
	}

	// Create the XDCR remote cluster.
	// When using mTLS you must specify the TLS configuration only and not any
	// username or password.
	clusterName := "remote"

	uuid, host, err := getRemoteUUIDAndHostTLS(dstK8s, target)
	if err != nil {
		return
	}

	xdcr := couchbasev2.XDCR{
		Managed: true,
		RemoteClusters: []couchbasev2.RemoteCluster{
			{
				Name:     clusterName,
				UUID:     uuid,
				Hostname: host,
				TLS: &couchbasev2.RemoteClusterTLS{
					Secret: &tlsSecret,
				},
			},
		},
	}
	if target.Spec.Networking.TLS.ClientCertificatePolicy == nil {
		xdcr.RemoteClusters[0].AuthenticationSecret = &xdcrSecret
	}
	if _, err = PatchCluster(srcK8s, source, jsonpatch.NewPatchSet().Replace("/Spec/XDCR", xdcr), time.Minute); err != nil {
		return
	}

	// Wait for the operator to successfully connect before continuing.
	name := fmt.Sprintf("%s/%s/%s", clusterName, srcBucket, dstBucket)
	if err = WaitForClusterEvent(srcK8s.KubeClient, source, k8sutil.ReplicationAddedEvent(source, name), 5*time.Minute); err != nil {
		return
	}

	return
}

func MustEstablishXDCRReplication(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, srcBucket, dstBucket string) (*couchbasev2.CouchbaseReplication, func()) {
	replication, cleanup, err := EstablishXDCRReplication(srcK8s, dstK8s, source, target, srcBucket, dstBucket)
	if err != nil {
		Die(t, err)
	}
	return replication, cleanup
}

func MustEstablishXDCRReplicationTLS(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, srcBucket, dstBucket string, ctx *TLSContext) func() {
	cleanup, err := EstablishXDCRReplicationTLS(srcK8s, dstK8s, source, target, srcBucket, dstBucket, ctx)
	if err != nil {
		Die(t, err)
	}
	return cleanup
}
