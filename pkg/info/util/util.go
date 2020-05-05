package util

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	Application = "cbopinfo"
)

var (
	// timestamp caches the timestamp returned by Timestamp()
	timestamp = ""
	// salt caches the uuid used to salt redacted logs
	salt = ""
	// saltMutex is used to avoid concurrent updates
	saltMutex = &sync.Mutex{}
)

// Timestamp returns an ISO8601 formatted timestamp suitable for
// use in generated file names.  These include the time zone for ease of
// use by end users.  This is a 'singleton' style function whose subsequent
// invocations return the same result.
func Timestamp() string {
	if timestamp != "" {
		return timestamp
	}

	format := "20060102T150405-0700"
	timestamp = time.Now().Format(format)

	return timestamp
}

// Salt returns a UUID used on a per-run basis for redacting server logs.
// This is a 'singleton' style function whose subsequent invocations return
// the same result.
func Salt() string {
	saltMutex.Lock()
	defer saltMutex.Unlock()

	if salt != "" {
		return salt
	}

	salt = uuid.New().String()

	return salt
}

// ArchiveName returns an archive name for an archive type.  The suffix is back end dependant.
func ArchiveName() string {
	return Application + "-" + Timestamp()
}

// ArchivePath returns the required path for an archive type.
func ArchivePath(namespace, kind, name, filename string) string {
	return ArchiveName() + "/" + namespace + "/" + strings.ToLower(kind) + "/" + name + "/" + filename
}

// ArchivePathUnscoped returns the required path for an archive type.
func ArchivePathUnscoped(kind, name, filename string) string {
	return ArchiveName() + "/" + strings.ToLower(kind) + "/" + name + "/" + filename
}
