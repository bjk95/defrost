# OTel-Aligned Storage and Metrics — Design

**Date:** 2026-04-30
**Status:** Draft, pending implementation

## Purpose

Reshape defrost's persisted data into the OpenTelemetry data model and add an OTLP metrics receiver embedded in `defrost exec`. Two parallel goals:

1. **Metrics support.** Users instrument tests with any standard OTel SDK (Go, Python, Node, Java, Rust, ...). `defrost exec` runs an OTLP/HTTP listener on localhost; metrics pushed during the run are persisted alongside test results. No defrost client library required in any language.

2. **OTel-aligned storage.** Test results are persisted as OTel spans rather than ad-hoc `Entry` records. A run is one OTel trace; the run itself is a root span; each test execution is a child span. Future signals (CI step spans, DB query spans, traces received via OTLP) plug into the same trace without storage rework.

The user-facing CLI is unchanged: `defrost exec <test-cmd>` still wraps any existing `go test` / `pytest` invocation with no test code modifications. Storage shape changes; UX does not.

Out of scope for this spec: traces and logs over OTLP (metrics only); gRPC transport; alerting / regression detection (read-time analysis, separate spec); auto-correlation of metrics to a currently-running test; a metric history CLI.

## High-level flow

When the user invokes `defrost exec go test ./...`:

1. defrost picks a free localhost TCP port, binds an HTTP server serving `POST /v1/metrics`, and notes the port.
2. defrost prepares a Resource attribute set from the local repo state (commit, branch, PR, dirty hash, host info), keyed by OTel semconv where applicable.
3. defrost generates a `trace_id` for this run, derived from the run id (`sha256(run_id)[:16]`), and a fresh root `span_id`.
4. defrost captures `start_time_unix_nano` for the root run span and spawns the child test command, with `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:<port>` and `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf` set in its environment.
5. The runner adapter parses test events from the child's output (existing `go test -json` and JUnit XML paths). Adapters produce `[]models.TestResult` exactly as today.
6. When the child exits, defrost waits a short grace period (2s) for in-flight OTLP pushes to drain, then shuts down the receiver. Buffered metric data points are translated into `MetricEntry` records.
7. `[]models.TestResult` is translated to test-case spans (parent = root run span). The root run span's `end_time_unix_nano` is captured.
8. defrost commits everything atomically to the data branch:
   - One line per test span appended to `traces/<test_name>.ndjson`.
   - One line for the root run span appended to `traces/defrost.run.ndjson`.
   - One line per metric data point appended to `metrics/<metric_name>.ndjson`.

The exit code is the child's exit code, with the existing override: a persist failure forces non-zero if the test command itself succeeded.

## On-disk layout

```
.gitattributes               — traces/*.ndjson merge=union
                               metrics/*.ndjson merge=union
traces/<span_name>.ndjson    — one OTel span per line, partitioned by span name
metrics/<metric_name>.ndjson — one OTel metric data point per line, partitioned by metric name
```

`<span_name>` and `<metric_name>` are URL-path-escaped, the same scheme used today for test IDs. Test spans use the existing test-name format (`github.com/x/p/TestFoo`). The run's root span lives at `traces/defrost.run.ndjson`, one line per invocation.

There are no `runs/` or `tests/` directories. The "run" concept is the trace; the "test" concept is a span family. Both fall out of partitioning by span name.

## Data shapes

### Span (one per line in `traces/<span_name>.ndjson`)

