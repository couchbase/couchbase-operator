package assets

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"k8s.io/client-go/util/homedir"
)

var (
	ErrIllegalKubectlPath        = errors.New("illegal kubectl path")
	ErrIllegalKubeconfigPath     = errors.New("illegal kubeconfig path")
	ErrIllegalResultsDirectory   = errors.New("illegal results directory")
	ErrIllegalArchitecture       = errors.New("illegal architecture")
	ErrIllegalOperatingSystem    = errors.New("illegal operating system")
	ErrUnsupportedOperatorSystem = errors.New("unsupported operating system")
	ErrUnsupportedArchitecture   = errors.New("unsupported architecture")
	ErrInvalidHomeDir            = errors.New("invalid home dir")
	ErrInvalidUserProfile        = errors.New("invalid user profile")
)

type OperatingSystemType string
type ArchitectureType string

const (
	MacOS   OperatingSystemType = "macos"
	Linux   OperatingSystemType = "linux"
	Windows OperatingSystemType = "windows"
)

const (
	AMD64 ArchitectureType = "amd64"
	ARM64 ArchitectureType = "arm64"
)

type TestAssets struct {
	resultsDirectory *fileutils.Directory
	kubectlPath      *fileutils.File
	kubeconfigPath   *fileutils.File
	operatingSystem  OperatingSystemType
	architecture     ArchitectureType

	k8sClusters *K8SClusters

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
	GetOperatingSystem() *OperatingSystemType
	GetArchitecture() *ArchitectureType
	GetK8SClustersGetter() K8SClustersGetter
}

type TestAssetGetterSetter interface {
	// Getters
	GetResultsDirectory() *fileutils.Directory
	GetKubectlPath() *fileutils.File
	GetKubeconfigPath() *fileutils.File
	GetOperatingSystem() *OperatingSystemType
	GetArchitecture() *ArchitectureType
	GetK8SClustersGetterSetter() K8SClustersGetterSetter

	// Setters
	SetResultsDirectory(resultsDirectory *fileutils.Directory) error
	SetKubectlPath(kubectlPath *fileutils.File) error
	SetKubeconfigPath(kubeconfigPath *fileutils.File) error
	SetOperatingSystem(os string) error
	SetArchitecture(arch string) error
	SetK8SClusters(k8sClusters *K8SClusters) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------Test Asset Getters----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ts *TestAssets) GetResultsDirectory() *fileutils.Directory {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.resultsDirectory
}

func (ts *TestAssets) GetKubectlPath() *fileutils.File {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.kubectlPath
}

func (ts *TestAssets) GetKubeconfigPath() *fileutils.File {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.kubeconfigPath
}

func (ts *TestAssets) GetOperatingSystem() *OperatingSystemType {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return &ts.operatingSystem
}

func (ts *TestAssets) GetArchitecture() *ArchitectureType {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return &ts.architecture
}

func (ts *TestAssets) GetK8SClustersGetter() K8SClustersGetter {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.k8sClusters
}

func (ts *TestAssets) GetK8SClustersGetterSetter() K8SClustersGetterSetter {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.k8sClusters
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------Test Asset Setters----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ts *TestAssets) SetOperatingSystem(os string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	switch os {
	case string(MacOS), string(Linux), string(Windows):
		// No op
	default:
		return ErrIllegalOperatingSystem
	}

	ts.operatingSystem = OperatingSystemType(os)

	return nil
}
func (ts *TestAssets) SetArchitecture(arch string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	switch arch {
	case string(AMD64), string(ARM64):
		// No op
	default:
		return ErrIllegalArchitecture
	}

	ts.architecture = ArchitectureType(arch)

	return nil
}

func (ts *TestAssets) SetKubeconfigPath(kubeconfigPath *fileutils.File) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !kubeconfigPath.IsFileExists() {
		return ErrIllegalKubeconfigPath
	}

	kubectl.WithKubeonfigPath(kubeconfigPath.FilePath)

	ts.kubeconfigPath = kubeconfigPath
	return nil
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

