# `defrost serve` — Design

**Date:** 2026-04-30
**Status:** Draft, pending implementation

## Purpose

Add a `defrost serve` subcommand that exposes a local web UI for inspecting test history persisted by `defrost exec`. v1 is a heatmap timeline grid — one row per test, one cell per recent run, click a cell to open a side panel with that run's failure output, duration, and commit metadata.

The design optimises for two things: (a) the smallest end-to-end slice that turns the existing `defrost history` data into something a human can scan, and (b) a structure that lets later additions (filters, auto-refresh, multi-language ingestion) slot in without rearchitecting.

## User-facing behavior

The user invokes:

```
defrost serve [--port 6969] [--repo-dir .] [--data-branch _defrost] [--no-remote]
```

The wrapper:

1. Validates flags and resolves the repo / data-branch options (same semantics as `defrost history`).
2. Reads the data branch into memory once via the `persist` package.
3. Starts an `http.Server` bound to `127.0.0.1:<port>` (default 6969).
4. Prints `→ http://localhost:6969` to stdout and blocks until interrupted.

The user opens the URL in a browser. The page renders a heatmap grid of recent test runs. Clicking a cell opens a side panel with that run's failure output and metadata. The selection is reflected in the URL (`?run=<rid>&test=<tid>`) so the panel state is deep-linkable and the back button works.

The server does not auto-refresh. The data branch is read once at startup. To see runs recorded after `defrost serve` started, the user restarts the server (`Ctrl-C` then `defrost serve` again). A refresh button or auto-refresh is an explicit v2 follow-up — see "Out of scope".

The server binds loopback only — there is no auth. If the data branch is empty the server still starts and the SPA shows an empty state.

## High-level flow

```
defrost serve
  │
  ├─ persist.LoadAll(opts)   # reads runs/ + tests/ from data branch
  │     └─ in-memory Dataset
  │
  └─ http.Server :6969
        ├─ GET /api/tests                  → light grid data (no Output)
        ├─ GET /api/test/:tid/run/:rid     → full cell detail (with Output)
        └─ GET /*                          → SPA assets (embedded), with SPA fallback

Browser
  │
  ├─ initial load: GET /api/tests → render <Grid />
  ├─ click cell: push URL ?run=&test= → GET /api/test/:tid/run/:rid → <RunDetailSheet />
  └─ restart `defrost serve` to ingest new runs (v1)
```

Three boundaries:

- **persist** — data branch I/O. Owns the on-disk schema.
- **HTTP API** — Go ↔ browser contract. Two JSON endpoints.
- **SPA** — pure presentation. Fetches, renders, no business logic.

## Components

### Go side

#### `cli.go`

Add a new `Serve` subcommand to the kong CLI struct:

```go
Serve struct {
    Port       int    `name:"port" default:"6969" help:"Port to bind on 127.0.0.1."`
    RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
    DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
    NoRemote   bool   `name:"no-remote" help:"Read from the local repo only — do not consult origin."`
} `cmd:"" help:"Serve a local UI for inspecting test history."`
```

Dispatch in `main.go` follows the existing `case strings.HasPrefix(cmd, "serve")` pattern.

#### `serve.go` (top level)

`HandleServe(opts ServeOpts) int`. Owns the lifecycle:

1. Calls `serve.Load(persist.Options{...})` to assemble the in-memory `Dataset`.
2. Builds the `http.Handler` via `serve.New(dataset, Assets)`.
3. Starts `http.Server` on `127.0.0.1:<port>`. On `listen` error (e.g., port in use), logs `port <N> already in use; pass --port to override` and returns 1.
4. Prints the URL and blocks.

#### `assets.go` (top level)

The `go:embed` directive must live at the repo root because `go:embed` paths cannot contain `..`. This file is the only reason for that constraint.

```go
package main

import "embed"

//go:embed all:web/dist
var Assets embed.FS
```

The `internal/serve` package accepts a `fs.FS` parameter so it remains decoupled from the embed declaration and is independently testable with an in-memory FS.

#### `internal/serve/data.go`

Defines the in-memory `Dataset` and the loader:

```go
type Dataset struct {
    Runs        []persist.RunRecord       // newest first, capped at 50 — these are the grid's columns
    TestEntries map[string][]persist.Entry // testID → entries that ran in one of Runs (with Output retained)
}

func Load(opts persist.Options) (Dataset, error)
```

`Load` calls `persist.LoadAll`, sorts runs by `Timestamp` descending, caps `Runs` to 50, then keeps only entries whose `RunID` is in that capped set. Test name comes from `Entry.TestName`. Bounded memory by construction.

