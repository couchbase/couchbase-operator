/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package util

import (
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

var UseANSIColor bool

// Closest we can get to unicorn mode for now...
func PrettyResult(t types.ResultType) string {
	result := ""

	if UseANSIColor {
		switch t {
		case types.ResultTypePass:
			result += "\033[1;32m"
		case types.ResultTypeFail:
			result += "\033[1;31m"
		case types.ResultTypeSkip:
			result += "\033[1;34m"
		case types.ResultTypeErr:
			result += "\033[1;33m"
		}
	}

	result += string(t)

	if UseANSIColor {
		result += "\033[0m"
	}

	return result
}

func PrettyHeading(s string) string {
	result := ""

	if UseANSIColor {
		result += "\033[1m"
	}

	result += s

	if UseANSIColor {
		result += "\033[0m"
	}

	return result
}
