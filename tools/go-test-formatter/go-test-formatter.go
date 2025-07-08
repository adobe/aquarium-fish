/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

// Tool for formatting Go test output from `go test -json` into junit and structured std output
// with various filtering and formatting options.
// Usage:
// go test -json -v -parallel 4 -count=1 -skip '_stress$' ./tests/... | \
//     tee integration_tests_report.full.log | \
//     go run ./tools/go-test-formatter/go-test-formatter.go -stdout_timestamp -stdout_color \
//         -stdout_filter failed -junit integration_tests_report.xml -junit_truncate 2000 \
//         -junit_filter failed -junit_timestamp

package main

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TestEvent represents a single test event from go test -json
type TestEvent struct {
	Time    string  `json:"Time"`
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// TestLine contains time and text output
type TestLine struct {
	Time time.Time
	Line string
}

// TestResult represents a completed test with all its output
type TestResult struct {
	Package   string
	Test      string
	Action    string
	StartTime time.Time
	EndTime   time.Time
	Elapsed   float64
	Output    []TestLine
	Failed    bool
	Passed    bool
	Subtests  map[string]*TestResult
	Parent    *TestResult

	activeSubtest string
}

// PackageResult represents all tests in a package
type PackageResult struct {
	Name      string
	Tests     map[string]*TestResult
	Failed    bool
	Passed    bool
	StartTime time.Time
	EndTime   time.Time
}

// JUnitTestSuite represents a JUnit XML test suite
type JUnitTestSuite struct {
	XMLName   xml.Name        `xml:"testsuite"`
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Skipped   int             `xml:"skipped,attr"`
	Time      float64         `xml:"time,attr"`
	Timestamp string          `xml:"timestamp,attr"`
	TestCases []JUnitTestCase `xml:"testcase"`
}

// JUnitTestCase represents a JUnit XML test case
type JUnitTestCase struct {
	XMLName   xml.Name      `xml:"testcase"`
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
	SystemErr string        `xml:"system-err,omitempty"`
}

// JUnitFailure represents a JUnit XML failure
type JUnitFailure struct {
	XMLName xml.Name `xml:"failure"`
	Message string   `xml:"message,attr"`
	Type    string   `xml:"type,attr"`
	Content string   `xml:",chardata"`
}

// Config holds the tool configuration
type Config struct {
	JUnitOutput     string
	StdoutFilter    string
	StdoutTruncate  int
	StdoutTimestamp bool
	StdoutColor     bool
	JUnitTruncate   int
	JUnitFilter     string
	JUnitTimestamp  bool
}

// Formatter holds the main formatter state
type Formatter struct {
	config        *Config
	packages      map[string]*PackageResult
	currentOutput []string
	hasFailures   bool
}

// Colors for terminal output
const (
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
)

func main() {
	config := parseFlags()

	formatter := &Formatter{
		config:   config,
		packages: make(map[string]*PackageResult),
	}

	if err := formatter.process(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if formatter.hasFailures {
		os.Exit(1)
	}
}

func parseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.StdoutFilter, "stdout_filter", "", "Filter stdout output (non-failed, non-error, all)")
	flag.IntVar(&config.StdoutTruncate, "stdout_truncate", 0, "Truncate stdout output to N lines")
	flag.BoolVar(&config.StdoutTimestamp, "stdout_timestamp", false, "Add timestamps to stdout output")
	flag.BoolVar(&config.StdoutColor, "stdout_color", false, "Enable colored stdout output")
	flag.StringVar(&config.JUnitOutput, "junit", "", "JUnit XML output file")
	flag.IntVar(&config.JUnitTruncate, "junit_truncate", 0, "Truncate JUnit test output to N lines")
	flag.StringVar(&config.JUnitFilter, "junit_filter", "", "Filter JUnit output (non-failed, non-error, all)")
	flag.BoolVar(&config.JUnitTimestamp, "junit_timestamp", false, "Add timestamps to JUnit output")

	flag.Parse()
	return config
}