```go
type Span struct {
    Schema            int               `json:"schema"`            // 3
    TraceID           string            `json:"trace_id"`          // 32 hex chars (16 bytes)
    SpanID            string            `json:"span_id"`           // 16 hex chars (8 bytes)
    ParentSpanID      string            `json:"parent_span_id,omitempty"`
    Name              string            `json:"name"`
    Kind              string            `json:"kind,omitempty"`    // "INTERNAL" by default
    StartTimeUnixNano int64             `json:"start_time_unix_nano"`
    EndTimeUnixNano   int64             `json:"end_time_unix_nano"`
    Status            SpanStatus        `json:"status"`
    Attributes        map[string]any    `json:"attributes,omitempty"`
    Events            []SpanEvent       `json:"events,omitempty"`
    Resource          map[string]any    `json:"resource"`          // inlined per span
}

type SpanStatus struct {
    Code    string `json:"code"`              // "OK" | "ERROR" | "UNSET"
    Message string `json:"message,omitempty"`
}

type SpanEvent struct {
    TimeUnixNano int64          `json:"time_unix_nano"`
    Name         string         `json:"name"`
    Attributes   map[string]any `json:"attributes,omitempty"`
}
```

Attribute values are typed (string, int64, float64, bool, or homogeneous arrays of those). Attribute keys follow OTel semconv where one applies; defrost-private keys use a `defrost.*` prefix.

### Test-case span attributes

| Attribute key | Value |
|---|---|
| `test.case.name` | full test name, e.g. `github.com/x/p/TestFoo` |
| `test.case.result.status` | `passed` / `failed` / `skipped` / `aborted` |
| `test.suite.name` | package or pytest module path |
| `code.namespace` | package path (Go) |
| `code.function` | test function name |
| `defrost.run_id` | run_id (for cheap lookups across files) |

Span status maps from result status: `passed` → `OK`, `failed`/`aborted` → `ERROR`, `skipped` → `UNSET`. Status `message` carries the failure summary when known. The captured `Output` becomes a single span event:

```json
{"time_unix_nano": ..., "name": "test.output", "attributes": {"body": "<full captured output>"}}
```

When a runner reports subtests / parametrized cases as separate results, each becomes a sibling span at the same level (parent = run root). Hierarchical parent-of-test relationships beyond the run root are not modeled in v3.

### Root run span (`traces/defrost.run.ndjson`)

Span name: `defrost.run`. Span kind: `INTERNAL`. Parent: none. Status: `OK` if every test span is `OK` or `UNSET` and persistence succeeded; `ERROR` otherwise.

Resource attributes (also inlined on every test span emitted in the same run):

| Attribute key | Source |
|---|---|
| `service.name` | constant `"defrost"` |
| `service.version` | defrost CLI build version |
| `cicd.pipeline.run.id` | run_id |
| `vcs.repository.ref.revision` | commit SHA |
| `vcs.repository.ref.name` | branch |
| `vcs.repository.change.id` | PR number, when known (string) |
| `host.os.type` | runtime.GOOS |
| `host.arch` | runtime.GOARCH |
| `process.runtime.version` | runtime.Version() |
| `defrost.cmd` | the wrapped command, as a string array |
| `defrost.cmd_hash` | sha1[:8] of the command |
| `defrost.dirty` | bool |
| `defrost.dirty_hash` | hash of `git diff HEAD` when dirty, else empty |
| `defrost.run_id` | run_id (also a span attribute, for partition-agnostic queries) |

The exact OTel CI/CD and VCS semconv attribute names are tracked as those conventions stabilize; renames within a defrost-owned data branch are a one-shot migration when needed.

### Metric entry (one per line in `metrics/<metric_name>.ndjson`)

```go
type MetricEntry struct {
    Schema            int               `json:"schema"`              // 3
    Name              string            `json:"name"`
    Description       string            `json:"description,omitempty"`
    Unit              string            `json:"unit,omitempty"`
    InstrumentType    string            `json:"instrument_type"`     // "gauge" | "sum" | "histogram"
    Temporality       string            `json:"temporality,omitempty"` // "delta" | "cumulative"
    Monotonic         bool              `json:"monotonic,omitempty"` // sum only
    TimeUnixNano      int64             `json:"time_unix_nano"`
    StartTimeUnixNano int64             `json:"start_time_unix_nano,omitempty"`
    Attributes        map[string]any    `json:"attributes,omitempty"`
    Resource          map[string]any    `json:"resource"`            // inlined per data point
    TraceID           string            `json:"trace_id,omitempty"`  // run trace_id
    SpanID            string            `json:"span_id,omitempty"`   // exemplar span when supplied

    // gauge / sum
    Value   *float64 `json:"value,omitempty"`

    // histogram
    Count   *uint64           `json:"count,omitempty"`
    Sum     *float64          `json:"sum,omitempty"`
    Min     *float64          `json:"min,omitempty"`
    Max     *float64          `json:"max,omitempty"`
    Buckets []HistogramBucket `json:"buckets,omitempty"`
}

type HistogramBucket struct {
    UpperBound *float64 `json:"upper_bound"` // nil represents the +Inf bucket
    Count      uint64   `json:"count"`
}
```

