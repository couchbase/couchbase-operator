package setupkubernetes

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type CreateGKECluster struct {
	ClusterName            string
	Region                 string
	KubernetesVersion      string
	GKENodePools           []*GKENodePool
	ReleaseChannel         managedk8sservices.ReleaseChannel
	KubeConfigPath         *fileutils.File
	ManagedServiceProvider *managedk8sservices.ManagedServiceProvider
}

type GKENodePool struct {
	NumNodesPerZone int    `yaml:"numNodesPerZone"` // 3 Zones are there for GKE
	MachineType     string `yaml:"machineType"`
	ImageType       string `yaml:"imageType"`
	DiskType        string `yaml:"diskType"`
	DiskSize        int    `yaml:"diskSize"`
}

const (
	ipCidrRange = "10.16.0.0/12"
)

var (
	ErrKubeConfigFileInvalid         = errors.New("kubeconfig file does not exist")
	ErrGKEDefaultNodePoolNotProvided = errors.New("default node pool not provided")
	ErrGKECountInvalid               = errors.New("for environment type 'cloud' and provider 'gcp', Count must be greater than 0")
	ErrGKEDiskSizeInvalid            = errors.New("for environment type 'cloud' and provider 'gcp', DiskSize must be greater than 0")
	ErrGKEClusterAlreadyExists       = errors.New("aks cluster already exists")
	ErrInvalidKubernetesVersion      = errors.New("for environment type 'cloud' and provider 'gcp', kubernetes version is invalid")
)

func (cgc *CreateGKECluster) CreateCluster(ctx context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{cgc.ManagedServiceProvider}, cgc.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(cgc.ManagedServiceProvider)
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

	logrus.Infof("Created virtual network %s", networkName)

	firewallRuleName := cgc.ClusterName + "-firewall"
	if err := gkeSession.CreateFirewallRule(ctx, firewallRuleName, networkName); err != nil {
		return fmt.Errorf("error creating firewall rule %s in network %s: %w", firewallRuleName, networkName, err)
	}

	logrus.Infof("Created firewall rule %s in virtual network %s", firewallRuleName, networkName)

	subnetName := cgc.ClusterName + "-subnet"
	if err := gkeSession.CreateSubnet(ctx, subnetName, networkName, ipCidrRange, true); err != nil {
		return fmt.Errorf("error creating subnet %s in network %s: %w", subnetName, networkName, err)
	}

	logrus.Infof("Created subnet %s in virtual network %s", subnetName, networkName)

	nodePoolName := cgc.ClusterName + "-np-0"
	if err := gkeSession.CreateCluster(ctx, networkName, subnetName, cgc.GKENodePools[0].MachineType, cgc.GKENodePools[0].ImageType,
		cgc.GKENodePools[0].DiskType, cgc.KubernetesVersion, nodePoolName, cgc.GKENodePools[0].DiskSize, cgc.GKENodePools[0].NumNodesPerZone, cgc.ReleaseChannel); err != nil {
		return fmt.Errorf("error creating cluster %s: %w", cgc.ClusterName, err)
	}

	logrus.Infof("Created cluster %s with default node pool %s", cgc.ClusterName, nodePoolName)

	for i := 1; i < len(cgc.GKENodePools); i++ {
		nodePoolName = fmt.Sprintf("%s-np-%d", cgc.ClusterName, i)

		if err := gkeSession.CreateNodePool(ctx, nodePoolName, cgc.GKENodePools[i].MachineType, cgc.GKENodePools[i].DiskType,
			cgc.GKENodePools[i].ImageType, cgc.GKENodePools[i].DiskSize, cgc.GKENodePools[i].NumNodesPerZone); err != nil {
			return fmt.Errorf("error creating node pool %s: %w", nodePoolName, err)
		}

		logrus.Infof("Created node pool %s for cluster %s", nodePoolName, cgc.ClusterName)
	}

	cluster, err := gkeSession.GetCluster(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch cluster %s details: %w", cgc.ClusterName, err)
	}

	if err := cgc.updateKubeconfig(cluster); err != nil {
		return fmt.Errorf("error updating kubeconfig of cluster %s: %w", cgc.ClusterName, err)
	}

	logrus.Infof("Updated kubeconfig with cluster %s details", cgc.ClusterName)

	_, filePath, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("unable to fetch current file path: %w", ErrCurrentFileFetchFail)
	}

	currentFile, _ := filepath.Abs(filePath)

	googleStorageClassPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "test_data", "cloud_storage_class", "cao-googlefile.yaml")

	if err := kubectl.ApplyFiles(googleStorageClassPath).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to apply google storage class path %s: %w", googleStorageClassPath, err)
	}

	logrus.Info("Google storage class yaml applied")

	return nil
}

