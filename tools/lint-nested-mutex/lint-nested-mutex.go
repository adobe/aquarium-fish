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

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// LintContext holds the analysis context
type LintContext struct {
	fset            *token.FileSet
	issues          []Issue
	functionInfo    map[string]*FunctionInfo
	verboseMode     bool
	deepMode        bool
	analyzedPkgs    map[string]bool // Track analyzed packages to avoid infinite recursion
	pendingImports  []string        // Track imports to analyze in deep mode
	excludePatterns []string        // Track exclude patterns for filtering files/directories
}

// FunctionInfo contains information about a function
type FunctionInfo struct {
	name      string
	mutexOps  []MutexOp
	callSites []CallSite // Track where function calls are made and mutex state at that point
}

// MutexOp represents a mutex operation
type MutexOp struct {
	mutexName string
	opType    string // "Lock", "RLock", "Unlock", "RUnlock"
	pos       token.Pos
}

// MutexState represents the state of a mutex at a given point
type MutexState struct {
	isLocked bool
	lockType string // "Lock", "RLock"
	lockPos  token.Pos
}

// CallSite represents a function call and the mutex state at that point
type CallSite struct {
	calledFunction string
	pos            token.Pos
	mutexState     map[string]MutexState // snapshot of mutex state at this call site
}

// Issue represents a detected mutex issue
type Issue struct {
	file        string
	line        int
	col         int
	severity    string
	category    string
	function    string
	mutexName   string
	description string
}

func main() {
	var deepMode bool
	var verboseMode bool
	var excludePatterns []string
	var paths []string

	// Parse command line arguments
	for i, arg := range os.Args[1:] {
		if arg == "-deep" {
			deepMode = true
		} else if arg == "-verbose" {
			verboseMode = true
		} else if arg == "-exclude" {
			// Get the next argument as the exclude pattern
			if i+1 < len(os.Args)-1 {
				excludePatterns = append(excludePatterns, os.Args[i+2])
			}
		} else if !strings.HasPrefix(arg, "-") {
			paths = append(paths, os.Args[i+1])
		}
	}

	if len(paths) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s [-deep] [-verbose] [-exclude pattern] <file_or_dir>...\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  -deep: Follow and analyze Go module dependencies\n")
		fmt.Fprintf(os.Stderr, "  -verbose: Enable debug output to stderr\n")
		fmt.Fprintf(os.Stderr, "  -exclude: Exclude files/directories matching glob pattern (can be used multiple times)\n")
		os.Exit(1)
	}

	ctx := &LintContext{
		fset:            token.NewFileSet(),
		functionInfo:    make(map[string]*FunctionInfo),
		verboseMode:     verboseMode,
		deepMode:        deepMode,
		analyzedPkgs:    make(map[string]bool),
		excludePatterns: excludePatterns,
	}

	// Process all paths
	for _, path := range paths {
		if err := processPath(ctx, path); err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", path, err)
			continue
		}
	}

	// In deep mode, process discovered imports
	if deepMode {
		ctx.processPendingImports()
	}

	// Analyze for issues
	analyzeIssues(ctx)

	// Report issues
	criticalCount := reportIssues(ctx)

	os.Exit(criticalCount)
}

func processPath(ctx *LintContext, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Check if path should be excluded
			if shouldExclude(ctx, path) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				if !ctx.deepMode && !isProjectFile(path) {
					return nil
				}
				return processFile(ctx, path)
			}
			return nil
		})
	} else if strings.HasSuffix(path, ".go") {
		// Check if single file should be excluded
		if shouldExclude(ctx, path) {
			return nil
		}
		return processFile(ctx, path)
	}

	return nil
}

// shouldExclude checks if a path should be excluded based on exclude patterns
func shouldExclude(ctx *LintContext, path string) bool {
	for _, pattern := range ctx.excludePatterns {
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err == nil && matched {
			return true
		}
		// Also try matching against the full path
		matched, err = filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
		// Try matching directory components
		if strings.Contains(path, filepath.Clean(pattern)) {
			return true
		}
	}
	return false
}

func isProjectFile(path string) bool {
	return !strings.Contains(path, "/vendor/") &&
		!strings.Contains(path, "/.git/") &&
		!strings.Contains(path, "/node_modules/")
}