func (f *Formatter) process() error {
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		var event TestEvent
		if err := json.Unmarshal([]byte(scanner.Text()), &event); err != nil {
			return fmt.Errorf("failed to parse JSON: %v", err)
		}

		if err := f.processEvent(&event); err != nil {
			return fmt.Errorf("failed to process event: %v", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input: %v", err)
	}

	return f.generateOutputs()
}

func (f *Formatter) processEvent(event *TestEvent) error {
	switch event.Action {
	case "run":
		f.runTest(event)
	case "output":
		f.addOutput(event)
	case "pass", "fail", "skip":
		f.completeTest(event)
	}
	return nil
}

func (f *Formatter) runTest(event *TestEvent) {
	if event.Test == "" {
		return
	}

	pkg := f.getOrCreatePackage(event.Package)
	testKey := event.Test

	// Check if this is a subtest
	parentTest, isSubtest := f.findParentTest(pkg, testKey)

	if _, exists := pkg.Tests[testKey]; !exists {
		test := &TestResult{
			Package:   event.Package,
			Test:      event.Test,
			StartTime: f.parseTime(event.Time),
			Output:    make([]TestLine, 0),
			Subtests:  make(map[string]*TestResult),
		}

		if isSubtest {
			test.Parent = parentTest
			parentTest.activeSubtest = testKey
			parentTest.Subtests[testKey] = test
		} else {
			pkg.Tests[testKey] = test
		}
	}

	f.currentOutput = make([]string, 0)
}

func (f *Formatter) addOutput(event *TestEvent) {
	if event.Test == "" {
		return
	}

	// Skip PAUSE and CONT due to not needed in structured output
	if strings.HasPrefix(event.Output, "=== PAUSE ") || strings.HasPrefix(event.Output, "=== CONT ") {
		return
	}

	// Remove not that needed RUN and PASS/FAIL/SKIP due to not needed in block output
	if strings.HasPrefix(event.Output, "=== RUN ") ||
		strings.HasPrefix(strings.TrimLeft(event.Output, " "), "--- FAIL: ") ||
		strings.HasPrefix(strings.TrimLeft(event.Output, " "), "--- SKIP: ") {
		return
	}

	pkg := f.getOrCreatePackage(event.Package)

	// Determine which test should receive this output
	var targetTest *TestResult = f.findTest(pkg, event.Test)
	if targetTest == nil {
		return
	}

	// If it's base test - then check if activeSubtest is set to put log line in it
	if targetTest.Parent == nil && targetTest.activeSubtest != "" {
		targetTest = f.findTest(pkg, targetTest.activeSubtest)
	}

	if targetTest == nil {
		return
	}

	targetTest.Output = append(targetTest.Output, TestLine{
		Time: f.parseTime(event.Time),
		Line: event.Output,
	})
	f.currentOutput = append(f.currentOutput, event.Output)
}

func (f *Formatter) completeTest(event *TestEvent) {
	if event.Test == "" {
		return
	}

	pkg := f.packages[event.Package]
	if pkg == nil {
		return
	}

	test := f.findTest(pkg, event.Test)
	if test == nil {
		return
	}

	test.Action = event.Action
	test.EndTime = f.parseTime(event.Time)
	test.Elapsed = event.Elapsed

	switch event.Action {
	case "pass":
		test.Passed = true
	case "fail":
		test.Failed = true
		f.hasFailures = true
		// Mark parent test as failed if this is a subtest
		if test.Parent != nil {
			test.Parent.Failed = true
		}
		pkg.Failed = true
	}

	// Update package status
	if test.Passed {
		pkg.Passed = true
	}

	// Clear active subtest if this was a subtest
	if test.Parent != nil {
		test.Parent.activeSubtest = ""
	} else {
		// Print test result immediately (only for top-level tests)
		f.printTestResult(pkg, test)
	}

	f.currentOutput = nil
}

func (f *Formatter) getOrCreatePackage(pkgName string) *PackageResult {
	if pkg, exists := f.packages[pkgName]; exists {
		return pkg
	}

	pkg := &PackageResult{
		Name:  pkgName,
		Tests: make(map[string]*TestResult),
	}
	f.packages[pkgName] = pkg
	return pkg
}

func (f *Formatter) parseTime(timeStr string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, timeStr); err == nil {
		return t
	}
	return time.Now()
}

