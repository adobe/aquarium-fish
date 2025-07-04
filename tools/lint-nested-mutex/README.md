# Mutex Deadlock Linter

A comprehensive Go static analysis tool for detecting mutex-related deadlock issues.

## Features

- **Cross-function mutex conflict detection**: Identifies when a function holding a mutex calls another function that also locks the same mutex
- **Multiple mutex operation types**: Detects conflicts with Lock, RLock, Unlock, and RUnlock operations
- **Accurate mutex instance tracking**: Only reports conflicts when mutexes actually belong to the same object instance
- **Control flow analysis**: Tracks mutex state through sequential statements, conditionals, and defer statements
- **Deep dependency analysis**: Optional analysis of Go module dependencies via `-deep` flag
- **Flexible exclusion**: Skip files/directories using `-exclude` patterns
- **CI/CD integration**: Exit code equals the number of critical errors found
- **Comprehensive reporting**: Detailed issue descriptions with file, line, and column information

## Installation

```bash
go build -o lint-nested-mutex ./tools/lint-nested-mutex/lint-nested-mutex.go
```

## Usage

Basic usage:
```bash
go run ./tools/lint-nested-mutex/lint-nested-mutex.go -- <file_or_dir>...
```

With options:
```bash
go run ./tools/lint-nested-mutex/lint-nested-mutex.go -- [options] <file_or_dir>...
```

### Options

- `-deep`: Follow and analyze Go module dependencies (slower but more thorough)
- `-verbose`: Enable debug output to stderr showing analysis details
- `-exclude <pattern>`: Exclude files/directories matching glob pattern (can be used multiple times)

### Examples

Analyze a single file:
```bash
go run ./tools/lint-nested-mutex/lint-nested-mutex.go -- ./lib/database/application.go
```

Analyze a directory excluding tests:
```bash
go run ./tools/lint-nested-mutex/lint-nested-mutex.go -- -exclude "tests" ./lib/
```

Analyze with dependency scanning and verbose output:
```bash
go run ./tools/lint-nested-mutex/lint-nested-mutex.go -- -deep -verbose -exclude "vendor" ./
```

Multiple exclusions:
```bash
go run ./tools/lint-nested-mutex/lint-nested-mutex.go -- -exclude "tests" -exclude "*.pb.go" ./
```

## Overview

This tool analyzes Go source code to detect various mutex-related issues that can lead to deadlocks, including:

- **Nested RLocks**: When a function holds an RLock and calls another function that also attempts to RLock the same mutex
- **Nested Locks**: When the same mutex is locked multiple times without proper unlocking
- **Cross-function mutex conflicts**: When functions call each other while holding mutexes
- **Inconsistent lock ordering**: When different functions acquire multiple mutexes in different orders
- **Unmatched locks**: When locks are not properly paired with unlocks

## Issue Categories

### Errors (Critical)

1. **nested-rlock**: Nested RLock on the same mutex within a single function
2. **nested-lock**: Nested Lock on the same mutex within a single function
3. **cross-function-nested-rlock**: RLock in caller, RLock in callee on same mutex
4. **cross-function-nested-lock**: Lock in caller, Lock in callee on same mutex
5. **cross-function-lock-conflict**: RLock in caller, Lock in callee on same mutex
6. **nested-function-rlock**: General nested RLock across function calls
7. **nested-function-lock**: General nested Lock across function calls
8. **inconsistent-lock-order**: Different functions acquire multiple mutexes in different orders
9. **unmatched-lock**: Lock without corresponding unlock
10. **unmatched-rlock**: RLock without corresponding RUnlock

### Warnings

1. **multiple-mutex**: Function uses multiple mutexes (potential deadlock risk)

## Example Output

```
Found 2 mutex issues:

[ERROR] lib/database/application.go:42:2: Nested RLock detected across functions: database.(Database).ApplicationCreate holds RLock on 'd.beMu' and calls database.(Database).ApplicationStateCreate which also attempts RLock on the same mutex
    Category: cross-function-nested-rlock
    Function: database.(Database).ApplicationCreate
    Mutex: d.beMu
    Call Chain: database.(Database).ApplicationCreate -> database.(Database).ApplicationStateCreate

[WARNING] lib/example/multi.go:15:2: Function uses multiple mutexes: [mu1, mu2] - potential deadlock risk
    Category: multiple-mutex
    Function: example.processData
    Mutex: mu1, mu2

Summary: 1 errors, 1 warnings
```

## Real-World Example

The tool successfully detected the deadlock issue in your `ApplicationCreate` function:

```go
func (d *Database) ApplicationCreate(a *typesv2.Application) error {
    d.beMu.RLock()              // First RLock
    defer d.beMu.RUnlock()

    // ... code ...

    d.ApplicationStateCreate(&typesv2.ApplicationState{  // Calls function that also RLocks
        ApplicationUid: a.Uid,
        Status: typesv2.ApplicationState_NEW,
        Description: "Just created by Fish " + d.node.Name,
    })
    return err
}
```

Where `ApplicationStateCreate` also does:
```go
func (d *Database) ApplicationStateCreate(as *typesv2.ApplicationState) error {
    d.beMu.RLock()              // Nested RLock!
    defer d.beMu.RUnlock()
    // ...
}
```

This creates a potential deadlock if another goroutine tries to acquire a write lock between the two RLocks.

## Integration

You can integrate this tool into your CI/CD pipeline or pre-commit hooks:

```bash
#!/bin/bash
# check.sh
go run ./tools/lint-nested-mutex/lint-nested-mutex.go -- ./lib/
if [ $? -ne 0 ]; then
    echo "Mutex deadlock issues found!"
    exit 1
fi
```

## Limitations

- Uses heuristics for method call resolution (works well for common patterns)
- Doesn't perform full type analysis (would require additional dependencies)
- May have false positives with complex inheritance patterns
- Deferred operations are handled but may not catch all edge cases

## Contributing

The tool can be extended to detect additional mutex-related patterns by:
1. Adding new issue categories to the `Issue` struct
2. Implementing detection logic in the analysis functions
3. Adding corresponding test cases
