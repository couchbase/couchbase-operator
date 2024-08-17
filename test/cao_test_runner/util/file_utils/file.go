package fileutils

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrNotImplemented = errors.New("not implemented error")
	ErrUnknownType    = errors.New("unknown type")
	ErrFileExists     = errors.New("file already exists")
	ErrDirExists      = errors.New("directory already exists")
	ErrFileNotOpened  = errors.New("file not open")
)

type File struct {
	Name      string
	FilePath  string
	Extension string
	OsFile    *os.File
}

type Directory struct {
	Name          string
	DirectoryPath string
	Permissions   fs.FileMode
}

func NewFile(filePath string) *File {
	return &File{
		Name:      filepath.Base(filePath),
		FilePath:  filePath,
		Extension: filepath.Ext(filePath),
	}
}

func NewDirectory(directoryPath string, permissions fs.FileMode) *Directory {
	return &Directory{
		Name:          filepath.Base(directoryPath),
		DirectoryPath: directoryPath,
		Permissions:   permissions,
	}
}

func (file *File) CloseFile() {
	if file.OsFile == nil {
		return
	}

	file.OsFile.Close()
}

func (file *File) CreateFile() error {
	if file.IsFileExists() {
		return fmt.Errorf("file:%s already exists: %w", file.FilePath, ErrFileExists)
	}

	fileP, err := os.Create(file.FilePath)

	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	file.OsFile = fileP

	return nil
}

func (file *File) OpenFile(flag int, perm fs.FileMode) error {
	if file.OsFile != nil {
		defer file.OsFile.Close()
	}

	fileP, err := os.OpenFile(file.FilePath, flag, perm)
	if err != nil {
		return err
	}

	file.OsFile = fileP

	return nil
}

func (file *File) IsFileExists() bool {
	_, err := os.Stat(file.FilePath)
	if err != nil {
		return false
	}

	return true
}

func (directory *Directory) IsDirectoryExists() bool {
	if _, err := os.Stat(directory.DirectoryPath); err != nil {
		return false
	}

	return true
}

func (dir *Directory) CreateDirectory() error {
	if dir.IsDirectoryExists() {
		return fmt.Errorf("directory:%s already exists: %w", dir.DirectoryPath, ErrDirExists)
	}

	if err := os.MkdirAll(dir.DirectoryPath, dir.Permissions); err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	return nil
}

func (file *File) extractFileGz(outputDir string) error {
	gzFile, err := os.Open(file.FilePath)
	if err != nil {
		return fmt.Errorf("error opening .gz file: %w", err)
	}
	defer gzFile.Close()

	gzReader, err := gzip.NewReader(gzFile)
	if err != nil {
		return fmt.Errorf("error creating gzip reader: %w", err)
	}
	defer gzReader.Close()

	if strings.HasSuffix(file.FilePath, ".tar.gz") {
		tarReader := tar.NewReader(gzReader)

		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}

			if err != nil {
				return fmt.Errorf("error reading tar archive: %w", err)
			}

			fullPath := filepath.Join(outputDir, header.Name)

			switch header.Typeflag {
			case tar.TypeDir:
				err := os.MkdirAll(fullPath, 0755)
				if err != nil {
					return fmt.Errorf("failed to create file: %w", err)
				}
			case tar.TypeReg:
				outFile, err := os.Create(fullPath)
				if err != nil {
					return fmt.Errorf("error creating file: %w", err)
				}
				defer outFile.Close()

				_, err = io.Copy(outFile, tarReader)
				if err != nil {
					return fmt.Errorf("error extracting file: %w", err)
				}
			default:
				return fmt.Errorf("unable to extract %s, unknown type: %c : %w", header.Name, header.Typeflag, ErrUnknownType)
			}
		}
	} else {
		return ErrNotImplemented
	}

	return nil
}

func (file *File) extractFileZip(outputDir string) error {
	zipFile, err := os.Open(file.FilePath)
	if err != nil {
		return fmt.Errorf("error opening .zip file: %w", err)
	}
	defer zipFile.Close()

	stat, err := zipFile.Stat()
	if err != nil {
		return fmt.Errorf("error getting file info: %w", err)
	}

	zipReader, err := zip.NewReader(zipFile, stat.Size())
	if err != nil {
		return fmt.Errorf("error creating zip reader: %w", err)
	}

	for _, file := range zipReader.File {
		fullPath := filepath.Join(outputDir, file.Name)
		if file.FileInfo().IsDir() {
			err = os.MkdirAll(fullPath, file.Mode())
			if err != nil {
				return fmt.Errorf("error creating directory: %w", err)
			}

			continue
		}

		if err := os.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
			return fmt.Errorf("error creating directories: %w", err)
		}

		zippedFile, err := file.Open()
		if err != nil {
			return fmt.Errorf("error opening zipped file: %w", err)
		}
		defer zippedFile.Close()

		destFile, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return fmt.Errorf("error creating destination file: %w", err)
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, zippedFile)
		if err != nil {
			return fmt.Errorf("error extracting file: %w", err)
		}
	}

	return nil
}

func (file *File) ExtractFile(outputDir string) error {
	switch file.Extension {
	case ".gz":
		return file.extractFileGz(outputDir)
	case ".zip":
		return file.extractFileZip(outputDir)
	default:
		return ErrNotImplemented
	}
}
