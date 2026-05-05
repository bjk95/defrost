# Spec #1 — Trace tree UI

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation.
**Phase:** 1 — close the trace UX gap.

## 1. Goal

Replace the heatmap-only dashboard's blind spot — the inability to inspect a single trace in detail — with a waterfall + nested-tree view of every span in a recorded run, plus a side panel showing one span's full attributes, events, and links. A user lands on `/trace/<trace-id>`, sees the full execution shape of the run, expands/collapses subtrees, and clicks any span to open the detail panel.

## 2. Why this matters competitively

The trace tree is the single highest-leverage UX investment defrost can make. It's the screen LangSmith and Langfuse demos open with — the one users screenshot when they explain why they bought it. Without it, defrost gets dismissed as "the test-history thing" rather than recognised as an LLM observability tool. With it, the rest of Phase 1 (cost/token surfacing, spec [`#2`](./2026-05-05-cost-token-surfacing.md)) hangs off the same screen at near-zero marginal cost.

## 3. Public docs to write first

Per the docs-as-spec rule in `CLAUDE.md`, the public docs page that describes this feature MUST be drafted in the same PR that ships it. Edit:

- **New page:** `docs/guides/inspecting-a-trace.md` — a 1-screen guide. Sections: "Open the dashboard", "Click a run", "Read the waterfall", "Span detail panel", "Search within a trace". Use a screenshot of a real run captured from `defrost exec go test ./...` against this repo (so the screenshot lives at `docs/guides/_assets/trace-tree.png`). Stay neutral — do **not** name LangSmith / Langfuse.
- **Edit:** `docs/guides/dashboard.md` — add a paragraph linking to the new "Inspecting a trace" guide and update any heatmap-only language that implies the dashboard has only one view.
- **Edit:** `docs/reference/serve-api.md` — document the two new endpoints from §6.
- **Edit:** `docs/index.md` — if the home page lists features, add "Inspect any run as a trace tree."

No new concept page is needed. Spans / traces / OTel are already covered in `docs/concepts/otel-as-ingestion.md`.

## 4. Storage layout

**No change.** This spec is read-only against the existing on-disk schema.

The trace tree reads from existing trace files at `<repo>/.defrost/traces/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst` (canonical OTLP `ExportTraceServiceRequest` protobufs, zstd-compressed). The OTLP message already carries parent/child relationships via `span.parent_span_id`; the tree is reconstructed from that, no schema migration needed.

The DuckDB cache at `<repo>/.defrost/cache.duckdb` is hydrated incrementally by `internal/query/duckdb/`. This spec adds a new column projection (span hierarchy fields) but no new tables — the existing span row already has `span_id`, `parent_span_id`, `name`, `start_time_unix_nano`, `end_time_unix_nano`, `status_code`, and the attribute JSON. Confirm column names against `internal/query/duckdb/` schema before extending.

## 5. CLI surface

**No new CLI commands.** A trace tree is a dashboard concern. If a CLI user wants to dump a trace as JSON, the existing `defrost history` already covers run-level inspection; spawning a span-level CLI is out of scope.

If a future `defrost trace show <trace-id> --json` is wanted, defer to a follow-up spec.

## 6. HTTP API surface

Two new endpoints under `internal/serve/server.go`. Both follow the existing handler conventions in that file (closure over `Deps`, `Cache-Control`, structured DTOs, `writeJSONError` for failure paths).

### `GET /api/trace/{trace-id}/tree`

Returns the nested span hierarchy for one trace.

**Path params:** `trace-id` is the 16-hex-character lower-case ID used by the storage layer (see [`docs/reference/storage-layout.md`](../../reference/storage-layout.md)).

**Query params:** none in v1. (Future: `?expand=root` etc.)

**Response (200):**

