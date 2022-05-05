package certification

import (
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
