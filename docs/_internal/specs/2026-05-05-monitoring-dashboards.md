# Spec #10 — Monitoring dashboards

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation. Builds on [`#1 trace-tree-ui`](./2026-05-05-trace-tree-ui.md), [`#2 cost-token-surfacing`](./2026-05-05-cost-token-surfacing.md).
**Phase:** 5 — feedback + observability polish.

## 1. Goal

Time-series production dashboards: latency (p50 / p95 / p99), error rate, throughput, and cost over time. Tiles on the home page; full charts on a dedicated `/monitoring` route. All charts read from existing trace data through the `query.Querier` seam — no new ingestion.

## 2. Why this matters competitively

Both LangSmith and Langfuse ship rich monitoring dashboards as one of their headline marketing screens. Without them, defrost gets "great for tests, weak for prod" feedback — even though the same captured trace data already supports prod monitoring. This spec closes the perception gap.

defrost's angle is honest scale: monitoring queries hit DuckDB on disk, which is fast for ~GB-scale history. Past that, hosted-tier ClickHouse takes over (the `query.Querier` interface was designed for this — see parent spec §7).

## 3. Public docs to write first

- **New page:** `docs/guides/production-monitoring.md` — "Latency, errors, and cost over time." Sections: "What's tracked", "Reading the charts", "Drilling into a spike". Stay neutral.
- **Edit:** `docs/reference/serve-api.md` — document `/api/timeseries`.

## 4. Storage layout

**No change.** All charts read from existing span data via DuckDB.

## 5. CLI surface

- `defrost monitor [--metric latency|errors|throughput|cost] [--from <date>] [--to <date>] [--interval <bucket>] [--json]` — print a timeseries to stdout. Useful for piping into terminal charting tools or for shell-based alerting in lieu of building alerts into defrost (see §12).

## 6. HTTP API surface

In `internal/serve/timeseries.go`:

`GET /api/timeseries?metric=<name>&from=<RFC3339>&to=<RFC3339>&interval=<bucket>&group_by=<dim>`

Where:

- `metric` is one of: `latency_p50`, `latency_p95`, `latency_p99`, `error_rate`, `throughput`, `cost`.
- `interval` is `1m`, `5m`, `15m`, `1h`, `6h`, `1d`. Default depends on the from/to range (auto-pick to keep ~100 buckets).
- `group_by` is optional: `model`, `session.user_id`, `service.name`. When set, returns one series per group.

Response:

```json
{
  "metric": "latency_p95",
  "from": "2026-04-05T00:00:00Z",
  "to":   "2026-05-05T00:00:00Z",
  "interval": "1h",
  "series": [
    {
      "key": "anthropic/claude-opus-4-7",
      "buckets": [
        { "ts": "2026-04-05T00:00:00Z", "value": 1234.5 }
      ]
    }
  ]
}
```

Cache-Control: `public, max-age=60`.

### Querier extension

```go
type QuerierWithTimeseries interface {
    Querier
    Timeseries(req TimeseriesRequest) (TimeseriesResult, error)
}
```

Implementation in `internal/query/duckdb/timeseries.go`. SQL over span durations and statuses with time-bucketing. `latency_p95` uses DuckDB's `quantile_disc(...)`.

## 7. Web UI surface

| File | Role |
|---|---|
| `web/src/pages/Monitoring.tsx` (new) | `/monitoring`. Multiple charts (latency, errors, throughput, cost) with shared time-range picker. |
| `web/src/components/TimeseriesChart.tsx` (new) | Generic line / area chart wrapper. Reuses recharts via shadcn `<Chart>`. |
| `web/src/components/TimeRangePicker.tsx` (new) | Shared date-range control (last 1h / 24h / 7d / 30d / custom). |
| `web/src/pages/Home.tsx` (or wherever the heatmap lives) | Add 4 sparkline tiles above the heatmap (latency p95, error rate, throughput, cost). |

Auto-refresh: tiles refetch every 60s when the page is visible. Use TanStack Query's `refetchInterval`.

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/query/duckdb/` | Add `timeseries.go`. Reuse the existing span / metric tables. |
| `internal/serve/server.go` | Wire endpoint; impl in `internal/serve/timeseries.go`. |
| `internal/cost/compute.go` (spec #2) | Compute cost when the metric is `cost`. |
| `web/src/components/DurationSparkline.tsx` (existing) | Pattern to copy for `TimeseriesChart`. |
| `web/src/components/CostChart.tsx` (spec #2) | Reuse for the `/monitoring` cost panel. |
| `web/src/queryClient.ts` | Existing TanStack Query config; reuse. |

**New files only:**

- `internal/query/duckdb/timeseries.go` (+ test)
- `internal/serve/timeseries.go` (+ test)
- `internal/cli/monitor.go` (+ test)
- `web/src/pages/Monitoring.tsx` (+ test)
- `web/src/components/TimeseriesChart.tsx`, `TimeRangePicker.tsx` plus tests.
- `docs/guides/production-monitoring.md`

## 9. OTel emission

This feature is read-only. No telemetry emitted.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/query/duckdb/timeseries_test.go` | Given a fixture with N traces over a known time range, all six metrics produce the expected buckets. `interval` auto-selection picks reasonable values. `group_by` produces multiple series. |
| `internal/serve/timeseries_test.go` | Endpoint shape correct; bad params (unknown metric, invalid date) → 400 with clear error. |
| `internal/cli/monitor_test.go` | Default metric is `latency_p95`. `--json` output is parseable. |
| `web/src/components/TimeseriesChart.test.tsx`, `TimeRangePicker.test.tsx` | Rendering + interaction. |
| `web/src/pages/Monitoring.test.tsx` | Multiple charts render; auto-refetch fires when interval elapses. |

## 11. Acceptance criteria

- The home page shows four sparkline tiles (latency p95, error rate, throughput, cost) for the last 24h.
- `/monitoring` shows the four charts with a shared time-range picker (1h / 24h / 7d / 30d / custom).
- Charts auto-refresh every 60s while visible; pause when the tab is backgrounded.
- `defrost monitor --metric latency_p95 --from <date> --to <date> --json` emits a stable JSON shape suitable for piping into shell-based alerting.
- All charts populated correctly from existing trace data — no new data sources required.
- Public docs at `docs/guides/production-monitoring.md` exist.
- All test cases in §10 pass.

## 12. Open questions / decisions

- **Alerting.** Out of scope for v1. Recommendation: explicitly defer. The CLI's `--json` output is the official integration point — users wire it into their existing alerting (Prometheus alertmanager, PagerDuty, etc.). This keeps defrost from reinventing alert routing.
- **Custom metrics.** Some users will want to chart `gen_ai.usage.input_tokens` directly, or a custom span attribute. Recommendation: ship the six fixed metrics in v1; add a `metric=custom&attribute=<name>&aggregation=<sum|avg|p95>` mode in a follow-up if demand surfaces.
- **Group-by cardinality blowup.** Grouping by `session.user_id` over a 30-day window can produce thousands of series. Recommendation: cap series count at 50 by default with a "show top N by total" rollup; flag in docs.
- **DuckDB query performance at scale.** A 1d window over 100k traces should be sub-second; 1y over 10M traces will not be. Recommendation: document the comfort zone in the new guide. When users exceed it, that's the hosted-tier conversation.
- **Time-zone handling.** Recommendation: API in UTC, dashboard renders in browser local time. Document the convention.
