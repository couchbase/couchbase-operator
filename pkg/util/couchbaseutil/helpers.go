package couchbaseutil

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
