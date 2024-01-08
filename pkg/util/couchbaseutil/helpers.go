package couchbaseutil

import "fmt"

// VersionAfter determines whether the configured version is greater than
// or equal to the required version, useful for enabling features at runtime.
func VersionAfter(version, required string) (bool, error) {
	v1, err := NewVersion(version)
	if err != nil {
		return false, err
	}

	v2, err := NewVersion(required)
	if err != nil {
		return false, err
	}

	return v1.GreaterEqual(v2), nil
}

func (b *Bucket) CanBeMigrated(backend CouchbaseStorageBackend) (bool, string) {
	if backend == CouchbaseStorageBackendMagma {
		if b.BucketMemoryQuota < 1024 {
			return false, fmt.Sprintf("memory quota (%v) below minimum %v", b.BucketMemoryQuota, 1024)
		}
	}

	return true, ""
}
