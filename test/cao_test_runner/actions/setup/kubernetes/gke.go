package setupkubernetes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"k8s.io/client-go/tools/clientcmd/api"
)

type CreateGKECluster struct {
	ClusterName       string
	Region            string
	KubernetesVersion string
	MachineType       string
	ImageType         string
	DiskType          string
	DiskSize          int
	NumNodePools      int
	Count             int
	ReleaseChannel    managedk8sservices.ReleaseChannel
	KubeConfigPath    string
}

const (
	ipCidrRange = "10.0.0.0/8"
)

var (
	ErrKubeConfigFileInvalid   = errors.New("kubeconfig file does not exist")
	ErrGKECountInvalid         = errors.New("for environment type 'cloud' and provider 'googleCloud', Count must be greater than 0")
	ErrGKENumNodePoolsInvalid  = errors.New("for environment type 'cloud' and provider 'googleCloud', NumNodePools must be greater than 0")
	ErrGKEDiskSizeInvalid      = errors.New("for environment type 'cloud' and provider 'googleCloud', DiskSize must be greater than 0")
	ErrGKEClusterAlreadyExists = errors.New("aks cluster already exists")
)

func (cgc *CreateGKECluster) CreateCluster(ctx *context.Context) error {
	if err := cgc.ValidateParams(ctx); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials([]managedk8sservices.ManagedServiceProvider{managedk8sservices.GKEManagedService}, cgc.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(managedk8sservices.GKEManagedService)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set gke session: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get gke session: %w", err)
	}

	networkName := cgc.ClusterName + "-network"
	if err := gkeSession.CreateVirtualNetwork(ctx, networkName, false); err != nil {
		return fmt.Errorf("error creating virtual network %s: %w", networkName, err)
	}

	logrus.Info(fmt.Sprintf("Created virtual network %s", networkName))

	firewallRuleName := cgc.ClusterName + "-firewall"
	if err := gkeSession.CreateFirewallRule(ctx, firewallRuleName, networkName); err != nil {
		return fmt.Errorf("error creating firewall rule %s in network %s: %w", firewallRuleName, networkName, err)
	}

	logrus.Info(fmt.Sprintf("Created firewall rule %s in virtual network %s", firewallRuleName, networkName))

	subnetName := cgc.ClusterName + "-subnet"
	if err := gkeSession.CreateSubnet(ctx, subnetName, networkName, ipCidrRange); err != nil {
		return fmt.Errorf("error creating subnet %s in network %s: %w", subnetName, networkName, err)
	}

	logrus.Info(fmt.Sprintf("Created subnet %s in virtual network %s", subnetName, networkName))

	nodePoolName := cgc.ClusterName + "nodepool-0"
	if err := gkeSession.CreateCluster(ctx, networkName, subnetName, cgc.MachineType, cgc.ImageType,
		cgc.DiskType, cgc.KubernetesVersion, nodePoolName, cgc.DiskSize, cgc.Count, cgc.ReleaseChannel); err != nil {
		return fmt.Errorf("error creating cluster %s: %w", cgc.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("Created cluster %s with 1 node pool %s", cgc.ClusterName, nodePoolName))

	for i := 1; i < cgc.NumNodePools; i++ {
		nodePoolName = fmt.Sprintf("%s-nodepool-%d", cgc.ClusterName, i)

		if err := gkeSession.CreateNodePool(ctx, nodePoolName, cgc.MachineType, cgc.DiskType, cgc.ImageType, cgc.DiskSize, cgc.Count); err != nil {
			return fmt.Errorf("error creating node pool %s: %w", nodePoolName, err)
		}

		logrus.Info(fmt.Sprintf("Created node pool %s for cluster %s", nodePoolName, cgc.ClusterName))
	}

	cluster, err := gkeSession.GetCluster(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch cluster %s details: %w", cgc.ClusterName, err)
	}

	if err := cgc.updateKubeconfig(cluster); err != nil {
		return fmt.Errorf("error updating kubeconfig of cluster %s: %w", cgc.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("Updated kubeconfig with cluster %s details", cgc.ClusterName))

	currentFile, err := os.Executable()
	if err != nil {
		return err
	}

	googleStorageClassPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "test_data", "cloud_storage_class", "cao-googlefile.yaml")
	if err := kubectl.ApplyFiles(googleStorageClassPath).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to apply google storage class path %s: %w", googleStorageClassPath, err)
	}

	logrus.Info("Google storage class yaml applied")

	return nil
}