func (ts *TestAssets) SetResultsDirectory(resultsDirectory *fileutils.Directory) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !resultsDirectory.IsDirectoryExists() {
		return ErrIllegalResultsDirectory
	}
	ts.resultsDirectory = resultsDirectory
	return nil
}

func (ts *TestAssets) SetK8SClusters(k8sClusters *K8SClusters) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.k8sClusters = k8sClusters
	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------Populate Test Asset Functions-----------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

// This function is used to populate test assets in the beginning with existing cluster details.
func (ts *TestAssets) PopulateTestAssets() error {
	if err := ts.PopulateOperatingSystem(); err != nil {
		return fmt.Errorf("populate test assets: %w", err)
	}

	if err := ts.PopulateArchitecture(); err != nil {
		return fmt.Errorf("populate test assets: %w", err)
	}

	if ts.GetKubectlPath() == nil {
		if err := ts.SetKubeconfigPath(fileutils.NewFile("kubectl")); err != nil {
			return fmt.Errorf("populate test assets: %w", err)
		}
	}

	if err := ts.PopulateKubeconfigPath(); err != nil {
		return fmt.Errorf("populate test assets: %w", err)
	}

	if ts.GetK8SClustersGetter() == nil {
		ts.mu.Lock()
		ts.k8sClusters = &K8SClusters{}
		ts.mu.Unlock()
	}

	if err := ts.k8sClusters.PopulateK8SClusters(ts.GetKubeconfigPath()); err != nil {
		return fmt.Errorf("populate test assets: %w", err)
	}

	return nil
}

func (ts *TestAssets) PopulateOperatingSystem() error {
	operatingSystem := runtime.GOOS

	switch operatingSystem {
	case "darwin":
		operatingSystem = string(MacOS)
	case "linux":
		operatingSystem = string(Linux)
	case "windows":
		operatingSystem = string(Windows)
	default:
		return ErrUnsupportedOperatorSystem
	}

	if err := ts.SetOperatingSystem(operatingSystem); err != nil {
		return fmt.Errorf("populate operating system: %w", err)
	}

	return nil
}

func (ts *TestAssets) PopulateArchitecture() error {
	architecture := runtime.GOARCH

	switch architecture {
	case "amd64":
		architecture = string(AMD64)
	case "arm64":
		architecture = string(ARM64)
	default:
		return ErrUnsupportedArchitecture
	}

	if err := ts.SetArchitecture(architecture); err != nil {
		return fmt.Errorf("populate architecture: %w", err)
	}

	return nil
}

func (ts *TestAssets) PopulateKubeconfigPath() error {
	if ts.kubeconfigPath != nil {
		return nil
	}

	kubeConfigPath := ""

	if envValue, ok := os.LookupEnv("KUBECONFIG"); ok {
		kubeConfigPath = envValue
	} else {
		switch ts.operatingSystem {
		case MacOS:
			homeDir := homedir.HomeDir()
			if homeDir != "" {
				kubeConfigPath = filepath.Join(homeDir, ".kube", "config")
			} else {
				return fmt.Errorf("populate kubeconfig path: %w", ErrInvalidHomeDir)
			}
		case Windows:
			userProfile := os.Getenv("USERPROFILE")
			if userProfile != "" {
				kubeConfigPath = filepath.Join(os.Getenv("USERPROFILE"), ".kube", "config")
			} else {
				return fmt.Errorf("populate kubeconfig path: %w", ErrInvalidUserProfile)
			}
		case Linux:
			homeDir := homedir.HomeDir()
			if homeDir != "" {
				kubeConfigPath = filepath.Join(homeDir, ".kube", "config")
			} else {
				return fmt.Errorf("populate kubeconfig path: %w", ErrInvalidHomeDir)
			}
		default:
			return ErrIllegalOperatingSystem
		}
	}

	if err := ts.SetKubeconfigPath(fileutils.NewFile(kubeConfigPath)); err != nil {
		return fmt.Errorf("populate kubeconfig path: %w", err)
	}

	return nil
}
