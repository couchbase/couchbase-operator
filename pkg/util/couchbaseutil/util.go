/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package couchbaseutil

import (
	"strconv"
)

func BoolToInt(b bool) int {
	return map[bool]int{false: 0, true: 1}[b]
}

func BoolToStr(b bool) string {
	return strconv.Itoa(BoolToInt(b))
}

func BoolAsStr(b bool) string {
	if b {
		return "true"
	}

	return "false"
}

func IntToStr(i int) string {
	return strconv.Itoa(i)
}