func (cgc *CreateGKECluster) ValidateParams(ctx *context.Context) error {
	if cgc.NumNodePools <= 0 {
		return ErrGKENumNodePoolsInvalid
	}

	if cgc.DiskSize <= 0 {
		return ErrGKEDiskSizeInvalid
	}

	if cgc.Count <= 0 {
		return ErrGKECountInvalid
	}

	if ok, err := managedk8sservices.ValidateReleaseChannel(cgc.ReleaseChannel); !ok || err != nil {
		return fmt.Errorf("invalid release channel %s: %w", cgc.ReleaseChannel, err)
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials([]managedk8sservices.ManagedServiceProvider{managedk8sservices.GKEManagedService}, cgc.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(managedk8sservices.GKEManagedService)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set gke session: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get gke session: %w", err)
	}

	if _, err := gkeSession.GetCluster(ctx); err == nil {
		return fmt.Errorf("cluster already exists: %w", ErrGKEClusterAlreadyExists)
	}

	return nil
}

func (cgc *CreateGKECluster) updateKubeconfig(cluster *containerpb.Cluster) error {
	apiServer := cluster.Endpoint
	caCertificate := cluster.MasterAuth.ClusterCaCertificate
	clientCertificate := cluster.MasterAuth.ClientCertificate
	clientKey := cluster.MasterAuth.ClientKey

	caCertBytes, err := base64.StdEncoding.DecodeString(caCertificate)
	if err != nil {
		return fmt.Errorf("failed to decode CA certificate: %w", err)
	}

	clientCertBytes, err := base64.StdEncoding.DecodeString(clientCertificate)
	if err != nil {
		return fmt.Errorf("failed to decode client certificate: %w", err)
	}

	clientKeyBytes, err := base64.StdEncoding.DecodeString(clientKey)
	if err != nil {
		return fmt.Errorf("failed to decode client key: %w", err)
	}

	var kubeconfig api.Config

	kubeConfigFile := fileutils.NewFile(cgc.KubeConfigPath)

	if ok := kubeConfigFile.IsFileExists(); !ok {
		return fmt.Errorf("kubeconfig file %s does not exist: %w", cgc.KubeConfigPath, ErrKubeConfigFileInvalid)
	}

	data, err := kubeConfigFile.ReadFile()
	if err != nil {
		return fmt.Errorf("unable to read kubeconfig file %s: %w", cgc.KubeConfigPath, err)
	}

	if err := yaml.Unmarshal(data, &kubeconfig); err != nil {
		return fmt.Errorf("failed to unmarshal existing kubeconfig: %w", err)
	}

	contextName := fmt.Sprintf("gke_%s_%s", cgc.Region, cgc.ClusterName)
	kubeconfig.Clusters[contextName] = &api.Cluster{
		Server:                   fmt.Sprintf("https://%s", apiServer),
		CertificateAuthorityData: caCertBytes,
	}
	kubeconfig.Contexts[contextName] = &api.Context{
		Cluster:  contextName,
		AuthInfo: contextName,
	}
	kubeconfig.AuthInfos[contextName] = &api.AuthInfo{
		ClientCertificateData: clientCertBytes,
		ClientKeyData:         clientKeyBytes,
	}
	kubeconfig.CurrentContext = contextName

	data, err = yaml.Marshal(&kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to marshal updated kubeconfig: %w", err)
	}

	if err := kubeConfigFile.WriteFile(data, 0600); err != nil {
		return fmt.Errorf("failed to write to kubeconfig file: %w", err)
	}

	return nil
}
