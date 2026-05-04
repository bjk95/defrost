# Defrost Wrapper MVP — Design

**Date:** 2026-04-29
**Status:** Draft, pending implementation

## Purpose

Build the smallest possible end of the `defrost` pipeline: a CLI that runs `go test ... -json`, parses each event into a Go struct, and re-emits it. This proves the parsing path works and gives later phases (server upload, classification, verdict application) a stable foundation to build on.

Out of scope for this spec: server, database, classifier, verdicts, retries, summary output, multi-language support.

## User-facing behavior

The user invokes:

```
defrost exec go test ./... -json
```

Everything after `exec` is treated as the literal child command and its arguments.

The wrapper:

1. Spawns the child command via `os/exec`.
2. Reads the child's stdout line-by-line.
3. For each line, attempts to decode it as a `TestEvent` JSON object.
   - On success: re-emits the event as a single-line JSON object to the wrapper's stdout.
   - On failure (line is not valid JSON for a `TestEvent`): writes the line verbatim to the wrapper's stdout. This preserves build errors, compile failures, and any framing output that `cmd/test2json` did not produce as events.
4. Streams the child's stderr verbatim to the wrapper's stderr.
5. Waits for the child to exit and uses the child's exit code as its own.

The wrapper does not transform exit codes, summarise results, retry tests, or contact any server. A successful run of the wrapper looks indistinguishable from `go test ... -json` to a downstream consumer.

## Components

### `go/main.go`
Entry point. Parses the first positional arg, dispatches to the CLI handler, exits with the returned exit code. Stays trivial.

### `go/cli.go`
CLI parsing and process orchestration. Responsibilities:

- Validate that argv[1] is `exec` and that at least one further arg follows.
- Build an `*exec.Cmd` from the remaining args.
- Wire the child's stdout into `result_collector.Stream`.
- Wire the child's stderr to `os.Stderr`.
- Start the child, wait for it, and return the child's exit code.

`cli.go` does not know what a `TestEvent` is. It only owns process and stream wiring.

### `go/result_collector/golang.go`
Owns the `TestEvent` schema and the parse-and-re-emit loop:

```go
type TestEvent struct {
    Time    time.Time
    Action  string
    Package string
    Test    string
    Elapsed float64
    Output  string
}

func Stream(r io.Reader, out io.Writer) error
```

`Stream` reads `r` line by line. For each line:
- If it decodes into `TestEvent`, marshal back to JSON and write one line to `out`.
- Otherwise, write the original bytes (with their trailing newline) to `out`.

`Stream` returns an error only on I/O failure of `r` or `out`, not on per-line decode failures (those are the trigger for passthrough).

### `go/result_collector/golang_test.go`
Table-driven tests that feed `Stream` a fixed `bytes.Buffer` of fixture lines and assert the output buffer. Fixtures cover:
- A single valid `TestEvent` line round-trips to a JSON line.
- A non-JSON line passes through verbatim.
- A mix of the above preserves order.

The test does not invoke `go test` itself. The fixture lines are the authoritative input.

## Data flow

```
user shell ──exec──► defrost
                     │
                     ├── argv[2:] ──► os/exec.Cmd ──► go test -json (child)
                     │                                 │
                     │                          stdout │ stderr
                     │                                 ▼     ▼
                     │                  Stream(stdout, os.Stdout)
                     │                  io.Copy(os.Stderr, stderr)
                     │
                     └── waits, propagates exit code
```

## Error handling

- Missing or wrong subcommand: print a one-line usage message to stderr, exit 2.
- Child fails to start (binary not found, permission denied): print the `exec` error to stderr, exit 1.
- Child exits non-zero: wrapper exits with the same code. No extra output.
- I/O error reading child stdout: print error to stderr, exit 1. (Should be rare; indicates pipe failure.)
- Per-line JSON decode failure: not an error — triggers verbatim passthrough by design.

## Testing

Unit tests live next to `result_collector.Stream`. Coverage targets only the parse-and-re-emit contract:
- Valid `TestEvent` line in → JSON line out.
- Invalid JSON line in → verbatim line out.
- Mixed input preserves line order.

No tests for `cli.go` or `main.go` in this MVP. The CLI plumbing is small enough that the existing manual smoke (`defrost exec go test ./...`) is the verification.

## Non-goals

The following are explicitly *not* part of this spec and must not appear in the implementation:

- HTTP client, server, or upload logic.
- Database, persistence, or local cache.
- Verdict classification, retry logic, or exit-code rewriting based on test outcomes.
- Human-readable summary output.
- Support for languages other than Go.
- Configuration files or flags beyond `defrost exec <cmd...>`.

These are deferred to follow-up specs once the wrapper is in place and the wire contract for the server is being designed.
