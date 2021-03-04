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
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GenerateHTTPRequest(requestType, hostURL, hostUsername, hostPassword string, reqParams io.Reader) ([]byte, error) {
	request, err := http.NewRequest(requestType, hostURL, reqParams)
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

	responseData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote call failed with response: %s %s", response.Status, string(responseData))
	}

	return responseData, nil
}

// insertAtMostDocuments creates as many documents as it can over a single connection and
// bails on an error.  Any retry logic needs to be handled at a higher level.
func insertAtMostDocuments(ctx context.Context, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket, prefix string, items, offset int) (int, error) {
	host, cleanup, err := GetAdminConsoleHostURL(k8s, cluster)
	if err != nil {
		return 0, err
	}

	defer cleanup()

	for i := 0; i < items; i++ {
		documentID := prefix + strconv.Itoa(offset+i)

		uri := "http://" + host + "/pools/default/buckets/" + bucket + "/docs/" + documentID
		values := url.Values{}
		values.Add(`flags`, `24`)
		values.Add(`value`, `{"key":"value"}`)

		if _, err := GenerateHTTPRequest("POST", uri, "Administrator", "password", strings.NewReader(values.Encode())); err != nil {
			return i, err
		}

		// Check to see if the timer has blown, respect the timeout and abort from the loop.
		select {
		case <-ctx.Done():
			return i, ctx.Err()
		default:
		}
	}

	return items, nil
}

// PopulateBucket selects a random pod from the cluster and then uses the API
// to create a defined number of documents.  The prefix is randomized so subsequent
// runs do not collide.  Documents are inserted one at a time, so we can keep a count
// of exactly how many were successfully committed.
func PopulateBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, items int) error {
	document := RandomSuffix()

	// This method of inserting buckets is p*** poor, and IRL you get something like
	// "inserted 435 documents in 1m0.105061306s".  Locally, this is pretty fast, but
	// over the internet it sucks (we MUST start testing in Kubernetes with SDKs...).
	// We need to scale any timeouts by the number of documents, so lets pick a
	// conservative estimate of way less than this, server may hang for a bit....  So if you
	// decide to run with 1,000,000 documents, don't be shocked if the test suddently takes
	// 3 days to run!
	timeout := (time.Duration(items) * time.Minute) / 250

	// This is a fudge for server blocking, say due to failover...
	timeout += 5 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	inserted := 0

	callback := func() error {
		i, err := insertAtMostDocuments(ctx, k8s, cluster, bucket, document, items-inserted, inserted)

		inserted += i

		return err
	}

	start := time.Now()

	defer func() {
		t.Logf("inserted %d documents in %v", inserted, time.Since(start))
	}()

	if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
		return err
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
	return retryutil.RetryFor(timeout, func() error {
		client, cleanup := MustCreateAdminConsoleClient(t, k8s, cluster)
		defer cleanup()

		info := &couchbaseutil.BucketStatus{}

		request := &couchbaseutil.Request{
			Path:   "/pools/default/buckets/" + bucket,
			Result: info,
		}

		if err := client.client.Get(request, client.host); err != nil {
			return err
		}

		if info.BasicStats.ItemCount != items {
			return fmt.Errorf("document count %d, expected %d", info.BasicStats.ItemCount, items)
		}

		return nil
	})
}

func MustVerifyDocCountInBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, items int, timeout time.Duration) {
	if err := VerifyDocCountInBucket(t, k8s, cluster, bucket, items, timeout); err != nil {
		Die(t, err)
	}
}

// VerifyDocCountInBucketNonZerp polls the Couchbase API for the named bucket and checks whether the
// document count is non-zero.
func VerifyDocCountInBucketNonZero(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		client, cleanup := MustCreateAdminConsoleClient(t, k8s, cluster)
		defer cleanup()

		info := &couchbaseutil.BucketStatus{}

		request := &couchbaseutil.Request{
			Path:   "/pools/default/buckets/" + bucket,
			Result: info,
		}

		if err := client.client.Get(request, client.host); err != nil {
			return err
		}

		if info.BasicStats.ItemCount == 0 {
			return fmt.Errorf("document count zero")
		}

		return nil
	})
}

