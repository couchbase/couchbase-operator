package command

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
)

var (
	ErrInvalidOutputArchive = fmt.Errorf("file to be verified is not a valid certification archive")
)

// verifyArchive streams through a provided .tar.gz archive, recalculating hashes
// and validating them against the embedded checksum.txt.
func verifyArchive(archiveName string) error {
	if !strings.HasSuffix(archiveName, "tar.gz") {
		return ErrInvalidOutputArchive
	}

	file, err := os.Open(archiveName)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to initialize gzip reader: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	var expectedChecksum string
	var manifest bytes.Buffer

	// Loop through each file in the archive
	// if it's a checksum file, store the value in a variable
	// for all other files, maintain a rolling hash for each file in the archive.
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		if header.Typeflag == tar.TypeDir {
			continue
		}

		// If it's the checksum file, capture its string value
		if strings.HasSuffix(header.Name, "checksum.txt") {
			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, tarReader); err != nil {
				return fmt.Errorf("failed to read checksum content: %w", err)
			}
			expectedChecksum = strings.TrimSpace(buf.String())
			continue
		}

		// For all other regular files, stream-calculate their SHA-256 hash
		hasher := sha256.New()
		if _, err := io.Copy(hasher, tarReader); err != nil {
			return fmt.Errorf("failed to hash file %s: %w", header.Name, err)
		}

		fmt.Fprintf(&manifest, "%x %s\n", hasher.Sum(nil), header.Name)
	}

	if expectedChecksum == "" {
		return fmt.Errorf("invalid archive: checksum.txt was not found")
	}

	// Calculate the master hash from the running hashes and compare against the data in checksum.txt
	masterHasher := sha256.New()
	masterHasher.Write(manifest.Bytes())
	calculatedChecksum := base64.StdEncoding.EncodeToString(masterHasher.Sum(nil))

	fmt.Println("Expected Checksum: " + expectedChecksum)
	fmt.Println("Calculated Found:  " + calculatedChecksum)

	if expectedChecksum != calculatedChecksum {
		return fmt.Errorf("verification failed: archive contents are corrupted or modified")
	}

	fmt.Println("Archive checksum is valid. All files are intact.")
	return nil
}
