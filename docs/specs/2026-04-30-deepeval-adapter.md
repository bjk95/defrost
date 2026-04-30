# DeepEval adapter spec

**Date:** 2026-04-30
**Status:** Ready for implementation
**Role:** B (plugin on pytest runner)
**Package:** `internal/eval/deepeval/`
**Precedent:** `internal/eval/promptfoo/` (Role A runner); this spec mirrors the parser/metric shape.

---

## 1. Purpose and scope

DeepEval is a Python LLM evaluation library that runs as a pytest plugin. Tests call
`assert_test(llm_output, [metric1, metric2, ...])` or use the `evaluate(...)` API inside
standard pytest test functions. At the end of the run DeepEval writes a timestamped JSON
report to a configurable results folder.

The defrost DeepEval adapter contributes per-(case × metric) gauge metrics with OTel
Gen-AI semconv attributes — the same shape as the Promptfoo adapter — without duplicating
the pass/fail extraction that the existing pytest runner already handles.

**In scope:**
- Detecting when a pytest run loaded DeepEval (via presence of the results JSON).
- Setting `DEEPEVAL_RESULTS_FOLDER` to a defrost-controlled tempdir in the child
  environment before execution.
- After the run, reading `<tempdir>/test_run_*.json` and emitting one metric per
  `metrics_data` entry.
- Tolerant teardown: if no DeepEval JSON appears (pure-pytest run, or DeepEval not
  installed) the plugin exits cleanly without error.

**Out of scope (v1):**
- Metrics that involve nested sub-metric structures (e.g. multi-step RAG breakdowns) —
  emit only the top-level `metrics_data` entries.
- Replicating pass/fail logic — the pytest runner already handles JUnit XML; this plugin
  adds metrics only.
- `evaluate(...)` API calls made outside of pytest (standalone script usage).

---

## 2. Architectural fit

**Role B** — plugin-style adapter that piggybacks on the existing pytest runner
(`internal/python/pytest/`).

The pytest runner (`Adapter.Run`) handles:
- Child execution via `runner.RunChild`.
- JUnit XML injection (`--junitxml`) and parsing.
- `runner.ApplyRepoPrefix` on test IDs.
- Pass/fail extraction and exit-code propagation.

The DeepEval plugin contributes **only metrics** — it never returns `[]models.TestResult`.
Composition is explicit: the pytest runner's `Run` calls each registered plugin's
`Prepare` (before exec) and `Teardown` (after exec), accumulating their returned metrics
alongside its own zero metrics.

This avoids duplicating process management, JUnit parsing, and repo-prefix logic.

### Interface the plugin implements

```go
// Plugin is the interface that per-framework metric plugins implement.
// The pytest runner calls Prepare before exec and Teardown after.
type Plugin interface {
    Prepare(env []string) ([]string, error)    // inject env vars; return modified env
    Teardown(exitCode int) ([]*metricspb.Metric, error)
}
```

If this interface doesn't yet exist in `internal/python/pytest/`, it must be added as
part of this implementation. The DeepEval plugin is the first consumer.

---

## 3. Invocation forms recognised

The DeepEval plugin attaches to **every pytest invocation** that the pytest runner
matches — it does not add its own `Matches` logic. This is the "always-attach + tolerant
teardown" approach: the plugin injects `DEEPEVAL_RESULTS_FOLDER` unconditionally. If
DeepEval is not installed or no tests use it, no JSON file appears and `Teardown` returns
nil metrics silently.

The pytest runner already recognises:
- `pytest <path>`
- `python -m pytest <path>`
- `deepeval test run <path>` — this is a thin shell wrapper around pytest that passes its
  arguments through. The pytest runner's `Matches` must be extended to recognise
  `deepeval test run <path>` as a pytest invocation. This is the only `Matches` change
  required.

