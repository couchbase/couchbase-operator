/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
