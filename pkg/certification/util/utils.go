package util

var UseANSIColor = true

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