func (f *Formatter) generateOutputs() error {
	if f.config.JUnitOutput != "" {
		if err := f.generateJUnitReports(); err != nil {
			return fmt.Errorf("failed to generate JUnit reports: %v", err)
		}
	}

	// Print summary at the end
	if err := f.printSummary(); err != nil {
		return fmt.Errorf("failed to print summary: %v", err)
	}

	return nil
}

func (f *Formatter) generateJUnitReports() error {
	// Group tests by package for JUnit
	suites := make([]*JUnitTestSuite, 0)

	for pkgName, pkg := range f.packages {
		suite := &JUnitTestSuite{
			Name:      pkgName,
			TestCases: make([]JUnitTestCase, 0),
			Timestamp: time.Now().Format(time.RFC3339),
		}

		var totalTime float64
		for testName, test := range pkg.Tests {
			// Apply JUnit filter
			if !f.shouldIncludeTest(test, f.config.JUnitFilter) {
				continue
			}

			// Add main test
			testCase := JUnitTestCase{
				Classname: pkgName,
				Name:      testName,
				Time:      test.Elapsed,
			}

			if test.Failed {
				suite.Failures++
				testCase.Failure = &JUnitFailure{
					Message: "Test failed",
					Type:    "failure",
					Content: "Test execution failed",
				}
			} else if test.Action == "skip" {
				suite.Skipped++
			}

			// Add output
			output := f.formatTestOutput(test, f.config.JUnitTruncate, f.config.JUnitTimestamp)
			if output != "" {
				testCase.SystemOut = output
			}

			suite.TestCases = append(suite.TestCases, testCase)
			suite.Tests++
			totalTime += test.Elapsed

			// Add subtests
			allSubtests := f.getAllSubtests(test)
			for _, subtest := range allSubtests {
				// Apply JUnit filter
				if !f.shouldIncludeTest(subtest, f.config.JUnitFilter) {
					continue
				}

				subtestCase := JUnitTestCase{
					Classname: pkgName,
					Name:      subtest.Test,
					Time:      subtest.Elapsed,
				}

				if subtest.Failed {
					suite.Failures++
					subtestCase.Failure = &JUnitFailure{
						Message: "Test failed",
						Type:    "failure",
						Content: "Test execution failed",
					}
				} else if subtest.Action == "skip" {
					suite.Skipped++
				}

				// Add output
				subtestOutput := f.formatTestOutput(subtest, f.config.JUnitTruncate, f.config.JUnitTimestamp)
				if subtestOutput != "" {
					subtestCase.SystemOut = subtestOutput
				}

				suite.TestCases = append(suite.TestCases, subtestCase)
				suite.Tests++
				totalTime += subtest.Elapsed
			}
		}

		suite.Time = totalTime

		if len(suite.TestCases) > 0 {
			suites = append(suites, suite)
		}
	}

	return f.writeSingleJUnitReport(suites)
}

func (f *Formatter) writeSingleJUnitReport(suites []*JUnitTestSuite) error {
	file, err := os.Create(f.config.JUnitOutput)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	// Write XML header
	fmt.Fprintf(writer, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(writer, "<testsuites>\n")

	// Write each suite
	for _, suite := range suites {
		suiteXML, err := xml.MarshalIndent(suite, "  ", "    ")
		if err != nil {
			return err
		}
		writer.Write(suiteXML)
		fmt.Fprintf(writer, "\n")
	}

	fmt.Fprintf(writer, "</testsuites>\n")
	return nil
}

func (f *Formatter) shouldIncludeTest(test *TestResult, filter string) bool {
	switch filter {
	case "non-failed":
		return !test.Failed
	case "non-passed":
		return !test.Passed
	case "failed":
		return test.Failed
	case "passed":
		return test.Passed
	case "all", "":
		return true
	default:
		return true
	}
}

func (f *Formatter) formatTestOutput(test *TestResult, truncate int, addTimestamp bool) string {
	if len(test.Output) == 0 {
		return ""
	}

	var lines []string
	for _, output := range test.Output {
		if addTimestamp {
			lines = append(lines, fmt.Sprintf("[%s] %s", output.Time.Format("15:04:05.000"), output.Line))
		} else {
			lines = append(lines, output.Line)
		}
	}
	output := strings.Join(lines, "")

	if truncate > 0 {
		lines = strings.Split(output, "\n")
		if len(lines) > truncate {
			// Keeping only beginning and end lines
			begin := lines[:truncate/2]
			lines = lines[len(lines)-truncate/2:]
			output = strings.Join(begin, "\n") + "\n\n... (truncated) ...\n\n" + strings.Join(lines, "\n")
		}
	}

	return output
}

func (f *Formatter) getAllSubtests(test *TestResult) []*TestResult {
	var allSubtests []*TestResult
	for _, subtest := range test.Subtests {
		allSubtests = append(allSubtests, subtest)
		allSubtests = append(allSubtests, f.getAllSubtests(subtest)...)
	}
	return allSubtests
}

func (f *Formatter) parseSize(sizeStr string) (int64, error) {
	if sizeStr == "" {
		return 0, nil
	}

	sizeStr = strings.ToUpper(sizeStr)
	var multiplier int64 = 1

	if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	}

	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return 0, err
	}

	return size * multiplier, nil
}

