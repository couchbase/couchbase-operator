package util

import (
	"fmt"
	"os"
	"strings"
)

var UseANSIColor = true

// Right now this is being stored in memory.  Even running with all tests, it does not use much memory
// however if we ever need an optimization, we can stream these to a temp file.
var outputLines []string

func GetStdout() []string {
	return outputLines
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

func PrintLine(s string) {
	outputLines = append(outputLines, RemovePretties(s))

	fmt.Println(s)
}

func PrintError(s string, err error) {
	outputLines = append(outputLines, fmt.Sprintf(RemovePretties(s), err))

	fmt.Println(s, err)
}

func outputContains(s string) bool {
	for _, v := range outputLines {
		if v == s {
			return true
		}
	}

	return false
}

func WriteToStdout(b []byte) {
	s := string(b)
	if !outputContains(s) {
		os.Stderr.Write(b)

		outputLines = append(outputLines, RemovePretties(s))
	}
}

type resultType string

const (
	// resultTypePass means the test passed.
	ResultTypePass resultType = "✔"

	// resultTypeFail means the test failed.
	ResultTypeFail resultType = "✗"

	// resultTypeSkip means the test was skipped, most likely this is due
	// to the test being incompatible with the environment or dynamic
	// configuration parameters.
	ResultTypeSkip resultType = "?"

	// resultTypeErr means the test itself errored, at present this means
	// it raise a panic.
	ResultTypeErr resultType = "!"
)

func RemovePretties(s string) string {
	s = strings.ReplaceAll(s, "\033[1;32m", "")
	s = strings.ReplaceAll(s, "\033[1;31m", "")
	s = strings.ReplaceAll(s, "\033[1;34m", "")
	s = strings.ReplaceAll(s, "\033[1;33m", "")
	s = strings.ReplaceAll(s, "\033[36m", "")
	s = strings.ReplaceAll(s, "\033[31m", "")
	s = strings.ReplaceAll(s, "\033[0m", "")

	return strings.ReplaceAll(s, "\033[1m", "")
}

func PrettyResult(t resultType) string {
	result := ""

	if UseANSIColor {
		switch t {
		case ResultTypePass:
			result += "\033[1;32m"
		case ResultTypeFail:
			result += "\033[1;31m"
		case ResultTypeSkip:
			result += "\033[1;34m"
		case ResultTypeErr:
			result += "\033[1;33m"
		}
	}

	result += string(t)

	if UseANSIColor {
		result += "\033[0m"
	}

	return result
}
