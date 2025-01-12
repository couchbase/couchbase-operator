package assets

import (
	"errors"
	"os/exec"
	"sync"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
)

var (
	ErrIllegalKubectlPath      = errors.New("illegal kubectl path")
	ErrIllegalKubeconfigPath   = errors.New("illegal kubeconfig path")
	ErrIllegalResultsDirectory = errors.New("illegal results directory")
)

type TestAssets struct {
	resultsDirectory *fileutils.Directory
	kubectlPath      *fileutils.File
	kubeconfigPath   *fileutils.File
	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewTestAssets() *TestAssets {
	return &TestAssets{}
}

type TestAssetGetter interface {
	GetResultsDirectory() *fileutils.Directory
	GetKubectlPath() *fileutils.File
	GetKubeconfigPath() *fileutils.File
}

type TestAssetGetterSetter interface {
	// Getters
	GetResultsDirectory() *fileutils.Directory
	GetKubectlPath() *fileutils.File
	GetKubeconfigPath() *fileutils.File

	// Setters
	SetResultsDirectory(resultsDirectory *fileutils.Directory) error
	SetKubectlPath(kubectlPath *fileutils.File) error
	SetKubeconfigPath(kubeconfigPath *fileutils.File) error
}

func (ts *TestAssets) GetResultsDirectory() *fileutils.Directory {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.resultsDirectory
}

func (ts *TestAssets) SetResultsDirectory(resultsDirectory *fileutils.Directory) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !resultsDirectory.IsDirectoryExists() {
		return ErrIllegalResultsDirectory
	}
	ts.resultsDirectory = resultsDirectory
	return nil
}

func (ts *TestAssets) GetKubectlPath() *fileutils.File {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.kubectlPath
}

func (ts *TestAssets) SetKubectlPath(kubectlPath *fileutils.File) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !kubectlPath.IsFileExists() {
		if _, err := exec.LookPath(kubectlPath.FilePath); err != nil {
			return ErrIllegalKubectlPath
		}
	}

	kubectl.WithBinaryPath(kubectlPath.FilePath)
	ts.kubectlPath = kubectlPath
	return nil
}

func (ts *TestAssets) GetKubeconfigPath() *fileutils.File {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.kubectlPath
}

func (ts *TestAssets) SetKubeconfigPath(kubeconfigPath *fileutils.File) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !kubeconfigPath.IsFileExists() {
		return ErrIllegalKubeconfigPath
	}

	ts.kubeconfigPath = kubeconfigPath
	return nil
}

// This function is used to populate test assets in the beginning with existing cluster details.
func (ts *TestAssets) PopulateTestAssets() error {
	return nil
}