func (f *Formatter) findParentTest(pkg *PackageResult, testName string) (*TestResult, bool) {
	// Check if this is a subtest (contains "/")
	if !strings.Contains(testName, "/") {
		return nil, false
	}

	// Extract parent test name (everything before the last "/")
	parts := strings.Split(testName, "/")
	parentName := strings.Join(parts[:len(parts)-1], "/")

	// Look for parent test in package tests
	if parentTest, exists := pkg.Tests[parentName]; exists {
		return parentTest, true
	}

	// Recursively check subtests
	for _, test := range pkg.Tests {
		if parentTest := f.findParentTestRecursive(test, parentName); parentTest != nil {
			return parentTest, true
		}
	}

	return nil, false
}

func (f *Formatter) findParentTestRecursive(test *TestResult, parentName string) *TestResult {
	if test.Test == parentName {
		return test
	}

	for _, subtest := range test.Subtests {
		if result := f.findParentTestRecursive(subtest, parentName); result != nil {
			return result
		}
	}

	return nil
}

func (f *Formatter) findTest(pkg *PackageResult, testName string) *TestResult {
	// First check direct tests
	if test, exists := pkg.Tests[testName]; exists {
		return test
	}

	// Then check recursively in subtests
	for _, test := range pkg.Tests {
		if result := f.findTestRecursive(test, testName); result != nil {
			return result
		}
	}

	return nil
}

func (f *Formatter) findTestRecursive(test *TestResult, testName string) *TestResult {
	if test.Test == testName {
		return test
	}

	for _, subtest := range test.Subtests {
		if result := f.findTestRecursive(subtest, testName); result != nil {
			return result
		}
	}

	return nil
}

func (f *Formatter) printTestResult(pkg *PackageResult, test *TestResult) {
	// Apply stdout filter
	if !f.shouldIncludeTest(test, f.config.StdoutFilter) {
		return
	}

	// Test status
	var icon, status string
	if test.Passed {
		icon = "✅"
		status = "PASS"
		if f.config.StdoutColor {
			status = colorGreen + status + colorReset
		}
	} else if test.Failed {
		icon = "❌"
		status = "FAIL"
		if f.config.StdoutColor {
			status = colorRed + status + colorReset
		}
	} else {
		icon = "⏭️"
		status = "SKIP"
		if f.config.StdoutColor {
			status = colorYellow + status + colorReset
		}
	}

	// Extract test name (remove parent prefix for display)
	displayName := test.Test
	if test.Parent != nil {
		parts := strings.Split(test.Test, "/")
		displayName = parts[len(parts)-1]
	}

	out := fmt.Sprintf("╒━ %s %s (%s) - %.3fs", icon, displayName, status, test.Elapsed)
	if f.config.StdoutTimestamp {
		fmt.Printf("[%s] %s", test.StartTime.Format("15:04:05.000"), out)
	} else {
		fmt.Printf(out)
	}

	// Print package as well
	pkgIcon := "📦"
	if f.config.StdoutColor {
		fmt.Printf(" (%s%s%s %s%s%s)\n", colorBlue, pkgIcon, colorReset, colorBold, pkg.Name, colorReset)
	} else {
		fmt.Printf(" (%s %s)\n", pkgIcon, pkg.Name)
	}

	// Organize and print output with subtests
	f.printOrganizedOutput(test, 1)

	out = fmt.Sprintf("╘━ %s %s (%s) - %.3fs\n", icon, displayName, status, test.Elapsed)
	if f.config.StdoutTimestamp {
		fmt.Printf("[%s] %s", test.EndTime.Format("15:04:05.000"), out)
	} else {
		fmt.Printf(out)
	}
	fmt.Println()
}

