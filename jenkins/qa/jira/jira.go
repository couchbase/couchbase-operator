/*
Copyright 2025-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/andygrunwald/go-jira"
	"github.com/couchbase/couchbase-operator/jenkins/qa/junit_util"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	jiraURL = "https://couchbasecloud.atlassian.net"
)

func main() {
	args := parseArguments()

	var suiteNames = []string{"validation", "sanity", "p0", "p1", "platform", "lpv", "custom"}

	allSuites := make([]junit_util.JUnitTestSuite, 0)
	// Read file
	for _, suite := range suiteNames {
		filename := fmt.Sprintf("%d_%s_modified.xml", args.jobBuildNumber, suite)

		if _, err := os.Stat(filename); err != nil {
			if args.verbose {
				log.Printf("File %s does not exist for suite %s\n", filename, suite)
				log.Println("Skipping...")
			}

			continue
		}

		xmlContent, err := os.ReadFile(filename)
		if err != nil {
			log.Printf("Error reading file %s: %v\n", filename, err)
			continue
		}

		testSuites := &junit_util.JUnitTestSuites{}
		if err := xml.Unmarshal(xmlContent, testSuites); err != nil {
			log.Printf("Error unmarshalling file %s: %v", filename, err)
			continue
		}
		allSuites = append(allSuites, testSuites.TestSuites...)
	}

	if !anyFailedTests(allSuites) {
		log.Printf("No failed tests, skipping Jira issue creation")
		return
	}

	tp := jira.BasicAuthTransport{
		Username: args.jiraUsername,
		Password: args.jiraAPIKey,
	}

	jiraClient, _ := jira.NewClient(tp.Client(), jiraURL)

	issueKey := args.issueKey

	if issueKey == "" {
		jiraIssue := buildJiraIssue(allSuites, args.testRunInfo)
		issue, response, err := jiraClient.Issue.Create(&jiraIssue)
		if err != nil {
			log.Printf("Error: %+v\n", err)
			log.Printf("Response: %+v\n", response)
			body, _ := io.ReadAll(response.Body)
			log.Println(string(body))
			if issue != nil {
				log.Printf("Issue: %+v\n", issue)
			}
			return
		}

		log.Printf("Issue created: %s (%s/browse/%s)\n", issue.Key, jiraURL, issue.Key)

		issueKey = issue.Key
	}

	attachments := getFailedSuitesArtifacts(allSuites)

	log.Println("Adding attachments")
	for _, attachment := range attachments {
		file, err := os.Open(attachment)
		if err != nil {
			log.Printf("Could not open file: %s: %s\n", attachment, err.Error())
			continue
		}
		defer file.Close()

		if args.verbose {
			log.Printf("Uploading attachment: %s\n", attachment)
		}
		_, _, err = jiraClient.Issue.PostAttachment(issueKey, file, attachment)

		if err != nil {
			log.Printf("Error uploading attachment: %s: %s\n", attachment, err.Error())
			continue
		}

		if args.verbose {
			log.Printf("Attachment uploaded: %s\n", attachment)
		}
	}
}

type arguments struct {
	jobBuildNumber int
	verbose        bool
	issueKey       string
	jiraUsername   string
	jiraAPIKey     string

	// Additional information about test run
	testRunInfo testRunInfo
}

func anyFailedTests(testSuites []junit_util.JUnitTestSuite) bool {
	for _, testSuite := range testSuites {
		if testSuite.Failures != "0" {
			return true
		}
	}

	return false
}

type testParameter struct {
	key   string
	value string
}

type testRunInfo struct {
	testParameters testParameters
	platform       string
}

type testParameters []testParameter

func (l *testParameters) String() string {
	return fmt.Sprintf("testRunInfo: %+v", *l)
}

func (tp *testParameters) Set(value string) error {
	s := strings.Split(value, "=")
	if len(s) != 2 {
		return fmt.Errorf("invalid additional info: %s, use key=value format", value)
	}

	*tp = append(*tp, testParameter{key: s[0], value: s[1]})
	return nil
}

func parseArguments() arguments {
	jobBuildNumber := flag.Int("jobBuildNumber", 0, "Job Build Number")
	verbose := flag.Bool("verbose", false, "Verbose output")
	issueKey := flag.String("issue", "", "Jira Issue Key")
	jiraUsername := flag.String("jira-username", "", "Jira Username")
	jiraAPIKey := flag.String("jira-api-key", "", "Jira API Key")

	testParameters := testParameters{}
	flag.Var(&testParameters, "test-info", "Additional information about the test run")
	platform := flag.String("platform", "", "Platform")
	flag.Parse()

	arguments := arguments{
		jobBuildNumber: *jobBuildNumber,
		verbose:        *verbose,
		issueKey:       *issueKey,
		jiraUsername:   *jiraUsername,
		jiraAPIKey:     *jiraAPIKey,
		testRunInfo: testRunInfo{
			testParameters: testParameters,
			platform:       *platform,
		},
	}

	validateArguments(arguments)
	return arguments
}

func validateArguments(arguments arguments) {
	if arguments.jobBuildNumber == 0 {
		log.Fatalf("jobBuildNumber is required")
	}

	if arguments.jiraUsername == "" {
		log.Fatalf("jiraUsername is required")
	}

	if arguments.jiraAPIKey == "" {
		log.Fatalf("jiraAPIKey is required")
	}
}

func buildJiraIssue(testSuites []junit_util.JUnitTestSuite, testRunInfo testRunInfo) jira.Issue {
	summary := fmt.Sprintf("[E2E Test] Suite Failures - %s", time.Now().Format("2006-01-02 15:04"))
	if len(testRunInfo.platform) > 0 {
		summary = fmt.Sprintf("[%s]%s", strings.ToUpper(testRunInfo.platform), summary)
	}
	issue := jira.Issue{
		Fields: &jira.IssueFields{
			Project: jira.Project{
				Key: "K8S",
			},
			Type: jira.IssueType{
				Name: "Bug",
			},
			Summary:     summary,
			Description: buildJiraDescription(testSuites, testRunInfo),
		},
	}
	return issue
}

func buildJiraDescription(testSuites []junit_util.JUnitTestSuite, testRunInfo testRunInfo) string {
	// Sort test suites by failures in descending order
	sort.Slice(testSuites, func(i, j int) bool {
		return testSuites[i].Failures > testSuites[j].Failures
	})

	var sb strings.Builder

	sb.WriteString("h2. Test Run Info:\n")
	sb.WriteString(fmt.Sprintf("*Date:* %s\n", time.Now().Format("2006-01-02 15:04")))
	sb.WriteString("\n\n")

	if len(testRunInfo.testParameters) > 0 {
		for _, info := range testRunInfo.testParameters {
			k := cases.Title(language.English, cases.Compact).String(strings.ReplaceAll(info.key, "-", " "))
			sb.WriteString(fmt.Sprintf("*%s:* %s\n", k, info.value))
		}
		sb.WriteString("\n\n")
	}

	for _, testSuite := range testSuites {
		if testSuite.Failures == "0" {
			continue
		}

		sb.WriteString(fmt.Sprintf("h2. Suite %s - %s/%s:\n\n", testSuite.Name, testSuite.Failures, testSuite.Tests))

		for _, testCase := range testSuite.TestCases {
			if testCase.Failure != nil {
				sb.WriteString(fmt.Sprintf("* %s\n", testCase.Name))
			}
		}
		sb.WriteString("\n")
	}
	description := sb.String()

	return description
}

func getFailedSuitesArtifacts(testSuites []junit_util.JUnitTestSuite) []string {

	artifacts := make([]string, 0)
	for _, testSuite := range testSuites {
		if testSuite.Failures != "0" {
			artifacts = append(artifacts, fmt.Sprintf("%s.tar.gz", testSuite.Name))
		}
	}

	artifacts = append(artifacts, "first_rerun.tar.gz")
	artifacts = append(artifacts, "second_rerun.tar.gz")

	return artifacts
}