Resource is inlined per data point — same denormalization as spans, same justification (self-contained reads, partitioning prevents cross-file Resource grouping anyway).

`TraceID` carries the run's trace id so a future alerting layer can correlate metric anomalies with the run that produced them. `SpanID` is populated only when the inbound OTLP data point carries an exemplar; otherwise omitted.

Exponential histograms are accepted on the wire and converted to explicit-bucket form at translate time, so on-disk format is single-shape.

## Components

### `internal/otlp/receiver.go`

Minimal OTLP/HTTP receiver. Responsibilities:

- Bind a `net/http` server on `127.0.0.1:<random-free-port>`.
- Route `POST /v1/metrics` with `Content-Type: application/x-protobuf` to a handler that decodes `ExportMetricsServiceRequest` and pushes the raw OTLP records onto an internal channel.
- Reject everything else (other paths, methods, content types) with the appropriate HTTP status (404 for paths, 405 for methods, 415 for content types).
- Expose `Start() (port int, err error)` — binds and serves, returns the chosen port.
- Expose `Shutdown(ctx context.Context) ([]otlp.Metric, error)` — stops accepting new connections, waits for in-flight handlers to drain (bounded by ctx), returns the accumulated raw OTLP records.

The receiver does not interpret the records; it only buffers them. Translation happens after shutdown so the full picture of a run is available when storage decisions are made.

### `internal/otlp/translate.go`

Pure functions converting raw OTLP records into the storage data shapes:

- `MetricsToEntries(records []otlp.Metric, run RunContext) []MetricEntry` — flattens each OTLP metric into per-data-point entries. Sums and gauges produce one entry per `NumberDataPoint`. Histograms produce one entry per `HistogramDataPoint`. Exponential histograms convert to explicit-bucket form.
- `TestResultsToSpans(results []models.TestResult, run RunContext) []Span` — converts the existing `TestResult` shape into test-case spans, applies attributes per the table above, encodes `Output` as a `test.output` span event, and maps result status to span status.