func processFile(ctx *LintContext, filename string) error {
	if ctx.verboseMode {
		fmt.Fprintf(os.Stderr, "Processing file: %s\n", filename)
	}

	src, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	file, err := parser.ParseFile(ctx.fset, filename, src, parser.ParseComments)
	if err != nil {
		return err
	}

	// In deep mode, collect imports for later analysis
	if ctx.deepMode {
		collectImports(ctx, file)
	}

	// Extract function information
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			if node.Name != nil {
				funcInfo := analyzeFunction(ctx, node)
				ctx.functionInfo[funcInfo.name] = funcInfo
				if ctx.verboseMode {
					fmt.Fprintf(os.Stderr, "  Found function: %s\n", funcInfo.name)
				}
			}
		}
		return true
	})

	return nil
}

func collectImports(ctx *LintContext, file *ast.File) {
	for _, imp := range file.Imports {
		if imp.Path != nil {
			importPath := strings.Trim(imp.Path.Value, `"`)
			// Skip standard library packages
			if !isStdLibPackage(importPath) && !ctx.analyzedPkgs[importPath] {
				ctx.pendingImports = append(ctx.pendingImports, importPath)
			}
		}
	}
}

func isStdLibPackage(pkg string) bool {
	// Common standard library prefixes
	stdPrefixes := []string{
		"fmt", "os", "io", "net", "time", "sync", "strings", "strconv",
		"context", "errors", "log", "path", "crypto", "encoding",
		"go/", "reflect", "runtime", "sort", "testing", "unsafe",
		"archive/", "bufio", "bytes", "compress/", "container/",
		"database/", "debug/", "expvar", "flag", "hash/", "html/",
		"image/", "index/", "math/", "mime/", "net/", "plugin",
		"regexp", "syscall", "text/", "unicode/",
	}

	for _, prefix := range stdPrefixes {
		if strings.HasPrefix(pkg, prefix) {
			return true
		}
	}
	return false
}

func (ctx *LintContext) processPendingImports() {
	if ctx.verboseMode && len(ctx.pendingImports) > 0 {
		fmt.Fprintf(os.Stderr, "Deep mode: analyzing %d dependencies\n", len(ctx.pendingImports))
	}

	for len(ctx.pendingImports) > 0 {
		importPath := ctx.pendingImports[0]
		ctx.pendingImports = ctx.pendingImports[1:]

		if ctx.analyzedPkgs[importPath] {
			continue
		}

		ctx.analyzedPkgs[importPath] = true

		if ctx.verboseMode {
			fmt.Fprintf(os.Stderr, "  Analyzing dependency: %s\n", importPath)
		}

		if err := ctx.analyzePackage(importPath); err != nil {
			if ctx.verboseMode {
				fmt.Fprintf(os.Stderr, "    Error analyzing %s: %v\n", importPath, err)
			}
		}
	}
}

func (ctx *LintContext) analyzePackage(packagePath string) error {
	// Use go list to find the package directory
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", packagePath)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to find package %s: %v", packagePath, err)
	}

	packageDir := strings.TrimSpace(string(output))
	if packageDir == "" {
		return fmt.Errorf("package directory not found for %s", packagePath)
	}

	// Process all Go files in the package directory
	return filepath.Walk(packageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			// Avoid processing files outside the specific package (subdirectories)
			if filepath.Dir(path) == packageDir {
				return processFile(ctx, path)
			}
		}
		return nil
	})
}

func analyzeFunction(ctx *LintContext, fn *ast.FuncDecl) *FunctionInfo {
	funcName := getFunctionName(fn)

	funcInfo := &FunctionInfo{
		name:      funcName,
		callSites: make([]CallSite, 0),
	}

	// Track control flow through the function
	if fn.Body != nil {
		mutexState := make(map[string]MutexState)
		analyzeBlockStatements(ctx, fn.Body.List, funcInfo, mutexState)
	}

	return funcInfo
}

// analyzeBlockStatements processes a list of statements sequentially
func analyzeBlockStatements(ctx *LintContext, stmts []ast.Stmt, funcInfo *FunctionInfo, mutexState map[string]MutexState) {
	for _, stmt := range stmts {
		analyzeStatement(ctx, stmt, funcInfo, mutexState)
	}
}