**Implementation note on `deepeval test run`:** the binary is just:
```
deepeval test run <args> → pytest <args>
```
Add a case to `internal/python/pytest/adapter.go`'s `Matches`:
```go
if cmd[0] == "deepeval" && len(cmd) >= 3 && cmd[1] == "test" && cmd[2] == "run" {
    return true
}
```
Run logic is identical: inject `--junitxml` and the DeepEval env var, then exec `cmd[0]`
verbatim — `deepeval test run` forwards unknown flags to pytest.

---

## 4. Auto-injection

Before child execution the DeepEval plugin's `Prepare` injects one environment variable:

| Variable | Value | Purpose |
|---|---|---|
| `DEEPEVAL_RESULTS_FOLDER` | `<tempdir>` | Directory where DeepEval writes its timestamped report |

The tempdir is created with `os.MkdirTemp("", "defrost-deepeval-*")` and removed in
`Teardown` after the report has been read.

No CLI flags are added to the pytest invocation — the env var is the only injection
point. DeepEval reads `DEEPEVAL_RESULTS_FOLDER` at startup and routes its JSON writer
there.

**Verify:** confirm the variable name is stable in the DeepEval version the project
targets (see Open questions §11).

---

## 5. Output schema

DeepEval writes one file per run: `<DEEPEVAL_RESULTS_FOLDER>/test_run_<timestamp>.json`.
The glob pattern `test_run_*.json` identifies it.

Relevant fields (other top-level keys are ignored):

```jsonc
{
  "testCases": [
    {
      "name": "test_capital_of_france",  // pytest test node ID
      "success": true,
      "metrics_data": [
        {
          "name": "Faithfulness",
          "score": 0.85,
          "threshold": 0.7,
          "success": true,
          "reason": "The answer is grounded in the context",
          "evaluation_model": "gpt-4o-mini"
        },
        {
          "name": "Answer Relevancy",
          "score": 0.91,
          "threshold": 0.8,
          "success": true,
          "reason": "Directly addresses the question",
          "evaluation_model": "gpt-4o-mini"
        }
      ]
    }
  ]
}
```

Key uncertainties (see §11):
- `name` in `testCases[]` — is it the pytest node ID (e.g. `test_file.py::test_name`) or
  a human-readable label? Needs verification against real output.
- `evaluation_model` — present when an LLM-as-judge metric runs; may be absent or `null`
  for deterministic metrics.

---

## 6. Mapping table

| DeepEval field | Defrost output | Notes |
|---|---|---|
| `testCases[i].name` | `test.case.name` attribute | Verify it is the pytest node ID |
| `metrics_data[j].name` | `gen_ai.evaluation.name` attribute; also forms the leaf segment of the qualified metric name (see below) | Normalise: lowercase, spaces → `_` |
| `metrics_data[j].score` | `gen_ai.evaluation.score.value` (float64); gauge `NumberDataPoint.AsDouble` | |
| `metrics_data[j].success` | `gen_ai.evaluation.score.label` → `"pass"` or `"fail"` | |
| `metrics_data[j].threshold` | `defrost.eval.threshold` | Omit attribute if field absent/null |
| `metrics_data[j].reason` | `gen_ai.evaluation.explanation` | May be empty string |
| `metrics_data[j].evaluation_model` | `defrost.eval.judge_model` | Omit attribute if absent/null |

One `*metricspb.Metric` is emitted per `(testCase, metric_data entry)` pair. Metric name:
`"eval." + scope + "." + normalised(metrics_data[j].name)`, where `scope` is the
dot-joined repo-relative path that uniquely locates the eval inside the repo —
`<runner.RepoRelCwd()>.<sourceFile>`, with empty segments dropped. For DeepEval
`<sourceFile>` is the pytest module that produced the run (e.g. `tests/test_rag.py`).
Example: `eval.tests/eval/test_rag.py.faithfulness`. The unscoped form
`"eval." + name` is reserved for unit tests of the parser. Each metric carries
exactly one `NumberDataPoint` (matches the `splitMetricByDataPoint` convention
the persistence layer expects — see `internal/eval/promptfoo/parser.go`:`mapComponentResult`).

