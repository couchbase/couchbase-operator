/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package analyzer

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
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

// JUnitTestSuites is a group of test suites.
// See https://llg.cubic.org/docs/junit/.
type JUnitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	Tests      string           `xml:"tests,attr"`
	Errors     string           `xml:"errors,attr,omitempty"`
	Failures   string           `xml:"failures,attr,omitempty"`
	Time       string           `xml:"time,attr,omitempty"`
	TestSuites []JUnitTestSuite `xml:",omitempty"`
}

// testSuite is an ordered list test cases so that we can control the output
// formatting.
type testSuite struct {
	name      string
	testCases []string
}

// testSuiteList is an ordered list of test suites so that we can control the
// output formatting.
type testSuiteList []*testSuite

// Find iterates over a test suite list and returns a matching suite if one
// exists.
func (l testSuiteList) Find(suite string) *testSuite {
	for _, s := range l {
		if s.name == suite {
			return s
		}
	}

	return nil
}

// Get gets an existing test suite from the list, or creates a new one.
func (l *testSuiteList) Get(suite string) *testSuite {
	if s := l.Find(suite); s != nil {
		return s
	}

	s := &testSuite{
		name: suite,
	}

	*l = append(*l, s)

	return s
}

// testSuites is an ordered list of suites we have run.
var testSuites testSuiteList

// RegisterTest maps a test to a specific suite.
func RegisterTest(suite, test string) {
	lock.Lock()
	defer lock.Unlock()

	s := testSuites.Get(suite)
	s.testCases = append(s.testCases, test)
}

// result is a type for caching information about a test run.
type result struct {
	// runtime is the length of time a test ran for,
	runtime time.Duration

	// result is the result of the test.
	result types.ResultType

	// message is an optional message for failed and errored tests.
	message string

	// stack is an option stack trace for a failed or errored test.
	stack string
}

// results is the global map of test name to results.
var results = map[string]result{}

// errorMessage is passed from the exception mechanism to cache the error.
type errorMessage struct {
	// message is used by Die to pass failure messages to the junit output.
	message string

	// stack is used by Die to pass failure stack traces to the junit output.
	stack string
}

// errorMessages is a map from test name to error message/stack trace.
var errorMessages = map[string]errorMessage{}

