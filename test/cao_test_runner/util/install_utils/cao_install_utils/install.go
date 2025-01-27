package caoinstallutils

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

var (
	ErrIllegalPlatform        = errors.New("illegal platform")
	ErrIllegalOperatingSystem = errors.New("illegal operating system")
	ErrIllegalArchitecture    = errors.New("illegal architecture")
	ErrIncompleteInstall      = errors.New("install is incomplete")
)

type InstallParams struct {
	buildVersion      string
	platform          managedk8sservices.PlatformType
	operatingSystem   assets.OperatingSystemType
	architecture      assets.ArchitectureType
	downloadDirectory string
}

type CAOInstallClient struct {
}

func NewInstallClient() *CAOInstallClient {
	return &CAOInstallClient{}
}

func NewInstallParams(buildVersion string, platform managedk8sservices.PlatformType,
	os assets.OperatingSystemType, arch assets.ArchitectureType, downloadDir string) (*InstallParams, error) {
	switch platform {
	case managedk8sservices.Kubernetes, managedk8sservices.Openshift:
		// No-op
	default:
		return nil, ErrIllegalPlatform
	}

	switch os {
	case assets.Linux, assets.Windows, assets.MacOS:
		// No-op
	default:
		return nil, ErrIllegalOperatingSystem
	}

	switch arch {
	case assets.AMD64, assets.ARM64:
		// No-op
	default:
		return nil, ErrIllegalArchitecture
	}

	return &InstallParams{
		buildVersion:      buildVersion,
		platform:          platform,
		operatingSystem:   os,
		architecture:      arch,
		downloadDirectory: downloadDir,
	}, nil
}

func (client *CAOInstallClient) install(installParams *InstallParams) error {
	url := client.generateURL(installParams)
	filePath := client.generateDownloadPath(installParams)

	downloadFile := fileutils.NewFile(filePath)

	err := downloadFile.CreateFile()
	if err != nil {
		return fmt.Errorf("cannot create file: %w", err)
	}

	defer downloadFile.CloseFile()

	requestParams := requestutils.NewRequestParams(url)

	err = requestParams.DownloadFile(downloadFile)
	if err != nil {
		return fmt.Errorf("cannot download file: %w", err)
	}

	err = downloadFile.ExtractFile(installParams.downloadDirectory)
	if err != nil {
		return fmt.Errorf("cannot extract file: %w", err)
	}

	return nil
}

func (client *CAOInstallClient) InstallCaoCrd(installParams *InstallParams) (string, string, error) {
	isInstallExist, err := client.isInstallAlreadyExists(installParams)
	if err != nil {
		return "", "", err
	}

	if !isInstallExist {
		err = client.install(installParams)
		if err != nil {
			return "", "", err
		}
	}

	caoPath := filepath.Join(client.generateDownloadedDirectory(installParams), "bin", "cao")
	crdPath := filepath.Join(client.generateDownloadedDirectory(installParams), "crd.yaml")

	if err := fileutils.NewFile(caoPath).ChangePermissions(0777); err != nil {
		return "", "", fmt.Errorf("unable to change cao file permissions: %w", err)
	}

	if err := fileutils.NewFile(crdPath).ChangePermissions(0777); err != nil {
		return "", "", fmt.Errorf("unable to change crd file permissions: %w", err)
	}

	return caoPath, crdPath, nil
}

func (client *CAOInstallClient) isInstallAlreadyExists(installParams *InstallParams) (bool, error) {
	fileName := client.generateFileName(installParams)
	filePath := filepath.Join(installParams.downloadDirectory, fileName)

	var isCompressedFilePresent, isDirectoryPresent bool

	isCompressedFilePresent = fileutils.NewFile(filePath).IsFileExists()

	filePath = client.generateDownloadedDirectory(installParams)
	isDirectoryPresent = fileutils.NewDirectory(filePath, 0777).IsDirectoryExists()

	if isCompressedFilePresent != isDirectoryPresent {
		return false, fmt.Errorf("file present %t, extracted directory present %t: %w", isCompressedFilePresent, isDirectoryPresent, ErrIncompleteInstall)
	} else {
		return isDirectoryPresent, nil
	}
}

func (client *CAOInstallClient) generateDownloadPath(installParams *InstallParams) string {
	fileName := client.generateFileName(installParams)
	filePath := filepath.Join(installParams.downloadDirectory, fileName)

	return filePath
}

func (client *CAOInstallClient) generateDownloadedDirectory(installParams *InstallParams) string {
	build := strings.Split(installParams.buildVersion, "-")[1]
	version := strings.Split(installParams.buildVersion, "-")[0]
	fileName := fmt.Sprintf(operatorPath,
		version, build, installParams.platform, installParams.operatingSystem, installParams.architecture, "")
	filePath := filepath.Join(installParams.downloadDirectory, fileName)

	return filePath
}

func (client *CAOInstallClient) generateFileName(installParams *InstallParams) string {
	build := strings.Split(installParams.buildVersion, "-")[1]
	version := strings.Split(installParams.buildVersion, "-")[0]

	return fmt.Sprintf(operatorPath,
		version, build, installParams.platform, installParams.operatingSystem, installParams.architecture, ext[installParams.operatingSystem])
}

func (client *CAOInstallClient) generateURL(installParams *InstallParams) string {
	build := strings.Split(installParams.buildVersion, "-")[1]
	version := strings.Split(installParams.buildVersion, "-")[0]

	fileName := client.generateFileName(installParams)

	var url string

	value, exists := releasedBuilds[build]
	if exists && value == version {
		url = releasesURL
	} else {
		url = latestBuildsURL
	}

	url = fmt.Sprintf("%s/%s/%s/%s", url, version, build, fileName)

	return url
}
