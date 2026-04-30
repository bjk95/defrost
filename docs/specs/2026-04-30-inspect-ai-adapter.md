# Inspect AI adapter spec

**Date:** 2026-04-30
**Status:** Ready for implementation
**Role:** A (standalone runner)
**Package:** `internal/eval/inspect/`
**Precedent:** `internal/eval/promptfoo/` — same Role A runner shape. Read
`internal/eval/promptfoo/adapter.go` and `internal/eval/promptfoo/parser.go` before
implementing.

---

## 1. Purpose and scope

Inspect AI (UK AISI) is a Python LLM evaluation framework with its own CLI.
Evaluations are defined as Python task files (`task.py`) and run with
`inspect eval task.py`. By default Inspect writes binary `.eval` log files; JSON
output requires `--log-format=json`.

The defrost Inspect AI adapter is a Role A runner: it owns the full lifecycle (Matches,
auto-injection, exec, parse, metric emission). The pytest runner is not involved.

**In scope:**
- Matching `inspect eval <file>` in all invocation forms.
- Auto-injecting `--log-dir <tempdir>` and `--log-format=json` so the parser can read
  structured output.
- Parsing JSON log files written to `<tempdir>/` after the run.
- Emitting one `eval.<scorer_name>` metric per `(sample, scorer)` pair with OTel Gen-AI
  semconv attributes.
- Skipping non-numeric scorer values (Letter grades, compound objects) with a stderr
  warning.

**Out of scope (v1):**
- Non-numeric scorers — skip with warning.
- Multiple tasks per invocation (`inspect eval task1.py task2.py`) — handle if the JSON
  schema is consistent across files, but treat as risk (see §11).
- Inspect's `--model` flag inference — the model name is read from the JSON log's
  `eval.model` field, not parsed from the command line.
- Watch/interactive mode.

---

## 2. Architectural fit

**Role A** — the Inspect AI adapter implements `runner.Adapter` directly, the same
interface as the Promptfoo adapter:

```go
type Adapter struct{}

func (a *Adapter) Matches(cmd []string) bool
func (a *Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int)
```

Inspect AI has its own CLI (`inspect eval`), its own output format, and its own
pass/fail concept (sample-level `scores` map). There is no existing defrost runner to
piggyback on — hence Role A.

Execution uses `runner.RunChild` (stdio passthrough + two-stage SIGINT), the same as the
Promptfoo adapter. `runner.ApplyRepoPrefix` is applied to `[]models.TestResult` before
returning.

---

## 3. Invocation forms recognised

The adapter matches any invocation where the executable resolves to `inspect` and the
subcommand is `eval`. The following forms are all valid:

| Form | Example |
|---|---|
| Direct binary | `inspect eval task.py` |
| With model flag | `inspect eval task.py --model openai/gpt-4o` |
| With extra flags | `inspect eval task.py --limit 10 --epochs 2` |
| Poetry/uv runner | `poetry run inspect eval task.py` |
| Python module | `python -m inspect_ai eval task.py` |

`Matches` implementation:

```go
func (a *Adapter) Matches(cmd []string) bool {
    if len(cmd) == 0 {
        return false
    }
    // Direct: inspect eval ...
    if filepath.Base(cmd[0]) == "inspect" {
        return len(cmd) >= 2 && cmd[1] == "eval"
    }
    // python -m inspect_ai eval ...
    if pythonRe.MatchString(cmd[0]) && len(cmd) >= 4 &&
        cmd[1] == "-m" && cmd[2] == "inspect_ai" && cmd[3] == "eval" {
        return true
    }
    // poetry run inspect eval ... / uv run inspect eval ...
    if toolRunners[cmd[0]] && len(cmd) >= 4 &&
        cmd[1] == "run" && filepath.Base(cmd[2]) == "inspect" && cmd[3] == "eval" {
        return true
    }
    return false
}
```

`pythonRe` and `toolRunners` can be lifted from `internal/python/pytest/adapter.go` or
moved to a shared location in `internal/runner/`.

---

## 4. Auto-injection

Before child execution `Run` injects two flags into the command:

| Flag | Value | Purpose |
|---|---|---|
| `--log-dir` | `<tempdir>` | Redirect log files to a defrost-controlled directory |
| `--log-format` | `json` | Override the default binary `.eval` format |

The tempdir is created with `os.MkdirTemp("", "defrost-inspect-*")` and removed after
parsing (defer in `Run`).

**Passthrough triggers:**

If the user has already supplied `--log-dir` or `--log-format` in their command, defrost
cannot safely override them. The adapter detects this and runs passthrough:

```
defrost: inspect --log-dir / --log-format present in argv;
running passthrough (no per-test results will be recorded)
```

This mirrors the Promptfoo adapter's treatment of user-supplied `--output` flags. See
`internal/eval/promptfoo/adapter.go`:`passthroughRun`.

---

## 5. Output schema

After the run, `<tempdir>/` contains one JSON file per task evaluated. Each file matches
the glob `*.json` (Inspect names them `<task>_<timestamp>.json` but the exact pattern
should be verified — see §11).

Each JSON file has the following relevant structure (verify with `inspect log schema` at
implementation time — this schema may have evolved):

```jsonc
{
  "eval": {
    "task": "my_task",           // task name, used as test.suite.name
    "model": "openai/gpt-4o",   // model used, maps to gen_ai.request.model
    "dataset": {
      "name": "my_dataset"
    }
  },
  "samples": [
    {
      "id": 1,                   // sample identifier (int or string)
      "input": "...",
      "target": "...",
      "output": {
        "completion": "..."      // model response text
      },
      "scores": {
        "accuracy": {
          "value": 1.0,          // numeric score; may also be "C"/"I" (Letter)
          "answer": "Paris",
          "explanation": "Correct",
          "metadata": {}
        },
        "f1_score": {
          "value": 0.85,
          "answer": "...",
          "explanation": "...",
          "metadata": {}
        }
      }
    }
  ],
  "results": {
    "scores": [
      {
        "name": "accuracy",
        "metrics": {
          "mean": {"value": 0.72, "name": "mean"},
          "stderr": {"value": 0.03, "name": "stderr"}
        }
      }
    ]
  }
}
```

The parser reads `samples[]` (per-sample results), not `results.scores[]` (aggregates).
Aggregate scores are summaries of the per-sample data and would double-count.

---

## 6. Mapping table

| Inspect AI field | Defrost output | Notes |
|---|---|---|
| `eval.task` | `test.suite.name` attribute | Stable across all samples in a file |
| `samples[i].id` | `TestResult.Id` (via `test.case.name`) | Convert int to string: `"sample_<id>"` |
| `samples[i].id` | `test.case.name` attribute on metrics | Same value |
| `samples[i].scores[k].value` (numeric) | `gen_ai.evaluation.score.value`; metric name `"eval.<k>"` | Skip if not parseable as float64 |
| `samples[i].scores[k].value` (Letter `"C"`/`"I"`) | Skip — emit stderr warning | See §11 |
| scorer name `k` | `gen_ai.evaluation.name` attribute | |
| `samples[i].scores[k].explanation` | `gen_ai.evaluation.explanation` | May be empty |
| `eval.model` | `gen_ai.request.model` attribute | Applied to all samples in the file |
| `samples[i].scores[k].value >= 0.5` heuristic | `TestResult.Passed` | Correct threshold TBD — see §11 |
| `samples[i].output.completion` | `TestResult.Output` | Raw model output for human review |

**TestResult construction:** one `TestResult` per sample (not per sample × scorer). The
sample passes if all its numeric scorers are above their respective thresholds, or by a
simple majority, or by the `correct` boolean if present. The exact pass/fail rule for
`TestResult.Passed` must be decided at implementation time — see §11.

---

## 7. Pure parser sketch

Package: `internal/eval/inspect/parser.go`

```go
// ParseFile reads a single Inspect AI JSON log file and emits
// per-sample TestResults and one *metricspb.Metric per (sample, scorer).
// Returns nil/nil/error only on JSON decode failure.
func ParseFile(r io.Reader) ([]models.TestResult, []*metricspb.Metric, error)
```

Helpers needed:
- `numericScore(v any) (float64, bool)` — attempts to parse the `value` field as
  float64. Returns false for strings like `"C"`, `"I"`, compound objects.