func MustVerifyDocCountInBucketNonZero(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) {
	if err := VerifyDocCountInBucketNonZero(t, k8s, cluster, bucket, timeout); err != nil {
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
		cluster, err := kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(context.Background(), cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if cluster.Status.ClusterID == "" {
			return fmt.Errorf("remote cluster UUID not populated")
		}

		uuid = cluster.Status.ClusterID

		return nil
	}

	if err := retryutil.RetryFor(time.Minute, callback); err != nil {
		return "", err
	}

	return uuid, nil
}

// getRemoteUUIDAndHostGeneric returns the remote hostname, based on IP and node port, and the cluster UUID.
// Used for generic XDCR testing.
func getRemoteUUIDAndHostGeneric(kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) (string, string, error) {
	// List the pods on the remote cluster and pick one
	svc, err := kubernetes.KubeClient.CoreV1().Services(cluster.Namespace).Get(context.Background(), cluster.Name+"-ui", metav1.GetOptions{})
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

	nodes, err := kubernetes.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
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

	return uuid, fmt.Sprintf("http://%s:%d?network=external", ip, nodePort), nil
}

// getRemoteUUIDAndHost returns the remote hostname, based on DNS, and the cluster UUID.
// Used for generic XDCR testing.
func getRemoteUUIDAndHost(kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, ctx *TLSContext) (string, string, error) {
	var err error

	cluster, err = kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(context.Background(), cluster.Name, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}

	uuid, err := getRemoteUUID(kubernetes, cluster)
	if err != nil {
		return "", "", err
	}

	scheme := "couchbase"

	if ctx != nil {
		scheme = "couchbases"
	}

	// Use an SRV lookup.
	return uuid, fmt.Sprintf("%s://%s-srv.%s?network=default", scheme, cluster.Name, cluster.Namespace), nil
}

// EstablishXDCRReplicationGeneric creates a remote cluster in the source, and a replication from the source bucket to the destination
// bucket.  If the function was successful (did not return an error) then the client is responsible for defered secret cleanup.
func EstablishXDCRReplicationGeneric(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication) (replicationSpec *couchbasev2.CouchbaseReplication, err error) {
	// Populate the namespace manually as we don't return the API object.
	replication.Namespace = srcK8s.Namespace

	// Create the remote cluster secret.
	xdcrSecret := fmt.Sprintf("%s-auth", target.Name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: xdcrSecret,
		},
		Data: dstK8s.DefaultSecret.Data,
	}

	if _, err = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		return
	}

	if replicationSpec, err = srcK8s.CRClient.CouchbaseV2().CouchbaseReplications(source.Namespace).Create(context.Background(), replication, metav1.CreateOptions{}); err != nil {
		return
	}

	// Create the XDCR remote cluster.
	clusterName := "remote"

	uuid, host, err := getRemoteUUIDAndHostGeneric(dstK8s, target)
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
	if _, err = patchCluster(srcK8s, source, jsonpatch.NewPatchSet().Replace("/spec/xdcr", xdcr), time.Minute); err != nil {
		return
	}

	// Wait for the operator to successfully connect before continuing.
	name := fmt.Sprintf("%s/%s/%s", clusterName, replication.Spec.Bucket, replication.Spec.RemoteBucket)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err = waitForResourceEventFromNow(ctx, nil, srcK8s, source, k8sutil.ReplicationAddedEvent(source, name)); err != nil {
		return
	}

	return
}