Grid cell semantics: a cell at `(testID, runID)` is *missing* if that test did not run in that run. Missing cells render as a dimmed empty square — distinct from green/red/yellow.

#### `internal/serve/server.go`

```go
func New(d Dataset, assets fs.FS) http.Handler
```

Wires three handlers on a `http.ServeMux`:

- `GET /api/tests` → returns:
  ```json
  {
    "runs": [
      {"run_id": "...", "ts": "2026-04-30T12:34:56Z", "commit": "abc1234", "branch": "main"}
    ],
    "tests": [
      {
        "test_id": "...",
        "test_name": "pkg/auth.TestLogin",
        "cells": [
          {"run_id": "...", "status": "pass", "duration_ms": 42}
        ]
      }
    ]
  }
  ```
  `cells` does *not* contain entries for runs in which the test did not execute — the SPA joins by `run_id` against `runs` and renders missing intersections as empty cells. `Output` is omitted.
- `GET /api/test/{tid}/run/{rid}` → returns `{test: Entry, run: RunRecord}` (full, with `Output`). Returns 404 with `{"error":"unknown test or run"}` if either ID is unknown.
- `GET /*` → serves SPA assets from `assets`. Unknown paths fall back to `index.html` so React Router handles routing.

The handler is a pure function of `(Dataset, fs.FS)` — no global state, easy to test with `httptest`.

#### `internal/persist/persist.go`

Add one new public function:

```go
func LoadAll(opts Options) ([]RunRecord, map[string][]Entry, error)
```

Reads every `runs/<run_id>.json` and every `tests/<test_id>.ndjson` from the data branch. Returns runs unsorted and entries grouped by test ID. The existing `History()` stays as-is — it remains the implementation backing `defrost history`.

`ErrNoOrigin` is returned with the same semantics as `History()`.

### Web side

#### Stack

