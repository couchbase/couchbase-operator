package analyzer

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"runtime/debug"
	"strconv"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/couchbase-operator/test/e2e/util"

	"github.com/sirupsen/logrus"
)

// JUnitSkipped is attached to a skipped test case.
// See https://llg.cubic.org/docs/junit/.
type JUnitSkipped struct {
	XMLName xml.Name `xml:"skipped"`
	Message string   `xml:"name,attr,omitempty"`
}

// JUnitError is attached to an errored test case.
// See https://llg.cubic.org/docs/junit/.
type JUnitError struct {
	XMLName     xml.Name `xml:"error"`
	Message     string   `xml:"message,attr,omitempty"`
	Description string   `xml:",chardata"`
}

// JUnitFailure is attached to a failed test case.
// See https://llg.cubic.org/docs/junit/.
type JUnitFailure struct {
	XMLName     xml.Name `xml:"failure"`
	Message     string   `xml:"message,attr,omitempty"`
	Type        string   `xml:"type,attr,omitempty"`
	Description string   `xml:",chardata"`
}

// JUnitTestCase is a test case.
// See https://llg.cubic.org/docs/junit/.
type JUnitTestCase struct {
	XMLName xml.Name      `xml:"testcase"`
	Name    string        `xml:"name,attr"`
	Time    string        `xml:"time,attr,omitempty"`
	Skipped *JUnitSkipped `xml:",omitempty"`
	Error   *JUnitError   `xml:",omitempty"`
	Failure *JUnitFailure `xml:",omitempty"`
}

// JUnitTestSuite is a test suite.
// See https://llg.cubic.org/docs/junit/.
type JUnitTestSuite struct {
	XMLName   xml.Name        `xml:"testsuite"`
	Name      string          `xml:"name,attr"`
	Tests     string          `xml:"tests,attr"`
	Errors    string          `xml:"errors,attr,omitempty"`
	Failures  string          `xml:"failures,attr,omitempty"`
	Skipped   string          `xml:"skipped,attr,omitempty"`
	Time      string          `xml:"time,attr,omitempty"`
	TestCases []JUnitTestCase `xml:",omitempty"`
}

// result is a type for caching information about a test run.
type result struct {
	// name is the name of the test.
	name string

	// runtime is the length of time a test ran for,
	runtime time.Duration

	// result is the result of the test.
	result types.ResultType

	// message is an optional message for failed and errored tests.
	message string

	// stack is an option stack trace for a failed or errored test.
	stack string
}

// results is the global list of results.  This is ordered by the completion
// time of the test.
var results []result

// message is used by Die to pass failure messages to the junit output.
var message string

// stack is used by Die to pass failure stack traces to the junit output.
var stack string

// RecordFailureMessage is used by Die to pass failure messages to the junit output.
func RecordFailureMessage(m, s string) {
	message = m
	stack = s
}

// Analyzer is an abstract type that is instantiated by every test.  It
// encapsulates analysis functionality.
type Analyzer interface {
	// Report must be invoked in a defer statement.  That is the only
	// place that can catch panics and see the full go testing state.
	Report(*testing.T)
}

// analyzerImpl realizes the Analyzer interface.
type analyzerImpl struct {
	// start is the start time of the test.  It is expected that this
	// will be initialized early on in the test run.
	start time.Time
}

// New creates a new test analyzer instance.
func New() Analyzer {
	return &analyzerImpl{
		start: time.Now(),
	}
}

// Report submits a test report.
func (a *analyzerImpl) Report(t *testing.T) {
	r := result{
		name:    t.Name(),
		runtime: time.Since(a.start),
	}

	var statusString string

	recoverError := recover()

	switch {
	case recoverError != nil:
		statusString = "error"
		r.result = types.ResultTypeErr
		r.message = fmt.Sprintf("%v", recoverError)
		r.stack = string(debug.Stack())

		// Flag the caught exception as a failure so it gets propagated.
		// Because we caught it it will look like a pass otherwise.
		t.Fail()
	case t.Failed():
		// Note that all tests failures need to use Die for this to work.
		statusString = "fail"
		r.result = types.ResultTypeFail
		r.message = message
		r.stack = stack
	case t.Skipped():
		statusString = "skipped"
		r.result = types.ResultTypeSkip
	default:
		statusString = "ok"
		r.result = types.ResultTypePass
	}

	logrus.Infof("%s %s", util.PrettyResult(r.result), statusString)

	results = append(results, r)
}

// Report is called on termination of the full test run to perform global anaysis.
func Report(suiteName string) {
	totalResults := len(results)

	if totalResults == 0 {
		return
	}

	var passes int

	var failures int

	var errors int

	var skipped int

	var totalTime time.Duration

	logrus.Info(util.PrettyHeading("Test Summary"))

	cases := make([]JUnitTestCase, totalResults)

	for i, r := range results {
		logrus.Infof("%4d: %s %s", i+1, r.name, util.PrettyResult(r.result))

		totalTime += r.runtime

		testCase := JUnitTestCase{
			Name: r.name,
			Time: strconv.Itoa(int(r.runtime.Seconds())),
		}

		switch r.result {
		case types.ResultTypeErr:
			errors++

			testCase.Error = &JUnitError{
				Message:     r.message,
				Description: r.stack,
			}
		case types.ResultTypeFail:
			failures++

			testCase.Failure = &JUnitFailure{
				Message:     r.message,
				Description: r.stack,
			}
		case types.ResultTypeSkip:
			skipped++

			testCase.Skipped = &JUnitSkipped{}
		default:
			passes++
		}

		cases[i] = testCase
	}

	logrus.Info(util.PrettyHeading("Suite Summary"))

	if passes > 0 {
		passRate := (float64(passes) / float64(totalResults)) * 100.0
		logrus.Infof(" %s Passes: %d (%0.2f%%)", util.PrettyResult(types.ResultTypePass), passes, passRate)
	}

	if failures > 0 {
		failRate := (float64(failures) / float64(totalResults)) * 100.0
		logrus.Infof(" %s Failures: %d (%0.2f%%)", util.PrettyResult(types.ResultTypeFail), failures, failRate)
	}

	if errors > 0 {
		errorRate := (float64(errors) / float64(totalResults)) * 100.0
		logrus.Infof(" %s Errors: %d (%0.2f%%)", util.PrettyResult(types.ResultTypeErr), errors, errorRate)
	}

	if skipped > 0 {
		skipRate := (float64(skipped) / float64(totalResults)) * 100.0
		logrus.Infof(" %s Skipped: %d (%0.2f%%)", util.PrettyResult(types.ResultTypeSkip), skipped, skipRate)
	}

	testSuite := &JUnitTestSuite{
		Name:      suiteName,
		Tests:     strconv.Itoa(totalResults),
		Errors:    strconv.Itoa(errors),
		Failures:  strconv.Itoa(failures),
		Skipped:   strconv.Itoa(skipped),
		Time:      strconv.Itoa(int(totalTime.Seconds())),
		TestCases: cases,
	}

	data, err := xml.MarshalIndent(testSuite, "", "    ")
	if err != nil {
		logrus.Warn("unable to marshal junit xml", err)
		return
	}

	data = []byte(xml.Header + string(data))

	if err := ioutil.WriteFile("results.xml", data, 0660); err != nil {
		logrus.Warn("unable to write junit xml", err)
		return
	}
}