- `sampleCaseName(id any) string` — converts int or string sample ID to
  `"sample_<id>"`.
- `mapSample(s inspectSample, taskName, model string, now uint64) (models.TestResult, []*metricspb.Metric)` —
  builds one TestResult and N metrics from a single sample.
- `passFailLabel(pass bool) string` — same as Promptfoo parser.

Internal types:
```go
type inspectDoc struct {
    Eval    inspectEval     `json:"eval"`
    Samples []inspectSample `json:"samples"`
}
type inspectEval struct {
    Task  string `json:"task"`
    Model string `json:"model"`
}
type inspectSample struct {
    ID     any                        `json:"id"` // int or string
    Output inspectOutput              `json:"output"`
    Scores map[string]inspectScore    `json:"scores"`
}
type inspectOutput struct {
    Completion string `json:"completion"`
}
type inspectScore struct {
    Value       any    `json:"value"` // float64, int, or string
    Answer      string `json:"answer"`
    Explanation string `json:"explanation"`
}
```

The parser is pure — no file I/O, no env access. `Run` owns directory scanning and
passes each file as an `io.Reader` to `ParseFile`.

---

## 8. Adapter sketch

Package: `internal/eval/inspect/adapter.go`

```go
type Adapter struct{}

func (a *Adapter) Matches(cmd []string) bool  // see §3

func (a *Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
    // 1. Check for user-supplied --log-dir / --log-format; if present, passthrough.
    // 2. Create tempdir.
    // 3. Build args: inject --log-dir <tempdir> --log-format json.
    // 4. runner.RunChild.
    // 5. Glob tempdir for *.json files.
    // 6. For each file: ParseFile, accumulate results + metrics.
    // 7. runner.ApplyRepoPrefix(results).
    // 8. Remove tempdir (defer).
    // 9. Return results, metrics, exitCode.
}
```

Helpers:
- `hasUserLogDir(args []string) bool` — checks for `--log-dir` / `--log-dir=<v>`.
- `hasUserLogFormat(args []string) bool` — checks for `--log-format` / `--log-format=<v>`.
- `buildArgs(cmd []string, tempDir string) []string` — appends `--log-dir <tempDir>
  --log-format json` to the command.
- `passthroughRun(cmd []string) ([]models.TestResult, []*metricspb.Metric, int)` —
  identical to Promptfoo's version; consider extracting to `internal/runner/`.

