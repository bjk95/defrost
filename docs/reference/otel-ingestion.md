# OpenTelemetry ingestion

`defrost exec` embeds an OTLP/HTTP receiver. Any OTel SDK in the child
process can export to it without configuration: defrost sets the
endpoint and protocol environment variables before the child starts.

## Receiver

- **Bind address:** `127.0.0.1` on a **random free port** chosen at
  startup. Never exposed beyond loopback.
- **Protocol:** OTLP/HTTP, protobuf-encoded
  (`Content-Type: application/x-protobuf`).
- **Endpoints:**
  - `POST /v1/metrics` — accepts `ExportMetricsServiceRequest`.
  - `POST /v1/traces` — accepts `ExportTraceServiceRequest`.
- **Lifetime:** receiver starts before the child is spawned and shuts
  down 2 seconds after the child exits. The 2-second drain window lets
  in-flight batched exports complete.

## Environment exposed to the child

Before exec'ing the test command, defrost sets:

```text
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:<port>
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
```

These override anything the user already had set. SDKs that use the
standard OTLP/HTTP defaults will auto-discover the receiver — no SDK
code change required.

## Trace shape

Every recorded run has the same structure:

- One **root span**, name `defrost.run`. Resource attributes describe
  the repo state — see [storage layout](./storage-layout.md#run-span-resource-attributes).
- One **child span per test result**. Span name = the test ID. Status
  code is `OK` for pass, `ERROR` for fail. Skips and other statuses use
  attributes documented in the relevant adapter section below.

The trace is constructed by the **adapter** that defrost selects based
on the child command (`go test`, `pytest`, `jest`, `vitest`, `inspect`,
`promptfoo`). Adapters parse the runner's structured output (Go's `-json`,
JUnit XML, Jest's reporter API, etc.) and emit one span per test.

Test runners that already speak OTel directly (e.g. an Inspect AI eval
that exports its own spans) are handled by a passthrough adapter — those
spans land in the same trace alongside any defrost-synthesised spans.

## Metric shape

Metrics arrive verbatim from the child SDK as OTLP `ResourceMetrics` and
are stored as a single zstd-compressed protobuf per run. defrost does
not reshape, downsample, or re-bucket metric data.

Each metric data point inherits the run's resource attributes, so a
metric `eval.accuracy` recorded during `defrost exec` is automatically
correlated with the commit, branch, and PR it was measured on.

## What is **not** ingested

- **Logs.** OTel logs are not accepted. Use stdout/stderr or test-framework
  output capture, which lands in the trace as span events on the
  relevant test span.
- **Cross-process traces.** The receiver does not propagate or correlate
  traces from outside the child process tree. Each `defrost exec`
  invocation is one self-contained trace.
