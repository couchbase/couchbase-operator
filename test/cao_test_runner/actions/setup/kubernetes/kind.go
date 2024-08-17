package setupkubernetes

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	kind "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kind"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
)

const kindConfigTemplate = `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
{{- range .Nodes }}
  - role: {{ .Role }}
{{- end }}
`

var (
	ErrClusterAlreadyExists = errors.New("cluster already exists")
)

type CreateKindCluster struct {
	ClusterName              string
	NumControlPlane          int
	NumWorkers               int
	ConfigDirectory          string
	ConfigPath               string
	OperatorImage            string
	AdmissionControllerImage string
}

type Node struct {
	Role string `yaml:"role"`
}

type KindConfig struct {
	Kind       string `yaml:"kind"`
	APIVersion string `yaml:"apiVersion"`
	Nodes      []Node `yaml:"nodes"`
}

func contains(array []string, str string) bool {
	for _, item := range array {
		if item == str {
			return true
		}
	}

	return false
}

func (ckc *CreateKindCluster) CreateCluster() error {
	if err := ckc.ValidateParams(); err != nil {
		return err
	}

	// TODO : Add this onto result directory instead of ./tmp
	directory := fileutils.NewDirectory(ckc.ConfigDirectory, 0777)
	if !directory.IsDirectoryExists() {
		if err := directory.CreateDirectory(); err != nil {
			return fmt.Errorf("error creating directory: %w", err)
		}
	}

	ckc.ConfigPath = filepath.Join(directory.DirectoryPath, fmt.Sprintf("kind-cluster-%s.yaml", time.Now().Format(time.RFC3339)))

	out, _, err := kind.GetClusters().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("cannot fetch clusters in kind environment: %w", err)
	}

	allClusters := strings.Split(out, "\n")
	if contains(allClusters, ckc.ClusterName) {
		return fmt.Errorf("cluster %s already exists: %w", ckc.ClusterName, ErrClusterAlreadyExists)
	}

	if err = ckc.generateKindConfig(); err != nil {
		return fmt.Errorf("unable to generate kind config: %w", err)
	}

	if err = kind.Create(ckc.ConfigPath, "", "", ckc.ClusterName, "120s", true).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to create cluster %s: %w", ckc.ClusterName, err)
	}

	if err = kind.LoadDockerImage(ckc.OperatorImage, ckc.ClusterName, nil).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to load operator image into cluster %s: %w", ckc.ClusterName, err)
	}

	if err = kind.LoadDockerImage(ckc.AdmissionControllerImage, ckc.ClusterName, nil).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to load admission controller image into cluster %s: %w", ckc.ClusterName, err)
	}

	return nil
}

func (ckc *CreateKindCluster) ValidateParams() error {
	if ckc.NumControlPlane < 0 {
		return fmt.Errorf("for environment type 'kind', numControlPlane must be present in the YAML configuration")
	}

	if ckc.NumWorkers < 0 {
		return fmt.Errorf("for environment type 'kind', numWorker must be present in the YAML configuration")
	}
	// if _, err := os.Stat(ckc.ConfigDirectory); err != nil {
	// 	return fmt.Errorf("the directory %s cannot be accessed: %w", ckc.ConfigDirectory, err)
	// }
	return nil
}

func (ckc *CreateKindCluster) generateKindConfig() error {
	nodes := []Node{}

	for i := 0; i < ckc.NumControlPlane; i++ {
		nodes = append(nodes, Node{Role: "control-plane"})
	}

	for i := 0; i < ckc.NumWorkers; i++ {
		nodes = append(nodes, Node{Role: "worker"})
	}

	kindConfig := KindConfig{
		Kind:       "Cluster",
		APIVersion: "kind.x-k8s.io/v1alpha4",
		Nodes:      nodes,
	}

	tmpl, err := template.New("kindConfig").Parse(kindConfigTemplate)
	if err != nil {
		return fmt.Errorf("error creating template: %w", err)
	}

	file := fileutils.NewFile(ckc.ConfigPath)
	if err = file.CreateFile(); err != nil {
		return fmt.Errorf("unable to create file %s: %w", ckc.ConfigPath, err)
	}

	defer file.CloseFile()

	err = tmpl.Execute(file.OsFile, kindConfig)
	if err != nil {
		return fmt.Errorf("error writing to %s: %w", ckc.ConfigPath, err)
	}

	return nil
}
