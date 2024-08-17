package installutils

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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
	BuildVersion      string
	Platform          PlatformType
	OperatingSystem   OperatingSystemType
	Architecture      ArchitectureType
	DownloadDirectory string
}

func NewInstallParams(buildVersion string, platform PlatformType, os OperatingSystemType, arch ArchitectureType, downloadDir string) (*InstallParams, error) {
	switch platform {
	case Kubernetes, Openshift:
		// No-op
	default:
		return nil, ErrIllegalPlatform
	}

	switch os {
	case Linux, Windows, MacOs:
		// No-op
	default:
		return nil, ErrIllegalOperatingSystem
	}

	switch arch {
	case Amd64, Arm64:
		// No-op
	default:
		return nil, ErrIllegalArchitecture
	}

	return &InstallParams{
		BuildVersion:      buildVersion,
		Platform:          platform,
		OperatingSystem:   os,
		Architecture:      arch,
		DownloadDirectory: downloadDir,
	}, nil
}

func (installParams *InstallParams) install() error {
	url := generateURL(installParams)
	filePath := generateDownloadPath(installParams)

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

	err = downloadFile.ExtractFile(installParams.DownloadDirectory)
	if err != nil {
		return fmt.Errorf("cannot extract file: %w", err)
	}

	return nil
}

func (installParams *InstallParams) InstallCaoCrd() (string, string, error) {
	isInstallExist, err := isInstallAlreadyExists(installParams)
	if err != nil {
		return "", "", err
	}

	if !isInstallExist {
		err = installParams.install()
		if err != nil {
			return "", "", err
		}
	}

	caoPath := filepath.Join(generateDownloadedDirectory(installParams), "bin", "cao")
	crdPath := filepath.Join(generateDownloadedDirectory(installParams), "crd.yaml")

	return caoPath, crdPath, nil
}

func isInstallAlreadyExists(installParams *InstallParams) (bool, error) {
	fileName := generateFileName(installParams)
	filePath := filepath.Join(installParams.DownloadDirectory, fileName)

	var isCompressedFilePresent, isDirectoryPresent bool

	isCompressedFilePresent = fileutils.NewFile(filePath).IsFileExists()

	filePath = generateDownloadedDirectory(installParams)
	isDirectoryPresent = fileutils.NewDirectory(filePath, 0777).IsDirectoryExists()

	if isCompressedFilePresent != isDirectoryPresent {
		return false, fmt.Errorf("file present %t, extracted directory present %t: %w", isCompressedFilePresent, isDirectoryPresent, ErrIncompleteInstall)
	} else {
		return isDirectoryPresent, nil
	}
}

func generateDownloadPath(installParams *InstallParams) string {
	fileName := generateFileName(installParams)
	filePath := filepath.Join(installParams.DownloadDirectory, fileName)

	return filePath
}

func generateDownloadedDirectory(installParams *InstallParams) string {
	build := strings.Split(installParams.BuildVersion, "-")[1]
	version := strings.Split(installParams.BuildVersion, "-")[0]
	fileName := fmt.Sprintf(operatorPath,
		version, build, installParams.Platform, installParams.OperatingSystem, installParams.Architecture, "")
	filePath := filepath.Join(installParams.DownloadDirectory, fileName)

	return filePath
}

func generateFileName(installParams *InstallParams) string {
	build := strings.Split(installParams.BuildVersion, "-")[1]
	version := strings.Split(installParams.BuildVersion, "-")[0]

	return fmt.Sprintf(operatorPath,
		version, build, installParams.Platform, installParams.OperatingSystem, installParams.Architecture, ext[installParams.OperatingSystem])
}

func generateURL(installParams *InstallParams) string {
	build := strings.Split(installParams.BuildVersion, "-")[1]
	version := strings.Split(installParams.BuildVersion, "-")[0]

	fileName := generateFileName(installParams)

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