// RecordFailureMessage is used by Die to pass failure messages to the junit output.
func RecordFailureMessage(t *testing.T, m, s string) {
	lock.Lock()
	defer lock.Unlock()

	errorMessages[t.Name()] = errorMessage{
		message: m,
		stack:   s,
	}
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

// lock is held during concurrent writes when running parallel tests.
var lock sync.Mutex

// Report submits a test report.
func (a *analyzerImpl) Report(t *testing.T) {
	lock.Lock()
	defer lock.Unlock()

	r := result{
		runtime: time.Since(a.start),
	}

	recoverError := recover()

	var message string

	var stack string

	if msg, ok := errorMessages[t.Name()]; ok {
		message = msg.message
		stack = msg.stack
	}

	switch {
	case recoverError != nil:
		r.result = types.ResultTypeErr
		r.message = fmt.Sprintf("%v", recoverError)
		r.stack = string(debug.Stack())

		// Flag the caught exception as a failure so it gets propagated.
		// Because we caught it it will look like a pass otherwise.
		t.Fail()
	case t.Failed():
		// Note that all tests failures need to use Die for this to work.
		r.result = types.ResultTypeFail
		r.message = message
		r.stack = stack
	case t.Skipped():
		r.result = types.ResultTypeSkip
	default:
		r.result = types.ResultTypePass
	}

	logrus.Infof("%s %s", t.Name(), util.PrettyResult(r.result))

	// The test name will be fully qualified e.g. TestA/TestB/TestC whereas we expect
	// a test to be mapped to one, and only one suite, and we don't know how it will
	// be invoked.  Therefore we only want to key this on the last test name element,
	// i.e. TestC
	names := strings.Split(t.Name(), "/")
	results[names[len(names)-1]] = r
}

// accounting is a container for statistics about a collection of tests.
type accounting struct {
	name     string
	tests    int
	passes   int
	failures int
	errors   int
	skips    int
	time     time.Duration
}

func (a *accounting) passed(in time.Duration) {
	a.tests++
	a.passes++
	a.time += in
}

func (a *accounting) failed(in time.Duration) {
	a.tests++
	a.failures++
	a.time += in
}

func (a *accounting) errored(in time.Duration) {
	a.tests++
	a.errors++
	a.time += in
}

func (a *accounting) skipped(in time.Duration) {
	a.tests++
	a.skips++
	a.time += in
}

// report prints the summary of a suite's accounting information.
func (a accounting) report() {
	logrus.Info(util.PrettyHeading(fmt.Sprintf("Suite Summary (%s)", a.name)))

	if a.passes > 0 {
		passRate := (float64(a.passes) / float64(a.tests)) * 100.0
		logrus.Infof(" %s Passes: %d (%0.2f%%)", util.PrettyResult(types.ResultTypePass), a.passes, passRate)
	}

	if a.failures > 0 {
		failRate := (float64(a.failures) / float64(a.tests)) * 100.0
		logrus.Infof(" %s Failures: %d (%0.2f%%)", util.PrettyResult(types.ResultTypeFail), a.failures, failRate)
	}

	if a.errors > 0 {
		errorRate := (float64(a.errors) / float64(a.tests)) * 100.0
		logrus.Infof(" %s Errors: %d (%0.2f%%)", util.PrettyResult(types.ResultTypeErr), a.errors, errorRate)
	}

	if a.skips > 0 {
		skipRate := (float64(a.skips) / float64(a.tests)) * 100.0
		logrus.Infof(" %s Skipped: %d (%0.2f%%)", util.PrettyResult(types.ResultTypeSkip), a.skips, skipRate)
	}
}

// Report is called on termination of the full test run to perform global anaysis.
func Report(suiteName string) {
	if len(results) == 0 {
		logrus.Warn("no test results collected")
		return
	}

	// Print out the individual test results as we iterate through the suites and tests.
	logrus.Info(util.PrettyHeading("Test Summary"))

	var testNumber int

	// Keep statistics of overall passes/fails as well as per-suite accounts, these
	// will be summarized at the veny end after all the per test output has been
	// emitted to the logs.
	globalAccounting := accounting{
		name: "overall",
	}

	suiteAccounts := []accounting{}

	suites := []JUnitTestSuite{}

	for _, testSuite := range testSuites {
		suiteAccounting := accounting{
			name: testSuite.name,
		}

		cases := []JUnitTestCase{}

		for _, testCase := range testSuite.testCases {
			r, ok := results[testCase]
			if !ok {
				logrus.Warnf("unable to locate result for %s/%s", testSuite.name, testCase)
				continue
			}

			logrus.Infof("%4d: %s %s", testNumber+1, testCase, util.PrettyResult(r.result))
			testNumber++

			junitTestCase := JUnitTestCase{
				Name: testCase,
				Time: strconv.Itoa(int(r.runtime.Seconds())),
			}

			switch r.result {
			case types.ResultTypeErr:
				globalAccounting.errored(r.runtime)
				suiteAccounting.errored(r.runtime)

				junitTestCase.Error = &JUnitError{
					Message:     r.message,
					Description: r.stack,
				}
			case types.ResultTypeFail:
				globalAccounting.failed(r.runtime)
				suiteAccounting.failed(r.runtime)

				junitTestCase.Failure = &JUnitFailure{
					Message:     r.message,
					Description: r.stack,
				}
			case types.ResultTypeSkip:
				globalAccounting.skipped(r.runtime)
				suiteAccounting.skipped(r.runtime)

				junitTestCase.Skipped = &JUnitSkipped{}
			default:
				globalAccounting.passed(r.runtime)
				suiteAccounting.passed(r.runtime)
			}

			cases = append(cases, junitTestCase)
		}

		junitTestSuite := JUnitTestSuite{
			Name:      testSuite.name,
			Tests:     strconv.Itoa(suiteAccounting.tests),
			Errors:    strconv.Itoa(suiteAccounting.errors),
			Failures:  strconv.Itoa(suiteAccounting.failures),
			Skipped:   strconv.Itoa(suiteAccounting.skips),
			Time:      strconv.Itoa(int(suiteAccounting.time.Seconds())),
			TestCases: cases,
		}

		suites = append(suites, junitTestSuite)

		suiteAccounts = append(suiteAccounts, suiteAccounting)
	}

	if len(suiteAccounts) > 1 {
		globalAccounting.report()
	}

	for _, accounting := range suiteAccounts {
		accounting.report()
	}

	testSuites := &JUnitTestSuites{
		Tests:      strconv.Itoa(globalAccounting.tests),
		Errors:     strconv.Itoa(globalAccounting.errors),
		Failures:   strconv.Itoa(globalAccounting.failures),
		Time:       strconv.Itoa(int(globalAccounting.time.Seconds())),
		TestSuites: suites,
	}

	data, err := xml.MarshalIndent(testSuites, "", "    ")
	if err != nil {
		logrus.Warn("unable to marshal junit xml", err)
		return
	}

	data = []byte(xml.Header + string(data))

	// TODO: There is a chance here that the timestamps will collide, work out how to make this
	// totally unique when running in parallel.
	if err := ioutil.WriteFile(fmt.Sprintf("results-%s.xml", time.Now().Format(time.RFC3339Nano)), data, 0660); err != nil {
		logrus.Warn("unable to write junit xml", err)
		return
	}
}
