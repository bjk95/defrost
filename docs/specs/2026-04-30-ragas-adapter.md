# RAGAS adapter spec

**Date:** 2026-04-30
**Status:** Ready for implementation — build last (see §1)
**Role:** B (plugin on pytest runner)
**Package:** `internal/eval/ragas/` (Go) + `examples/ragas/defrost_ragas.py` (Python helper)
**Precedent:** `internal/eval/promptfoo/` (Role A runner) for parser/metric shape;
`internal/eval/deepeval/` for pytest plugin composition.

---

## 1. Purpose and scope

RAGAS is a Python library for evaluating Retrieval-Augmented Generation pipelines.
Unlike DeepEval or Promptfoo, RAGAS has no CLI and no auto-dump behaviour. Users call
`ragas.evaluate(dataset, metrics=[...])` from inside ordinary Python code, and the call
returns an `EvaluationResult` object in memory. Nothing is written to disk unless the
user explicitly does so.

This makes RAGAS the highest-glue framework of the three: defrost cannot observe RAGAS
results without the user adding a one-liner to their test. **Build this adapter last**,
after the DeepEval and Inspect AI adapters are shipped and the plugin composition
infrastructure has stabilised.

**In scope:**
- Shipping a small Python helper module (`defrost_ragas.py`) with a
  `write_results(result)` function that serialises `EvaluationResult` to a
  defrost-controlled tempfile path.
- Setting `DEFROST_RAGAS_OUT=<tempfile>` in the child environment before execution.
- After the run, reading the JSON at `$DEFROST_RAGAS_OUT` and emitting one metric per
  `(row, metric)` pair.
- Tolerant teardown: if `$DEFROST_RAGAS_OUT` is absent or empty, return nil metrics
  silently (user didn't call `write_results`, or RAGAS isn't used).

**Out of scope (v1):**
- Auto-injecting `write_results` calls without user cooperation — this would require
  bytecode patching or monkey-patching `ragas.evaluate`, which is fragile.
- Supporting RAGAS async evaluation (`aevaluate`) — same shape, same helper call, but
  async tests are out of scope until there's a concrete need.
- Replicating pass/fail logic — pytest runner handles that.

---

## 2. Architectural fit

**Role B** — same plugin composition as the DeepEval adapter. Requires the plugin
interface defined in `internal/python/pytest/` as part of the DeepEval work.

The RAGAS plugin differs from DeepEval in one key respect: **no env var alone is
sufficient**. The user must actively call `defrost_ragas.write_results(result)` after
`ragas.evaluate(...)`. This is the explicit user-glue requirement. Defrost's
responsibility is limited to:

1. Providing `defrost_ragas.py` in a known location (shipped with the repo, importable
   via `PYTHONPATH` injection or explicit `sys.path` append in user tests).
2. Setting `DEFROST_RAGAS_OUT` to the tempfile path before the child runs.
3. Reading and parsing that file after the child exits.

### Composition

The pytest runner calls:
- `Plugin.Prepare(env)` — creates a tempfile, injects `DEFROST_RAGAS_OUT`.
- Child executes (user's tests call `ragas.evaluate(...)` then
  `defrost_ragas.write_results(result)`).
- `Plugin.Teardown(exitCode)` — reads `$DEFROST_RAGAS_OUT`, parses JSON, returns metrics.

The pytest runner continues to own JUnit XML, pass/fail extraction, and
`runner.ApplyRepoPrefix`. RAGAS plugin contributes only `[]*metricspb.Metric`.

---

## 3. Invocation forms recognised

Same as the DeepEval plugin — the RAGAS plugin attaches to every pytest invocation
recognised by the pytest runner. It does not add `Matches` logic. Tolerant teardown
means it is safe to attach unconditionally.

RAGAS tests are written as standard pytest tests:

```python
# examples/ragas/test_rag_pipeline.py
import defrost_ragas
from ragas import evaluate
from ragas.metrics import faithfulness, answer_relevancy

def test_rag_scores():
    dataset = ...  # HuggingFace Dataset or ragas.Dataset
    result = evaluate(dataset, metrics=[faithfulness, answer_relevancy])
    defrost_ragas.write_results(result)
    # pytest assertions here (e.g. assert result["faithfulness"] > 0.7)
```

