/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// package util provides generic helpers.
package util

// Interface provides a stable interface for these algorithms.
type Interface interface {
	// Len is the length of provided slice.
	Len() int

	// Predicate is a function used to determine whether a
	// particular algorithm passes or fails.
	Predicate(int) bool
}

// Any returns true if any of the slice elements return true for the
// given function.
func Any(a Interface) bool {
	for i := 0; i < a.Len(); i++ {
		if a.Predicate(i) {
			return true
		}
	}

	return false
}

// All returns true if all of the slice elements return true for the
// given function.
func All(a Interface) bool {
	for i := 0; i < a.Len(); i++ {
		if !a.Predicate(i) {
			return false
		}
	}

	return true
}
