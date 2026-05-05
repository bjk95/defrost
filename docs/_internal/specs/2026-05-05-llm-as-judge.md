# Spec #4 — LLM-as-judge primitive

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation. Builds on [`#3 datasets`](./2026-05-05-datasets.md).
**Phase:** 3 — evals as first-class.

## 1. Goal

A defrost-native LLM-as-judge primitive: a configurable evaluator that runs a judge prompt over an `(input, output)` pair and emits a score plus reasoning. Configurations live on the data branch as YAML; runs emit standard OTel spans tagged with `gen_ai.evaluation.*` so the same trace tree (spec #1), cost surfacing (spec #2), and dataset machinery (spec #3) light up for free.

A user can:

1. Define a judge config (model, prompt, score schema) on the data branch.
2. Run the judge against any captured trace span, or against a dataset.
3. See judge scores as a column on the trace tree and on dataset run summaries.

## 2. Why this matters competitively

LLM-as-judge is the workhorse evaluator pattern in 2026. LangSmith ships it as a built-in. Langfuse runs judges as a managed service.

defrost's angle: judges run **client-side** (CLI or `defrost serve` with user-supplied keys), emit **standard OTel** with `gen_ai.evaluation.*` semantic conventions, and persist via the **same git pipeline** as everything else. There's no new storage backend, no service to operate, and the score data is `git diff`-able alongside the dataset rows it scored.

This also closes the Langfuse-specific gap of "server-side LLM-as-judge" — defrost matches the workflow without operating an inference server.

## 3. Public docs to write first

- **New page:** `docs/guides/llm-as-judge.md` — "Score outputs with an LLM judge." Sections: "Define a judge", "Run against a trace", "Run against a dataset", "Read judge scores in the dashboard", "Reproducibility caveats".
- **New page:** `docs/reference/cli/judge.md` — full CLI reference.
- **New concept page:** `docs/concepts/evaluators.md` — short page covering the relationship between datasets, judges, and the existing test runners. Make clear that judges are *one* type of evaluator; existing runners (Inspect AI, Promptfoo, etc.) are others.
- **Edit:** `docs/reference/storage-layout.md` — add `judges/` to the directory tree.
- **Edit:** `docs/reference/serve-api.md` — document new endpoints.

## 4. Storage layout

On the data branch:

```text
<repo>/.defrost/judges/
├── <name>.yaml
└── ...
```

`<name>.yaml`:

```yaml
schema: 1
name: helpfulness-v1
description: Scores helpfulness of a customer-support reply on a 0–5 scale
model: anthropic/claude-opus-4-7
system_prompt: |
  You are an evaluator. Given a customer question and an agent reply,
  score the reply for helpfulness on a 0–5 scale...
user_prompt_template: |
  Question: {{input}}
  Reply:    {{output}}
  Return JSON: {"score": <0-5>, "reason": "<short>"}
score_schema:
  type: scalar          # scalar | categorical
  range: [0, 5]
temperature: 0.0
max_tokens: 256
```

Judge **runs** are stored only as OTel trace files (the canonical traces in `traces/<YYYY>/<MM>/<DD>/...otlp.pb.zst`) — no separate `judge_runs/` directory. The judge span has attributes:

- `gen_ai.evaluation.judge_name` — `<name>`
- `gen_ai.evaluation.judge_version` — git SHA of the judge YAML at run time
- `gen_ai.evaluation.score` — float
- `gen_ai.evaluation.label` — for categorical judges
- `gen_ai.evaluation.reason` — string
- `gen_ai.evaluation.target_trace_id` — the trace this judge scored
- `gen_ai.evaluation.target_span_id` — optional, when scoring a single span

`gen_ai.evaluation.*` is **not** an upstream OTel semantic convention as of today. Document explicitly that defrost is squatting on this namespace pending an upstream RFC, and contribute one. Reference the OTel SemConv repo from the new docs page so contributors can track the upstream conversation.

## 5. CLI surface

`defrost judge` parent command. Subcommands:

### `defrost judge create <name> --model <m> --prompt <file> [--score scalar|categorical] [--range <min,max>]`

Write `judges/<name>.yaml`. `--prompt` accepts a file containing the user prompt template (with `{{input}}` and `{{output}}` placeholders). System prompt can be passed via `--system-prompt <file>` or defaulted.

### `defrost judge edit <name>`

Open `$EDITOR` on `judges/<name>.yaml`. Validate before commit.

### `defrost judge list [--json]`

List judges. Default: `<name>\t<model>\t<score-schema>`.

### `defrost judge show <name> [--ref <git-ref>]`

Print the YAML at HEAD or at a specific ref. Useful for "what scoring did we use back then?"

### `defrost judge run <name> --trace <trace-id> [--span <span-id>]`

Score a single trace. If `--span` is omitted, scores the run's root user-visible LLM span (heuristic: deepest descendant with `gen_ai.usage.*` attributes; if ambiguous, fail with a clear error and require `--span`). The judge's input/output is extracted from the target span:

- `input` ← `gen_ai.request.input` / `gen_ai.prompt` / `gen_ai.input.messages` (try in order)
- `output` ← `gen_ai.response.output` / `gen_ai.completion` / `gen_ai.response.text`

Emits a new trace span recorded under the same machinery as `defrost exec`. Prints the score to stdout.

### `defrost judge run <name> --dataset <dataset> [--version <vN|latest>]`

Score every row in a dataset. For each row, the judge is invoked with `input = row.input`, `output = row.expected` (or `row.metadata.actual_output` if set). Wraps `defrost dataset run` from spec #3 — i.e., judges are just another command you can run against a dataset.

Flags: `--parallel <N>`, `--limit <N>`, `--api-key <env-var-name>`. The api-key flag is the **name** of an env var defrost should read at child-process exec time (e.g., `--api-key ANTHROPIC_API_KEY`). The key itself never appears in process args, never in logs, never on disk.

### `defrost judge` Go library

`internal/eval/judge/`:

```go
package judge

type Judge struct { /* loaded from <repo>/.defrost/judges/<name>.yaml */ }

type Score struct {
    Value     float64
    Label     string  // for categorical
    Reason    string
    JudgeName string
    JudgeRef  string  // git SHA of YAML at score time
}

func Load(name string) (*Judge, error)
func (j *Judge) Score(ctx context.Context, input, output string) (Score, error)
```

Internal users (e.g., the Inspect AI / Promptfoo adapters in `internal/eval/`) can call `judge.Load(...).Score(...)` directly without the CLI — same OTel emission, same persistence.

### Python helper

Distribute via the existing pytest-plugin pattern at `internal/runner/python/pytest/`. Provide `defrost.judge(name).score(input, output)` callable so judge invocation looks the same in pytest fixtures as it does in Go.

## 6. HTTP API surface

In `internal/serve/judges.go`:

| Method + path | Returns |
|---|---|
| `GET /api/judges` | `{judges: [{name, model, score_schema, updated_at}]}` |
| `GET /api/judges/{name}?ref=<git-ref>` | The full YAML config at HEAD or ref |
| `POST /api/judges/{name}/run` | Body: `{trace_id, span_id?}` or `{dataset, version?}`. Kicks off async run; returns `{run_id}`. Emits server-sent progress on `/api/loading/progress` (reuse the existing bus from `internal/serve/progress.go`). |
| `PUT /api/judges/{name}` | Create / update. Body: judge YAML as JSON. |

Judge runs from the dashboard need API keys. The dashboard uses the same client-side-key model as spec [`#7 playground`](./2026-05-05-playground.md): keys are stored in browser localStorage, sent per-request, and the server holds them only for the request duration. **Server logs never contain keys.** Verify via test.

Cache-Control:
- `GET /api/judges` → `public, max-age=60`
- `GET /api/judges/{name}` → `public, max-age=60` (mutable on edit)

## 7. Web UI surface

| File | Role |
|---|---|
| `web/src/pages/Judges.tsx` (new) | List judges at `/judges`. New / edit. Editor uses Monaco or shadcn `Textarea` with YAML highlighting (defer Monaco if footprint is too large; spec #6 prompt management likely justifies Monaco regardless — coordinate). |
| `web/src/pages/JudgeDetail.tsx` (new) | Detail view at `/judges/:name`. Edit YAML; "Run against dataset" picker; score history sparkline. |
| `web/src/components/JudgeBadge.tsx` (new) | Renders a judge score on a span row in the trace tree (spec #1) when a judge has scored it. Subtle: small numeric badge, tooltip shows reason. |
| `web/src/components/RunJudgeButton.tsx` (new) | Mounted in `SpanDetail` (spec #1). One-click "score this span with judge X". |

App.tsx routes: `/judges`, `/judges/:name`.

The trace tree from spec #1 needs to know about judge spans so it can show the `JudgeBadge`. Add a span-attribute filter: any span with `gen_ai.evaluation.*` is a judge span. Render judge spans inline with the rest of the tree but visually distinguished (different row icon).

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/eval/inspect/`, `internal/eval/promptfoo/` | Adapter pattern. Copy the structure: a package that wraps a third-party tool, emits OTel, registers an `Adapter` in `internal/runner/adapter.go`. The judge is conceptually similar — a "tool" that runs over outputs. |
| `internal/runner/exec.go` | Where the OTel pipe-through is set up. The judge runs inside this same pipe so its spans land in the recorded trace. |
| `internal/persist/persist.go` | Reuse the same clone-mutate-push helpers that suppressions and (per spec #3) datasets use. Add a `WriteJudge(name string, yaml []byte, msg string) error`. |
| `internal/cli/cli.go`, `wire.go` | Existing kong + DI patterns. Add the `Judge` command. |
| `internal/runner/python/pytest/` | Existing pytest plugin scaffold. Extend with `defrost.judge(...)`. |
| `internal/serve/progress.go` | Reuse the SSE bus for async judge run progress in the dashboard. |
| `web/src/components/SpanDetail.tsx` (spec #1) | Mount `RunJudgeButton` here. |

**New files only:**

- `internal/eval/judge/` — `judge.go`, `load.go`, `score.go`, `score_test.go`, `load_test.go`.
- `internal/persist/judge.go` (+ `judge_test.go`) — git-write helpers.
- `internal/serve/judges.go` (+ `judges_test.go`)
- `internal/cli/judge.go` (+ `judge_test.go`)
- `internal/runner/python/pytest/judge.py` — Python-side adapter (calls into the Go binary, or directly into the LLM API; see open questions).
- `web/src/pages/Judges.tsx`, `JudgeDetail.tsx`, plus tests.
- `web/src/components/JudgeBadge.tsx`, `RunJudgeButton.tsx`, plus tests.
- Public docs as listed in §3.

## 9. OTel emission

Judge runs emit one span per scoring call:

```text
span.name        = "defrost.judge.score"
span.kind        = INTERNAL
span.attributes  = {
  "gen_ai.evaluation.judge_name":   <name>,
  "gen_ai.evaluation.judge_version": <git SHA of YAML>,
  "gen_ai.evaluation.score":         <float>,
  "gen_ai.evaluation.label":         <string, optional>,
  "gen_ai.evaluation.reason":        <string>,
  "gen_ai.evaluation.target_trace_id": <hex>,
  "gen_ai.evaluation.target_span_id":  <hex, optional>,
  "gen_ai.evaluation.input":         <serialized input>,
  "gen_ai.evaluation.output":        <serialized output>,
  // standard gen_ai.* for the judge-LLM call itself:
  "gen_ai.request.model":            <judge model>,
  "gen_ai.usage.input_tokens":       ...,
  "gen_ai.usage.output_tokens":      ...,
  "gen_ai.response.model":           ...,
}
span.events      = [...]  // any tool-use events from the judge call
```

Cost / token columns from spec #2 light up automatically because the same `gen_ai.usage.*` attributes are present.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/eval/judge/score_test.go` | Given a stub `Provider` that returns a known JSON response, `Judge.Score` returns the expected `Score`. Malformed responses (non-JSON, missing keys) → typed errors. Templating: `{{input}}` / `{{output}}` substitution is correct, including with empty strings, with newlines, with quotes. |
| `internal/eval/judge/load_test.go` | YAML round-trip; missing required fields → typed errors with field paths; unknown fields → warning, not error (forward compat). |
| `internal/persist/judge_test.go` | `WriteJudge` produces a commit on the data branch with the right path. Concurrent writers retry on non-FF. |
| `internal/serve/judges_test.go` | All endpoints return expected shapes. POST `/api/judges/{name}/run` accepts both trace-mode and dataset-mode bodies. `Authorization` headers / API keys are not logged (assert via a captured logger). |
| `internal/cli/judge_test.go` | `judge create` → file exists. `judge run --trace` against a fixture trace produces an OTel span with the expected attributes. `judge run --dataset` invokes once per row. |
| `web/src/pages/Judges.test.tsx`, `JudgeDetail.test.tsx`, plus the two new components. | Render + fetch + POST flows. |

## 11. Acceptance criteria

- A user can define a judge in YAML, run it against a captured trace from the CLI, and see a span with `gen_ai.evaluation.score` recorded under the same trace pipeline.
- A user can run the same judge against every row in a dataset; results show up as scored rows in the dataset detail view.
- The trace tree shows `JudgeBadge` next to spans that have been scored.
- The dashboard never persists API keys server-side. Verified via test that POSTed keys do not appear in any log line.
- Pytest users can call `defrost.judge("name").score(...)` and get the same OTel emission as the CLI.
- Public docs `docs/guides/llm-as-judge.md`, `docs/concepts/evaluators.md`, `docs/reference/cli/judge.md` exist and match the implementation.
- All test cases in §10 pass.

## 12. Open questions / decisions

- **Squatting on `gen_ai.evaluation.*`.** This namespace isn't ratified upstream. Recommendation: ship under it, file an OTel SemConv RFC, and migrate to whatever upstream lands. Document the RFC link from the docs page.
- **Reproducibility.** LLM judges are nondeterministic at `temperature > 0`. Recommendation: default `temperature: 0.0`. Document that scores can vary across re-runs and that defrost stores the raw response for audit.
- **Score schema scaling.** v1 supports scalar and categorical. What about multi-axis (e.g., `{helpfulness: 4, correctness: 5}`)? Recommendation: defer. When demand surfaces, multi-axis is a YAML schema change + new attribute scheme.
- **Where does the Python `defrost.judge` impl live?** Two options: (a) shell out to `defrost judge run`, or (b) re-implement the judge in Python. Recommendation: (a) for v1 — keeps the source of truth in Go, avoids drift. Document the perf cost of `exec` per row; fold (b) in only if it becomes a bottleneck.
- **Caching judge results.** A judge over the same `(input, output, judge SHA)` always returns the same answer at temp=0. Recommendation: don't cache in v1; rerunning is cheap relative to the LLM cost which the user already paid. Cache later if needed.
- **Score storage on the data branch.** Currently scores live only inside trace files. Should we materialize a flat `judges/<name>/scores.jsonl` for easier diff? Recommendation: no — DuckDB hydration over trace spans is fast enough, and a denormalized file would get out of sync. Reconsider only if `git diff` ergonomics suffer.