func (cgc *CreateGKECluster) ValidateParams(ctx context.Context) error {
	if len(cgc.GKENodePools) == 0 {
		return fmt.Errorf("validate create gke params: %w", ErrGKEDefaultNodePoolNotProvided)
	}

	for _, nodePool := range cgc.GKENodePools {
		if nodePool.NumNodesPerZone <= 0 {
			return fmt.Errorf("validate create gke params: %w", ErrGKECountInvalid)
		}

		if nodePool.DiskSize <= 0 {
			return fmt.Errorf("validate create gke params: %w", ErrGKEDiskSizeInvalid)
		}
	}

	if ok, err := managedk8sservices.ValidateReleaseChannel(cgc.ReleaseChannel); !ok || err != nil {
		return fmt.Errorf("invalid release channel %s: %w", cgc.ReleaseChannel, err)
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{cgc.ManagedServiceProvider}, cgc.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(cgc.ManagedServiceProvider)
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

	validKubernetesVersions, err := gkeSession.ListAvailableKubernetesVersions(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch valid kubernetes versions: %w", err)
	}

	var possibleVersions []string

	for _, version := range validKubernetesVersions {
		if strings.Split(version, "-")[0] == cgc.KubernetesVersion {
			possibleVersions = append(possibleVersions, version)
		}
	}

	if len(possibleVersions) == 0 {
		logrus.Warn("Available K8S Versions:\n", validKubernetesVersions)
		return fmt.Errorf("invalid kubernetes version, not available in GKE: %w", ErrInvalidKubernetesVersion)
	}

	highestBuild := 0

	var highestVersion string

	for _, version := range possibleVersions {
		build := strings.Split(strings.Split(version, "-")[1], ".")[1]

		buildNumber, err := strconv.Atoi(build)
		if err != nil {
			return fmt.Errorf("unable to convert %s to int: %w", build, err)
		}

		if buildNumber > highestBuild {
			highestBuild = buildNumber
			highestVersion = version
		}
	}

	cgc.KubernetesVersion = highestVersion

	if !cgc.KubeConfigPath.IsFileExists() {
		return fmt.Errorf("kubeconfig path %s does not exist: %w", cgc.KubeConfigPath.FilePath, ErrKubeconfigFileInvalid)
	}

	return nil
}

func (cgc *CreateGKECluster) updateKubeconfig(cluster *containerpb.Cluster) error {
	apiServer := cluster.Endpoint
	caCertificate := cluster.MasterAuth.ClusterCaCertificate

	var kubeconfig map[string]interface{}

	kubeconfigBytes, err := cgc.KubeConfigPath.ReadFile()
	if err != nil {
		return fmt.Errorf("unable to read kubeconfig file %s: %w", cgc.KubeConfigPath.FilePath, err)
	}

	if err := yaml.Unmarshal(kubeconfigBytes, &kubeconfig); err != nil {
		return fmt.Errorf("failed to unmarshal existing kubeconfig: %w", err)
	}

	contextName := fmt.Sprintf("gke_%s_%s", cgc.Region, cgc.ClusterName)

	clusters, ok := kubeconfig["clusters"].([]interface{})
	if !ok {
		clusters = make([]interface{}, 0)
	}

	contexts, ok := kubeconfig["contexts"].([]interface{})
	if !ok {
		contexts = make([]interface{}, 0)
	}

	users, ok := kubeconfig["users"].([]interface{})
	if !ok {
		users = make([]interface{}, 0)
	}

	clusters = append(clusters, map[interface{}]interface{}{
		"name": cgc.ClusterName,
		"cluster": map[interface{}]interface{}{
			"server":                     fmt.Sprintf("https://%s", apiServer),
			"certificate-authority-data": caCertificate,
		},
	})

	contexts = append(contexts, map[interface{}]interface{}{
		"name": contextName,
		"context": map[interface{}]interface{}{
			"cluster": cgc.ClusterName,
			"user":    contextName,
		},
	})

	users = append(users, map[interface{}]interface{}{
		"name": contextName,
		"user": map[interface{}]interface{}{
			"exec": map[interface{}]interface{}{
				"apiVersion": "client.authentication.k8s.io/v1beta1",
				"command":    "gke-gcloud-auth-plugin",
				"installHint": "Install gke-gcloud-auth-plugin for use with kubectl by following " +
					"https://cloud.google.com/blog/products/containers-kubernetes/kubectl-auth-changes-in-gke",
				"provideClusterInfo": true,
			},
		},
	})

	kubeconfig["clusters"] = clusters
	kubeconfig["contexts"] = contexts
	kubeconfig["users"] = users
	kubeconfig["current-context"] = contextName

	data, err := yaml.Marshal(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to marshal updated kubeconfig: %w", err)
	}

	if err := cgc.KubeConfigPath.WriteFile(data, 0600); err != nil {
		return fmt.Errorf("failed to write to kubeconfig file: %w", err)
	}

	return nil
}
