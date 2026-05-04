---
title: 'OpenTelemetry as the ingestion API'
---

defrost speaks [OpenTelemetry](https://opentelemetry.io/) over OTLP/HTTP
rather than shipping a defrost-specific client library. This page
explains why.

## The decision

`defrost exec` runs the upstream OpenTelemetry Collector's
[`otlpreceiver`](https://pkg.go.dev/go.opentelemetry.io/collector/receiver/otlpreceiver)
in library mode on `127.0.0.1` and exposes its endpoint to the child
via the standard `OTEL_EXPORTER_OTLP_ENDPOINT` /
`OTEL_EXPORTER_OTLP_PROTOCOL` environment variables. Any OTel SDK in
the child — Python, Go, Node, Java, Rust, .NET, anything — can record
traces, metrics, or logs without knowing defrost exists.

Internally the whole pipeline speaks one data model: OTel `pdata`.
Adapters convert their parsed runner output to `ptrace.Traces`. The
receiver hands its decoded `pdata.{Traces,Metrics,Logs}` straight to
the same sink the adapters write into. The persist layer serializes
that pdata via `ptraceotlp.MarshalProto` (and friends) — the canonical
OTLP wire format an OTel Collector exporter would produce — and writes
it to disk. Downstream readers decode the same bytes without translation.

## Why OTel and not a defrost SDK

- **No client library to maintain per language.** OTel SDKs already
  exist for every language teams record evals from. Building and
  shipping a separate `defrost-py`, `defrost-js`, `defrost-go` would
  duplicate work the OTel community has already done well.
- **No custom protocol.** OTLP is an open spec. Anyone wanting to
  forward defrost data to a different observability backend later can
  re-export it without translation.
- **Existing instrumentation comes along for the ride.** If your code
  already exports HTTP request durations or LLM token counts to OTel,
  those land in the trace alongside test results automatically when run
  under `defrost exec`.

## What this means for users

For test runners (pytest, jest, go test, vitest, Inspect AI, PromptFoo),
defrost's adapter parses the runner's structured output and constructs
the trace itself — you don't need to instrument anything. Just wrap the
command.

For evals, write an OTel metric or span from inside your eval code:

```python
from opentelemetry import metrics
meter = metrics.get_meter("my-evals")
accuracy = meter.create_gauge("eval.accuracy")
accuracy.set(0.87, attributes={"task": "summarisation"})
```

Run it under `defrost exec`. The metric lands on the data branch
correlated with the commit, branch, and PR number automatically.

## All three OTel signals are captured

The receiver registers factories for traces, metrics, AND logs. SDK-
emitted log records (the standard OTel log pipeline, with auto-
correlation to the active span via `trace_id` / `span_id`) are stored
as a third signal alongside traces and metrics. You can query them
from the dashboard, drop them with `defrost drop --logs-only` (or
selectively keep them with `--no-logs`-style flags), and `defrost
history` prints log lines correlated with the test span that produced
them.

Adapter-captured stdout/stderr (e.g. pytest's `print()` output) is
still attached to the test span as a `test.output` event — that's a
deliberate non-change. Span events keep stdout adjacent to the span
in the data model. SDK-emitted log records are a separate, higher-
fidelity signal.

## What's deliberately out of scope

- **Cross-process traces.** Each `defrost exec` invocation is one
  self-contained trace. defrost does not propagate trace context into
  external services or correlate across multiple `exec` runs.

If that constraint doesn't fit, defrost is probably not the right tool
for your case. If it does, you get every OTel SDK on Earth as your
client library.
