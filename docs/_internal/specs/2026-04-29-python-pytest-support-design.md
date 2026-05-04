# Python (pytest) Support — Design

**Date:** 2026-04-29
**Status:** Draft, pending implementation

## Purpose

Extend `defrost` to wrap pytest invocations the same way it wraps `go test`. After this change, a user with `defrost` and `pytest` already installed can run `defrost exec pytest tests/` (or any of the common pytest invocation forms) and get back parsed `models.TestResult` records — no extra setup, no `pip install`, no flags they have to remember.

This spec also introduces an adapter registry so future languages slot in without growing a `switch` in `exec.go`.

Out of scope for this spec: server upload, classification, retries, exit-code rewriting, summary output, frameworks other than pytest (unittest/nose/doctest), Python venv activation, streaming per-test events, approximating per-test `StartTime`, auto-installing third-party pytest plugins.

## High-level flow

```
user shell ──► defrost exec <pytest-form> [user args]
                  │
                  ├─► runner.Find(cmd) picks the pytest adapter
                  │
                  ├─► adapter spawns: <pytest-form> [user args] \
                  │                   --junitxml=<tempfile> \
                  │                   -o junit_family=xunit2 \
                  │                   -o junit_logging=system-out
                  │
                  │   child stdout ─► defrost stdout (live progress preserved)
                  │   child stderr ─► defrost stderr
                  │
                  ├─► waits, captures exit code
                  ├─► parses tempfile JUnit XML → []models.TestResult
                  ├─► emits results via fmt.Printf("%+v\n", r)
                  └─► deletes tempfile, exits with pytest's code
```

## User-facing behavior

The following invocations all trigger the pytest adapter (any args after `pytest` are passed through):

```
defrost exec pytest tests/
defrost exec python -m pytest tests/
defrost exec python3 -m pytest tests/
defrost exec python3.12 -m pytest tests/
defrost exec poetry run pytest tests/
defrost exec uv run pytest tests/
defrost exec pipenv run pytest tests/
```

The wrapper:

1. Detects pytest from the argv shape (no probing, no env inspection).
2. Appends three flags to the user's argv: `--junitxml=<defrost-tempfile>`, `-o junit_family=xunit2`, `-o junit_logging=system-out`.
3. Streams the child's stdout to the wrapper's stdout verbatim, and stderr to stderr verbatim.
4. Waits for the child to exit and parses the JUnit XML file the child wrote.
5. Emits each `TestResult` to stdout (after the live pytest output) using the same `fmt.Printf("%+v\n", r)` format the Go path uses.
6. Deletes the tempfile and exits with the child's exit code.

If the user already passed `--junitxml=...` (or `--junit-xml`, or the space-separated form), defrost prints an error to stderr and exits 2 — defrost will not silently override an explicit user choice.

If the child fails to start, exits without writing the XML file, or writes an unparseable XML file, defrost logs to stderr and exits 1.

## Components

### `internal/runner/adapter.go`

New package introducing the registry the rest of the codebase dispatches through:

```go
package runner

type Adapter interface {
    Matches(cmd []string) bool
    Run(cmd []string) int   // returns child exit code
}

func Register(a Adapter)
func Find(cmd []string) Adapter   // first match wins; nil if none
```

`Register` is intended to be called from each language adapter's `init()`. `Find` iterates registrations in registration order and returns the first whose `Matches` returns true.

### `internal/runner/registry_test.go`

Verifies registration order, that `Find` returns nil when nothing matches, and that the first matching adapter wins when multiple would match.

### `internal/golang/adapter.go`

New file. Registers the existing Go test path as an `Adapter`:

- `Matches`: `len(cmd) >= 2 && cmd[0] == "go" && cmd[1] == "test"`. (Tighter than today's `strings.HasPrefix(cmd, "exec ")` switch — narrows to the actual case the existing code handles.)
- `Run`: calls the existing `ExecuteGoTest` logic, returning the child's exit code.

### `internal/golang/executor.go`

Edit. `ExecuteGoTest` currently calls `os.Exit` directly. Change it to return an `int` exit code so it slots into the `Adapter.Run` signature. `main.go` becomes the only place `os.Exit` is called.

### `internal/python/pytest/adapter.go`

New. Implements `Adapter` for pytest:

- `Matches`: returns true for any of the three forms (direct, `python -m pytest`, tool-runner).
- `Run`: orchestrates tempfile creation, argv mutation, child spawn, XML parse, result emission, exit-code propagation, and tempfile cleanup.

Detection logic (no regex unless needed; simple string checks):

- `cmd[0] == "pytest"` → match.
- `cmd[0]` is `python` or matches `python3` / `python3.N` AND `cmd[1] == "-m"` AND `cmd[2] == "pytest"` → match.
- `cmd[0] ∈ {"poetry", "uv", "pipenv"}` AND `cmd[1] == "run"` AND `cmd[2] == "pytest"` → match.

`Matches` does not look at user args beyond what's needed to identify the invocation form. The `--junitxml` collision check happens inside `Run` so the error message can name the actual cause (rather than falling through to the generic "no adapter" path).

### `internal/python/pytest/parser.go`

New. Parses a JUnit XML file (`encoding/xml`) into `[]models.TestResult`.

Mapping rules per `<testcase>`:

| `models.TestResult` field | Source |
|---|---|
| `Id` | `classname + "::" + name` |
| `Ran` | `false` if a `<skipped>` child is present, else `true` |
| `Passed` | `true` only if no `<failure>`, `<error>`, or `<skipped>` children |
| `Duration` | `time.Duration(time_attr_seconds * float64(time.Second))` |
| `StartTime` | Zero `time.Time`. JUnit XML has no per-test start; approximating is out of scope. |
| `Output` | Concatenation in this order, separated by newlines, of whichever parts are present: `<failure>` or `<error>` message + body, then `<system-out>`, then `<system-err>`. Skipped tests don't contribute to `Output`. |

The `<failure>` / `<error>` / `<skipped>` distinction collapses into `Passed` / `Ran`, matching what the Go path already does. If finer-grained outcome states are needed later, the model gets extended in a follow-up spec.

### `internal/python/pytest/parser_test.go`

Table-driven tests over fixture XML files in `internal/python/pytest/testdata/`:

- single passing test → `Passed=true`, `Ran=true`, duration parsed
- single failing test → `Passed=false`, `Ran=true`, message in `Output`
- single errored test → `Passed=false`, `Ran=true`, message in `Output`
- single skipped test → `Passed=false`, `Ran=false`
- mixed suite with stdout/stderr captured via `junit_logging=system-out`
- empty suite → empty result slice

### `internal/python/pytest/adapter_test.go`

Table-driven tests over the matcher:

- `pytest` (no args) → match
- `pytest tests/` → match
- `python -m pytest tests/` → match
- `python3 -m pytest tests/` → match
- `python3.12 -m pytest tests/` → match
- `poetry run pytest tests/` → match
- `uv run pytest tests/` → match
- `pipenv run pytest tests/` → match
- `go test ./...` → no match
- `pytest --junitxml=foo.xml` → match (collision is enforced inside `Run`, not the matcher)

### `internal/python/pytest/testdata/`

Fixture XML files used by `parser_test.go`. Each fixture is a minimal valid `xunit2` document covering one row in the parser test table.

### `exec.go`

Edit. Replace the existing `switch cmd[0]` with a registry lookup:

```go
func HandleExecution(cmd []string) {
    a := runner.Find(cmd)
    if a == nil {
        fmt.Fprintf(os.Stderr, "defrost: no adapter for %q\n", cmd)
        os.Exit(2)
    }
    os.Exit(a.Run(cmd))
}
```

### `main.go`

Unchanged.

## Data flow

```
defrost exec pytest tests/
   │
   ▼
exec.HandleExecution(cmd)
   │
   ├─► runner.Find(cmd) → pytest adapter
   │
   ▼
pytest.Adapter.Run(cmd)
   │
   ├─► os.CreateTemp("", "defrost-pytest-*.xml") → path
   ├─► spawn pytest with [user args, --junitxml=path, -o junit_family=xunit2, -o junit_logging=system-out]
   │      child stdout ──► os.Stdout
   │      child stderr ──► os.Stderr
   │
   ├─► cmd.Wait() → exitCode
   │
   ├─► parser.Parse(path) → []models.TestResult, err
   │
   ├─► for r := range results: fmt.Printf("%+v\n", r)
   │
   ├─► defer os.Remove(path)
   │
   └─► return exitCode
```

## Error handling

| Condition | Behaviour |
|---|---|
| User passed their own `--junitxml=` (any form) | Adapter `Matches` still returns true; `Run` detects the collision, prints `defrost: pytest adapter requires control of --junitxml; remove your --junitxml flag` to stderr, returns 2. |
| pytest binary not found | `cmd.Start` returns error; adapter prints to stderr, returns 1. |
| pytest exits non-zero (test failures) | Adapter parses XML normally and returns pytest's exit code unchanged. |
| pytest exited but XML file missing | Adapter prints to stderr, returns 1. |
| pytest exited and XML file is malformed | `encoding/xml` returns an error; adapter prints it to stderr, returns 1. |
| I/O error during stdout/stderr piping | Adapter prints to stderr, returns 1. |
| No adapter matches the cmd | `exec.go` prints `defrost: no adapter for <cmd>` to stderr, exits 2. |

## Testing

Coverage targets only the parts with non-trivial logic:

- `internal/python/pytest/parser_test.go` — JUnit XML → `TestResult` mapping (table-driven, fixtures).
- `internal/python/pytest/adapter_test.go` — matcher (table-driven).
- `internal/runner/registry_test.go` — register/find semantics.

No tests for `executor`-style argv mutation, child-process plumbing, or `exec.go`. The plumbing is small enough that manual smoke (`defrost exec pytest tests/`) is the verification, mirroring the existing Go-side decision in `2026-04-29-defrost-wrapper-mvp-design.md`.

## Non-goals

The following are explicitly *not* part of this spec:

- HTTP client, server, or upload logic.
- Database, persistence, or local cache.
- Verdict classification, retry logic, or exit-code rewriting based on test outcomes.
- Human-readable summary output.
- Support for Python frameworks other than pytest (unittest, nose, doctest, hypothesis-stats).
- Auto-installing `pytest-reportlog` or any third-party pytest plugin.
- Python venv detection or activation.
- Streaming per-test events (JUnit XML is batch-at-end; matches today's Go behaviour).
- Approximating per-test `StartTime` for Python tests.
- Distinguishing `failure` / `error` / `skip` beyond what `Passed` / `Ran` already convey.

These are deferred to follow-up specs once the basic pytest path is in place.
