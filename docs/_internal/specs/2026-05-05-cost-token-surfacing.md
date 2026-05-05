# Spec #2 — Cost & token surfacing

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation. Hangs off spec [`#1 trace-tree-ui`](./2026-05-05-trace-tree-ui.md).
**Phase:** 1 — close the trace UX gap.

## 1. Goal

Surface the OTel `gen_ai.usage.*` and `gen_ai.response.*` data already captured in existing traces. Every span row in the trace tree shows tokens-in / tokens-out / cost. The home page gains a daily-cost chart spanning the last 30 days. No on-disk schema change — this is pure read-side surfacing of data we already collect.

## 2. Why this matters competitively

LangSmith's marketing leans hard on "we automatically capture token counts and calculate cost." So does Langfuse's. Without a cost column on the trace view, users assume defrost doesn't track it — even though we do, in standard `gen_ai.usage.*` attributes. This spec closes the perception gap with one PR's worth of UI work.

It also opens the door to cost regression detection in CI, which becomes a defrost-only feature once history-travels-with-code combines with cost-per-trace data: `git diff main..feature` can flag PRs that increase cost.

## 3. Public docs to write first

- **New page:** `docs/guides/tracking-llm-cost.md` — a 1-screen guide. Sections: "How costs are computed (from `gen_ai.*` attributes)", "Configuring the price map", "Reading cost in the dashboard", "Cost regressions in PRs". Stay neutral.
- **Edit:** `docs/reference/otel-ingestion.md` — add a section listing the `gen_ai.*` attributes we read and what each one is used for. Cite the OTel semantic conventions URL.
- **Edit:** `docs/reference/serve-api.md` — document the new `/api/cost/summary` endpoint from §6.
- **Edit:** `docs/guides/inspecting-a-trace.md` (created in spec #1) — add a paragraph on the cost/token columns.

## 4. Storage layout

**No change.** Cost and token data is already in the OTLP traces under span attributes. The DuckDB cache already projects span attributes; this spec adds a query pattern, not a schema change.

A new file ships in the binary, **not** on the data branch:

- `internal/cost/prices.go` — a static price map keyed by model name. Compiled in. Users override via a config file (see §5 / §12).

## 5. CLI surface

One new command:

- `defrost cost summary [--from <date>] [--to <date>] [--by <model|run|day>] [--json]` — print cost rollup. Defaults: last 30 days, grouped by day, human-readable.

Flag for overriding the bundled price map:

- Existing `defrost serve` and `defrost exec` both gain `--prices <path>` (a JSON or YAML file). Default: bundled. The flag lives in `internal/cli/runtime.go` so both commands pick it up uniformly.

Price file format:

```yaml
# prices.yaml — overrides the bundled price map
"openai/gpt-5":
  input_per_mtok: 5.00
  output_per_mtok: 15.00
"anthropic/claude-opus-4-7":
  input_per_mtok: 15.00
  output_per_mtok: 75.00
```

Bundled defaults live in `internal/cost/prices.go` as a Go map literal. Bump them when models ship; treat as a regular code change.

## 6. HTTP API surface

### Extend `GET /api/trace/{trace-id}/tree` (from spec #1)

Each span in the response gains three optional fields:

```json
{
  "tokens_in": 1234,
  "tokens_out": 567,
  "cost_usd": 0.018
}
```

If any of the three can't be computed (no `gen_ai.usage.*` attrs, no model match in the price map), omit it from the JSON. The dashboard shows `—`.

### New: `GET /api/cost/summary`

```
GET /api/cost/summary?from=<RFC3339>&to=<RFC3339>&by=<day|run|model>
```

Returns:

```json
{
  "from": "2026-04-05T00:00:00Z",
  "to":   "2026-05-05T00:00:00Z",
  "buckets": [
    { "key": "2026-04-30", "cost_usd": 12.34, "tokens_in": 1234567, "tokens_out": 345678 }
  ],
  "total": { "cost_usd": 234.56, "tokens_in": ..., "tokens_out": ... },
  "estimated": true
}
```

`estimated: true` whenever any contributing span had no provider-emitted `gen_ai.usage.cost` attr and we computed cost from the price map. The dashboard renders an "estimate" badge when this is set.

**Cache-Control:** `public, max-age=60` (matches `/api/tests`).

### Querier extension

```go
type QuerierWithCost interface {
    Querier
    CostByDay(from, to time.Time) ([]CostBucket, error)
    CostByRun(from, to time.Time) ([]CostBucket, error)
    CostByModel(from, to time.Time) ([]CostBucket, error)
}

type CostBucket struct {
    Key        string  // date-stamp / run-id / model name depending on grouping
    CostUSD    float64
    TokensIn   int64
    TokensOut  int64
    Estimated  bool
}
```

Implement in `internal/query/duckdb/cost.go`. Reuse the existing span scan; aggregate via DuckDB SQL.

## 7. Web UI surface

- **`web/src/components/TraceTree.tsx` (from spec #1):** add three columns on each span row — `Tokens in`, `Tokens out`, `Cost`. Right-aligned, monospace. Empty cell if not applicable.
- **`web/src/components/SpanDetail.tsx` (from spec #1):** add a "Cost" mini-section above the attributes table when cost data is present. Shows breakdown: tokens, model, computed-vs-emitted source, $ amount.
- **`web/src/components/CostChart.tsx` (new):** a recharts `LineChart` of daily cost across the last 30 days. Hover tooltip shows day + cost + token breakdown. "Estimate" badge if any bucket is estimated.
- **`web/src/pages/Home.tsx` (or wherever the heatmap lives — see App.tsx):** add `<CostChart />` above the heatmap.
- **Existing `RunCell` / `RunDetailSheet`:** add a small `$N.NN` cost badge per run cell when cost data exists. Keep it subtle so the green/red status stays the dominant signal.

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/query/duckdb/` | Add `cost.go` next to existing query files. Reuse the span row table and attribute JSON projection. |
| `internal/serve/server.go` | Extend the `/api/trace/.../tree` handler to enrich span DTOs with cost fields. Add `/api/cost/summary` handler in a new `internal/serve/cost.go`. |
| `internal/cli/runtime.go` | Existing flag-merging point for `defrost exec` / `defrost serve`. Add `--prices`. |
| `internal/cli/cli.go` | Add the `Cost` subcommand struct (kong) following the existing `Suppress` / `Drop` patterns. |
| `web/src/components/TraceTree.tsx`, `SpanDetail.tsx` | Created in spec #1; extend in this spec. |
| Existing `web/src/components/` chart code (e.g., `DurationSparkline.tsx`) | Reuse the recharts + shadcn chart wrapper pattern for `CostChart`. |
| `docs/concepts/otel-as-ingestion.md` | Cite — explains why OTel attributes are the source of truth. |

**New files only:**

- `internal/cost/prices.go` (+ `prices_test.go`)
- `internal/cost/compute.go` — `func ComputeCost(model string, in, out int64) (float64, bool /*estimated*/)`. Pure function; no I/O.
- `internal/serve/cost.go` (+ `cost_test.go`)
- `internal/query/duckdb/cost.go` (+ `cost_test.go`)
- `internal/cli/cost.go` (+ `cost_test.go`) — wires the new `defrost cost summary` command.
- `web/src/components/CostChart.tsx` (+ `CostChart.test.tsx`)
- `docs/guides/tracking-llm-cost.md`

## 9. OTel emission

This feature **consumes** OTel attributes, doesn't emit them. We read:

| Attribute | Used for |
|---|---|
| `gen_ai.usage.input_tokens` | Tokens-in column |
| `gen_ai.usage.output_tokens` | Tokens-out column |
| `gen_ai.usage.total_tokens` | Fallback if `input` / `output` are absent (split via 50/50 if unknown — flag as estimate) |
| `gen_ai.response.model` | Price-map lookup key |
| `gen_ai.request.model` | Fallback when response.model is absent |
| `gen_ai.usage.cost` | If present, use directly; mark `estimated: false` |

Reference: [OpenTelemetry Semantic Conventions for Generative AI](https://opentelemetry.io/docs/specs/semconv/gen-ai/) — link from the new doc page.

If a span has none of these, it's not an LLM call (could be a tool, a retrieval step, etc.) — show empty cells, not `0`.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/cost/compute_test.go` | Pure-function unit tests for the price-map lookup. Covers: known model, unknown model (returns `(0, false, ErrUnknownModel)` or similar — pick a sentinel), zero tokens, large counts, custom override file. |
| `internal/cost/prices_test.go` | Bundled price map parses cleanly. Loading a YAML override merges correctly (override wins). |
| `internal/query/duckdb/cost_test.go` | Given a fixture trace with three LLM spans (mixed providers, mixed token counts), `CostByDay` / `CostByRun` / `CostByModel` produce the expected aggregates. `Estimated` is true iff at least one span used the price map. |
| `internal/serve/cost_test.go` | `/api/cost/summary?from&to&by=day` returns the expected JSON shape and 200 + `Cache-Control: public, max-age=60`. Invalid `from`/`to` → 400 with a clear error. |
| `internal/serve/trace_test.go` (extended from spec #1) | `/api/trace/.../tree` includes `tokens_in`, `tokens_out`, `cost_usd` on LLM spans and omits them on non-LLM spans. |
| `web/src/components/CostChart.test.tsx` | Renders given a 30-day sample; hover tooltip shape correct; "Estimate" badge appears when any bucket is estimated. |
| `web/src/components/TraceTree.test.tsx` (extended from spec #1) | Token / cost columns render with values and with `—` for non-LLM spans. |

## 11. Acceptance criteria

- Every trace tree row that represents an LLM call shows `tokens_in`, `tokens_out`, and `$ cost`. Rows for non-LLM spans show `—` (empty cell, not zero).
- The home page shows a daily-cost line chart for the last 30 days. Hover reveals the per-day breakdown.
- An "Estimate" badge appears anywhere a cost was computed from the price map rather than emitted by the producer.
- Unknown models render `—` with a console warning (no error toast). The user can pass `--prices` to override.
- `defrost cost summary` prints a human-readable rollup. `--json` prints JSON identical to `/api/cost/summary`.
- The bundled price map covers, at minimum, today's defaults: gpt-5, gpt-5-mini, claude-opus-4-7, claude-sonnet-4-6, claude-haiku-4-5, gemini-2.5-pro, gemini-2.5-flash. Document refresh cadence in the new guide.
- Public docs `docs/guides/tracking-llm-cost.md` exists and matches the implemented behaviour.
- All test cases in §10 pass.

## 12. Open questions / decisions

- **Ship a default price map or refuse to estimate?** Recommendation: **ship default, mark as estimate.** The cost gap is a perception problem; refusing to estimate would re-open it. Public docs make the estimation explicit.
- **Per-token vs per-call pricing.** Some models (e.g., image gen) bill per request. Out of scope for v1 — flag in the new doc page that defrost only handles per-token text models. Add image/audio in a follow-up spec when the demand surfaces.
- **Currency.** USD only in v1. Add `--currency` later if needed.
- **Cost regressions in PRs.** Mentioned in §2 as a follow-up. Specced separately when prioritised — don't bloat this spec.
- **Where does `--prices` config land on disk for `defrost serve`?** Recommendation: `~/.config/defrost/prices.yaml` if the flag is unset and the file exists. Otherwise bundled defaults. Spec the precedence: `--prices` flag > `~/.config/defrost/prices.yaml` > bundled.
