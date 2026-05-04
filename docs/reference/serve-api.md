# Serve HTTP API

`defrost serve` exposes a JSON / SSE API alongside the SPA. The dashboard
uses these endpoints; you can also use them directly from scripts.

All endpoints are served on `127.0.0.1:<port>` (default `6969`) — never
on a public interface.

## Read-only

### `GET /api/tests`

Light index of every recorded run and test. Used to render the heatmap
grid. The `Output` field is omitted to keep responses small — fetch
`/api/test/:tid/run/:rid` for full per-run detail.

- **Cache:** `Cache-Control: max-age=60`. The data branch is re-read on
  every request, so changes appear within the cache window.

### `GET /api/test/:tid/run/:rid`

Full detail for one (test, run) pair, including captured output,
duration, and run-level commit metadata.

- **Path params:** `:tid` is the test ID, URL-encoded. `:rid` is the
  trace ID (16 hex chars).
- **Cache:** `Cache-Control: max-age=86400`. A specific (test, run) pair
  is immutable once recorded.

### `GET /api/metrics`

Summary of recorded metrics for the dashboard's metric charts. Returns
metric names, points over time, and aggregated values per run.

### `GET /api/loading/progress`

Server-Sent Events stream emitted while the data branch is being loaded.
Phases include `connect`, `clone`, `spans`, `parse`, `metrics`, `index`,
`ready`, `error`. Each event carries a phase name and a human-readable
message.

## Suppressions

### `GET /api/suppressions`

Returns the current list of suppressed test IDs.

### `POST /api/suppressions`

Adds one or more test IDs to the suppression list. Body:

```json
{ "test_ids": ["github.com/x/p.TestA", "src/foo.test.ts > Foo > x"] }
```

Equivalent to `defrost suppress add`. Idempotent.

### `GET /api/suppressions/:id`

Returns whether a single ID is currently suppressed. `:id` is URL-encoded.

### `DELETE /api/suppressions/:id`

Removes a test ID from the suppression list. Equivalent to
`defrost suppress remove`. Idempotent.

## Drop

### `GET /api/drop/plan`

Dry-run of `defrost drop history`. Returns the count and total size of
files that would be removed under the current settings. No mutation.

### `POST /api/drop`

Executes the drop. Body:

```json
{ "tracesOnly": false, "metricsOnly": false, "confirm": true }
```

`confirm: true` is required — the server refuses the request without
it, mirroring the CLI's confirmation prompt. Equivalent to
`defrost drop history`.

## Static assets

### `GET /*`

Anything that does not match the `/api/` prefix is served from the
embedded SPA bundle. The SPA implements client-side routing, so URLs
like `/runs/<rid>` are served by returning `index.html`.