No special invocation form. Users run `pytest examples/ragas/` and defrost intercepts as
usual.

---

## 4. Auto-injection

Before child execution the RAGAS plugin's `Prepare` injects:

| Variable | Value | Purpose |
|---|---|---|
| `DEFROST_RAGAS_OUT` | `<tempfile path>` | Path where `write_results` writes JSON |

The tempfile is created with `os.CreateTemp("", "defrost-ragas-*.json")` and its path is
stored on the plugin struct. The file is removed in `Teardown` after reading.

Additionally, defrost must ensure `defrost_ragas.py` is importable in the child's Python
environment. Two options:

**Option A (recommended):** Inject `PYTHONPATH=<dir containing defrost_ragas.py>:$PYTHONPATH`
into the child env in `Prepare`. The helper is shipped at a fixed path relative to the
defrost binary (or embedded via Go `//go:embed`).

**Option B:** Ship `defrost_ragas.py` as a standalone file users are expected to copy or
install. Lower complexity on defrost's side, but worse user experience.

Document the chosen approach in the implementation PR. Option A is preferred.

---

## 5. Output schema (defrost-side JSON contract)

RAGAS's `EvaluationResult` is a pandas DataFrame internally. `defrost_ragas.write_results`
must flatten it into a list of per-(row × metric) records. Defrost owns this schema —
it is not RAGAS's own output format.

```jsonc
{
  "ragas_version": "0.1.x",    // string, from ragas.__version__
  "rows": [
    {
      "row_index": 0,
      "question": "What is the capital of France?",  // optional; from dataset
      "scores": {
        "faithfulness": 0.85,
        "answer_relevancy": 0.91
      }
    },
    {
      "row_index": 1,
      "question": "Who wrote Hamlet?",
      "scores": {
        "faithfulness": 0.72,
        "answer_relevancy": 0.68
      }
    }
  ]
}
```

Notes:
- `question` is included for human readability but is not used in metric attribute
  mapping — it is not a stable key across all RAGAS datasets.
- Metric names in `scores` are the RAGAS metric object's `.name` attribute
  (e.g. `"faithfulness"`, `"answer_relevancy"`).
- Each `scores[metric_name]` is a float in [0.0, 1.0].
- `ragas_version` is recorded for debugging but not surfaced in metric attributes.

### `defrost_ragas.py` implementation sketch

```python
import json
import os
import ragas

def write_results(result):
    """Serialise a ragas.EvaluationResult to $DEFROST_RAGAS_OUT.

    Call this immediately after ragas.evaluate(...):
        result = evaluate(dataset, metrics=[...])
        defrost_ragas.write_results(result)
    """
    out_path = os.environ.get("DEFROST_RAGAS_OUT")
    if not out_path:
        return  # defrost not active; no-op

    df = result.to_pandas()
    metric_cols = [c for c in df.columns if c not in ("question", "answer",
                                                        "contexts", "ground_truth")]
    rows = []
    for i, row in df.iterrows():
        scores = {col: float(row[col]) for col in metric_cols
                  if not __import__("math").isnan(row[col])}
        entry = {"row_index": int(i), "scores": scores}
        if "question" in df.columns:
            entry["question"] = str(row["question"])
        rows.append(entry)

    payload = {
        "ragas_version": ragas.__version__,
        "rows": rows,
    }
    with open(out_path, "w") as f:
        json.dump(payload, f)
```

---

## 6. Mapping table

| defrost JSON field | Defrost output | Notes |
|---|---|---|
| `rows[i].row_index` | `test.case.name` → `"ragas_row_<i>"` | No richer name available without user labels |
| `rows[i].scores[k]` (metric name `k`) | `gen_ai.evaluation.name` → `k`; metric name → `"eval." + k` | One metric per (row, scorer) |
| `rows[i].scores[k]` (numeric value) | `gen_ai.evaluation.score.value`; gauge `NumberDataPoint.AsDouble` | |
| threshold (none in RAGAS output) | omit `defrost.eval.threshold` | RAGAS doesn't surface per-metric thresholds in its result object |
| judge model (none in RAGAS output) | omit `defrost.eval.judge_model` | RAGAS uses the user's LLM config; not in result object |