```json
{
  "trace_id": "0123456789abcdef",
  "run_id": "<run-id>",
  "root": {
    "span_id": "...",
    "parent_span_id": null,
    "name": "defrost.run",
    "kind": "internal",
    "start_unix_nano": 1746432000000000000,
    "end_unix_nano":   1746432001234000000,
    "duration_ms": 1234,
    "status": "ok",
    "attributes": { "service.name": "defrost", ... },
    "events": [ { "name": "...", "time_unix_nano": ..., "attributes": {...} } ],
    "children": [ { ...same shape... } ]
  }
}
```

The response is one nested tree, root-first. If a trace has multiple roots (orphan spans), wrap them under a synthetic `root: { name: "<trace>", children: [...] }` node and set `synthetic: true`. The dashboard renders the synthetic node collapsed by default.

**Cache-Control:** `public, max-age=86400`. Trace data is immutable after persist (same reasoning as `/api/test/{tid}/run/{rid}`).

**Error paths:**
- 404 `{"error":"unknown trace"}` if no file matches.
- 500 with the wrapped error otherwise.

### `GET /api/trace/{trace-id}/raw`

Streams the raw OTLP protobuf for the trace as `application/x-protobuf`. Useful for debugging and for power users who want to pipe into `otel-cli`.

**Cache-Control:** `public, max-age=86400`.

**Body:** the un-zstd'd OTLP `ExportTraceServiceRequest` bytes. (Decompress on the server; we don't push zstd to browsers.)

### Querier extension

Mirror the existing `query.QuerierWithLookup` pattern (defined in `internal/query/iface.go`). Add:

```go
// QuerierWithTraceTree extends Querier with the per-trace nested
// hierarchy the /api/trace/<trace-id>/tree endpoint needs. Kept off
// the base interface so a hosted ClickHouse impl can lazily wire this
// up — same pattern as QuerierWithLookup.
type QuerierWithTraceTree interface {
    Querier
    LookupTrace(traceID string) (TraceTree, bool, error)
}

type TraceTree struct {
    TraceID string
    RunID   string
    Root    *Span
}

type Span struct {
    SpanID        string
    ParentSpanID  string
    Name          string
    Kind          string
    StartUnixNano int64
    EndUnixNano   int64
    DurationMs    int64
    Status        string
    Attributes    map[string]any
    Events        []SpanEvent
    Children      []*Span
}

type SpanEvent struct {
    Name          string
    TimeUnixNano  int64
    Attributes    map[string]any
}
```

Implement `LookupTrace` in `internal/query/duckdb/` by SELECTing all spans for a given `trace_id` from the existing span row table, then reconstructing parent/child links in Go.

If the `Querier` does not implement `QuerierWithTraceTree`, the handler returns 500 with `"trace tree not supported by this querier"` (mirrors the existing pattern at `internal/serve/server.go:174–177`).

## 7. Web UI surface

Two new components under `web/src/components/`, one new page under `web/src/pages/`, one route added to `web/src/App.tsx`.

### Components

| Component | File | Role |
|---|---|---|
| `TraceTree` | `web/src/components/TraceTree.tsx` | Waterfall + nested-tree rendering. Indented rows with collapsible carets, duration bars sized by span duration, hover highlight. Selects a span on click → updates URL `?span=<span-id>`. |
| `SpanDetail` | `web/src/components/SpanDetail.tsx` | Right-side panel (shadcn `Sheet`, same primitive as `RunDetailSheet`). Tabs: "Attributes" (key-value table), "Events" (timeline list), "Raw" (JSON dump). Reads `?span=<span-id>` from the URL. |
| `TraceSearch` | `web/src/components/TraceSearch.tsx` | Inline search box at the top of the tree. Filters span rows whose `name` or any attribute value matches. Highlights matches. Pure client-side (no extra API call). |

### Page

`web/src/pages/TracePage.tsx`:

- Route: `/trace/:traceId` (added to `App.tsx`).
- Reads `traceId` from the URL.
- Uses `useQuery({ queryKey: ['trace', traceId], queryFn: () => getTrace(traceId), staleTime: Infinity })` — same staleTime-Infinity pattern as `RunDetailSheet` since trace data is immutable.
- Renders `<TraceSearch /> <TraceTree /> <SpanDetail />` in a three-column-ish layout (search + tree on the left, detail panel on the right via shadcn `Sheet`).
- 404 state: matches the existing `RunDetailSheet` 404 message style.

