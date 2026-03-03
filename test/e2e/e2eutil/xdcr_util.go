/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"fmt"
	"strconv"
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

type XDCRInfo struct {
	Replication *couchbasev2.CouchbaseReplication
	SecretName  string
}

// getBucketInfo returns information of the bucket.
func getBucketInfo(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string) (*couchbaseutil.BucketStatus, error) {
	client := MustCreateAdminConsoleClient(t, k8s, cluster)

	info := &couchbaseutil.BucketStatus{}

	request := &couchbaseutil.Request{
		Path:   "/pools/default/buckets/" + bucket,
		Result: info,
	}

	if err := client.client.Get(request, client.host); err != nil {
		return info, err
	}

	return info, nil
}

// VerifyDocCountInBucket polls the Couchbase API for the named bucket and checks whether the
// document count matches the expected number of items.
func VerifyDocCountInBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, items int, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucket)
		if err != nil {
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

func MustVerifyDocCountInCollection(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, scope string, collection string, items int, timeout time.Duration) {
	if err := VerifyDocCountInCollection(t, k8s, cluster, bucket, scope, collection, items, timeout); err != nil {
		Die(t, err)
	}
}

// VerifyDocCountInCollection uses Server 7.0's metrics to check that the current number of items in a collection is equal to a given number.
func VerifyDocCountInCollection(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, scope string, collection string, items int, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		// Labels are passed to specify which collection we're looking at.
		labels := make(map[string]string)
		labels["bucket"] = bucket
		labels["scope"] = scope
		labels["collection"] = collection

		// Metric name of doc count in bucket. See: https://docs.couchbase.com/server/current/metrics-reference/metrics-reference.html
		metricName := "kv_collection_item_count"

		metrics, err := GetCouchbaseMetric(t, k8s, cluster, metricName, labels, time.Minute)
		if err != nil {
			return err
		}

		// We're only getting a single metric, so we can just get the first 'Data'.
		if len(metrics.Data) == 0 {
			return fmt.Errorf("metrics response had no data")
		}
		values := metrics.Data[0].Values
		// Server returns multiple values, and we want the most recent/last one, and turn it into an int from a string.
		if len(values) == 0 {
			return fmt.Errorf("metrics response had no values")
		}

		itemCount, err := strconv.Atoi(values[len(values)-1][1].(string))
		if err != nil {
			return err
		}

		if itemCount != items {
			return fmt.Errorf("document count %d, expected %d", itemCount, items)
		}

		return nil
	})
}

func VerifyDocCountInScope(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, scope string, items int, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		// Labels are passed to specify which collection we're looking at.
		labels := make(map[string]string)
		labels["bucket"] = bucket
		labels["scope"] = scope

		// Metric name of doc count in bucket. See: https://docs.couchbase.com/server/current/metrics-reference/metrics-reference.html
		metricName := "kv_collection_item_count"

		metrics, err := GetCouchbaseMetric(t, k8s, cluster, metricName, labels, time.Minute)
		if err != nil {
			return err
		}

		// We're only getting a single type of metric (doc count), so we can just get the first 'Data'.
		if len(metrics.Data) == 0 {
			return fmt.Errorf("metrics response had no data")
		}
		totalCount := 0

		// The doc count is returned per collection, so we need to add them all up to get the count in the scope.
		for _, d := range metrics.Data {
			// Server returns multiple values, and we want the most recent/last one, and turn it into an int from a string.
			if len(d.Values) == 0 {
				return fmt.Errorf("metrics response had no values")
			}

			itemCount, err := getLatestMetric(d.Values)
			if err != nil {
				return err
			}

			totalCount += itemCount
		}

		if totalCount != items {
			return fmt.Errorf("document count %d, expected %d", totalCount, items)
		}

		return nil
	})
}

func MustVerifyDocCountInScope(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, scope string, items int, timeout time.Duration) {
	if err := VerifyDocCountInScope(t, k8s, cluster, bucket, scope, items, timeout); err != nil {
		Die(t, err)
	}
}

// getLatestMetric takes the 2D array that server returns - an array of [timestamp, value] - and gets the last/most recent one.
// Naturally, the returned value is a string, so we convert it to an int, then return it as the value we're looking for.
// Sorry.
func getLatestMetric(values [][]interface{}) (int, error) {
	return strconv.Atoi(values[len(values)-1][1].(string))
}

// VerifyDocCountInBucketNonZero polls the Couchbase API for the named bucket and checks whether the
// document count is non-zero.
func VerifyDocCountInBucketNonZero(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucket)
		if err != nil {
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

// VerifyReplicaCount polls the Couchbase API for the named bucket and checks whether the
// Replica number matches the expected replicaNumber.
func VerifyReplicaCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucket)
		if err != nil {
			return err
		}

		if info.ReplicaNumber != 1 {
			return fmt.Errorf("replica Number %d, expected %d", info.ReplicaNumber, 1)
		}

		return nil
	})
}

func MustVerifyReplicaCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) {
	if err := VerifyReplicaCount(t, k8s, cluster, bucket, timeout); err != nil {
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

func createRemoteClusterSecret(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster) (string, error) {
	// Create the remote cluster secret.
	xdcrSecret := fmt.Sprintf("%s-auth-", target.Name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: xdcrSecret,
		},
		Data: dstK8s.DefaultSecret.Data,
	}

	// Caller is responsible for deletion.
	secret, err := srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	return secret.GetName(), nil
}

func createRemoteCluster(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, xdcrSecret string) (string, error) {
	// Create the XDCR remote cluster.
	clusterName := target.Name

	uuid, host, err := getRemoteUUIDAndHostGeneric(dstK8s, target)
	if err != nil {
		return "", err
	}

	xdcr := []couchbasev2.RemoteCluster{
		{
			Name:                 clusterName,
			UUID:                 uuid,
			Hostname:             host,
			AuthenticationSecret: &xdcrSecret,
		},
	}

	_, err = patchCluster(srcK8s, source, jsonpatch.NewPatchSet().Replace("/spec/xdcr/managed", true), time.Minute)
	if err != nil {
		return "", err
	}

	_, err = patchCluster(srcK8s, source, jsonpatch.NewPatchSet().Add("/spec/xdcr/remoteClusters", xdcr), time.Minute)

	return clusterName, err
}

func waitForRemoteClusterConnection(srcK8s *types.Cluster, source *couchbasev2.CouchbaseCluster, clusterName, sourceBucketName, targetBucketName string) error {
	// Wait for the operator to successfully connect before continuing.
	name := fmt.Sprintf("%s/%s/%s", clusterName, sourceBucketName, targetBucketName)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return waitForResourceEventFromNow(ctx, nil, srcK8s, source, k8sutil.ReplicationAddedEvent(source, name))
}

// establishXDCRReplicationGeneric creates a remote cluster in the source, and a replication from the source bucket to the destination
// bucket.  If the function was successful (did not return an error) then the client is responsible for defered secret cleanup.
func establishXDCRReplicationGeneric(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication) (*XDCRInfo, error) {
	// Populate the namespace manually as we don't return the API object.
	replication.Namespace = srcK8s.Namespace

	xdcrSecret, err := createRemoteClusterSecret(srcK8s, dstK8s, source, target)
	if err != nil {
		return nil, err
	}

	replicationSpec, err := srcK8s.CRClient.CouchbaseV2().CouchbaseReplications(source.Namespace).Create(context.Background(), replication, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	sourceBucketName := string(replicationSpec.Spec.Bucket)
	targetBucketName := string(replicationSpec.Spec.RemoteBucket)

	clusterName, err := createRemoteCluster(srcK8s, dstK8s, source, target, xdcrSecret)
	if err != nil {
		return nil, err
	}

	err = waitForRemoteClusterConnection(srcK8s, source, clusterName, sourceBucketName, targetBucketName)
	if err != nil {
		return nil, err
	}

	info := &XDCRInfo{
		Replication: replicationSpec,
		SecretName:  xdcrSecret,
	}

	return info, nil
}

func establishXDCRMigrationGeneric(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, migration *couchbasev2.CouchbaseMigrationReplication) (*couchbasev2.CouchbaseMigrationReplication, error) {
	// Populate the namespace manually as we don't return the API object.
	migration.Namespace = srcK8s.Namespace

	xdcrSecret, err := createRemoteClusterSecret(srcK8s, dstK8s, source, target)
	if err != nil {
		return nil, err
	}

	migrationSpec, err := srcK8s.CRClient.CouchbaseV2().CouchbaseMigrationReplications(source.Namespace).Create(context.Background(), migration, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	sourceBucketName := string(migrationSpec.Spec.Bucket)
	targetBucketName := string(migrationSpec.Spec.RemoteBucket)

	clusterName, err := createRemoteCluster(srcK8s, dstK8s, source, target, xdcrSecret)
	if err != nil {
		return nil, err
	}

	err = waitForRemoteClusterConnection(srcK8s, source, clusterName, sourceBucketName, targetBucketName)
	if err != nil {
		return nil, err
	}

	return migrationSpec, nil
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

func MustRotateXDCRReplicationPassword(t *testing.T, src *types.Cluster, dst *types.Cluster, target string) {
	secret, err := src.KubeClient.CoreV1().Secrets(src.Namespace).Get(context.Background(), target, metav1.GetOptions{})
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

func MustEstablishXDCRReplicationGeneric(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication) *XDCRInfo {
	info, err := establishXDCRReplicationGeneric(srcK8s, dstK8s, source, target, replication)
	if err != nil {
		Die(t, err)
	}

	return info
}

func MustEstablishXDCRMigrationReplicationGeneric(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseMigrationReplication) *couchbasev2.CouchbaseMigrationReplication {
	replication, err := establishXDCRMigrationGeneric(srcK8s, dstK8s, source, target, replication)
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