func (f *Formatter) printOrganizedOutput(test *TestResult, indent int) {
	indentStr := strings.Repeat(" │", indent)

	// Sort subtests by start time
	subtestNames := make([]string, 0, len(test.Subtests))
	for subtestName := range test.Subtests {
		subtestNames = append(subtestNames, subtestName)
	}
	sort.Slice(subtestNames, func(i, j int) bool {
		return test.Subtests[subtestNames[i]].StartTime.Before(test.Subtests[subtestNames[j]].StartTime)
	})

	// Print output lines, inserting subtest runs and results at the right time
	outputIndex := 0
	subtestIndex := 0

	for outputIndex < len(test.Output) {
		currentOutput := test.Output[outputIndex]

		// Check if we need to insert subtest runs before this output
		for subtestIndex < len(subtestNames) {
			subtest := test.Subtests[subtestNames[subtestIndex]]
			if subtest.StartTime.Before(currentOutput.Time) {
				// Print subtest run
				f.printSubtestRun(subtest, indent)
				// Print subtest output block
				f.printSubtestOutput(subtest, indent+1)
				// Print subtest result
				f.printSubtestResult(subtest, indent)
				subtestIndex++
			} else {
				break
			}
		}

		// Print current output line
		if f.config.StdoutTimestamp {
			fmt.Printf("[%s]%s %s", currentOutput.Time.Format("15:04:05.000"), indentStr, currentOutput.Line)
		} else {
			fmt.Printf("%s%s", indentStr, currentOutput.Line)
		}
		outputIndex++
	}

	// Print any remaining subtests
	for subtestIndex < len(subtestNames) {
		subtest := test.Subtests[subtestNames[subtestIndex]]
		f.printSubtestRun(subtest, indent)
		f.printSubtestOutput(subtest, indent+1)
		f.printSubtestResult(subtest, indent)
		subtestIndex++
	}
}

func (f *Formatter) printSubtestRun(subtest *TestResult, indent int) {
	indentStr := strings.Repeat(" │", indent)

	if f.config.StdoutTimestamp {
		fmt.Printf("[%s]%s ┍━ RUN %s\n", subtest.StartTime.Format("15:04:05.000"), indentStr, subtest.Test)
	} else {
		fmt.Printf("%s┍━ RUN   %s\n", indentStr, subtest.Test)
	}
}

func (f *Formatter) printSubtestResult(subtest *TestResult, indent int) {
	indentStr := strings.Repeat(" │", indent)
	displayName := strings.Split(subtest.Test, "/")[len(strings.Split(subtest.Test, "/"))-1]

	var status string
	if subtest.Passed {
		status = "✅ PASS"
	} else if subtest.Failed {
		status = "❌ FAIL"
	} else {
		status = "⏭️ SKIP"
	}

	if f.config.StdoutTimestamp {
		fmt.Printf("[%s]%s ┕━ %s: %s (%.3fs)\n", subtest.EndTime.Format("15:04:05.000"), indentStr, status, displayName, subtest.Elapsed)
	} else {
		fmt.Printf("%s┕━ %s: %s (%.3fs)\n", indentStr, status, displayName, subtest.Elapsed)
	}
}

func (f *Formatter) printSubtestOutput(subtest *TestResult, indent int) {
	indentStr := strings.Repeat(" │", indent)

	for _, output := range subtest.Output {
		if f.config.StdoutTimestamp {
			fmt.Printf("[%s]%s %s", output.Time.Format("15:04:05.000"), indentStr, output.Line)
		} else {
			fmt.Printf("%s%s", indentStr, output.Line)
		}
	}
}

