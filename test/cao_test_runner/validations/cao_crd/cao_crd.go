package caocrdvalidator

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/cao"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/crds"
)

var (
	ErrIllegalConfig      = errors.New("illegal config, nothing to validate")
	ErrIllegalCAOPath     = errors.New("illegal cao path")
	ErrCAOVersionMismatch = errors.New("cao version mismatch")
	ErrCRDCountMismatch   = errors.New("crd count mismatch")
	ErrCRDMismatch        = errors.New("crd mismatch")
	ErrCRDVersionMismatch = errors.New("crd mismatch")
)

type CAOCRDValidator struct {
	Name            string `yaml:"name" caoCli:"required"`
	ClusterName     string `yaml:"clusterName" caoCli:"required,context"`
	State           string `yaml:"state" caoCli:"required"`
	CAOPath         string `yaml:"caoPath"`
	OperatorVersion string `yaml:"operatorVersion"`
}

func (c *CAOCRDValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	/*
		One can run this validator when using custom CAO Paths or re-using existing CAO Paths.

		If CAOPath is not provided, OperatorVersion is not provided and testAssets does not hold the CAOPath, testAssets does not hold the crds,
		Validator does not know what to validate and returns an error.

		If CAOPath is not provided, OperatorVersion is not provided and testAssets does not hold the CAOPath, testAssets does hold the crds,
		Validator validates if the crds exist as per previous state.

		If CAOPath is not provided, OperatorVersion is not provided and testAssets holds the CAOPath, testAssets does not hold the crds,
		Validator validates if the CAOPath exists as per previous state.

		If CAOPath is not provided, OperatorVersion is not provided and testAssets holds the CAOPath, testAssets holds the crds,
		Validator validates if the CAOPath exists as per previous state and crds exist as per previous state.

		If CAOPath is not provided, OperatorVersion is provided and testAssets does not hold the CAOPath, testAssets does not hold the crds,
		Validator fetches cao path from results dir, validates if it is according to the version.
		Validator also validates if the right crds are present for the version.
		Finally sets all this in testAssets.

		If CAOPath is not provided, OperatorVersion is provided and testAssets does not hold the CAOPath, testAssets does hold the crds,
		Validator fetches cao path from results dir, validates if it is according to the version and sets in testAssets
		Validator also validates if the right crds are present for the version and updates in testAssets

		If CAOPath is not provided, OperatorVersion is provided and testAssets holds the CAOPath, testAssets does not hold the crds,
		This could mean that CAOPath is getting updated with new version.
		Validator fetches cao path from results dir, validates if it is according to the version and updates in testAssets.
		Validator also validates if the right crds are present for the version and sets in testAssets.

		If CAOPath is not provided, OperatorVersion is provided and testAssets holds the CAOPath, testAssets holds the crds,
		This could mean that CAOPath is getting updated with new version.
		Validator fetches cao path from results dir, validates if it is according to the version.
		Validator also validates if the right crds are present for the version.
		Finally updates all this in testAssets.

		If CAOPath is provided, OperatorVersion is not provided and testAssets does not hold the CAOPath, testAssets does not hold the crds,
		This could mean that CAOPath is a new cao path.
		Validator validates if the new CAOPath exists and sets in testAssets.
		CRDs are not validated as they are not present in testAssets.

		If CAOPath is provided, OperatorVersion is not provided and testAssets does not hold the CAOPath, testAssets does hold the crds,
		This could mean that CAOPath is a new cao path.
		Validator validates if the new CAOPath exists and sets in testAssets.
		CRDs are validated as per previous state.

		If CAOPath is provided, OperatorVersion is not provided and testAssets holds the CAOPath, testAssets does not hold the crds,
		This could mean that CAOPath is getting updated with new cao path.
		Validator validates if the new CAOPath exists and updates in testAssets.
		Validator does not validate the crds here.

		If CAOPath is provided, OperatorVersion is not provided and testAssets holds the CAOPath, testAssets holds the crds,
		This could mean that CAOPath is getting updated with new cao path.
		Validator validates if the new CAOPath exists and crds exist as per previous state.

		If CAOPath is provided, OperatorVersion is provided and testAssets does not hold the CAOPath, testAssets does not hold the crds,
		Validator validates if provided caoPath is according to the version.
		Validator also validates if the right crds are present for the version.
		Finally sets all this in testAssets.

		If CAOPath is provided, OperatorVersion is provided and testAssets does not hold the CAOPath, testAssets does hold the crds,
		Validator validates if provided caoPath is according to the version and sets in testAssets.
		Validator also validates if the right crds are present for the version and updates in testAssets.

		If CAOPath is provided, OperatorVersion is provided and testAssets holds the CAOPath, testAssets does not hold the crds,
		This could mean that CAOPath is getting updated with new cao path.
		Validator validates if the new CAOPath exists and is according to the version provided and updates in testAssets.
		Validator also validates if the right crds are present for the version and sets in testAssets.

		If CAOPath is provided, OperatorVersion is provided and testAssets holds the CAOPath, testAssets holds the crds,
		This could mean that CAOPath is getting updated with new cao path.
		Validator validates if the new CAOPath exists and is according to the version provided.
		Validator also validates if the right crds are present for the version.
		Finally updates all this in testAssets.
	*/

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster is not present
		return fmt.Errorf("validator run: %w", err)
	}

	assetCAOPath := k8sCluster.GetCAOPath()

	assetCRDs := k8sCluster.GetAllCouchbaseCRDsGetterSetter()

	// Nothing to validate
	if c.CAOPath == "" && c.OperatorVersion == "" && assetCAOPath == nil && len(assetCRDs) == 0 {
		return ErrIllegalConfig
	}

	if c.CAOPath != "" || c.OperatorVersion != "" {
		if err := c.ValidateNewCAOPath(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	} else {
		if err := c.ValidatePreviousStateCAOPath(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	if c.OperatorVersion != "" {
		if err := c.ValidateNewCRDs(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	} else {
		if err := c.ValidatePreviousStateCRDs(testAssets); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	return nil
}

func (c *CAOCRDValidator) ValidateNewCAOPath(testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster is not present
		return fmt.Errorf("validate new cao path: %w", err)
	}

	var caoPath *fileutils.File

	if c.CAOPath != "" {
		caoPath = fileutils.NewFile(c.CAOPath)
		if !caoPath.IsFileExists() {
			if _, err := exec.LookPath(caoPath.FilePath); err != nil {
				return fmt.Errorf("validate new cao path: %w", ErrIllegalCAOPath)
			}
		}
	} else {
		// Fetch cao path from results dir according to version
		build := strings.Split(c.OperatorVersion, "-")[1]

		version := strings.Split(c.OperatorVersion, "-")[0]

		fileName := fmt.Sprintf(operatorPath, version, build,
			k8sCluster.GetServiceProvider().GetPlatform(),
			*testAssets.GetOperatingSystem(),
			*testAssets.GetArchitecture(), "")

		filePath := filepath.Join(testAssets.GetResultsDirectory().DirectoryPath, fileName)

		caoPath = fileutils.NewFile(filepath.Join(filePath, "bin", "cao"))
	}

	cao.WithBinaryPath(caoPath.FilePath)

	if c.OperatorVersion != "" {
		out, _, err := cao.GetVersion().ExecWithOutputCapture()
		if err != nil {
			return fmt.Errorf("validate new cao path: %w", err)
		}

		build := strings.Split(c.OperatorVersion, "-")[1]
		version := strings.Split(c.OperatorVersion, "-")[0]

		if !strings.Contains(out, fmt.Sprintf("cao %s (build %s", version, build)) {
			return fmt.Errorf("validate new cao path: %w", ErrCAOVersionMismatch)
		}
	}

	if err := k8sCluster.SetCAOPath(caoPath); err != nil {
		return fmt.Errorf("validate new cao path: %w", err)
	}

	return nil
}

func (c *CAOCRDValidator) ValidatePreviousStateCAOPath(testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster is not present
		return fmt.Errorf("validate previous state cao path: %w", err)
	}

	assetCAOPath := k8sCluster.GetCAOPath()
	if assetCAOPath == nil {
		// Nothing to validate
		return nil
	}

	if !assetCAOPath.IsFileExists() {
		if _, err := exec.LookPath(assetCAOPath.FilePath); err != nil {
			return fmt.Errorf("validate previous state cao path: %w", ErrIllegalCAOPath)
		}
	}

	return nil
}

func (c *CAOCRDValidator) ValidateNewCRDs(testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster is not present
		return fmt.Errorf("validate new crds: %w", err)
	}

	var crdsToValidate []string

	version := strings.Split(c.OperatorVersion, "-")[0]
	crdsToValidate = versionCRDs[version]

	allCRDs, err := crds.GetCRDNames()
	if err != nil && !errors.Is(err, crds.ErrNoCRDsInCluster) {
		return fmt.Errorf("validate new crds: %w", err)
	}

	if len(allCRDs) != len(crdsToValidate) {
		return fmt.Errorf("validate new crds: %w", ErrCRDCountMismatch)
	}

	for _, crdName := range crdsToValidate {
		if !contains(allCRDs, crdName) {
			return fmt.Errorf("validate new crds: %w", ErrCRDMismatch)
		}

		crd, err := crds.GetCRD(crdName)
		if err != nil {
			return fmt.Errorf("validate new crds: %w", err)
		}

		if crd.Metadata.Annotations["config.couchbase.com/version"] != version {
			return fmt.Errorf("validate new crds: %w", ErrCRDVersionMismatch)
		}

		if err := k8sCluster.SetCouchbaseCRD(assets.NewCouchbaseCRD(crdName, version)); err != nil {
			return fmt.Errorf("validate new crds: %w", err)
		}
	}

	return nil
}

func (c *CAOCRDValidator) ValidatePreviousStateCRDs(testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster is not present
		return fmt.Errorf("validate previous state crds: %w", err)
	}

	assetCRDs := k8sCluster.GetAllCouchbaseCRDsGetterSetter()

	if len(assetCRDs) == 0 {
		// Nothing to validate
		return nil
	}

	allCRDs, err := crds.GetCRDNames()
	if err != nil && !errors.Is(err, crds.ErrNoCRDsInCluster) {
		return fmt.Errorf("validate previous state crds: %w", err)
	}

	if len(allCRDs) != len(assetCRDs) {
		return fmt.Errorf("validate previous state crds: %w", ErrCRDCountMismatch)
	}

	for _, crd := range assetCRDs {
		crdName := crd.GetCRDName()
		crdVersion := crd.GetVersion()

		crd, err := crds.GetCRD(crdName)
		if err != nil {
			return fmt.Errorf("validate previous state crds: %w", err)
		}

		if crd.Metadata.Annotations["config.couchbase.com/version"] != crdVersion {
			return fmt.Errorf("validate previous state crds: %w", ErrCRDVersionMismatch)
		}
	}

	return nil
}

func (c *CAOCRDValidator) GetState() string {
	return c.State
}

func contains(array []string, str string) bool {
	for _, item := range array {
		if item == str {
			return true
		}
	}

	return false
}
