# Spec #5 — Pairwise comparison view

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation. Depends on [`#1 trace-tree-ui`](./2026-05-05-trace-tree-ui.md), [`#2 cost-token-surfacing`](./2026-05-05-cost-token-surfacing.md), [`#4 llm-as-judge`](./2026-05-05-llm-as-judge.md).
**Phase:** 3 — evals as first-class.

## 1. Goal

A side-by-side view of two runs (A vs B) — comparing prompts, models, or commits — at `/compare/:a/:b`. Two trace trees rendered in parallel with aligned spans highlighted, divergent spans flagged, and a summary header showing Δ cost, Δ latency, and Δ judge score.

## 2. Why this matters competitively

Pairwise comparison is the screen developers open when evaluating "did the new prompt help?" — the exact question both LangSmith and Langfuse pitch their product on. Without it, any A/B claim about a prompt or model swap requires the user to flip between two browser tabs and eyeball the difference.

defrost's structural advantage: **comparing two runs is `git diff` of two trace files.** Once we render it well, this is also the screen that makes "PR-diffable evals" tangible — a CI bot can post `/compare/<main-run>/<feature-run>` URLs as PR comments.

## 3. Public docs to write first

- **New page:** `docs/guides/comparing-runs.md` — "A/B two runs side-by-side." Sections: "Open the comparison view", "Read the alignment", "Δ cost / latency / score", "Linking comparisons in PR comments".
- **Edit:** `docs/reference/serve-api.md` — document `/api/runs/compare`.
- **Edit:** `docs/guides/inspecting-a-trace.md` (from spec #1) — link to the new comparison page.

## 4. Storage layout

**No change.** Comparison is purely a read-side view over two existing trace files.

## 5. CLI surface

One new command, optional but useful for CI / scripting:

- `defrost compare <run-a> <run-b> [--json]` — print a comparison summary (Δ cost, Δ latency, Δ judge score, # aligned vs unaligned spans). `--json` for machine consumption (e.g., a PR-comment bot).

The command is sugar over `GET /api/runs/compare`. Same data, no new logic.

## 6. HTTP API surface

In `internal/serve/compare.go`:

`GET /api/runs/compare?a=<run-id>&b=<run-id>`:

```json
{
  "a": { "run_id": "...", "trace_id": "...", "commit": "...", "branch": "...", "ts": "..." },
  "b": { ...same shape... },
  "summary": {
    "delta_cost_usd": -0.018,
    "delta_latency_ms": +120,
    "delta_judge_scores": {
      "helpfulness-v1": +0.4,
      "correctness-v1": -0.1
    },
    "spans_aligned": 18,
    "spans_only_in_a": 2,
    "spans_only_in_b": 1
  },
  "alignment": [
    { "kind": "match", "a_span_id": "...", "b_span_id": "...", "key": "<input-hash>" },
    { "kind": "only_a", "a_span_id": "..." },
    { "kind": "only_b", "b_span_id": "..." }
  ]
}
```

**Cache-Control:** `public, max-age=86400` (the pair is immutable once both runs are persisted).

### Alignment algorithm

In `internal/serve/compare.go` (or `internal/compare/` if richer logic justifies its own package):

1. Load both trace trees via `query.QuerierWithTraceTree.LookupTrace` (spec #1).
2. Compute an alignment key for each span: `sha256(span.name + canonical_json(input_attr))[:16]` where `input_attr` is the first present of `gen_ai.request.input`, `gen_ai.prompt`, `gen_ai.input.messages`. Spans without LLM-input attributes use `sha256(span.name + parent_alignment_key)`.
3. Match A→B greedily by alignment key, breaking ties by depth-first order. Unmatched spans become `only_a` / `only_b`.
4. Compute deltas from already-projected attributes (cost from spec #2, judge scores from spec #4).

This is intentionally simple. When trace structures are very different, expect many unaligned spans. The UI surfaces this honestly rather than fabricating false matches.

## 7. Web UI surface

| File | Role |
|---|---|
| `web/src/pages/Compare.tsx` (new) | Route `/compare/:a/:b`. Renders summary header + two `TraceTree` instances side-by-side. |
| `web/src/components/CompareSummary.tsx` (new) | Header card. Shows Δ cost, Δ latency, Δ judge score, alignment stats. Color: green for "B is better", red for "B is worse" — heuristic per metric (lower cost = better, lower latency = better, higher judge score = better). |
| `web/src/components/TraceTree.tsx` (extended from spec #1) | Accept an optional `alignment` prop. When provided, color-code rows: matched (default), only-this-side (highlighted), and on hover in pane A, scroll to matched row in pane B. |

App.tsx route: `/compare/:a/:b`.

UX: deep-linkable URL. The "Compare with…" button on `RunDetailSheet` opens a picker (most recent N runs by default; search by commit SHA / branch). Picking one navigates to `/compare/<current>/<picked>`.

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/query/duckdb/` (spec #1's `LookupTrace`) | Two calls per comparison. No new query method. |
| `internal/serve/server.go` | Wire the new endpoint. Implementation in `internal/serve/compare.go`. |
| `internal/cost/compute.go` (spec #2) | Sum cost per side. |
| `web/src/components/TraceTree.tsx`, `SpanDetail.tsx` (spec #1) | Reuse directly. |
| `web/src/components/RunDetailSheet.tsx` | Add "Compare with…" button. |

**New files only:**

- `internal/serve/compare.go` (+ `compare_test.go`)
- `internal/compare/` (only if alignment logic grows beyond ~150 lines — start in `internal/serve/compare.go` and split if it bloats)
- `internal/cli/compare.go` (+ `compare_test.go`)
- `web/src/pages/Compare.tsx` (+ test)
- `web/src/components/CompareSummary.tsx` (+ test)
- `docs/guides/comparing-runs.md`

## 9. OTel emission

This feature is read-only. No telemetry emitted.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/serve/compare_test.go` | Given two fixture traces with overlapping and divergent spans, `/api/runs/compare` returns the expected `summary` and `alignment`. Edge case: identical traces → all matched, deltas zero. Edge case: completely disjoint traces → all unaligned. Cache-Control set. |
| `internal/cli/compare_test.go` | `defrost compare a b` prints the human summary; `--json` prints exact JSON shape. |
| `web/src/pages/Compare.test.tsx` | Renders both tree panes; summary card shows correct deltas; matched-row hover triggers paired scroll (jsdom limitation: assert handler called rather than visual scroll). |
| `web/src/components/CompareSummary.test.tsx` | Color heuristics correct. Missing scores rendered as `—`, not zero. |
| `web/src/components/TraceTree.test.tsx` (extended from spec #1) | Alignment prop colors rows correctly. |

## 11. Acceptance criteria

- A user with two captured runs can navigate to `/compare/<a>/<b>` and see both trace trees side-by-side with aligned spans visually paired.
- The summary header shows Δ cost (USD), Δ latency (ms), and one Δ row per judge that scored both runs.
- `defrost compare a b --json` produces a stable, scriptable output suitable for a CI PR-comment bot.
- The "Compare with…" button on a run-detail panel offers a picker; picking navigates to the comparison page.
- Completely disjoint traces render correctly (no false matches), with the alignment stats reporting them as unaligned.
- Public docs `docs/guides/comparing-runs.md` exists. Links from the trace-tree guide work.
- All test cases in §10 pass.

## 12. Open questions / decisions

- **Alignment heuristic when trace structures differ a lot.** Recommendation: ship the simple input-hash heuristic in v1; it correctly handles the most common case (same dataset row, different model). Surface alignment-stats prominently so users know when they're looking at high-divergence pairs.
- **Three-way comparison.** `/compare/a/b/c`? Recommendation: defer. Demand pull this when it lands.
- **PR-comment bot.** Out of scope for this spec, but the JSON shape from `--json` is designed so a follow-up GitHub Action can post `/compare/<base-run>/<head-run>` links. Spec the action separately.
- **Aggregate dataset comparison.** "Compare A vs B over an entire dataset" is genuinely useful but requires aggregating across N row-pairs. Recommendation: spec separately. This spec covers single-run pairs only.
- **What if both run IDs hash to traces on different commits with different code paths?** That's the point — the comparison is honest about divergence. Don't try to "fix" it.
