package e2eutil

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type XDCRInfo struct {
	Replication *couchbasev2.CouchbaseReplication
	SecretName  string
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

// getRemoteUUIDAndHostTLS returns the remote hostname, based on DNS, and the cluster UUID.
// Used for generic XDCR testing.
func getRemoteUUIDAndHostTLS(kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, serverTLS *TLSContext) (string, string, error) {
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

	if serverTLS != nil {
		scheme = "couchbases"
	}

	// Use an SRV lookup.
	return uuid, fmt.Sprintf("%s://%s-srv.%s?network=default", scheme, cluster.Name, cluster.Namespace), nil
}

// applyXDCRRemoteClusterName checks and amends the XDCR remote cluster name, if necessary.
func applyXDCRRemoteClusterName(clusterName string) string {
	remoteClusterOperatorManagedSuffix := "-operator-managed"
	if !strings.HasSuffix(clusterName, remoteClusterOperatorManagedSuffix) {
		clusterName += remoteClusterOperatorManagedSuffix
	}

	return clusterName
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

func createRemoteClusterGeneric(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, xdcrSecretName string) (string, error) {
	remoteClusterName := target.Name

	uuid, host, err := getRemoteUUIDAndHostGeneric(dstK8s, target)
	if err != nil {
		return "", err
	}

	remoteClusters := []couchbasev2.RemoteCluster{
		{
			Name:                 remoteClusterName,
			UUID:                 uuid,
			Hostname:             host,
			AuthenticationSecret: &xdcrSecretName,
		},
	}

	src, err := patchCluster(srcK8s, source, jsonpatch.NewPatchSet().Replace("/spec/xdcr/managed", true), time.Minute)
	if err != nil {
		return "", err
	}

	src, err = patchCluster(srcK8s, src, jsonpatch.NewPatchSet().Add("/spec/xdcr/remoteClusters", remoteClusters), time.Minute)
	*source = *src

	return remoteClusterName, err
}

func createRemoteClusterTLS(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, xdcrRemoteClusterSecretName string, serverTLS, clientTLS *TLSContext) (string, error) {
	remoteClusterName := target.Name

	uuid, host, err := getRemoteUUIDAndHostTLS(dstK8s, target, serverTLS)
	if err != nil {
		return "", err
	}

	remoteCluster := couchbasev2.RemoteCluster{
		Name:     remoteClusterName,
		UUID:     uuid,
		Hostname: host,
	}

	if serverTLS != nil {
		tlsSecret := fmt.Sprintf("%s-xdcr-tls", target.Name)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: tlsSecret,
			},
			Data: map[string][]byte{
				couchbasev2.RemoteClusterTLSCA: serverTLS.CA.Certificate,
			},
		}

		if target.Spec.Networking.TLS.ClientCertificatePolicy != nil {
			secret.Data[couchbasev2.RemoteClusterTLSCertificate] = clientTLS.ClientCert
			secret.Data[couchbasev2.RemoteClusterTLSKey] = clientTLS.ClientKey
		}

		if _, err = srcK8s.KubeClient.CoreV1().Secrets(source.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
			return "", err
		}

		remoteCluster.TLS = &couchbasev2.RemoteClusterTLS{
			Secret: &tlsSecret,
		}
	}

	// If we are not using client authentication then we need the remote username and password.
	if serverTLS == nil || target.Spec.Networking.TLS.ClientCertificatePolicy == nil {
		remoteCluster.AuthenticationSecret = &xdcrRemoteClusterSecretName
	}

	xdcr := couchbasev2.XDCR{
		Managed:        true,
		RemoteClusters: []couchbasev2.RemoteCluster{remoteCluster},
	}

	if _, err = patchCluster(srcK8s, source, jsonpatch.NewPatchSet().Replace("/spec/xdcr", xdcr), time.Minute); err != nil {
		return "", err
	}

	return remoteClusterName, nil
}

func waitForReplicationAddedEvent(srcK8s *types.Cluster, source *couchbasev2.CouchbaseCluster, clusterName, sourceBucketName, targetBucketName string) error {
	addedEvent := ReplicationAddedEvent(source, clusterName, sourceBucketName, targetBucketName)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return waitForResourceEventFromNow(ctx, nil, srcK8s, source, addedEvent)
}

func waitForReplicationRemovedEvent(srcK8s *types.Cluster, source *couchbasev2.CouchbaseCluster, clusterName, sourceBucketName, targetBucketName string) error {
	removedEvent := ReplicationRemovedEvent(source, clusterName, sourceBucketName, targetBucketName)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return waitForResourceEventFromNow(ctx, nil, srcK8s, source, removedEvent)
}

// establishXDCRReplicationGeneric creates a remote cluster in the source and a replication from the source bucket to the destination
// bucket. No TLS setup is configured for source/remote clusters.
// If the function was successful (did not return an error) then the client is responsible for defered secret cleanup.
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

	clusterName, err := createRemoteClusterGeneric(srcK8s, dstK8s, source, target, xdcrSecret)
	if err != nil {
		return nil, err
	}

	err = waitForReplicationAddedEvent(srcK8s, source, clusterName, sourceBucketName, targetBucketName)
	if err != nil {
		return nil, err
	}

	info := &XDCRInfo{
		Replication: replicationSpec,
		SecretName:  xdcrSecret,
	}

	return info, nil
}