Vite + React 18 + TypeScript + React Router v6 + Tailwind + shadcn/ui + recharts (via shadcn's `<Chart>` wrapper) + Vitest + @testing-library/react + jsdom.

#### Routes

- `/` — grid view. Optional `?run=<rid>&test=<tid>` query params select a cell and open the side panel. Deep-linkable; back button closes the panel.

#### Components (`web/src/components/`)

| Component | Role | shadcn primitives |
|---|---|---|
| `App` | Router + global `<Toaster />` | — |
| `Grid` | Fetches `/api/tests`, renders one `TestRow` per test | — |
| `TestRow` | Test name on left + `RunCell` strip on right | `Tooltip` on cell hover (timestamp + status) |
| `RunCell` | Colored 14×14 square. `onClick` updates URL search params | — |
| `RunDetailSheet` | Reads `?run=&test=` from URL, fetches detail, renders side panel | `Sheet` |
| `DurationSparkline` | Line chart of selected test's duration across recent runs; selected run highlighted | shadcn `ChartContainer` + recharts `LineChart` |
| `StatusBadge` | green/red/yellow status pill | `Badge` |

#### Status → color

- `pass` → green-500
- `fail` → red-500
- `skip` → neutral-300
- not run in this run → neutral-100 (empty cell, slightly dimmed border)

#### Data fetching

Plain `fetch` in `useEffect` for v1. Typed wrappers in `web/src/api.ts` (`getTests()`, `getTestRun(tid, rid)`) return `Promise<T>` and surface errors as thrown exceptions for the caller to render via shadcn `Toaster`.

No react-query, no SWR. Adding either later is a one-component swap.

## Data flow

1. **Server startup:** `HandleServe` calls `serve.Load`, which calls `persist.LoadAll`. The full data branch is read once. Runs are sorted newest-first and capped at 50; per-test entries are filtered to only those whose `RunID` is in that capped set.
2. **Initial page load:** SPA mounts, `Grid` fetches `/api/tests`. Server serializes the in-memory `Dataset` minus per-entry `Output` fields. Response is one JSON document.
3. **Click cell:** `RunCell.onClick` updates `?run=&test=` via `useSearchParams`. `RunDetailSheet` observes the change, fetches `/api/test/:tid/run/:rid`, renders the panel.
4. **Seeing new runs:** the `Dataset` is loaded once at server startup. Page reload re-renders the same data — no new runs appear without restarting `defrost serve`. This keeps v1 server simple (no file-watching, no cache invalidation). A refresh endpoint is an explicit v2 follow-up.

## Repo layout after this work

```
defrost/
├── main.go
├── cli.go                 (+ Serve subcommand)
├── serve.go               (new)
├── exec.go
├── history.go
├── assets.go              (new: //go:embed all:web/dist)
├── go.mod
├── Makefile               (new: `make build` runs npm build then go build)
├── internal/
│   ├── serve/             (new package)
│   │   ├── server.go
│   │   ├── server_test.go
│   │   ├── data.go
│   │   └── data_test.go
│   ├── persist/           (+ LoadAll, + persist_test.go cases)
│   ├── resultcollector/
│   ├── golang/
│   └── models/
└── web/                   (new)
    ├── package.json
    ├── vite.config.ts
    ├── tailwind.config.ts
    ├── tsconfig.json
    ├── index.html
    ├── src/
    │   ├── main.tsx
    │   ├── App.tsx
    │   ├── api.ts
    │   ├── lib/utils.ts
    │   └── components/
    │       ├── Grid.tsx           (+ Grid.test.tsx)
    │       ├── TestRow.tsx
    │       ├── RunCell.tsx        (+ RunCell.test.tsx)
    │       ├── RunDetailSheet.tsx (+ RunDetailSheet.test.tsx)
    │       ├── DurationSparkline.tsx (+ DurationSparkline.test.tsx)
    │       ├── StatusBadge.tsx
    │       └── ui/                (shadcn-generated: sheet, badge, tooltip, button, toaster, chart)
    └── dist/              (committed; rebuilt by `npm run build`)
```

## Build pipeline

- **Dev:** `cd web && npm run dev` runs Vite on its own port (5173 by default). `vite.config.ts` proxies `/api/*` to `http://127.0.0.1:6969`. The Go server runs separately via `go run . serve`.
- **Production / install:** `cd web && npm run build` populates `web/dist/`. The directory is committed. `go install github.com/bjk95/defrost@latest` then embeds the prebuilt assets — no Node required for end users.
- **Convenience:** `Makefile` target `build` chains `npm install`, `npm run build`, then `go build`. `test` target runs `go test ./...` and `npm test`.
- **Staleness:** documented in README that `web/dist/` must be rebuilt before pushing changes to `web/src/`. No CI guard in v1; easy follow-up.

`.gitignore` additions:

- `web/node_modules/`
- `.superpowers/`
- (Intentionally not `web/dist/` — that's committed.)

## Error handling

| Scenario | Behavior |
|---|---|
| Port in use | Log `port <N> already in use; pass --port to override` and exit 1. No port scanning. |
| Empty data branch | Server starts; `/api/tests` returns `{"runs": [], "tests": []}`; SPA shows empty state: *"No test runs yet — run `defrost exec go test ./...` to record some."* |
| `persist.ErrNoOrigin` | Same hint as `defrost history`: configure origin or pass `--no-remote`. Exit 1 before binding. |
| Unknown `tid` or `rid` in `/api/test/:tid/run/:rid` | 404 with `{"error":"unknown test or run"}`. SPA renders inline error in the sheet; grid stays functional. |
| SPA fetch failure | `fetch` rejection → toast via shadcn `Toaster` with a retry button. |

## Testing

Per the user's "verification is mandatory; minimal, public-interface only" rule:

| Test file | Asserts |
|---|---|
| `internal/persist/persist_test.go` | New cases for `LoadAll`: seeded fixture branch → expected runs and per-test entries. |
| `internal/serve/data_test.go` | `Load` produces a `Dataset` with the expected shape from a stub `persist.LoadAll` result; sort + cap correctness. |
| `internal/serve/server_test.go` | `httptest.NewServer(New(...))` — `/api/tests` returns the expected JSON shape and 200; `/api/test/:tid/run/:rid` returns 200 on a known pair and 404 on an unknown one. SPA fallback returns `index.html` for unknown paths. |
| `web/src/components/Grid.test.tsx` | Mocks `/api/tests`, asserts rows + cells render with correct status classes. |
| `web/src/components/RunCell.test.tsx` | Status → CSS class mapping; click updates URL search params (uses `MemoryRouter`). |
| `web/src/components/RunDetailSheet.test.tsx` | When URL has `?run=&test=`, mocks `/api/test/:tid/run/:rid`, asserts output and sparkline render. |
| `web/src/components/DurationSparkline.test.tsx` | Smoke: renders given sample data without throwing. |
| `web/src/api.test.ts` | Fetch wrappers parse JSON shape; surface errors. |

The SPA tests exist deliberately — once `defrost exec` gains TypeScript/Vitest support, defrost will display its own test history. Dogfooding hook.

## Out of scope for v1

Documented so they're not accidentally built and so they're easy follow-ups:

- Auto-refresh (polling, SSE, websockets).
- Filters / search / sort.
- Pagination — fixed cap of last 50 runs.
- Run-diff view.
- Per-test detail page with full history.
- Auth — `127.0.0.1` only.
- Multi-repo / project picker.
- Auto-open browser.
- Dark mode toggle.
- TypeScript / pytest / jest result ingestion.
