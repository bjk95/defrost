# Recording evals

Test runs give you pass/fail. Evals usually want a number — accuracy,
latency, token cost, judge score. Push those as OpenTelemetry metrics
from inside your eval code. defrost records them on the data branch
correlated with the commit, branch, and PR they were measured on.

You don't need a defrost client library. Any OTel SDK works.

## How it works

1. Your eval code uses an OTel SDK to record metrics.
2. You run it under `defrost exec`.
3. defrost sets `OTEL_EXPORTER_OTLP_ENDPOINT` and
   `OTEL_EXPORTER_OTLP_PROTOCOL` in the child environment so the SDK
   auto-points at the embedded receiver.
4. After the child exits, defrost writes a metrics file to the data
   branch — see [storage layout](../reference/storage-layout.md).

## Python

```python
# eval_quality.py
from opentelemetry import metrics
from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
    OTLPMetricExporter,
)
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader

reader = PeriodicExportingMetricReader(OTLPMetricExporter())
metrics.set_meter_provider(MeterProvider(metric_readers=[reader]))

meter = metrics.get_meter("eval-quality")
accuracy = meter.create_gauge("eval.accuracy")
latency = meter.create_histogram("eval.latency_ms")

for case in cases:
    score, ms = run_case(case)
    accuracy.set(score, attributes={"task": case.task})
    latency.record(ms, attributes={"task": case.task})
```

Run it:

```sh
defrost exec python eval_quality.py
```

The OTLP HTTP exporter reads `OTEL_EXPORTER_OTLP_ENDPOINT` from the
environment by default. defrost sets it. No further config needed.

## Node

```ts
// eval-quality.ts
import { MeterProvider, PeriodicExportingMetricReader }
  from "@opentelemetry/sdk-metrics";
import { OTLPMetricExporter }
  from "@opentelemetry/exporter-metrics-otlp-proto";

const provider = new MeterProvider({
  readers: [new PeriodicExportingMetricReader({
    exporter: new OTLPMetricExporter(),
  })],
});

const meter = provider.getMeter("eval-quality");
const accuracy = meter.createGauge("eval.accuracy");

for (const c of cases) {
  accuracy.record(await scoreCase(c), { task: c.task });
}
await provider.forceFlush();
```

```sh
defrost exec node --loader ts-node/esm eval-quality.ts
```

## Go

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

exp, _ := otlpmetrichttp.New(ctx)
mp := sdkmetric.NewMeterProvider(
    sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
)
otel.SetMeterProvider(mp)

meter := otel.Meter("eval-quality")
acc, _ := meter.Float64Gauge("eval.accuracy")
acc.Record(ctx, 0.87, metric.WithAttributes(attribute.String("task", "summarisation")))
```

Run with `defrost exec go run ./cmd/eval`.

## Inspect AI and PromptFoo

defrost has built-in adapters for both. Just wrap the eval command:

```sh
defrost exec inspect eval evals/my_eval.py
defrost exec promptfoo eval -c promptfooconfig.yaml
```

Test IDs come from the eval framework directly. See
[`examples/inspect/`](https://github.com/bjk95/defrost/tree/main/examples/inspect)
and
[`examples/promptfoo/`](https://github.com/bjk95/defrost/tree/main/examples/promptfoo)
for working setups.

## Drain window

defrost waits up to 2 seconds after the child exits for in-flight
metric exports to complete. If your SDK uses a long export interval
(e.g. 60 seconds default for `PeriodicExportingMetricReader`), call
`forceFlush()` (or the language equivalent) before exiting your eval —
otherwise the last batch may be dropped.

## Inspecting recorded metrics

The dashboard's metrics view groups data points by metric name and
plots them over time:

```sh
defrost serve
```

Or pull NDJSON for a specific span via `defrost history`. Metrics are
stored as a separate file per run alongside traces — see
[storage layout](../reference/storage-layout.md#run-files).
