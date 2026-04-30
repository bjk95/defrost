<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-light-128.png">
    <img alt="defrost" src="assets/logo-128.png" width="96" height="96">
  </picture>
</p>

# defrost

> Track AI evals, metrics, and tests with Git as the database.

Every team wants a record of how their evals, benchmarks, and tests have changed
over time. Almost nobody wants to host a database for it. **defrost** records
each run as commits on a `_defrost` branch in the same repo, so the history
travels with the code — no database, no SaaS, no API keys.

## What you get

- **Persisted history** — every test run, eval, and metric is recorded automatically. Clone the repo, get the history.
- **Universal instrumentation** — push evals and metrics from any language using a standard OpenTelemetry SDK. No defrost client library to install.
- **Suppression** — mark known-failing tests as suppressed so red CI goes green without skipping them in source.
- **Local dashboard** — `defrost serve` opens your testing, evals, and metrics dashboard at `http://localhost:6969`.

## Install

```sh
go install github.com/bjk95/defrost@latest
```

## Quickstart

From inside any Git repo with an `origin`:

```sh
# Run your tests through defrost. Results, evals, and metrics are saved automatically.
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

## Pushing evals and metrics

Use your normal OpenTelemetry SDK. `defrost exec` exports the standard OTLP
environment variables to the child process, so a default-configured SDK
exports to defrost with no extra wiring:

```python
# Record an eval score
score_gauge = meter.create_gauge("eval.score")
score_gauge.set(0.87, {"model": "claude-opus-4-7", "suite": "summarization"})

# Record a perf metric
latency = meter.create_histogram("request.latency_ms")
latency.record(elapsed_ms, {"endpoint": "/api/search"})
```

Counters, gauges, sums, and histograms are all captured. Same SDK pattern
works in Go, Node, Rust, Java, and every other OTel-supported language.

## Commands

### `defrost exec <cmd...>`

Runs the test command, captures any evals or metrics emitted during the run,
and records everything. Exit code:

- Normally the child's exit code.
- If every failing test is on the suppression list, the exit is rewritten to `0`.
- If persisting to the `_defrost` branch fails (e.g. push rejected,
  no `origin`), the exit is forced to `1` even when the test command itself
  succeeded — so CI never silently loses data.

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
repo to share it. Concurrent runs from different machines never conflict
because each writer owns a unique filename (one file per run, named by its
OTel `trace_id`).

To experiment without touching git, pass `--no-persist` (don't store
anything) or `--dev` (write to `.defrost-dev/` instead of the data branch).

### Under the hood

For the curious — you do not need to know any of this to use defrost.

The data branch is shaped like OpenTelemetry. One `defrost exec` invocation
is one trace; the run is the root span; each test is a child span. Evals
and metrics emitted during the run are persisted as OTel metric data
points. Each file is the canonical OTLP/Protobuf payload, zstd-compressed.

```
_defrost branch
├── traces/<YYYY>/<MM>/<DD>/<trace_id>.otlp.pb.zst    # one ExportTraceServiceRequest per run
├── metrics/<YYYY>/<MM>/<DD>/<trace_id>.otlp.pb.zst   # one ExportMetricsServiceRequest per run
└── suppressions.json
```

Run-scoped attributes (commit, branch, author, command, OS/arch, run id,
…) live exactly once on each file's `Resource`. The OTel span `name` is
the canonical fully qualified test name — no lossy projections are stored
alongside it. Compaction (collapsing many per-run files into one daily
file) is on the roadmap.
