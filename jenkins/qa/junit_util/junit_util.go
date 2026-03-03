/*
Copyright 2025-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package junit_util

import "encoding/xml"

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
	Skipped *JUnitSkipped `xml:"skipped,omitempty"`
	Error   *JUnitError   `xml:"error,omitempty"`
	Failure *JUnitFailure `xml:"failure,omitempty"`
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
	TestCases []JUnitTestCase `xml:"testcase,omitempty"`
}

// JUnitTestSuites is a group of test suites.
// See https://llg.cubic.org/docs/junit/.
type JUnitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	Tests      string           `xml:"tests,attr"`
	Errors     string           `xml:"errors,attr,omitempty"`
	Failures   string           `xml:"failures,attr,omitempty"`
	Time       string           `xml:"time,attr,omitempty"`
	TestSuites []JUnitTestSuite `xml:"testsuite,omitempty"`
}