`RunContext` carries the run's `trace_id`, the root span's `span_id`, the Resource attribute set, and the run start time. Translators are pure (no I/O, no clocks beyond what's in `RunContext`) so they're trivially testable.

### `internal/persist/persist.go` changes

`Backend` evolves:

```go
type Backend interface {
    InitialisePersistence() error
    InsertNewRun(root Span, testSpans []Span, metrics []MetricEntry) error
    GetTestHistory(testName string) ([]Span, error)
}
```

- `InsertNewTestResults` is removed; `InsertNewRun` takes the root span, child spans, and metric data points so the entire run lands in one commit.
- `RunRecord` and `Entry` types are removed.
- `DetectRun` is replaced by a `DetectRunContext` helper (same git/env probing) returning a `RunContext` plus the seed Resource attribute map.
- `GetTestHistory` returns `[]Span` directly. Callers render them.
- Both `gitBackend` and `fileBackend` implement the new interface. The git path writes to `traces/` and `metrics/`; the file path writes the same layout under `<repo>/.defrost-dev/`.
- The seed `.gitattributes` content becomes:
  ```
  traces/*.ndjson merge=union
  metrics/*.ndjson merge=union
  ```

Existing git push-with-retry, branch creation, and bot-identity logic are unchanged — only the file layout differs.

### `internal/runner` adapters

External contract is unchanged: adapters return `[]models.TestResult`. `models.TestResult` itself stays as-is; translation to spans happens at persist time, not in the adapter. Adding new test runners (`vitest`, `cargo test`, ...) keeps the same shape it has today.

### `exec.go`

1. Build `RunContext` from `DetectRunContext`.
2. Start OTLP receiver, capture port.
3. Set `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL` on the child command env. Capture the root span start time.
4. Run the child to completion.
5. Shut down receiver (2s grace), translate buffered OTLP records into `[]MetricEntry`.
6. Translate `[]TestResult` to `[]Span`.
7. Construct the root run span (start time from step 3; end time = now; status derived from child results and persistence).
8. Pass the bundle to `Backend.InsertNewRun`.

### `history.go`

Reads `traces/<test_name>.ndjson`, returns one `Span` per line. Output remains NDJSON-per-line (each line is a span); the human-readable rendering of a span is a follow-on concern.

### `cli.go`, `main.go`

No changes.

## Defaults

| Decision | Default | Reason |
|---|---|---|
| Transport | HTTP/protobuf only | Avoids `google.golang.org/grpc` dep tree; SDKs honor the env-var swap |
| Receiver port | Random free localhost port | Parallel `defrost exec` invocations work without coordination |
| Drain grace | 2 seconds after child exit | Long enough for SDK background flushes; short enough not to stall CI |
| Aggregation temporality | Stored as-arrived | We don't convert delta↔cumulative; document `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=delta` as recommended |
| Receiver bind failure | Warn to stderr, continue without metric collection | Metrics path must never break the test run |
| Cardinality cap | None (v1) | Document as a known limit; revisit when real runs hit it |
| Schema version | 3 | Bumps from 2 (the current `Entry` schema); all records use the new shape |
| Migration of existing data | Hard cut, no compatibility reader | Defrost has no deployed users; reset cost is zero |

## Error handling

| Failure | Behavior |
|---|---|
| Receiver fails to bind | Log to stderr; continue without metric collection. Test command still runs and persists. |
| Receiver decode error on a single request | Return HTTP 400; log; other valid requests in the run still land. |
| Translate error on a single test result or metric record | Log; skip the offending record. Other tests/metrics persist normally. |
| Persist failure | Surface to stderr; force exit code ≥ 1 if tests succeeded (existing behavior). |
| Child crashes before any metric is pushed | Persist root run span and test spans as today; `metrics/` simply isn't written for this run. |

## Testing

- **Unit:** `MetricsToEntries` translation — hand-crafted OTLP records (gauge, sum, histogram, exponential histogram) → expected `MetricEntry` shape. Cover delta and cumulative temporality, monotonic and non-monotonic sums.
- **Unit:** `TestResultsToSpans` translation — `TestResult` cases (pass, fail, skip, panic, with output) → expected `Span` shape, attributes, events, and status.
- **Unit:** OTLP receiver — issue an HTTP POST with a binary protobuf body; verify the buffered raw record matches; verify rejected paths return correct status codes.
- **Integration:** receiver + real OTel Go SDK pointed at it; verify a counter, gauge, and histogram each round-trip end-to-end into `MetricEntry` shape.
- **End-to-end:** `defrost exec go test ./...` against a tiny suite that emits one custom metric — verify the post-run state of `traces/` and `metrics/` files is exactly as specified.

Existing persist tests are rewritten against the new `Backend` interface; tests for the old `Entry`/`RunRecord` shapes are deleted along with those types.

## Non-goals

The following are explicitly out of scope for this spec and must not appear in the implementation:

- OTLP traces and logs receivers.
- gRPC OTLP transport.
- Alerting, regression detection, or threshold/budget enforcement on metrics. These are read-time analyses against the `metrics/` directory, addressed in a separate spec.
- Auto-attaching `test.case.name` to metrics emitted while a particular test is running. Users who want this attach the attribute themselves through standard OTel APIs.
- A metric history CLI (`defrost history` for metrics).
- Cardinality enforcement.
- Compatibility readers for the pre-schema-3 data shape; existing data branches are abandoned.
- Schema evolution within v3 beyond what is specified here.
