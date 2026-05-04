---
title: 'OpenTelemetry as the ingestion API'
---

defrost speaks [OpenTelemetry](https://opentelemetry.io/) over OTLP/HTTP
rather than shipping a defrost-specific client library. This page
explains why.

## The decision

`defrost exec` runs an OTLP/HTTP receiver on `127.0.0.1` and exposes its
endpoint to the child via the standard
`OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_PROTOCOL` environment
variables. Any OTel SDK in the child — Python, Go, Node, Java, Rust,
.NET, anything — can record metrics or spans without knowing defrost
exists.

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

## What's deliberately out of scope

- **OTel logs.** Test output capture is handled by the adapter and
  attached to test spans as events. A separate logs pipeline would add
  duplication without adding signal.
- **Cross-process traces.** Each `defrost exec` invocation is one
  self-contained trace. defrost does not propagate trace context into
  external services or correlate across multiple `exec` runs.

If those constraints don't fit, defrost is probably not the right tool
for your case. If they do, you get every OTel SDK on Earth as your
client library.