There is no `success` boolean in the RAGAS result — RAGAS doesn't have a built-in
pass/fail concept. The `gen_ai.evaluation.score.label` attribute is omitted (or set to
`"none"` — decide at implementation time). Document this gap clearly in the metric.

---

## 7. Pure parser sketch

Package: `internal/eval/ragas/parser.go`

```go
// Parse reads a defrost_ragas.write_results() JSON document and emits one
// *metricspb.Metric per (row, scorer) pair.
// Returns nil/nil/error only on JSON decode failure.
func Parse(r io.Reader) ([]*metricspb.Metric, error)
```

Helpers needed:
- `mapScore(caseName, metricName string, value float64, now uint64) *metricspb.Metric` —
  builds the gauge metric. Same structure as `mapComponentResult` in the Promptfoo parser.
- `rowCaseName(rowIndex int) string` — returns `"ragas_row_<rowIndex>"`.

Internal types:
```go
type ragasDoc struct {
    RagasVersion string     `json:"ragas_version"`
    Rows         []ragasRow `json:"rows"`
}
type ragasRow struct {
    RowIndex int                `json:"row_index"`
    Question string             `json:"question"`
    Scores   map[string]float64 `json:"scores"`
}
```

The parser is pure — no file I/O, no env access.

---

## 8. Plugin sketch

Package: `internal/eval/ragas/plugin.go`

```go
type Plugin struct {
    tempFile string
}

// Prepare creates a tempfile and injects DEFROST_RAGAS_OUT + PYTHONPATH into env.
func (p *Plugin) Prepare(env []string) ([]string, error)

// Teardown reads the tempfile (if present), parses it, and returns metrics.
// Removes the tempfile. Tolerant: absent file returns nil, nil.
func (p *Plugin) Teardown(exitCode int) ([]*metricspb.Metric, error)
```