func analyzeStatement(ctx *LintContext, stmt ast.Stmt, funcInfo *FunctionInfo, mutexState map[string]MutexState) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		analyzeExpression(ctx, s.X, funcInfo, mutexState)
	case *ast.DeferStmt:
		// Handle defer statements - they execute at function end, so we DON'T change current state
		// We just track the mutex operation for completeness but don't modify the running state
		handleDeferStatement(ctx, s.Call, funcInfo)
	case *ast.IfStmt:
		// Analyze condition first
		if s.Cond != nil {
			analyzeExpression(ctx, s.Cond, funcInfo, mutexState)
		}
		// For if statements, we need to analyze both branches but they don't affect each other
		// Save current state
		savedState := copyMutexState(mutexState)

		// Analyze if body
		analyzeBlockStatements(ctx, s.Body.List, funcInfo, mutexState)

		// Restore state and analyze else branch if exists
		if s.Else != nil {
			// Reset to saved state
			for k := range mutexState {
				delete(mutexState, k)
			}
			for k, v := range savedState {
				mutexState[k] = v
			}
			analyzeStatement(ctx, s.Else, funcInfo, mutexState)
		}
	case *ast.BlockStmt:
		analyzeBlockStatements(ctx, s.List, funcInfo, mutexState)
	case *ast.ForStmt:
		// For loops - analyze body without changing our mutex state assumptions
		if s.Body != nil {
			analyzeBlockStatements(ctx, s.Body.List, funcInfo, mutexState)
		}
	case *ast.RangeStmt:
		// Range loops - analyze body without changing our mutex state assumptions
		if s.Body != nil {
			analyzeBlockStatements(ctx, s.Body.List, funcInfo, mutexState)
		}
	case *ast.AssignStmt:
		// Check for assignments that might contain function calls on the right side
		for _, expr := range s.Rhs {
			analyzeExpression(ctx, expr, funcInfo, mutexState)
		}
	case *ast.ReturnStmt:
		// Analyze return expressions
		for _, expr := range s.Results {
			if expr != nil {
				analyzeExpression(ctx, expr, funcInfo, mutexState)
			}
		}
	}
}

func handleDeferStatement(ctx *LintContext, call *ast.CallExpr, funcInfo *FunctionInfo) {
	// For defer statements, we just track the mutex operation but don't modify state
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		selector := sel.Sel.Name
		if isMutexOp(selector) {
			mutexName := getMutexName(call)
			if mutexName != "" {
				mutexOp := MutexOp{
					mutexName: mutexName,
					opType:    selector,
					pos:       call.Pos(),
				}
				funcInfo.mutexOps = append(funcInfo.mutexOps, mutexOp)
			}
		}
	}
}

func analyzeExpression(ctx *LintContext, expr ast.Expr, funcInfo *FunctionInfo, mutexState map[string]MutexState) {
	switch e := expr.(type) {
	case *ast.CallExpr:
		analyzeCallExpression(ctx, e, funcInfo, mutexState)
	}
}

func analyzeCallExpression(ctx *LintContext, call *ast.CallExpr, funcInfo *FunctionInfo, mutexState map[string]MutexState) {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		selector := fun.Sel.Name

		// Check if this is a mutex operation
		if isMutexOp(selector) {
			handleMutexOperation(ctx, call, funcInfo, selector, mutexState)
		} else {
			// This is a regular method call - record it with current mutex state
			calledFunc := getMethodCallName(fun)
			if calledFunc != "" {
				// Create snapshot of current mutex state
				mutexSnapshot := copyMutexState(mutexState)

				callSite := CallSite{
					calledFunction: calledFunc,
					pos:            call.Pos(),
					mutexState:     mutexSnapshot,
				}
				funcInfo.callSites = append(funcInfo.callSites, callSite)
			}
		}
	}
}

func handleMutexOperation(ctx *LintContext, call *ast.CallExpr, funcInfo *FunctionInfo, opType string, mutexState map[string]MutexState) {
	mutexName := getMutexName(call)
	if mutexName == "" {
		return
	}

	mutexOp := MutexOp{
		mutexName: mutexName,
		opType:    opType,
		pos:       call.Pos(),
	}
	funcInfo.mutexOps = append(funcInfo.mutexOps, mutexOp)

	// Update mutex state tracking
	switch opType {
	case "Lock", "RLock":
		mutexState[mutexName] = MutexState{
			isLocked: true,
			lockType: opType,
			lockPos:  call.Pos(),
		}
	case "Unlock", "RUnlock":
		delete(mutexState, mutexName)
	}
}

func copyMutexState(state map[string]MutexState) map[string]MutexState {
	copy := make(map[string]MutexState)
	for k, v := range state {
		copy[k] = v
	}
	return copy
}

func isMutexOp(name string) bool {
	return name == "Lock" || name == "RLock" || name == "Unlock" || name == "RUnlock"
}

func getMutexName(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if x, ok := sel.X.(*ast.SelectorExpr); ok {
			return fmt.Sprintf("%s.%s", getExprName(x.X), x.Sel.Name)
		}
		return getExprName(sel.X)
	}
	return ""
}

func getExprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", getExprName(e.X), e.Sel.Name)
	default:
		return ""
	}
}