// EstablishXDCRReplication creates a remote cluster in the source, and a replication from the source bucket to the destination
// bucket.  If the function was successful (did not return an error) then the client is responsible for defered secret cleanup.
func EstablishXDCRReplication(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, tls *TLSContext) (err error) {
	// Populate the namespace manually as we don't return the API object.
	replication.Namespace = srcK8s.Namespace

	// Create the remote cluster secret.
	xdcrSecret := fmt.Sprintf("%s-auth", target.Name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: xdcrSecret,
		},
		Data: dstK8s.DefaultSecret.Data,
	}

	if _, err = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		return
	}

	// Define the TLS secret if we are using it.
	tlsSecret := ""

	if tls != nil {
		tlsSecret = fmt.Sprintf("%s-xdcr-tls", target.Name)
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: tlsSecret,
			},
			Data: map[string][]byte{
				couchbasev2.RemoteClusterTLSCA: tls.CA.Certificate,
			},
		}

		if target.Spec.Networking.TLS.ClientCertificatePolicy != nil {
			secret.Data[couchbasev2.RemoteClusterTLSCertificate] = tls.ClientCert
			secret.Data[couchbasev2.RemoteClusterTLSKey] = tls.ClientKey
		}

		if _, err = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
			return
		}
	}

	if _, err = srcK8s.CRClient.CouchbaseV2().CouchbaseReplications(source.Namespace).Create(context.Background(), replication, metav1.CreateOptions{}); err != nil {
		return
	}

	// Create the XDCR remote cluster.
	// When using mTLS you must specify the TLS configuration only and not any
	// username or password.
	clusterName := "remote"

	uuid, host, err := getRemoteUUIDAndHost(dstK8s, target, tls)
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
			},
		},
	}

	// If we are using TLS then attach the CA and optional client cert/key.
	if tls != nil {
		xdcr.RemoteClusters[0].TLS = &couchbasev2.RemoteClusterTLS{
			Secret: &tlsSecret,
		}
	}

	// If we are not using client authentication then we need the remote username and password.
	if tls == nil || target.Spec.Networking.TLS.ClientCertificatePolicy == nil {
		xdcr.RemoteClusters[0].AuthenticationSecret = &xdcrSecret
	}

	if _, err = patchCluster(srcK8s, source, jsonpatch.NewPatchSet().Replace("/spec/xdcr", xdcr), time.Minute); err != nil {
		return
	}

	// Wait for the operator to successfully connect before continuing.
	name := fmt.Sprintf("%s/%s/%s", clusterName, replication.Spec.Bucket, replication.Spec.RemoteBucket)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err = waitForResourceEventFromNow(ctx, nil, srcK8s, source, k8sutil.ReplicationAddedEvent(source, name)); err != nil {
		return
	}

	return
}

func MustRotateXDCRReplicationPassword(t *testing.T, src *types.Cluster, dst *types.Cluster, target *couchbasev2.CouchbaseCluster) {
	xdcrSecret := fmt.Sprintf("%s-auth", target.Name)

	secret, err := src.KubeClient.CoreV1().Secrets(src.Namespace).Get(context.Background(), xdcrSecret, metav1.GetOptions{})
	if err != nil {
		Die(t, err)
	}

	secret.Data = dst.DefaultSecret.Data

	if _, err := src.KubeClient.CoreV1().Secrets(src.Namespace).Update(context.Background(), secret, metav1.UpdateOptions{}); err != nil {
		Die(t, err)
	}
}

func MustRotateXDCRReplicationTLS(t *testing.T, src *types.Cluster, dst *types.Cluster, target *couchbasev2.CouchbaseCluster, tls *TLSContext) {
	xdcrSecret := fmt.Sprintf("%s-xdcr-tls", target.Name)

	srcSecret, err := src.KubeClient.CoreV1().Secrets(src.Namespace).Get(context.Background(), xdcrSecret, metav1.GetOptions{})
	if err != nil {
		Die(t, err)
	}

	srcSecret.Data[couchbasev2.RemoteClusterTLSCA] = tls.CA.Certificate
	srcSecret.Data[couchbasev2.RemoteClusterTLSCertificate] = tls.ClientCert
	srcSecret.Data[couchbasev2.RemoteClusterTLSKey] = tls.ClientKey

	if _, err := src.KubeClient.CoreV1().Secrets(src.Namespace).Update(context.Background(), srcSecret, metav1.UpdateOptions{}); err != nil {
		Die(t, err)
	}
}

func DeleteXDCRReplication(k8s *types.Cluster, source *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		if err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(replication.Namespace).Delete(context.Background(), replication.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}

		// Everything successful
		return nil
	})
}

func MustEstablishXDCRReplicationGeneric(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication) *couchbasev2.CouchbaseReplication {
	replication, err := EstablishXDCRReplicationGeneric(srcK8s, dstK8s, source, target, replication)
	if err != nil {
		Die(t, err)
	}

	return replication
}

func MustEstablishXDCRReplication(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, ctx *TLSContext) {
	err := EstablishXDCRReplication(srcK8s, dstK8s, source, target, replication, ctx)
	if err != nil {
		Die(t, err)
	}
}

func MustDeleteXDCRReplication(t *testing.T, k8s *types.Cluster, source *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, timeout time.Duration) {
	err := DeleteXDCRReplication(k8s, source, replication, timeout)
	if err != nil {
		Die(t, err)
	}
}
