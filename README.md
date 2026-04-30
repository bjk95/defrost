# defrost

> Track AI evals, metrics, and tests with Git as the database.

Every team wants a record of how their evals, benchmarks, and tests have changed
over time. Almost nobody wants to host a database for it. **defrost** records
each run as a commit on a `_defrost` branch in the same repo, so the history
travels with the code — no database, no SaaS, no API keys.

The on-disk shape is OpenTelemetry: a run is one trace, each test is a span,
and any metrics emitted during the run are persisted alongside. Anything that
already speaks OTLP can push data to defrost, in any language.

## What you get

- **Persisted history** — every run lands as commits on a `_defrost` branch in your repo and pushes alongside your code.
- **OTLP metrics ingest** — `defrost exec` runs a localhost OTLP/HTTP listener; instrument with any standard OTel SDK and the data is captured automatically. No defrost client library.
- **Suppression** — mark known-failing tests as suppressed so red CI goes green without skipping them in source.
- **Local dashboard** — `defrost serve` opens your testing, evals, and metrics dashboard at `http://localhost:6969`.

## Install

```sh
go install github.com/bjk95/defrost@latest
```

## Quickstart

From inside any Git repo with an `origin`:

```sh
# Run your tests through defrost. The run is persisted on the _defrost branch
# as an OTel trace; any OTLP metrics pushed during the run are persisted too.
defrost exec go test ./...

# Inspect a single test's recorded spans.
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

Metrics arrive via OTLP, so any language with an OpenTelemetry SDK can push
data to the receiver running inside `defrost exec`.

## How storage works

defrost stores its data on a side branch named `_defrost` (configurable with
`--data-branch`). The branch contains:

- `traces/<span_name>.ndjson` — one OTel span per line. Each `defrost exec`
  invocation writes one root span (`defrost.run`) plus one child span per
  test. `<span_name>` is the test's full name, URL-path-escaped.
- `metrics/<metric_name>.ndjson` — one OTel metric data point per line.
  Populated by anything that pushes OTLP at the receiver `defrost exec` runs
  on `127.0.0.1`.
- `suppressions.json` — the current suppression list.

`traces/` and `metrics/` files are configured with Git's `merge=union`
attribute so concurrent runs append without conflicts. Two writers committing
simultaneously rebase against each other automatically.

Cloning the repo gives you the full history. Pushing the repo shares it.

To experiment without touching git, pass `--no-persist` (don't store anything)
or `--dev` (write to `.defrost-dev/` instead of the data branch).

### Pushing metrics from your tests

`defrost exec` sets `OTEL_EXPORTER_OTLP_ENDPOINT` and
`OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` in the child's environment, so a
standard OTel SDK configured from env will export to defrost with no extra
wiring. Counters, gauges, sums, and histograms are all supported. Exponential
histograms are converted to explicit-bucket form on ingest.

## Commands

### `defrost exec <cmd...>`

Runs the test command, parses results, captures any OTLP metrics pushed during
the run, and commits everything as one trace + metrics bundle. Exits with the
child's exit code — unless every failing test is on the suppression list, in
which case the exit is rewritten to 0.

### `defrost history <test_id>`

Prints the recorded spans for a single test as NDJSON, oldest first. The test
ID is the same string the runner emits (e.g. `github.com/x/p/TestFoo`,
`tests/test_x.py::TestY::test_z`,
`src/foo.test.ts > Foo > does the thing`).

### `defrost suppress add | remove | list`

Manage the suppression list. When every failing test in a run is suppressed,
defrost rewrites the exit code to 0 — anything outside the list (a new
failure, a build error, a panic) still exits non-zero.

### `defrost serve`

Serves the dashboard at `127.0.0.1:6969` (override with `--port`). View test
history, evals, and metrics for the runs persisted on the data branch.
