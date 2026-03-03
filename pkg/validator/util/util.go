/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package util

import (
	"fmt"
)

func UniqueString(strList []string) bool {
	set := map[string]interface{}{}
	for _, str := range strList {
		set[str] = nil
	}

	return len(set) == len(strList)
}

// StringArrayCompare compares two arrays and ensure the elements are the same
// but unordered.
func StringArrayCompare(a1, a2 []string) bool {
	m := make(map[string]int)
	for _, val := range a1 {
		m[val]++
	}

	for _, val := range a2 {
		if _, ok := m[val]; ok {
			if m[val] > 0 {
				m[val]--
				continue
			}
		}

		return false
	}

	for _, cnt := range m {
		if cnt > 0 {
			return false
		}
	}

	return true
}

type UpdateError struct {
	field string
	in    string
}

func NewUpdateError(field, in string) error {
	return &UpdateError{field: field, in: in}
}

func (e *UpdateError) Error() string {
	return fmt.Sprintf("%s in %s cannot be updated", e.field, e.in)
}