### `web/src/api.ts` additions

```ts
export async function getTrace(traceId: string): Promise<TraceTreeResponse> { ... }
```

Plus the `TraceTreeResponse` and `Span` types in `web/src/types.ts`.

### Heatmap link-out

In `web/src/components/RunCell.tsx` (the existing per-cell click target), keep the existing `?run=&test=` behavior, but **add** a "View trace" affordance in `RunDetailSheet` — a button that links to `/trace/<trace-id-derived-from-run-id>`. The `trace_id` for a run is `sha256(run-id)[:16]` per `docs/reference/storage-layout.md`; surface that in the run-detail JSON so the SPA doesn't recompute hashes.

Update `runDetailDTO` in `internal/serve/server.go` to include `TraceID string` field, populated by reading the run's root span's trace_id (already available via the existing entry/run lookup).

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/serve/server.go` | Add the two new handlers in `New(deps Deps)` following the existing closure-over-deps + `writeJSONError` patterns. |
| `internal/query/iface.go` | Add `QuerierWithTraceTree` extension and `TraceTree`/`Span`/`SpanEvent` types beside the existing `QuerierWithLookup` / `TestEntry`. |
| `internal/query/duckdb/` | Implement `LookupTrace`. The schema is already span-shaped from existing OTLP ingest. |
| `internal/otlp/sink.go` | Existing OTLP-to-on-disk path. **No change.** Verify span-level fields needed for the tree are already persisted (they are, since the heatmap reads them). |
| `internal/persist/cache.go` | Existing cache hydration. **No change.** The new query method reads from already-hydrated tables. |
| `web/src/components/RunDetailSheet.tsx` | Pattern to copy for `SpanDetail`. Both are shadcn `Sheet`-based right-side panels. |
| `web/src/components/Grid.tsx` | Pattern to copy for `TraceTree` data-loading and rendering. |
| `web/src/api.ts` | Add `getTrace` next to the existing `getTests` / `getTestRun` typed wrappers. |
| `web/src/queryClient.ts` | Reuse the existing `QueryClient` config. No changes. |
| `docs/concepts/otel-as-ingestion.md` | Existing concept page; reference from the new guide. |
| `docs/reference/storage-layout.md` | Source of truth for the `trace_id = sha256(run-id)[:16]` rule. |

**New files only:**

- `internal/query/duckdb/trace_tree.go` (+ `trace_tree_test.go`)
- `internal/serve/trace.go` (+ `trace_test.go`) — keeps `server.go` from ballooning; mirror how `drop.go`, `metrics.go`, `progress.go` already split out of `server.go`.
- `web/src/components/TraceTree.tsx` (+ `TraceTree.test.tsx`)
- `web/src/components/SpanDetail.tsx` (+ `SpanDetail.test.tsx`)
- `web/src/components/TraceSearch.tsx` (+ `TraceSearch.test.tsx`)
- `web/src/pages/TracePage.tsx` (+ `TracePage.test.tsx`)
- `docs/guides/inspecting-a-trace.md` (+ `docs/guides/_assets/trace-tree.png`)

## 9. OTel emission

This feature is read-only. It does **not** emit new telemetry. It surfaces existing OTel data already captured by `internal/otlp/`.

Spans displayed in the tree carry whatever attributes the producer emitted. The tree renders all attributes verbatim — no semantic-convention parsing in this spec. Cost/token surfacing on top of those attributes is spec [`#2`](./2026-05-05-cost-token-surfacing.md), and lands on the same screen at near-zero marginal cost.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/query/duckdb/trace_tree_test.go` | Given a fixture trace with N spans (including orphan / multi-root cases), `LookupTrace` returns the expected tree shape. Parent/child wiring is correct. Synthetic root is added iff multi-root. Returns `ok=false` for unknown trace ID. |
| `internal/serve/trace_test.go` | `httptest.NewServer(New(Deps{...}))` with a stub `Querier` impl satisfying `QuerierWithTraceTree`. Asserts 200 + `Cache-Control: public, max-age=86400` on a known trace; 404 on unknown; 500 with a clean message when the Querier doesn't implement the extension. Asserts `/api/trace/<id>/raw` returns `application/x-protobuf` and the bytes round-trip through `ptraceotlp.UnmarshalProto`. |
| `web/src/components/TraceTree.test.tsx` | Renders nested rows; collapse/expand toggles children visibility; click row updates the URL via `useSearchParams`; `synthetic: true` root renders collapsed; performance smoke (500-span fixture renders in <500ms in jsdom — use `performance.now()` and assert; this is not a hard guarantee in jsdom but flags pathological regressions). |
| `web/src/components/SpanDetail.test.tsx` | With `?span=<id>`, reads from query data and renders the three tabs. Empty events list shows empty state. Unknown `?span=` shows inline error, not crash. |
| `web/src/components/TraceSearch.test.tsx` | Typed query filters rows by name and by any attribute value; clearing query restores all rows. Highlights match in the rendered span name. |
| `web/src/pages/TracePage.test.tsx` | Route renders against a mocked `getTrace`. 404 path renders a friendly empty state with a link back to the heatmap. |
| `web/src/api.test.ts` | Existing file; add `getTrace` cases (parses shape; throws on non-2xx). |

