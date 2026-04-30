# defrost

> Track AI evals, metrics, and tests with Git as the database.

Every team wants a record of how their evals, benchmarks, and tests have changed
over time. Almost nobody wants to host a database for it. **defrost** records
each run as commits on a `_defrost` branch in the same repo, so the history
travels with the code — no database, no SaaS, no API keys.

## What you get

- **Persisted history** — every test run, eval, and metric is recorded automatically. Clone the repo, get the history.
- **Universal instrumentation** — push metrics from any language using a standard OpenTelemetry SDK. No defrost client library to install.
- **Suppression** — mark known-failing tests as suppressed so red CI goes green without skipping them in source.
- **Local dashboard** — `defrost serve` opens your testing, evals, and metrics dashboard at `http://localhost:6969`.

## Install

```sh
go install github.com/bjk95/defrost@latest
```

## Quickstart

From inside any Git repo with an `origin`:

```sh
# Run your tests through defrost. Results are saved automatically.
defrost exec go test ./...

# Inspect a single test's history.
defrost history github.com/you/pkg.TestThing

# Open the dashboard.
defrost serve
```

## Supported runners

| Language | Runner | Invocation |
|---|---|---|
| Go | `go test` | `defrost exec go test ./...` |
| Python | pytest (JUnit XML) | `defrost exec pytest path/` |
| JavaScript / TypeScript | jest | `defrost exec npm test` |

## Pushing metrics from your tests

Use your normal OpenTelemetry SDK. `defrost exec` exports the standard OTLP
environment variables to the child process, so a default-configured SDK
exports to defrost with no extra wiring:

```python
# Python example — works the same in Go, Node, Rust, Java, ...
counter = meter.create_counter("eval.score")
counter.add(score, {"model": "claude-opus-4-7", "suite": "summarization"})
```

Counters, gauges, sums, and histograms are all captured.

## Commands

### `defrost exec <cmd...>`

Runs the test command, captures any metrics emitted during the run, and
records everything. Exits with the child's exit code — unless every failing
test is on the suppression list, in which case the exit is rewritten to 0.

### `defrost history <test_id>`

Prints the recorded history for a single test as NDJSON, oldest first.

### `defrost suppress add | remove | list`

Manage the suppression list. When every failing test in a run is suppressed,
defrost rewrites the exit code to 0 — anything outside the list (a new
failure, a build error, a panic) still exits non-zero.

### `defrost serve`

Serves the dashboard at `127.0.0.1:6969` (override with `--port`).

## Storage

Every run is committed to a `_defrost` branch (configurable with
`--data-branch`) in your repo. Clone the repo to get the history. Push the
repo to share it. Concurrent runs from different machines append cleanly.

To experiment without touching git, pass `--no-persist` (don't store
anything) or `--dev` (write to `.defrost-dev/` instead of the data branch).

### Under the hood

For the curious — you do not need to know any of this to use defrost.

The data branch is shaped like OpenTelemetry. One `defrost exec` invocation
is one trace; the run is the root span; each test is a child span. Metrics
emitted during the run are persisted as OTel metric data points alongside.

```
_defrost branch
├── traces/<span_name>.ndjson    # one OTel span per line
├── metrics/<metric_name>.ndjson # one OTel metric data point per line
└── suppressions.json
```

`traces/` and `metrics/` files use Git's `merge=union` attribute so
concurrent writers append without conflicts.