---

## 7. Pure parser sketch

Package: `internal/eval/deepeval/parser.go`

```go
// Parse reads a DeepEval test_run_*.json file and emits one
// *metricspb.Metric per (testCase, metric_data) pair.
// Returns nil/nil/error only on JSON decode failure.
func Parse(r io.Reader) ([]*metricspb.Metric, error)
```

Helpers needed:
- `normaliseMetricName(s string) string` — lowercase, replace spaces with `_`, strip
  characters that are not `[a-z0-9_.]` (keep metric names safe as OTel names and Go
  identifiers).
- `mapMetricData(caseName string, m deepevalMetricData, now uint64) *metricspb.Metric` —
  builds the `metricspb.Metric` with `Gauge` data and the attribute set from §6.
- `passFailLabel(success bool) string` — returns `"pass"` or `"fail"` (identical to
  Promptfoo parser; consider extracting to `internal/models`).

Internal types:
```go
type deepevalDoc struct {
    TestCases []deepevalTestCase `json:"testCases"`
}
type deepevalTestCase struct {
    Name        string               `json:"name"`
    Success     bool                 `json:"success"`
    MetricsData []deepevalMetricData `json:"metrics_data"`
}
type deepevalMetricData struct {
    Name            string   `json:"name"`
    Score           float64  `json:"score"`
    Threshold       *float64 `json:"threshold"`
    Success         bool     `json:"success"`
    Reason          string   `json:"reason"`
    EvaluationModel string   `json:"evaluation_model"`
}
```

The parser is pure — no file I/O, no env access. The plugin's `Teardown` owns file
finding and passes an `io.Reader` to `Parse`.

---

## 8. Plugin sketch

Package: `internal/eval/deepeval/plugin.go`

```go
type Plugin struct {
    tempDir string
}

// Prepare creates a tempdir and injects DEEPEVAL_RESULTS_FOLDER into env.
func (p *Plugin) Prepare(env []string) ([]string, error)

// Teardown globs for test_run_*.json in the tempdir, parses it, and
// returns the metrics. Removes the tempdir. Tolerant: if no file is
// found, returns nil, nil (not an error).
func (p *Plugin) Teardown(exitCode int) ([]*metricspb.Metric, error)
```

Error paths:
- `os.MkdirTemp` failure in `Prepare` → return error; pytest runner logs and falls back
  to passthrough (or runs without metric capture, depending on how the plugin interface
  defines failure semantics — specify this in the plugin interface design).
- No JSON file found in `Teardown` → return nil, nil (tolerant; pure-pytest run is fine).
- Multiple JSON files found → read the lexicographically last file (most recent run);
  warn to stderr if more than one is present.
- JSON decode failure → return nil, error; pytest runner logs the warning but still
  returns the JUnit-based test results with exit code preserved.

---

## 9. Hermetic CI dogfood

DeepEval metrics that invoke an LLM judge (Faithfulness, Answer Relevancy, etc.) require
an LLM API key at runtime. For CI dogfood we need a fully deterministic setup with no
real API calls.

**Recommended approach: `BaseMetric` subclasses with hardcoded scoring.**

DeepEval allows subclassing `BaseMetric` and overriding `measure()` to return a
hardcoded score:

```python
from deepeval.metrics import BaseMetric
from deepeval.test_case import LLMTestCase

class AlwaysPassMetric(BaseMetric):
    threshold = 0.5
    name = "always_pass"

    def measure(self, test_case: LLMTestCase):
        self.score = 1.0
        self.success = True
        self.reason = "deterministic mock: always pass"
        return self.score

    async def a_measure(self, test_case: LLMTestCase, *args, **kwargs):
        return self.measure(test_case)

    def is_successful(self):
        return self.success
```

One `AlwaysPassMetric` and one `AlwaysFailMetric` (hardcodes `score=0.0`,
`success=False`) gives a deterministic mix. No `OPENAI_API_KEY` or any other secret
needed.