func (f *Formatter) printSummary() error {
	// Collect all tests and subtests
	var allTests []*TestResult
	var failedTests []string

	totalTests := 0
	totalPassed := 0
	totalFailed := 0
	totalSkipped := 0

	for _, pkg := range f.packages {
		for _, test := range pkg.Tests {
			totalTests++
			allTests = append(allTests, test)

			if test.Passed {
				totalPassed++
			} else if test.Failed {
				totalFailed++
				failedTests = append(failedTests, pkg.Name + ":" +test.Test)
			} else {
				totalSkipped++
			}

			// Add subtests to failed list
			allSubtests := f.getAllSubtests(test)
			for _, subtest := range allSubtests {
				if subtest.Failed {
					failedTests = append(failedTests, pkg.Name + ":" +subtest.Test)
				}
			}
		}
	}

	// Calculate statistics
	var totalLines, totalTime float64
	var lineCounts []int
	var timeCounts []float64

	for _, test := range allTests {
		lines := len(test.Output)
		for _, subtest := range test.Subtests {
			lines += len(subtest.Output)
		}
		lineCounts = append(lineCounts, lines)
		timeCounts = append(timeCounts, test.Elapsed)
		totalLines += float64(lines)
		totalTime += test.Elapsed
	}

	// Find min/max
	var minLines, maxLines int
	var minTime, maxTime float64
	if len(lineCounts) > 0 {
		minLines = lineCounts[0]
		maxLines = lineCounts[0]
		minTime = timeCounts[0]
		maxTime = timeCounts[0]

		for i := 1; i < len(lineCounts); i++ {
			if lineCounts[i] < minLines {
				minLines = lineCounts[i]
			}
			if lineCounts[i] > maxLines {
				maxLines = lineCounts[i]
			}
			if timeCounts[i] < minTime {
				minTime = timeCounts[i]
			}
			if timeCounts[i] > maxTime {
				maxTime = timeCounts[i]
			}
		}
	}

	avgLines := 0.0
	avgTime := 0.0
	if len(allTests) > 0 {
		avgLines = totalLines / float64(len(allTests))
		avgTime = totalTime / float64(len(allTests))
	}

	fmt.Println()
	if f.config.StdoutColor {
		fmt.Printf("📊 Summary: %d tests total", totalTests)
		if totalPassed > 0 {
			fmt.Printf(", %s%d passed%s", colorGreen, totalPassed, colorReset)
		}
		if totalFailed > 0 {
			fmt.Printf(", %s%d failed%s", colorRed, totalFailed, colorReset)
		}
		if totalSkipped > 0 {
			fmt.Printf(", %s%d skipped%s", colorYellow, totalSkipped, colorReset)
		}
		fmt.Println()

		fmt.Printf("📈 Statistics:\n")
		fmt.Printf("  Lines per test: avg %.1f, min %d, max %d\n", avgLines, minLines, maxLines)
		fmt.Printf("  Time per test: avg %.3fs, min %.3fs, max %.3fs\n", avgTime, minTime, maxTime)

		if len(failedTests) > 0 {
			fmt.Printf("❌ Failed tests:\n")
			for _, failedTest := range failedTests {
				fmt.Printf("  %s%s%s\n", colorRed, failedTest, colorReset)
			}
		}
	} else {
		fmt.Printf("📊 Summary: %d tests total, %d passed, %d failed, %d skipped\n",
			totalTests, totalPassed, totalFailed, totalSkipped)

		fmt.Printf("📈 Statistics:\n")
		fmt.Printf("  Lines per test: avg %.1f, min %d, max %d\n", avgLines, minLines, maxLines)
		fmt.Printf("  Time per test: avg %.3fs, min %.3fs, max %.3fs\n", avgTime, minTime, maxTime)

		if len(failedTests) > 0 {
			fmt.Printf("❌ Failed tests:\n")
			for _, failedTest := range failedTests {
				fmt.Printf("  %s\n", failedTest)
			}
		}
	}

	return nil
}
