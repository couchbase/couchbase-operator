package util

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	Application = "cbopinfo"
)

// AbsolutePath takes a path, expands the the home directory if
// included or appends the current directory if a relative path
// is specified
func AbsolutePath(path string) (string, error) {
	// References a home directory
	re, err := regexp.Compile(`^~([^/]+)?/`)
	if err != nil {
		return "", fmt.Errorf("unable to compile regular expression: %v", err)
	}

	matches := re.FindStringSubmatch(path)
	if matches != nil {
		// If no explicit directory was stated use HOME from the environment
		// otherwise lookup the user home directory
		if matches[1] == "" {
			home, ok := os.LookupEnv("HOME")
			if !ok {
				return "", fmt.Errorf("HOME environment variable not set")
			}
			path = home + path[1:]
		} else {
			u, err := user.Lookup(matches[1])
			if err != nil {
				return "", fmt.Errorf("user not defined: %v", err)
			}
			path = u.HomeDir + path[len(matches[1])+1:]
		}
	}

	// Handle relative paths and clean up the path
	return filepath.Abs(path)
}

var (
	// timestamp caches the timestamp returned by Timestamp()
	timestamp = ""
)

// Timestamp returns an ISO8601 formatted timestamp suitable for
// use in generated file names.  These include the time zone for ease of
// use by end users.  This is a 'singleton' style function whose subsequent
// invokations return the same result
func Timestamp() string {
	if timestamp != "" {
		return timestamp
	}

	format := "20060102T150405-0700"
	timestamp = time.Now().Format(format)
	return timestamp
}

// ArchiveName returns an archive name for an archive type.  The suffix is back end dependant
func ArchiveName() string {
	return Application + "-" + Timestamp()
}

// ArchivePath returns the required path for an archive type
func ArchivePath(namespace, kind, name, filename string) string {
	return ArchiveName() + "/" + namespace + "/" + strings.ToLower(kind) + "/" + name + "/" + filename
}

// ArchivePathUnscoped returns the required path for an archive type
func ArchivePathUnscoped(kind, name, filename string) string {
	return ArchiveName() + "/" + strings.ToLower(kind) + "/" + name + "/" + filename
}