Error paths:
- `os.MkdirTemp` failure → log to stderr, return nil/nil/1.
- `runner.RunChild` error → log to stderr, return nil/nil/1.
- No JSON files in tempdir after run → log warning (inspect may have failed before
  writing), return nil/nil/exitCode (preserve child's exit signal).
- JSON decode failure for a file → log warning, skip that file, continue with remaining
  files.
- Sample has no numeric scorers → TestResult with `Ran=true`, `Passed` by heuristic,
  zero metrics; warn to stderr.

---

## 9. Hermetic CI dogfood

Inspect AI supports a `none` model provider that returns empty completions without any
LLM calls, and custom `Solver` implementations that return canned responses. For hermetic
dogfood either approach works:

**Option A (recommended): `TaskState` mock solver**

```python
from inspect_ai import task, Task
from inspect_ai.solver import solver, TaskState, Generate
from inspect_ai.scorer import match

@solver
def mock_solver():
    async def solve(state: TaskState, generate: Generate) -> TaskState:
        # Return a deterministic answer based on the input
        state.output.completion = "Paris"
        return state
    return solve

@task
def capital_cities():
    return Task(
        dataset=[
            Sample(input="Capital of France?", target="Paris"),
            Sample(input="Capital of Germany?", target="Berlin"),
        ],
        solver=mock_solver(),
        scorer=match(),
    )
```

This exercises the real scoring path (Inspect's `match()` scorer) with no LLM API calls.

**Option B: `--model mockllm/model`**

Inspect AI ships a `mockllm` provider for testing. It returns configurable canned
responses. Less idiomatic than Option A but requires no custom Python.

**Expected CI breakdown:**
- 2 samples: one passes `match()` (answer == target), one fails.
- One scorer (`match`) → one `eval.match` metric per sample.
- `gen_ai.evaluation.score.value` ∈ {0.0, 1.0} (Inspect's `match` scorer is binary).

**CI job shape** (mirrors `promptfoo:` job in `.github/workflows/integration.yml`):
1. `pip install inspect-ai`.
2. Build defrost.
3. Suppress intentional failures via `defrost suppress add`.
4. `defrost exec inspect eval examples/inspect/task.py 2>&1 | tee out.txt` — exits 0
   after suppression.
5. Assert `defrost: results:` line shows expected total count.

---

## 10. Implementation order

Follow the TDD shape established by the Promptfoo implementation.

1. **Write `internal/eval/inspect/parser_test.go`** with golden JSON fixtures
   (`testdata/inspect_smoke.json`) covering: two samples with one numeric scorer each
   (one passes, one fails), a sample with a Letter scorer (`"C"`) that should be skipped
   with a warning, a sample with multiple scorers, a sample with no scores map.

2. **Write `internal/eval/inspect/parser.go`** until parser tests pass.

3. **Write `internal/eval/inspect/adapter_test.go`** testing: `Matches` for all forms in
   §3, passthrough when `--log-dir` or `--log-format` present, passthrough when user
   passes unknown flags that conflict with injection.

4. **Write `internal/eval/inspect/adapter.go`** until adapter tests pass (use a fake
   child binary in tests, same pattern as Promptfoo tests).

5. **Register the Inspect adapter** in the adapter registry (wherever adapters are
   wired up — check `cmd/` or `main.go`).

6. **Write `examples/inspect/`** dogfood (task.py + requirements or `pyproject.toml`).

7. **Add `inspect:` CI job** to `.github/workflows/integration.yml`.

8. **Suppress known-intentional failures** in CI using `defrost suppress add`.

---

## 11. Open questions / risks

1. **JSON log file naming pattern:** verify what Inspect AI names its JSON log files in
   `--log-dir`. The glob `*.json` should be safe, but confirm there are no other JSON
   files written to the log directory that shouldn't be parsed.

2. **`samples[i].id` type:** the sample ID may be an integer or a string depending on
   the dataset. The Go struct uses `any` for `id`. Verify that `json.Unmarshal` into
   `any` gives a `float64` for integer IDs (standard JSON decoding in Go) and handle
   the conversion to string accordingly.

3. **Score value types:** `scores[k].value` may be:
   - A float64 (e.g. `0.85`).
   - An integer decoded as float64 (e.g. `1`).
   - A string `"C"` (Correct) or `"I"` (Incorrect) for `exact_match` / `includes`
     scorers in some configurations.
   - A compound object for multi-dimensional scorers.
   The `numericScore` helper must handle all these. Verify against real Inspect output
   before finalising.

4. **TestResult pass/fail rule:** Inspect doesn't have a single "did this sample pass?"
   boolean at the top level in v1. Options:
   - All numeric scorers ≥ their threshold (requires knowing thresholds, which aren't
     in the per-sample JSON).
   - Simple heuristic: sample passes iff all numeric scores are ≥ 0.5.
   - Read the `results.scores[].metrics.mean` and compare against a threshold.
   This needs a decision before implementation. The heuristic (≥ 0.5) is the simplest
   and avoids requiring threshold configuration. Verify against real Inspect output.

5. **Multiple tasks per invocation:** `inspect eval task1.py task2.py` may write multiple
   JSON files to `--log-dir`. The adapter should handle this gracefully (iterate over
   all `*.json` files). Verify that the per-file schema is the same whether one or
   multiple task files are provided.

6. **`inspect log schema` command:** run `inspect log schema` against the version pinned
   in dogfood requirements to get the authoritative JSON schema. The schema described in
   §5 is based on documentation; the actual output may differ.

7. **Inspect AI `--log-format` flag name:** confirm the flag is `--log-format` (not
   `--log_format` or `-f`). Check `inspect eval --help`.

8. **Exit code semantics:** does Inspect AI exit non-zero if any sample fails scoring,
   or only on framework errors? The adapter should preserve the child's exit code
   regardless — don't synthesise a non-zero exit from failing samples.
