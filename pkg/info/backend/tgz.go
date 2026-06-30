/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package backend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
)

// tgzBackend realizes the Backend interface for a gzipped tape archive.
type tgzBackend struct {
	// buffer is used to accumulate TAR data
	buffer bytes.Buffer
	// writer is the TAR writer which populates buffer
	writer *tar.Writer
	// directory is an optional directory to write files to
	directory string
	// manifest is the rolling list of file hashes and they're being
	// written to this backend
	manifest bytes.Buffer
}

// NewTGZ returns a new initialized TGZ backend.
func NewTGZ(config *config.Configuration) (Backend, error) {
	b := &tgzBackend{
		directory: config.Directory,
	}
	b.writer = tar.NewWriter(&b.buffer)

	return b, nil
}

// WriteFile buffers up the TGZ header and data.
func (b *tgzBackend) WriteFile(path, data string) error {
	header := &tar.Header{
		Name: path,
		Mode: 0o644,
		Size: int64(len(data)),
	}
	if err := b.writer.WriteHeader(header); err != nil {
		return err
	}

	hasher := sha256.New()
	hasher.Write([]byte(data))

	fmt.Fprintf(&b.manifest, "%x %s\n", hasher.Sum(nil), path)

	_, err := b.writer.Write([]byte(data))

	return err
}

// Close closes TGZ resources, compresses the output and writes it
// to a file.
func (b *tgzBackend) Close() error {
	if err := b.WriteMasterHash(); err != nil {
		return fmt.Errorf("error writing checksum file to the archive: %w", err)
	}
	// Stop buffering new files
	if err := b.writer.Close(); err != nil {
		return err
	}

	// Create the target file
	path := util.ArchiveName() + ".tar.gz"

	if b.directory != "" {
		path = filepath.Join(b.directory, path)
	}

	// We are only saving the archive to this file, we never read from it.
	// So open it for writing only. This also means the file path can't be
	// used to read other files on the system.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	defer file.Close()

	// Compress the buffered output, and close to finialize the footer
	lz := gzip.NewWriter(file)
	if _, err := lz.Write(b.buffer.Bytes()); err != nil {
		return err
	}

	if err := lz.Close(); err != nil {
		return err
	}

	// Notify the user
	fmt.Println("Wrote cluster information to", path)

	return nil
}

func (b *tgzBackend) WriteMasterHash() error {
	masterHasher := sha256.New()
	masterHasher.Write(b.manifest.Bytes())
	checksumStr := base64.StdEncoding.EncodeToString(masterHasher.Sum(nil))

	checksumPath := util.ArchiveName() + "/checksum.txt"
	header := &tar.Header{
		Name: checksumPath,
		Mode: 0o644,
		Size: int64(len(checksumStr)),
	}
	if err := b.writer.WriteHeader(header); err != nil {
		return err
	}
	if _, err := b.writer.Write([]byte(checksumStr)); err != nil {
		return err
	}

	fmt.Println("Results Checksum:", checksumStr)
	return nil
}