Error paths:
- `os.CreateTemp` failure in `Prepare` → return error.
- Tempfile absent or empty in `Teardown` → nil, nil (user didn't call `write_results`,
  or RAGAS isn't installed — both are valid normal cases).
- JSON decode failure → nil, error; pytest runner logs warning but continues.
- `NaN` scores in the JSON → `write_results` already filters these (`isnan` check), but
  the Go parser should handle `NaN`-via-JSON gracefully if it appears (JSON doesn't
  natively represent NaN; `write_results` skips NaN rows, so this should be a non-issue).

---

## 9. Hermetic CI dogfood

RAGAS metrics (Faithfulness, Answer Relevancy, Context Precision, etc.) all invoke an
LLM to score. This creates a hard dependency on an API key for any realistic dogfood.
Two approaches for CI:

**Option A (recommended): Mock the LLM via RAGAS `LLMFactory`.**

RAGAS allows injecting a custom LLM. Ship a `MockLLM` in the dogfood test file that
returns hardcoded responses for known inputs:

```python
from ragas.llms import BaseRagasLLM

class MockLLM(BaseRagasLLM):
    async def agenerate_text(self, prompt, *args, **kwargs):
        # Return deterministic JSON that the metric scorer expects
        return CompletionResult(text='{"verdict": "1"}')
```

Inject via `faithfulness.llm = MockLLM()`. This requires understanding RAGAS's
internal scorer expectations per metric — the canned response format varies.

**Option B: Custom `BaseMetric`-style scorer.**

Implement `defrost_ragas.write_results` in the dogfood to hardcode scores rather than
calling `ragas.evaluate`:

```python
def test_rag_scores_hermetic():
    # Bypass ragas.evaluate entirely; write canned scores for CI
    import defrost_ragas
    defrost_ragas._write_raw({"ragas_version": "test", "rows": [
        {"row_index": 0, "question": "q1", "scores": {"faithfulness": 0.9}},
        {"row_index": 1, "question": "q2", "scores": {"faithfulness": 0.4}},
    ]})
```

This is simple and fully deterministic, but doesn't exercise the real RAGAS code path
(and requires exposing a `_write_raw` backdoor in `defrost_ragas.py`).

**Recommendation:** Use Option A for one metric (Faithfulness) and document the
`MockLLM` pattern. This exercises the real metric scoring path. Fall back to Option B if
Option A proves too brittle across RAGAS versions.

**Expected CI breakdown:**
- At least 2 rows: one high-scoring (pass by convention), one low-scoring.
- No `defrost.eval.threshold` attributes (RAGAS doesn't surface them).
- No `gen_ai.evaluation.score.label` attributes.

---

## 10. Implementation order

The DeepEval adapter (and its plugin interface) must land first — the RAGAS plugin
depends on the same `Plugin` interface.

1. **Write `internal/eval/ragas/parser_test.go`** with golden JSON fixture
   (`testdata/ragas_smoke.json`) covering: two rows with two metrics each, a row with
   empty scores map, a metric value of exactly 0.0.

2. **Write `internal/eval/ragas/parser.go`** until parser tests pass.

3. **Write `internal/eval/ragas/plugin_test.go`** testing `Prepare` injects env vars,
   `Teardown` returns nil metrics on empty tempfile, returns parsed metrics on valid JSON,
   removes tempfile.

4. **Write `internal/eval/ragas/plugin.go`** until plugin tests pass.

5. **Write `examples/ragas/defrost_ragas.py`** with `write_results` and optional
   `_write_raw` backdoor.

6. **Write `examples/ragas/test_defrost_smoke.py`** dogfood test using Option A or B
   hermetic strategy.

7. **Register the plugin** with the pytest runner alongside the DeepEval plugin.

8. **Add `ragas:` CI job** to `.github/workflows/integration.yml`.

---

## 11. Open questions / risks

1. **RAGAS `EvaluationResult` API stability:** the `to_pandas()` method and the column
   names emitted by RAGAS metrics have changed between minor versions (0.1.x → 0.2.x).
   Pin a specific RAGAS version in dogfood requirements and document the minimum
   supported version.

2. **Metric column naming:** RAGAS metric names in the DataFrame depend on the metric
   object's `.name` attribute. Verify the canonical name for built-in metrics against the
   RAGAS version pinned. `faithfulness` → `"faithfulness"`, `answer_relevancy` →
   `"answer_relevancy"` appears stable but should be confirmed.

3. **`NaN` in scores:** RAGAS emits `NaN` for rows where scoring fails (e.g. empty
   context). The `write_results` helper filters these, but the CI dogfood should include
   a test row that triggers a NaN to confirm the filter works.

4. **PYTHONPATH injection:** `defrost_ragas.py` must be importable in the child Python
   process. If users have complex virtual env setups, injecting `PYTHONPATH` may conflict
   with their environment. An alternative is shipping `defrost_ragas` as a PyPI package
   (`pip install defrost-ragas`), which would require maintaining a Python package.
   Out of scope for v1 — document the `PYTHONPATH` approach and its limitation.

5. **Multiple `evaluate()` calls in one test file:** if a test file calls
   `write_results` multiple times, the second call overwrites the first. Decide whether
   to append or overwrite, and document the limitation.

6. **No pass/fail label:** RAGAS has no threshold concept in its result object. The
   decision about whether a score constitutes "pass" or "fail" belongs to the user's
   pytest assertions. Document that `gen_ai.evaluation.score.label` is absent from RAGAS
   metrics, and what downstream tooling should do with unlabelled scores.

7. **`defrost_ragas.py` discovery:** how does the user know to add
   `import defrost_ragas` to their test? This is a documentation problem more than a
   technical one, but it's the biggest friction point for adoption. Consider whether the
   plugin's `Prepare` should print a hint to stderr if the tempfile is still empty after
   the run.
