package backend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
)

// tgzBackend realizes the Backend interface for a gzipped tape archive
type tgzBackend struct {
	// buffer is used to accumulate TAR data
	buffer bytes.Buffer
	// writer is the TAR writer which populates buffer
	writer *tar.Writer
}

// NewTGZ returns a new initialized TGZ backend
func NewTGZ(config *config.Configuration) (Backend, error) {
	b := &tgzBackend{}
	b.writer = tar.NewWriter(&b.buffer)
	return b, nil
}

// WriteFile buffers up the TGZ header and data
func (b *tgzBackend) WriteFile(path, data string) error {
	header := &tar.Header{
		Name: path,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := b.writer.WriteHeader(header); err != nil {
		return err
	}
	if _, err := b.writer.Write([]byte(data)); err != nil {
		return err
	}
	return nil
}

// Close closes TGZ resources, compresses the output and writes it
// to a file
func (b *tgzBackend) Close() error {
	// Stop buffering new files
	if err := b.writer.Close(); err != nil {
		return err
	}

	// Create the target file
	path := util.ArchiveName() + ".tar.gz"
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
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