func getMethodCallName(sel *ast.SelectorExpr) string {
	receiverName := getExprName(sel.X)
	methodName := sel.Sel.Name

	// Try to determine the package/type for better naming
	// For now, we'll use a more generic approach that doesn't hardcode specific receiver names
	return fmt.Sprintf("%s.%s", receiverName, methodName)
}

func getFunctionName(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		// Method receiver
		recvType := ""
		if starExpr, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
			if ident, ok := starExpr.X.(*ast.Ident); ok {
				recvType = ident.Name
			}
		} else if ident, ok := fn.Recv.List[0].Type.(*ast.Ident); ok {
			recvType = ident.Name
		}
		return fmt.Sprintf("(%s).%s", recvType, fn.Name.Name)
	}
	return fn.Name.Name
}

func analyzeIssues(ctx *LintContext) {
	// Check for cross-function mutex conflicts with proper state tracking
	for funcName, funcInfo := range ctx.functionInfo {
		for _, callSite := range funcInfo.callSites {
			// Try to find the called function by matching different name formats
			var calledFuncInfo *FunctionInfo

			// First try exact match
			calledFuncInfo = ctx.functionInfo[callSite.calledFunction]

			// If not found, try to match by method name only
			if calledFuncInfo == nil {
				for candidateName, candidateInfo := range ctx.functionInfo {
					// Extract method name from both function names for comparison
					if endsWithSameMethodName(candidateName, callSite.calledFunction) {
						calledFuncInfo = candidateInfo
						break
					}
				}
			}

			if calledFuncInfo == nil {
				continue
			}

			// Check if any mutex held at call site conflicts with called function's mutex usage
			for heldMutex, heldState := range callSite.mutexState {
				for _, mutexOp := range calledFuncInfo.mutexOps {
					if mutexOp.mutexName == heldMutex {
						// Additional check: Only report conflicts if it's likely the same mutex instance
						if !isSameMutexInstance(funcName, callSite.calledFunction, heldMutex, calledFuncInfo.name) {
							continue
						}

						// Found a potential conflict - mutex is held and called function also uses it
						pos := ctx.fset.Position(callSite.pos)

						var severity, category, description string

						if heldState.lockType == "RLock" && mutexOp.opType == "RLock" {
							severity = "ERROR"
							category = "NestedRLockCrossFunctions"
							description = fmt.Sprintf("Nested RLock detected across functions: %s holds RLock on '%s' and calls %s which also attempts RLock on the same mutex",
								funcName, heldMutex, callSite.calledFunction)
						} else if heldState.lockType == mutexOp.opType {
							severity = "ERROR"
							category = "NestedSameLockCrossFunctions"
							description = fmt.Sprintf("Nested %s detected across functions: %s holds %s on '%s' and calls %s which also attempts %s on the same mutex",
								mutexOp.opType, funcName, heldState.lockType, heldMutex, callSite.calledFunction, mutexOp.opType)
						} else {
							severity = "ERROR"
							category = "CrossFunctionMutexConflict"
							description = fmt.Sprintf("Cross-function mutex conflict: %s holds %s on '%s' and calls %s which attempts %s on the same mutex",
								funcName, heldState.lockType, heldMutex, callSite.calledFunction, mutexOp.opType)
						}

						ctx.issues = append(ctx.issues, Issue{
							file:        pos.Filename,
							line:        pos.Line,
							col:         pos.Column,
							severity:    severity,
							category:    category,
							function:    funcName,
							mutexName:   heldMutex,
							description: description,
						})
					}
				}
			}
		}
	}
}

// isSameMutexInstance determines if two mutex references likely refer to the same mutex instance
func isSameMutexInstance(callingFunc, calledFuncCall, mutexName, calledFuncDef string) bool {
	// Extract receiver types from function names
	callingReceiverType := extractReceiverType(callingFunc)
	calledReceiverType := extractReceiverType(calledFuncDef)

	// If the called function is not a method (no receiver type), it can't share the same mutex
	if calledReceiverType == "" {
		return false
	}

	// If calling function is not a method, we can't determine mutex instance relationship
	if callingReceiverType == "" {
		return false
	}

	// Only consider it the same mutex if:
	// 1. Both functions are methods on the same receiver type
	// 2. The called function is being called on the same receiver (self-call)
	if callingReceiverType == calledReceiverType {
		// Extract the receiver variable from the function call
		receiverVar := extractReceiverVariable(calledFuncCall)
		mutexReceiver := extractMutexReceiver(mutexName)

		// The call must be on the same receiver variable that owns the mutex
		// AND it must be a direct method call (not a call on a field)
		if receiverVar == mutexReceiver && !isFieldMethodCall(calledFuncCall) {
			return true
		}
	}

	return false
}