Reuse the existing `web/src/test-utils.tsx` `renderWithProviders` helper for all SPA tests. Reuse the existing `httptest`-based server-test pattern from `internal/serve/progress_test.go`.

## 11. Acceptance criteria

The feature is done when **all** are true:

- A user with at least one trace stored can navigate to `/trace/<trace-id>` and see the full span tree with durations.
- Clicking any span row opens the detail panel; the panel shows all attributes, all events, and the raw JSON tab.
- The detail panel is deep-linkable: visiting `/trace/<id>?span=<span-id>` opens with that span selected.
- Search filters span rows by name and attribute value, with matches highlighted.
- A 500-span trace renders the initial view in <500 ms on a developer's laptop (Chrome, M-series Mac or equivalent). Asserted via the SPA test as a smoke check; manually re-verified during PR review.
- Multi-root traces render under a synthetic root, collapsed by default, labelled clearly.
- An unknown `<trace-id>` shows an empty state with a link back to the heatmap, **not** a crash or a 500.
- The "View trace" button on `RunDetailSheet` links to the correct trace.
- Public docs `docs/guides/inspecting-a-trace.md` exists with a screenshot, and `docs/reference/serve-api.md` documents the new endpoints. Neither names LangSmith or Langfuse.
- All test cases in §10 pass.

## 12. Open questions / decisions

These should be flagged at PR time, not silently resolved by the sub-agent.

- **Heatmap vs trace tree as default landing.** Recommendation: heatmap stays the default at `/`. Trace tree is per-run drill-down at `/trace/:id`. Don't replace the heatmap until usage data shows it's dead.
- **Span row width when durations are extreme.** A trace where one span dominates (>99% of total) makes other bars invisible. Recommendation: switch to log scale via a toggle in the search bar. Spec the toggle but ship linear-scale default.
- **Performance ceiling.** A trace with >5,000 spans may exceed the 500ms budget. Recommendation: in v1, render the first 1,000 sorted depth-first and show "+ N more spans (show all)". Defer virtualization (`react-virtuoso`) to a follow-up if needed.
- **Color coding on duration bars.** Use status-derived color (green/red/yellow) or duration-derived color (heatmap)? Recommendation: status-derived, because trace tree's job is debugging not micro-benchmarking. Reuse the existing status palette from `web/src/components/StatusBadge.tsx`.
- **Trace ID format in the URL.** 16-hex is short and shareable, but OTLP traces in the wild often use 32-hex. Today our writer truncates to 16. Recommendation: continue to use the 16-hex on-disk ID as the URL token; if a user pastes a 32-hex W3C trace ID, the route handler can substring it before lookup. Document this in the new guide.
