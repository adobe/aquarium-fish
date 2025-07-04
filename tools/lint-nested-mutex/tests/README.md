# Mutex Linting Tool Test Cases

This directory contains test cases for the nested mutex linting tool. Each test file is designed to test a specific type of mutex conflict or safe pattern.

## Test Files

### `test1_nested_rlock.go`
- **Issue Type**: Nested RLock across functions
- **Expected Result**: 2 errors (nested RLock + RLock/RUnlock conflict)
- **Description**: ParentFunction holds RLock and calls ChildFunction which also uses RLock on the same mutex

### `test2_nested_lock.go`
- **Issue Type**: Nested Lock across functions
- **Expected Result**: 2 errors (nested Lock + Lock/Unlock conflict)
- **Description**: WriteData holds Lock and calls InternalWrite which also uses Lock on the same mutex

### `test3_mixed_lock_conflict.go`
- **Issue Type**: Mixed Lock/RLock conflict
- **Expected Result**: 2 errors (RLock/Lock conflict + RLock/Unlock conflict)
- **Description**: ReadAndUpdate holds RLock and calls Update which uses Lock on the same mutex

### `test4_different_mutexes.go`
- **Issue Type**: Different mutexes (safe pattern)
- **Expected Result**: 0 errors
- **Description**: Functions use different mutexes, so no conflicts should occur

### `test5_safe_pattern.go`
- **Issue Type**: Safe pattern (no conflicts)
- **Expected Result**: 0 errors
- **Description**: Function calls happen before lock acquisition, demonstrating safe usage

## Running Tests

To test all cases:
```bash
go run ../lint-nested-mutex.go -- .
```

To test individual files:
```bash
go run ../lint-nested-mutex.go -- test1_nested_rlock.go
```

With verbose output:
```bash
go run ../lint-nested-mutex.go -- -verbose .
```

## Expected Total
When running on all test files: **6 total errors**
- test1: 2 errors
- test2: 2 errors
- test3: 2 errors
- test4: 0 errors
- test5: 0 errors

## Exit Code Verification
The tool exits with the number of errors found:
- Single file tests: Exit code equals number of errors in that file
- All files: Exit code 6 (total errors across all files)
- Clean files: Exit code 0
