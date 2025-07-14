package util

import (
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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

func StrPtr(s string) *string {
	return &s
}

func IntPtr(i int) *int {
	return &i
}

func BoolPtr(b bool) *bool {
	return &b
}

func IntOrStringPtr(s string) *intstr.IntOrString {
	val := intstr.Parse(s)
	return &val
}

var FrameworkBackupStorageClass func() string