func MustEstablishXDCRReplicationGeneric(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication) *XDCRInfo {
	info, err := establishXDCRReplicationGeneric(srcK8s, dstK8s, source, target, replication)
	if err != nil {
		Die(t, err)
	}

	return info
}

// establishXDCRReplicationWithTLS creates a remote cluster in the source, and a replication from the source bucket to the destination
// bucket. TLS setup is configured for source/remote clusters.
// If the function was successful (did not return an error) then the client is responsible for defered secret cleanup.
func establishXDCRReplicationWithTLS(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, serverTLS, clientTLS *TLSContext) (err error) {
	// Populate the namespace manually as we don't return the API object.
	replication.Namespace = srcK8s.Namespace

	// Create the remote cb cluster secret.
	xdcrRemoteClusterSecretName, err := createRemoteClusterSecret(srcK8s, dstK8s, source, target)
	if err != nil {
		return err
	}

	// Create the CouchbaseReplication in the source namepsace.
	if _, err = srcK8s.CRClient.CouchbaseV2().CouchbaseReplications(source.Namespace).Create(context.Background(), replication, metav1.CreateOptions{}); err != nil {
		return err
	}

	remoteClusterName, err := createRemoteClusterTLS(srcK8s, dstK8s, source, target, xdcrRemoteClusterSecretName, serverTLS, clientTLS)
	if err != nil {
		return err
	}

	return waitForReplicationAddedEvent(srcK8s, source, remoteClusterName, string(replication.Spec.Bucket), string(replication.Spec.RemoteBucket))
}

func MustEstablishXDCRReplicationWithTLS(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, serverTLS *TLSContext) {
	err := establishXDCRReplicationWithTLS(srcK8s, dstK8s, source, target, replication, serverTLS, serverTLS)
	if err != nil {
		Die(t, err)
	}
}

func MustEstablishXDCRReplicationWithMultipleCAs(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, serverTLS, clientTLS *TLSContext) {
	err := establishXDCRReplicationWithTLS(srcK8s, dstK8s, source, target, replication, serverTLS, clientTLS)
	if err != nil {
		Die(t, err)
	}
}

// establishXDCRMigrationReplicationGeneric creates a remote cluster in the source and a CouchbaseMigrationReplications from the source bucket to the destination
// bucket. No TLS setup is configured for source/remote clusters.
// If the function was successful (did not return an error) then the client is responsible for defered secret cleanup.
func establishXDCRMigrationReplicationGeneric(srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, migrationReplication *couchbasev2.CouchbaseMigrationReplication) (*couchbasev2.CouchbaseMigrationReplication, error) {
	// Populate the namespace manually as we don't return the API object.
	migrationReplication.Namespace = srcK8s.Namespace

	xdcrSecret, err := createRemoteClusterSecret(srcK8s, dstK8s, source, target)
	if err != nil {
		return nil, err
	}

	migrationSpec, err := srcK8s.CRClient.CouchbaseV2().CouchbaseMigrationReplications(source.Namespace).Create(context.Background(), migrationReplication, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	sourceBucketName := string(migrationSpec.Spec.Bucket)
	targetBucketName := string(migrationSpec.Spec.RemoteBucket)

	clusterName, err := createRemoteClusterGeneric(srcK8s, dstK8s, source, target, xdcrSecret)
	if err != nil {
		return nil, err
	}

	err = waitForReplicationAddedEvent(srcK8s, source, clusterName, sourceBucketName, targetBucketName)
	if err != nil {
		return nil, err
	}

	return migrationSpec, nil
}

func MustEstablishXDCRMigrationReplicationGeneric(t *testing.T, srcK8s, dstK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseMigrationReplication) *couchbasev2.CouchbaseMigrationReplication {
	replication, err := establishXDCRMigrationReplicationGeneric(srcK8s, dstK8s, source, target, replication)
	if err != nil {
		Die(t, err)
	}

	return replication
}

// deleteXDCRReplication deletes a CouchbaseReplication.
func deleteXDCRReplication(srcK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, timeout time.Duration) error {
	callback := func() error {
		return srcK8s.CRClient.CouchbaseV2().CouchbaseReplications(replication.Namespace).Delete(context.Background(), replication.Name, metav1.DeleteOptions{})
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		return err
	}

	return waitForReplicationRemovedEvent(srcK8s, source, target.Name, string(replication.Spec.Bucket), string(replication.Spec.RemoteBucket))
}

func MustDeleteXDCRReplication(t *testing.T, srcK8s *types.Cluster, source, target *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication, timeout time.Duration) {
	err := deleteXDCRReplication(srcK8s, source, target, replication, timeout)
	if err != nil {
		Die(t, err)
	}
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

func MustRotateXDCRReplicationTLS(t *testing.T, src *types.Cluster, target *couchbasev2.CouchbaseCluster, tls *TLSContext) {
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

func MustCreateReplication(t *testing.T, kubernetes *types.Cluster, src *types.Cluster, cluster *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplication) *couchbasev2.CouchbaseReplication {
	replication, err := kubernetes.CRClient.CouchbaseV2().CouchbaseReplications(cluster.Namespace).Create(context.Background(), replication, metav1.CreateOptions{})
	if err != nil {
		Die(t, err)
	}

	err = waitForReplicationAddedEvent(kubernetes, cluster, cluster.GetName(), string(replication.Spec.Bucket), string(replication.Spec.RemoteBucket))
	if err != nil {
		Die(t, err)
	}

	return replication
}
