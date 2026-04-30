# Eval Framework Integration — Design

**Date:** 2026-04-30
**Status:** Draft, pending implementation

## Purpose

Land per-framework adapters that translate native eval-tool output into OTel
metric data points and test spans, so a user running their existing eval suite
through `defrost exec` gets per-case scores persisted as named time series on
the `_defrost` branch — with no defrost-specific changes to their eval code.

Two parallel goals:

1. **Coverage of the popular batch eval frameworks.** DeepEval, Promptfoo,
   Inspect AI, and (with user-side glue) RAGAS. Each framework's native
   structured output is parsed at the end of the run by a dedicated adapter.
2. **Consistent on-wire schema across frameworks.** Every adapter emits the
   same OTel-shaped attributes anchored on the OpenTelemetry Gen-AI semantic
   conventions, so `defrost serve` and any future analysis layer can read
   `metrics/eval.<criterion>.ndjson` without per-framework branching.

The user-facing CLI is unchanged: `defrost exec <eval-cmd>` wraps any
existing invocation. Storage shape is unchanged from the OTel storage spec —
this design only specifies what fills the existing `metrics/` and `traces/`
directories when an eval framework is the wrapped command.

Out of scope: production eval streams from observability platforms
(Langfuse, Braintrust, Phoenix) — defrost remains batch-only; benchmark
sweeps from `lm-evaluation-harness` / OpenAI Evals / HELM (separate spec
once A is shipped); auto-correlating user-emitted OTLP metrics to specific
eval cases beyond what `test.case.name` already gives; a dedicated
`defrost eval` subcommand (the existing `defrost exec` and `defrost history`
suffice).

## Prerequisites

This spec assumes the OTel-aligned storage and metrics design
(`docs/superpowers/specs/2026-04-30-otel-storage-and-metrics-design.md`) has
shipped. Specifically:

- Per-run trace, root run span, test-case child spans.
- `metrics/<metric_name>.ndjson` partitioning with `merge=union`.
- An OTLP/HTTP receiver embedded in `defrost exec`.
- The `RunContext` / `Resource` attribute plumbing.

If that design changes shape, this spec is recut.

## High-level flow

When the user invokes `defrost exec <eval-cmd>`:

1. Defrost's registry resolves which adapters apply: exactly one runner
   adapter (existing pytest / jest / go-test, or new Promptfoo / Inspect
   AI) plus zero-or-more plugin adapters (DeepEval, RAGAS) that piggyback
   on a runner.
2. Each plugin's `Prepare` returns arg or env mutations (e.g.
   `DEEPEVAL_RESULTS_FOLDER=<tempdir>`, `--output <tempfile>`,
   `--log-format=json`). Defrost merges them and applies to the child.
   This mirrors the existing pattern where the Go adapter auto-inserts
   `-json`.
3. Defrost starts the OTLP/HTTP receiver, exports `OTEL_EXPORTER_OTLP_*`
   env vars to the child, and the runner adapter runs the (mutated)
   eval command to completion. The runner returns its `[]TestResult`
   (pass/fail per case) and, for runners that own framework-specific
   tools, also a `[]*metricspb.Metric` parsed from the framework's output file.
4. After the child exits, each plugin's `Teardown` reads whatever output
   file the plugin asked the framework to write and emits per-(case ×
   criterion) `[]*metricspb.Metric`.
5. The OTLP receiver drains; user-emitted custom metrics (anything *not*
   produced by the eval framework, e.g. a hand-written `meter.create_gauge`
   for "PR review human comments") are translated as today.
6. Test spans (from the runner adapter), eval-score metric data points
   (from the runner and any plugins), and user-emitted OTLP metrics all
   land in a single commit on the data branch.

The two metric sources (file parse + OTLP receiver) are complementary:

- **File parse** owns framework-emitted scores. The file is the source of
  truth, survives child crashes, and avoids merging wire data with file data.
- **OTLP receiver** owns user-emitted custom metrics. Required for any
  metric that doesn't come from a known eval framework.

## Standards mapping

Every metric data point an eval adapter emits to
`metrics/<eval_name>.ndjson` carries the following attribute set. The metric
*name* itself is `eval.<criterion>` where `<criterion>` is the framework's
own name for the metric, lowercased and dotted (`faithfulness`,
`answer_relevancy`, `factuality`, `llm-rubric`, etc.).

