package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
)

const (
	maxReruns = 3
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

func main() {
	/* Purpose of this go file
	 * Parse the results.xml generated for the test suites: (1) to get the failed tests (2) to generate final results.
	 * If there are reruns to be done, then it will provide the failed tests to the Jenkins job for the rerun. Failed tests are saved in a text file.
	 * If we want to build final result, then it will parse all the test results that have completed for the job build and will generate the final result.xml
	 */
	jobBuildNumber, numReruns, buildFinalResult := parseArguments()

	var suiteNames = []string{"validation", "sanity", "p0", "p1", "platform", "lpv", "custom"}

	if numReruns > 0 {
		inc := 0
		if buildFinalResult {
			inc = 1
		}

		for i := 1; i < numReruns+inc; i++ {
			suiteNames = append(suiteNames, fmt.Sprintf("rerun_%d", i))
		}
	}

	testSuiteResults := make(map[string]*JUnitTestSuites) // maps suite name to the JUnitTestSuites

	for _, suite := range suiteNames {
		resultXMLFileName := fmt.Sprintf("%d_%s.xml", jobBuildNumber, suite)
		if _, err := os.Stat(resultXMLFileName); os.IsNotExist(err) {
			log.Printf("NOT FOUND! Result XML: %s for Test Suite: %s", resultXMLFileName, suite)
			log.Println("Skipping...")

			continue
		}

		log.Printf("FOUND! Result XML: %s for Test Suite: %s", resultXMLFileName, suite)
		log.Println("Added...")

		resultXML, err := os.ReadFile(resultXMLFileName)
		if err != nil {
			log.Fatalf("Read file %s: %v", resultXMLFileName, err)
		}

		testSuites := &JUnitTestSuites{}
		if err := xml.Unmarshal(resultXML, testSuites); err != nil {
			log.Printf("Error unmarshalling file %s: %v", resultXMLFileName, err)
			continue
		}

		testSuiteResults[suite] = testSuites
	}

	// Save the Failed Tests.
	/*
	 * Only if there are some Reruns (numReruns > 0) then it will be saved.
	 * If buildFinalResult=true then this step will be skipped.
	 */
	if numReruns > 0 && !buildFinalResult {
		saveFailedTests(testSuiteResults, jobBuildNumber, numReruns)
	}

	// Generate the Final Result.
	/*
	 * If numReruns >= 1, then it updates the older results with the new rerun results.
	 * If numReruns < 1, then simply the same result XMLs will be copied with the modified naming convention.
	 */
	if buildFinalResult {
		generateFinalResult(testSuiteResults, numReruns, jobBuildNumber)
	}
}

// saveFailedTests saves all the failed tests.
/*
 * Jenkins job picks the failed tests (by reading the saved file) to execute the custom rerun tests.
 * Saves the failed tests in the format as required by Certification Pod i.e. -test <testName> -test <testName> ...
 * File Naming Convention: <Build-Number>_failed_tests.txt.
 */
func saveFailedTests(testSuiteResults map[string]*JUnitTestSuites, buildNum, numReruns int) {
	failedTestsList := getFailedTests(testSuiteResults, numReruns)

	modifiedFailedTestsList := make([]string, len(failedTestsList))

	// Adding -test to the test names.
	for i, testName := range failedTestsList {
		modifiedFailedTestsList[i] = "-test " + testName
	}

	// Sorting the tests in ascending order.
	slices.Sort(modifiedFailedTestsList)

	log.Println(strings.TrimSpace(strings.Join(modifiedFailedTestsList, " ")))

	tempFileName := fmt.Sprintf("%d_failed_tests.txt", buildNum)

	err := os.WriteFile(tempFileName, []byte(strings.TrimSpace(strings.Join(modifiedFailedTestsList, " "))), 0644)
	if err != nil {
		log.Fatalf("write file temp.txt: %v", err)
	}
}

func getFailedTests(testSuiteResults map[string]*JUnitTestSuites, numReruns int) []string {
	failedTestsMap := make(map[string]struct{})

	if numReruns < 2 {
		for _, testSuites := range testSuiteResults {
			for _, testSuite := range testSuites.TestSuites {
				for _, testCase := range testSuite.TestCases {
					if testCase.Failure != nil {
						failedTestsMap[testCase.Name] = struct{}{}
					}
				}
			}
		}
	} else {
		latestRerunSuiteName := fmt.Sprintf("rerun_%d", numReruns-1)

		for _, testSuite := range testSuiteResults[latestRerunSuiteName].TestSuites {
			for _, testCase := range testSuite.TestCases {
				if testCase.Failure != nil {
					failedTestsMap[testCase.Name] = struct{}{}
				}
			}
		}
	}

	var failedTestsList []string
	for testName := range failedTestsMap {
		failedTestsList = append(failedTestsList, testName)
	}

	return failedTestsList
}

// generateFinalResult updates the existing TestSuites TestCases with the value of latest rerun TestCases.
/*
 * We go through all the rerun results and get their TestCases.
 * Then we update the existing TestSuites with the value of latest rerun TestCases.
 * If numReruns < 1, then simply the same result XMLs will be copied with the modified naming convention.
 * If numReruns < 1, there is no need to generate the final result as no test results got updated.
 */
func generateFinalResult(testSuitesResults map[string]*JUnitTestSuites, numReruns, buildNum int) {
	var suiteNames = []string{"validation", "sanity", "p0", "p1", "platform", "lpv", "custom"}

	if numReruns < 1 {
		log.Println("numReruns < 1, simply copying the existing results.")
		generateModifiedResultsXML(testSuitesResults, suiteNames, buildNum)

		return
	}

	rerunTestCases := make(map[string]JUnitTestCase)

	var rerunSuiteNames []string

	// Generating the rerun test suite names in reverse order. E.g. [rerun_2, rerun_1]
	for i := numReruns; i > 0; i-- {
		rerunSuiteNames = append(rerunSuiteNames, fmt.Sprintf("rerun_%d", i))
	}

	// Validating if the testSuitesResults has all the rerunSuiteNames
	i := 0

	for _, rerunSuiteName := range rerunSuiteNames {
		if _, ok := testSuitesResults[rerunSuiteName]; !ok {
			// Instead of ending the program we will log and continue.
			// This ensures that even if a particular test suite failed, the results of the previous test suites will be recorded.
			log.Printf("ERROR! test suite: %s not found in testSuitesResults\n", rerunSuiteName)

			// Removing the missing rerunSuiteName from the rerunSuiteNames slice.
			rerunSuiteNames = append(rerunSuiteNames[:i], rerunSuiteNames[i+1:]...)

			continue
		}

		i++
	}

	// Parsing the rerun test suites in reverse order (rerunSuiteNames is in reverse order) and adding them to the map only once.
	// Parsing in reverse so that all the test cases which eventually passed are added appropriately.
	// E.g. if TestX failed in Rerun 1 and passed in Rerun 2. Since, we are checking rerun 2 first so, the passed TestX
	// will be added to the map. And next time when we are checking rerun 1, the map won't be updated with failed TestX.
	for _, rerunSuiteName := range rerunSuiteNames {
		for testSuiteIdx, testSuite := range testSuitesResults[rerunSuiteName].TestSuites {
			for testCaseIdx, testCase := range testSuite.TestCases {
				if _, ok := rerunTestCases[testCase.Name]; ok {
					// Already added to the rerunTestCases map, so we will skip it.
					continue
				} else {
					rerunTestCases[testCase.Name] = testSuitesResults[rerunSuiteName].TestSuites[testSuiteIdx].TestCases[testCaseIdx]
				}
			}
		}
	}

	updateExistingTestSuites(testSuitesResults, rerunTestCases, suiteNames)

	generateModifiedResultsXML(testSuitesResults, suiteNames, buildNum)
}

// updateExistingTestSuites updates the TestCases (of existing Test Suites results) with the rerun TestCases.
/*
 * All the TestSuite TestCases (present in Reruns) will be replaced with the rerun TestCases.
 * Also, the Time and Failure number for the TestSuites will be updated accordingly.
 */
func updateExistingTestSuites(testSuitesResults map[string]*JUnitTestSuites, rerunTestCases map[string]JUnitTestCase, suiteNames []string) {
	for _, suite := range suiteNames {
		// If a suite is not found in the testSuitesResults, then we will skip it.
		if _, ok := testSuitesResults[suite]; !ok {
			continue
		}

		for testSuiteIdx, testSuite := range testSuitesResults[suite].TestSuites {
			for testCaseIdx, testCase := range testSuite.TestCases {
				if _, ok := rerunTestCases[testCase.Name]; ok {
					// Updating the Test Run Times
					oldTestCaseTime, _ := strconv.Atoi(testSuitesResults[suite].TestSuites[testSuiteIdx].TestCases[testCaseIdx].Time)
					newTestCaseTime, _ := strconv.Atoi(rerunTestCases[testCase.Name].Time)

					testSuiteTime, _ := strconv.Atoi(testSuitesResults[suite].TestSuites[testSuiteIdx].Time)
					testSuiteTime = testSuiteTime - oldTestCaseTime + newTestCaseTime

					testSuitesTime, _ := strconv.Atoi(testSuitesResults[suite].Time)
					testSuitesTime = testSuitesTime - oldTestCaseTime + newTestCaseTime

					testSuitesResults[suite].TestSuites[testSuiteIdx].Time = strconv.Itoa(testSuiteTime)
					testSuitesResults[suite].Time = strconv.Itoa(testSuitesTime)

					// Updating the number of failures if the test has passed.
					if rerunTestCases[testCase.Name].Failure == nil {
						testSuiteFailures, _ := strconv.Atoi(testSuitesResults[suite].TestSuites[testSuiteIdx].Failures)
						testSuitesFailures, _ := strconv.Atoi(testSuitesResults[suite].Failures)

						testSuiteFailures--
						testSuitesFailures--

						testSuitesResults[suite].TestSuites[testSuiteIdx].Failures = strconv.Itoa(testSuiteFailures)
						testSuitesResults[suite].Failures = strconv.Itoa(testSuitesFailures)
					}

					// Updating the existing TestSuite TestCase with that of the Rerun TestCase.
					testSuitesResults[suite].TestSuites[testSuiteIdx].TestCases[testCaseIdx] = rerunTestCases[testCase.Name]
				}
			}
		}
	}
}

// generateModifiedResultsXML helps generate the modified XMLs for the test suites.
/*
 * Naming Convention: <Build Number>_<Suite Name>_modified.xml.
 * All the files are written to the workspace directory, same as where the original results are present.
 * These XMLs will be used to generate Jenkins TestReports using junit.
 */
func generateModifiedResultsXML(testSuitesResults map[string]*JUnitTestSuites, suiteNames []string, buildNum int) {
	for _, suite := range suiteNames {
		// If a suite is not found in the testSuitesResults, then we will skip it.
		if _, ok := testSuitesResults[suite]; !ok {
			log.Printf("Generating Final Results XML: Test Suite %s NOT FOUND!", suite)
			continue
		}

		log.Printf("Generating Final Results XML: Test Suite %s FOUND!", suite)

		resultFileName := fmt.Sprintf("%d_%s_modified.xml", buildNum, suite)

		resultXML, err := xml.MarshalIndent(testSuitesResults[suite], "", "    ")
		if err != nil {
			log.Fatalf("marshal final result %s: %v", resultFileName, err)
		}

		if err := os.WriteFile(resultFileName, resultXML, 0644); err != nil {
			log.Fatalf("write file %s: %v", resultFileName, err)
		}

		log.Printf("Test Suite: %s Result XML written to: %s", suite, resultFileName)
	}
}

func parseArguments() (int, int, bool) {
	jobBuildNumber := flag.Int("jobBuildNumber", 0, "Job Build Number")
	numReruns := flag.Int("numReruns", 0, "Rerun Number")
	buildFinalResult := flag.Bool("buildFinalResult", false, "Generate Final Result XML")

	flag.Parse()

	validateArguments(*jobBuildNumber, *numReruns)

	return *jobBuildNumber, *numReruns, *buildFinalResult
}

func validateArguments(jobBuildNumber, numReruns int) {
	if jobBuildNumber == 0 {
		log.Fatalf("jobBuildNumber is required")
	}

	if numReruns > maxReruns {
		log.Fatalf("rerunNum should be less than 3")
	}

	if numReruns < 0 {
		log.Fatalf("rerunNum should be greater than 0")
	}
}