// isFieldMethodCall checks if the call is on a field of the receiver (e.g., "e.enforcer.Method")
func isFieldMethodCall(callName string) bool {
	// Count dots - if more than 1, it's likely a call on a field
	dotCount := strings.Count(callName, ".")
	return dotCount > 1
}

// extractReceiverType extracts the receiver type from a function name like "(Database).MethodName"
func extractReceiverType(funcName string) string {
	if strings.HasPrefix(funcName, "(") {
		endIdx := strings.Index(funcName, ")")
		if endIdx > 1 {
			return funcName[1:endIdx]
		}
	}
	return ""
}

// extractReceiverVariable extracts the receiver variable from a call like "d.MethodName"
func extractReceiverVariable(callName string) string {
	if idx := strings.Index(callName, "."); idx > 0 {
		return callName[:idx]
	}
	return ""
}

// extractMutexReceiver extracts the receiver variable from a mutex name like "d.beMu"
func extractMutexReceiver(mutexName string) string {
	if idx := strings.Index(mutexName, "."); idx > 0 {
		return mutexName[:idx]
	}
	return ""
}

// Helper function to check if two function names refer to the same method
func endsWithSameMethodName(fullName, callName string) bool {
	// Extract method name from full name like "(TestStruct).MethodName"
	if idx := strings.LastIndex(fullName, ")."); idx != -1 {
		fullMethodName := fullName[idx+2:]
		// Extract method name from call name like "t.MethodName"
		if idx := strings.LastIndex(callName, "."); idx != -1 {
			callMethodName := callName[idx+1:]
			return fullMethodName == callMethodName
		}
	}
	return false
}

func reportIssues(ctx *LintContext) int {
	if ctx.verboseMode {
		fmt.Fprintf(os.Stderr, "\nAnalysis Summary:\n")
		fmt.Fprintf(os.Stderr, "Found %d functions\n", len(ctx.functionInfo))

		functionsWithMutex := 0
		functionsWithCalls := 0
		for _, funcInfo := range ctx.functionInfo {
			if len(funcInfo.mutexOps) > 0 {
				functionsWithMutex++
			}
			if len(funcInfo.callSites) > 0 {
				functionsWithCalls++
			}
		}
		fmt.Fprintf(os.Stderr, "Functions with mutex operations: %d\n", functionsWithMutex)
		fmt.Fprintf(os.Stderr, "Functions with call sites: %d\n", functionsWithCalls)

		// Show detailed function analysis for ALL functions in verbose mode
		fmt.Fprintf(os.Stderr, "\nDetailed analysis for all functions:\n")
		for funcName, funcInfo := range ctx.functionInfo {
			fmt.Fprintf(os.Stderr, "Function %s:\n", funcName)
			for _, op := range funcInfo.mutexOps {
				fmt.Fprintf(os.Stderr, "  - Mutex op: %s %s\n", op.opType, op.mutexName)
			}
			for _, call := range funcInfo.callSites {
				fmt.Fprintf(os.Stderr, "  - Calls: %s (with %d mutexes held)\n", call.calledFunction, len(call.mutexState))
				for mutex, state := range call.mutexState {
					fmt.Fprintf(os.Stderr, "    - Holding %s: %s\n", mutex, state.lockType)
				}
			}
			if len(funcInfo.mutexOps) == 0 && len(funcInfo.callSites) == 0 {
				fmt.Fprintf(os.Stderr, "  - No operations\n")
			}
		}
		fmt.Fprintf(os.Stderr, "\n")
	}

	if len(ctx.issues) == 0 {
		if ctx.verboseMode {
			fmt.Fprintf(os.Stderr, "No mutex issues found.\n")
		}
		return 0
	}

	// Sort issues by file, line, column
	sort.Slice(ctx.issues, func(i, j int) bool {
		if ctx.issues[i].file != ctx.issues[j].file {
			return ctx.issues[i].file < ctx.issues[j].file
		}
		if ctx.issues[i].line != ctx.issues[j].line {
			return ctx.issues[i].line < ctx.issues[j].line
		}
		return ctx.issues[i].col < ctx.issues[j].col
	})

	criticalCount := 0
	for _, issue := range ctx.issues {
		fmt.Printf("[%s] %s:%d:%d: %s\n",
			issue.severity,
			issue.file,
			issue.line,
			issue.col,
			issue.description)

		if issue.severity == "ERROR" {
			criticalCount++
		}
	}

	return criticalCount
}