In wire terms: each metric is a `*metricspb.Metric` (gauge) containing one
`*metricspb.NumberDataPoint`; the table's "Attribute" rows live on
`NumberDataPoint.Attributes` as `[]*commonpb.KeyValue`. The score itself
goes in `NumberDataPoint.Value` as `*metricspb.NumberDataPoint_AsDouble`.
The `models.StringAttr` / `IntAttr` / `BoolAttr` helpers in
`internal/models/runcontext.go` cover string/int/bool attribute
construction; doubles need a new `models.DoubleAttr` helper (added
alongside the existing ones).

| Attribute key | Value | Required |
|---|---|---|
| (metric name) | `eval.<criterion>` | always |
| (metric value) | numeric score, typically 0.0–1.0 | always |
| `gen_ai.evaluation.name` | same as `<criterion>`, semconv-canonical | always |
| `gen_ai.evaluation.score.value` | duplicate of metric value (semconv requires it on the eval-result attribute group) | always |
| `gen_ai.evaluation.score.label` | `"pass"` / `"fail"` / framework-specific label | when framework provides |
| `gen_ai.evaluation.explanation` | judge reason text | when framework provides |
| `gen_ai.request.model` | model under test | when framework provides |
| `gen_ai.response.id` | LLM completion id, for correlation back to the call being evaluated | when framework provides |
| `test.case.name` | full case identifier, identical to the `traces/<name>.ndjson` partition key | always |
| `test.suite.name` | grouping (pytest module path, Promptfoo file, Inspect AI task name) | when framework groups |
| `defrost.eval.threshold` | pass-boundary score, when distinct from label | when framework provides |
| `defrost.eval.prompt_version` | prompt-template version, if framework tracks it | when framework provides |
| `defrost.eval.judge_model` | judge model, when distinct from model under test | when framework provides |
| inherited resource attrs | commit, branch, run_id, etc. | always (from `RunContext`) |

`gen_ai.evaluation.*` is currently experimental in OTel semconv. Defrost's
data branch is per-repo; renames are a one-shot migration. Same posture as
the existing OTel storage spec for CI/CD and VCS attributes.

`defrost.eval.*` covers the three semconv gaps (threshold, prompt_version,
judge_model). These attribute keys are dropped — replaced by their
semconv-stable equivalents — once those land upstream.

OpenInference is *not* used. Its only eval primitive is the `EVALUATOR`
span kind; it has no typed eval-score attributes. Phoenix derives score
data by parsing free-form `output.value` JSON, which is a Phoenix UI
convention rather than a wire standard.

## Pass/fail vs. score: the dual write

For every eval case, two kinds of records land in different files:

- **One `models.TestResult`** with the case's pass/fail. Always produced
  by the runner adapter (existing pytest / jest / go-test for plugin
  scenarios; the Promptfoo / Inspect AI adapter when they're the runner).
  Persisted via the existing test-span path to `traces/<test_name>.ndjson`.
- **One or more metric data points** — one per criterion the framework
  scores. Produced by either the runner adapter (Promptfoo, Inspect AI)
  or the plugin adapter (DeepEval, RAGAS) depending on the framework's
  shape. Each lands in its own `metrics/eval.<criterion>.ndjson` file.

A single DeepEval test case scored on three metrics produces one
test-span row (from pytest) and three metric-data-point rows in three
different metric files (from the DeepEval plugin's teardown). The
metric files are the time series; the trace file is the case's run
history.

## Per-framework adapters

### `internal/eval/promptfoo/` — Role A (runner)

**Recognises:** the first arg is `promptfoo` and the second is `eval`.
(Also matches `npx promptfoo` / `pnpm promptfoo` invocations — the
adapter searches for the literal `promptfoo` token followed by `eval`.)

**Auto-injection:** if no `--output <path>` (or `-o <path>`) is in the
args, append `--output <tempfile>` where `<tempfile>` is
`os.MkdirTemp(...)` + `/results.json`. Promptfoo derives format from
the extension; `.json` produces JSON. (Verified flag: `--output` /
`-o`, multiple invocations supported.)

**Output to parse:** Promptfoo's `results.json`:

- `results.results[]` — one entry per (test × prompt × provider).
- `results.results[i].success` — bool, top-level pass/fail for the entry.
- `results.results[i].provider.label` (or `provider.id`) — model under test.
- `results.results[i].response.output` — actual LLM output.
- `results.results[i].gradingResult.componentResults[]` — one entry per
  assertion, with `pass`, `score`, `reason`, and `assertion.threshold`.

**Mapping:**
- `test.case.name`: `<test_id>` derived from the result entry's index +
  prompt label (Promptfoo doesn't have stable case names by default; the
  adapter synthesises `<file>::<row>` from the config path).
- `gen_ai.request.model`: `provider.id` or `provider.label`.
- One metric row per `componentResults[i]`:
  - metric name: `eval.<assertion_type>` (e.g. `eval.factuality`,
    `eval.contains`, `eval.llm-rubric`).
  - score: `componentResults[i].score`.
  - `gen_ai.evaluation.score.label`: `"pass"` / `"fail"` from
    `componentResults[i].pass`.
  - `gen_ai.evaluation.explanation`: `componentResults[i].reason`.
  - `defrost.eval.threshold`: `componentResults[i].assertion.threshold`
    when present.
- Top-level `pass: false` plus the joined assertion reasons becomes the
  `models.TestResult` failure output.

### `internal/eval/deepeval/` — Role B (plugin on top of pytest)

DeepEval can be driven through `deepeval test run ...` (pytest under the
hood) *or* through plain `pytest path/`. Both end up running pytest. The
existing `internal/python/pytest` runner adapter therefore owns
execution and pass/fail; DeepEval is a Role-B plugin that contributes
metric rows after the run.

**Recognises:** the command, after stripping wrapper prefixes, starts
with `pytest`, `python -m pytest`, or `deepeval test run`. (DeepEval
guarantees the test files import from `deepeval`; the plugin adapter
trusts that the user wouldn't have asked for the eval pipeline if their
tests don't actually use DeepEval.)

**Prepare:** DeepEval auto-writes a timestamped JSON report into
`DEEPEVAL_RESULTS_FOLDER` (defaults to `.deepeval/`) at the end of the
run. The plugin sets `DEEPEVAL_RESULTS_FOLDER=<tempdir>` in the child
env, where `<tempdir>` is `os.MkdirTemp(...)`. No argv mutation. (Verified
mechanism: env-var override, results written via DeepEval's
`global_test_run_manager`.)

**Teardown:** read the most recent `test_run_*.json` from `<tempdir>`.
The schema:

- `testCases[]` — one entry per `LLMTestCase`.
- `testCases[i].metrics_data[]` — one entry per metric the user attached.
- `testCases[i].metrics_data[k]` carries `name`, `score`, `threshold`,
  `success` (bool), `reason`, and `evaluation_model`.
- `hyperparameters` and `metricsScores` exist at the top level for
  aggregate context (not used in v1).

**Mapping:**
- `test.case.name`: derived from the pytest node id, which DeepEval
  preserves on each `LLMApiTestCase`.
- One metric row per `metrics_data[k]` per test case:
  - metric name: `eval.<metrics_data[k].name>` (lowercased, dotted; e.g.
    `eval.faithfulness`, `eval.answer_relevancy`).
  - score: `metrics_data[k].score`.
  - `gen_ai.evaluation.score.label`: `"pass"` / `"fail"` from `success`.
  - `gen_ai.evaluation.explanation`: `metrics_data[k].reason`.
  - `defrost.eval.threshold`: `metrics_data[k].threshold`.
  - `defrost.eval.judge_model`: `metrics_data[k].evaluation_model`.

If `<tempdir>` contains no `test_run_*.json` after the child exits
(e.g. the user's tests didn't actually invoke DeepEval), the teardown
emits zero metric rows and exits cleanly — DeepEval's absence is a
no-op, not an error.

### `internal/eval/inspect/` — Role A (runner)

**Recognises:** the first arg is `inspect` and the second is `eval`.

**Auto-injection:**

- If no `--log-dir <path>` is in the args, append one pointing at a
  defrost-controlled tempdir. (Verified flag: `--log-dir`.)
- If no `--log-format <format>` is in the args, append `--log-format=json`.
  Inspect AI's default log format is `eval` (a compact binary format);
  defrost forces `json` so the adapter parses without spawning
  `inspect log dump`. (Verified flag: `--log-format=json`. Schema is
  retrievable via `inspect log schema` for impl-time validation.)
- Defrost respects user-supplied values for either flag. If the user
  picks `--log-format=eval`, the adapter falls back to invoking
  `inspect log dump <file> --to json` per log file as a sub-step before
  parse.

**Output to parse:** one JSON log file per task in `<log-dir>`. The log
schema (per `inspect log schema`):

- `eval.tasks[]` — task definitions.
- `samples[]` — per-sample results. Each sample has:
  - `id` — sample id.
  - `scores` — map from scorer name to a value object containing
    `value`, `answer`, `explanation`, `metadata`.
  - `model` — model used.

**Mapping:**
- `test.case.name`: `<task_name>::<sample.id>`.
- `test.suite.name`: `<task_name>`.
- One metric row per scorer per sample:
  - metric name: `eval.<scorer_name>` (lowercased, dotted).
  - score: numeric value of `scores[<scorer>].value`. Inspect AI scorers
    may emit `Value` (numeric), `Letter` (categorical), or compound
    types; v1 supports numeric only, skipping non-numeric scorers with
    a one-line stderr warning.
  - `gen_ai.evaluation.explanation`: `scores[<scorer>].explanation`.
  - `gen_ai.request.model`: from the task's model spec.

### `internal/eval/ragas/` — Role B (plugin on top of pytest)

RAGAS has no CLI and no native dump-to-disk. Cases are evaluated inside
user pytest tests via the `ragas.evaluate(dataset)` API.

The adapter ships a small Python helper, `defrost_ragas.write_results`,
that the user calls inside their test:

```python
from ragas import evaluate
from defrost_ragas import write_results

result = evaluate(dataset, metrics=[faithfulness, answer_relevancy])
write_results(result)  # writes to $DEFROST_RAGAS_OUT or a default path
```

Defrost sets `DEFROST_RAGAS_OUT=<tempfile>` in the child env. The Python
helper writes RAGAS results as a defrost-defined JSON shape (essentially
the RAGAS `EvaluationResult` flattened to per-(row × metric) entries).
After the run, the Go adapter reads that file.

This is the most user-glue of the four. It is also the lowest-leverage —
RAGAS is widely used through DeepEval and other wrappers in 2026 — so
build order is intentionally last.

## Components

Two distinct roles. Each framework adapter is exactly one of them, never
both.

### Role A — Runner adapter (extends existing `runner.Adapter`)

Today's `runner.Adapter`:

```go
type Adapter interface {
    Matches(cmd []string) bool
    Run(cmd []string) ([]models.TestResult, int)
}
```

is extended to also return metric data points:

```go
type Adapter interface {
    Matches(cmd []string) bool
    Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int)
}
```

Existing pytest, jest, and go-test adapters return a `nil` slice for
metrics — no behaviour change. New runner adapters that own framework-
specific tools (Promptfoo, Inspect AI) parse the framework's output file
inside `Run` and return both test results and per-(case × criterion)
metric entries.

### Role B — Eval plugin adapter (new, in `internal/eval/`)

For frameworks that piggyback on an existing runner — DeepEval and
RAGAS both run inside pytest — a plugin adapter contributes arg/env
mutations *before* the runner runs the child, and reads the framework's
output file *after* the child exits. The runner adapter still owns
execution and pass/fail extraction.

```go
package eval

type PluginAdapter interface {
    // Matches reports whether this plugin attaches to the given command.
    Matches(cmd []string) bool

    // Prepare returns arg mutations and env vars to apply before the
    // runner adapter executes the child. Mutations are pure: the plugin
    // does not run anything.
    Prepare(cmd []string) (Prepared, error)

    // Teardown is called after the child exits and after the runner
    // adapter has produced its TestResults. It reads whatever output
    // file the plugin asked the framework to write and returns the
    // per-(case × criterion) metric data points.
    Teardown(p Prepared) ([]*metricspb.Metric, error)
}

type Prepared struct {
    Args []string // mutated argv to pass into the runner adapter's Run
    Env  []string // env to add to the child
    // Plugin-private state (e.g. the tempfile path it injected) is closed
    // over by the implementation; not part of this struct.
}
```

Plugin adapters never return `models.TestResult`s — pass/fail is owned by
the runner adapter. This keeps the contract clean and avoids two adapters
disagreeing about whether a case passed.

### `internal/runner/registry.go` change

The existing `Find(cmd) Adapter` is unchanged: still returns the single
matching runner adapter. A new `FindPlugins(cmd) []eval.PluginAdapter`
returns every plugin that matches. Plugins are registered separately
from runners (`Registry.RegisterPlugin`), so the existing `Register` call
sites are untouched.

Two runner adapters matching the same command is a panic at registry-build
time (programmer error — adapters live in this repo). Multiple plugins
matching is normal.

### `exec.go` change

The exec loop:

1. `runnerAdapter := reg.Find(cmd)`. If `nil`, exit 2 as today.
2. `plugins := reg.FindPlugins(cmd)`.
3. For each plugin, call `Prepare(cmd)` and accumulate arg + env
   mutations. Conflicting arg mutations (two plugins both wanting the
   same flag) are an error returned to the user.
4. Call `runnerAdapter.Run(mutatedCmd)` with the merged env applied to
   the child. The runner returns `(testResults, runnerMetrics, exitCode)`.
5. For each plugin, call `Teardown` and accumulate `[]*metricspb.Metric`s.
6. Drain the OTLP receiver as today and accumulate user-emitted metrics.
7. Concatenate `runnerMetrics ++ pluginMetrics ++ otlpMetrics` into the
   existing `metrics []*metricspb.Metric` slice that `persistRun` already
   takes. Each `*metricspb.Metric` in the concatenated slice contains
   exactly one data point (matching the `splitMetricByDataPoint`
   convention on the OTLP-receiver path).

`persistRun` then calls `persist.WrapMetricsInResource(persist.MetricResource(run), metrics)`
to wrap into `[]*metricspb.ResourceMetrics`, and `Backend.InsertNewRun(traces, wrappedMetrics)`
as it does today. The persistence interface is unchanged — only the
*sources* of the metrics slice grow.

### Architectural note: adapter-emitted metrics bypass the OTLP wire path

Today, all metrics reach `persistRun` via the OTLP receiver path: child
SDK → HTTP POST to `localhost:<port>/v1/metrics` → receiver buffer →
`MetricsToEntries` → `[]*metricspb.Metric`. Adapter-emitted metrics from
this spec take a *different* path: parse framework output file →
adapter's `Run` returns `[]*metricspb.Metric` directly → exec loop merges
with receiver-emitted metrics.

This is a deliberate deviation from the OTel storage spec's pure
"all metrics through OTLP" stance. Reasons:

- Framework output files are the source of truth for framework-emitted
  scores. Routing them through localhost HTTP would require the adapter
  to act as both producer and OTLP client — added complexity for no
  semantic gain.
- `*metricspb.Metric` is the canonical proto type the persistence layer
  consumes. The adapter just produces it directly. No wire round-trip.
- The OTLP receiver path remains the canonical entry point for
  user-emitted metrics (custom gauges in test code, the "PR review human
  comments" example). Both paths converge at `persistRun`'s metrics
  slice, so storage doesn't see a difference.

This split is what makes Option A (the design choice taken in this spec)
distinct from a hypothetical Option B where adapters POST to the local
OTLP receiver. Option A trades architectural purity for adapter
simplicity.

## Build order

1. **Promptfoo** (smallest end-to-end demo of the file-parse pattern; own
   CLI, no pytest composition). This step also lands the `Run` signature
   change from `([]TestResult, int)` to `([]TestResult, []*metricspb.Metric,
   int)`. Existing pytest / jest / go-test adapters return a `nil` slice
   for metrics — no behaviour change.
2. **DeepEval** (validates the runner+plugin composition; lands the new
   `eval.PluginAdapter` interface and `Registry.RegisterPlugin` /
   `FindPlugins` methods).
3. **Inspect AI** (more involved log format; benefits from patterns
   established by 1+2).
4. **RAGAS** (requires the Python helper; lowest leverage).

Each ships independently. Steps 1 and 2 each introduce one of the two
interface changes; steps 3 and 4 only add new adapter implementations.

## Defaults

| Decision | Default | Reason |
|---|---|---|
| Output file location | `os.MkdirTemp("", "defrost-<framework>-")` | Avoids polluting cwd; auto-cleaned on exit |
| Cleanup of tempfiles | Removed after parse | No reason to keep them; report contents are already in the data branch |
| User-supplied output flag/env-var | Respected, not overridden | Lets users debug by inspecting the file themselves |
| Metric naming | `eval.<criterion>`, lowercased + dotted | Matches `defrost.run` convention from the OTel storage spec |
| Score type | float64 | Most frameworks emit 0.0–1.0; Inspect AI's letter/number scorers narrow to numeric in v1 |
| Non-numeric scorer in Inspect AI | Skipped with stderr warning | Avoids forcing a "score is sometimes a string" union into the metric type |
| Two runner adapters claiming a command | Panic at registry build | Adapters are owned in this repo; conflicts are programmer error |
| Two plugins requesting the same arg/env mutation | Error returned to user | Plugins are pure mutations; conflict is recoverable by user re-invoking with explicit args |

## Error handling

| Failure | Behavior |
|---|---|
| Eval framework's output file missing after child exit | Stderr warning; no metric rows persisted; test spans still go through the runner adapter |
| Output file present but malformed | Stderr warning with the parse error; no metric rows persisted; child exit code preserved |
| Single record in the output file is unparseable | Skip that record with a stderr warning; remaining records persist |
| Auto-injected output flag/env-var collides with a user-supplied value | Respect the user's; parse from where they pointed it. If that parse fails, see above |
| Two plugins request the same flag/env mutation | Return the conflict to the user via stderr with both plugin names and the mutation; exit non-zero before running the child |
| Child crashes before producing any output | Stderr message; runner-adapter-derived test results (if any) persist; no eval-score metrics |

## Testing

- **Unit (runner adapters):** per-adapter parse logic — fixture file
  for each framework → expected `([]TestResult, []*metricspb.Metric)`.
  Fixtures committed under `internal/eval/<framework>/testdata/`
  (Promptfoo, Inspect AI) or `internal/python/pytest/testdata/` for
  the existing pytest adapter (unchanged).
- **Unit (plugin adapters):** for DeepEval and RAGAS, `Teardown` against
  fixture output files → expected `[]*metricspb.Metric`. Fixtures committed
  under `internal/eval/<framework>/testdata/`.
- **Unit (registry):** `Registry.Find(cmd)` continues to return the
  single matching runner. New `Registry.FindPlugins(cmd)` test matrix
  covers single-plugin, multi-plugin, and no-plugin cases.
- **Unit (exec merge):** stub runner returning `(TestResult, *metricspb.Metric)`
  + stub plugin returning `*metricspb.Metric`; verify the persisted union and
  the order of `Teardown` calls.
- **Integration (Promptfoo):** real `promptfoo eval` against a tiny YAML
  config (one prompt, two assertions) → verify
  `metrics/eval.<assertion_type>.ndjson` rows + `traces/<case>.ndjson`
  rows match the framework's actual output.
- **Integration (DeepEval):** real `pytest` invocation with a single
  DeepEval-instrumented test → verify pytest's pass/fail trace lands
  via the existing pytest adapter and DeepEval's score metric lands via
  the plugin teardown.
- Inspect AI and RAGAS get integration coverage when their adapters land.

## Non-goals

- **Mode B** (production eval streams from Langfuse, Braintrust,
  Phoenix, LangSmith). Defrost stays batch-only.
- **Mode C** (lm-evaluation-harness, OpenAI Evals, HELM). Separate
  design pass once A is shipped.
- A `defrost eval` CLI namespace. `defrost exec <eval-cmd>`,
  `defrost history <metric-or-test>`, and `defrost serve` already cover
  the surface.
- Auto-attaching `test.case.name` to user-emitted OTLP metrics during
  an eval run. Same non-goal as the OTel storage spec.
- Streaming production data into the data branch.
- Adapter-level deduplication of eval records across the file-parse and
  OTLP paths. By design these are disjoint sources; if a user pushes the
  same score over both, both rows land.
- A `defrost.eval.*` migration tool. When semconv catches up, adapters
  switch to `gen_ai.*` keys for new rows; old rows on the data branch
  retain `defrost.eval.*` and are read with both names.