**Dogfood test file:** `examples/deepeval/test_defrost_smoke.py`

Expected breakdown for CI assertions:
- 2 test cases minimum (one intended pass, one intended fail).
- AlwaysPassMetric cases → `gen_ai.evaluation.score.label = "pass"`.
- AlwaysFailMetric cases → `gen_ai.evaluation.score.label = "fail"`.

**CI job shape** (mirrors the `promptfoo:` job in `.github/workflows/integration.yml`):
1. `pip install pytest deepeval`.
2. Build defrost.
3. Suppress intentional failures via `defrost suppress add`.
4. `defrost exec pytest examples/deepeval/ 2>&1 | tee out.txt` — exits 0 after
   suppression.
5. Assert `defrost: results:` line shows expected total count.

---

## 10. Implementation order

Follow the TDD shape established by the Promptfoo implementation.

1. **Define the plugin interface** in `internal/python/pytest/` (if it doesn't exist).
   Write a test that a nil plugin list composes cleanly with the existing pytest adapter.

2. **Write `internal/eval/deepeval/parser_test.go`** with a golden JSON fixture
   (`testdata/test_run_smoke.json`) covering: one passing case with two metrics, one
   failing case with one metric, a case with no metrics_data (should produce zero
   metrics), a metric with no evaluation_model (attribute omitted).

3. **Write `internal/eval/deepeval/parser.go`** until the parser tests pass.

4. **Write `internal/eval/deepeval/plugin_test.go`** testing `Prepare` injects the env
   var, and `Teardown` returns nil metrics on empty tempdir, returns parsed metrics on
   valid JSON, removes the tempdir afterward.

5. **Write `internal/eval/deepeval/plugin.go`** until plugin tests pass.

6. **Extend `internal/python/pytest/adapter.go`**:
   - Add `deepeval test run` to `Matches`.
   - Wire the plugin interface into `Run` (Prepare → exec → Teardown).
   - Write adapter-level tests covering both code paths.

7. **Write `examples/deepeval/`** dogfood (test file + requirements or `pyproject.toml`).

8. **Add `deepeval:` CI job** to `.github/workflows/integration.yml`.

9. **Suppress known-intentional failures** in CI using `defrost suppress add`.

---

## 11. Open questions / risks

1. **`DEEPEVAL_RESULTS_FOLDER` variable name:** confirm the env var name is stable across
   DeepEval versions. Verify by checking DeepEval source (`deepeval/constants.py` or
   similar) against the version pinned in dogfood requirements.

2. **`testCases[i].name` format:** does DeepEval populate this with the pytest node ID
   (`path/to/test_file.py::test_function_name`) or with a user-supplied display name?
   If it's the pytest node ID, it can be correlated with JUnit XML results for richer
   linking. Verify with a real run before finalising the mapping.

3. **Single vs. multiple JSON files:** can one pytest run produce multiple
   `test_run_*.json` files (e.g. parallel execution)? Verify with `pytest -n 4` and
   DeepEval.

4. **`metrics_data` presence when no DeepEval metrics are used:** some test cases in a
   mixed pytest file may not call `assert_test`. Confirm that `metrics_data` is present
   as an empty array (not absent) for such cases.

5. **DeepEval `evaluate()` API vs. `assert_test`:** does `evaluate()` called outside
   pytest still write to `DEEPEVAL_RESULTS_FOLDER`? If so, the plugin may capture
   metrics from non-pytest DeepEval runs inadvertently. Out of scope for v1 but worth
   noting.

6. **`evaluation_model` field:** is this `null`, absent, or an empty string when a
   metric doesn't use an LLM judge? The Go struct uses `*float64` for threshold (pointer
   to distinguish absent from zero). Use the same pattern for `evaluation_model` if
   needed.

7. **Plugin interface failure semantics:** if `Prepare` fails (e.g. can't create
   tempdir), should the pytest adapter run passthrough, run normally without metric
   capture, or abort? Define this in the interface before implementing.
