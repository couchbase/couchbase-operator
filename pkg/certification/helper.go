/*
Copyright 2022-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package certification

import (
	"strconv"
	"strings"
)

// getOverrides returns shared flags that have been set to a value other
// than default for passing down to lower level test suite.
// Also ensures that test suite does not receive duplicate values from the
// cao cli in the scenario where both top level and test level args exist.
func getOverrides(t SharedTestFlags, testArgs []string) []string {
	args := []string{}

	if t.IPv6 {
		flagName := "-" + ipv6Flag
		if !contains(testArgs, flagName) {
			args = append(args, flagName, "true")
		}
	}

	if t.StorageClassName != "" {
		flagName := "-" + storageClassFlag
		if !contains(testArgs, flagName) {
			args = append(args, flagName, t.StorageClassName)
		}
	}

	if t.CollectedLogLevel != 0 {
		flagName := "-" + collectLogLevelFlag
		if !contains(testArgs, flagName) {
			args = append(args, flagName, strconv.Itoa(t.CollectedLogLevel))
		}
	}

	return args
}

// contains returns true if a value is in the list.
func contains(s []string, e string) bool {
	for _, a := range s {
		if strings.Contains(a, e) {
			return true
		}
	}

	return false
}
