---
title: 'OpenTelemetry ingestion'
---

`defrost exec` embeds an OTLP/HTTP receiver. Any OTel SDK in the child
process can export to it without configuration: defrost sets the
endpoint and protocol environment variables before the child starts.

## Receiver

The embedded receiver is the upstream
[`otlpreceiver`](https://pkg.go.dev/go.opentelemetry.io/collector/receiver/otlpreceiver)
from the OpenTelemetry Collector, run in library mode. That means
defrost speaks the same wire format the OTel ecosystem produces — no
custom protocol, no shim code on the receive path. Library-mode
upstream usage also means future migration to a hosted Collector
distribution (`otelcol-defrost`) reuses the same ingestion contract.

- **Bind address:** `127.0.0.1` on a **random free port** chosen at
  startup. Never exposed beyond loopback.
- **Protocol:** OTLP/HTTP, protobuf-encoded
  (`Content-Type: application/x-protobuf`). gRPC is one config-line
  flip away but currently disabled — there's no observed need.
- **Endpoints (all three signals):**
  - `POST /v1/traces` — accepts `ExportTraceServiceRequest`.
  - `POST /v1/metrics` — accepts `ExportMetricsServiceRequest`.
  - `POST /v1/logs` — accepts `ExportLogsServiceRequest`.
- **Lifetime:** receiver starts before the child is spawned and shuts
  down 2 seconds after the child exits. The 2-second drain window lets
  in-flight batched exports complete.

## Environment exposed to the child

Before exec'ing the test command, defrost sets:

```text
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:<port>
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
```

These are the **generic** OTLP env vars (not the per-signal
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` etc.), so the same configuration
captures all three signals at once. Default-configured OTel SDKs in
any language auto-discover the receiver without code changes.

## Trace shape

Every recorded run has the same structure:

- One **root span**, name `defrost.run`. Resource attributes describe
  the repo state — see [storage layout](../storage-layout/#run-span-resource-attributes).
- One **child span per test result** synthesised by the adapter from
  the runner's structured output. Span name = the test ID. Status
  code is `OK` for pass, `ERROR` for fail.
- Any **additional spans the child SDK exported directly** (e.g. an
  Inspect AI eval that emits its own custom spans) land in the same
  trace alongside the adapter-synthesised spans.

The adapter is selected from the command (`go test`, `pytest`, `jest`,
`vitest`, `inspect`, `promptfoo`). It parses the runner's structured
output (`go test -json`, JUnit XML, Jest's reporter API, etc.) and
emits one span per test.

## Metric shape

Metrics arrive verbatim from the child SDK as OTLP `ResourceMetrics`
and are stored as a single zstd-compressed protobuf per run. defrost
does not reshape, downsample, or re-bucket metric data.

Each metric data point inherits the run's resource attributes, so a
metric `eval.accuracy` recorded during `defrost exec` is automatically
correlated with the commit, branch, and PR it was measured on.

## Log shape

Log records arrive verbatim from the child SDK as OTLP `ResourceLogs`
and are stored as a single zstd-compressed protobuf per run. When the
SDK's logger instrumentation is wired correctly (the default for most
OTel logging integrations), each record carries the active `trace_id`
and `span_id`, so logs auto-correlate with the test-case span that
produced them.

The logs file is omitted entirely if the run produced no log records.

### Why structured logs and not span events?

Adapter-captured stdout/stderr (`go test` printlns, pytest `print`
calls, etc.) is recorded as a `test.output` **span event** on the
test-case span — same as before. SDK-emitted log records are a
distinct concept and land in the logs signal.

This is a deliberate non-change: span events keep test output adjacent
to the span in the data model, and the dashboard renders span events
without needing to fetch from a second source. A future iteration may
unify them, but for now: span events for adapter-captured output, log
records for SDK-emitted user logs.

## What is **not** ingested

- **Cross-process traces.** The receiver does not propagate or
  correlate traces from outside the child process tree. Each
  `defrost exec` invocation is one self-contained trace.
- **Spans/metrics/logs after the drain window.** Anything still
  in-flight 2 seconds after the child exits is lost.
