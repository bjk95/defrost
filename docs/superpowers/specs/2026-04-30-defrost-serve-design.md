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
2. Starts an `http.Server` bound to `127.0.0.1:<port>` (default 6969). No upfront data load — the data branch is read lazily on the first API request.
3. Prints `→ http://localhost:6969` to stdout and blocks until interrupted.

The user opens the URL in a browser. The page renders a heatmap grid of recent test runs. Clicking a cell opens a side panel with that run's failure output and metadata. The selection is reflected in the URL (`?run=<rid>&test=<tid>`) so the panel state is deep-linkable and the back button works.

**Refresh model:** there is no in-page refresh button. To see new runs recorded by other `defrost exec` invocations, the user reloads the browser tab. Each `/api/tests` request re-reads the data branch — git ops are real work, so the response sets `Cache-Control: public, max-age=60` and the browser throttles repeated fetches. (User-initiated hard reload bypasses cache and forces a fresh git read; that's the intended escape hatch.)

The server binds loopback only — there is no auth. If the data branch is empty the server still starts and the SPA shows an empty state.

## High-level flow

```
defrost serve
  │
  └─ http.Server :6969
        ├─ GET /api/tests                  → calls persist.LoadAll → light grid data (no Output)
        │                                    Cache-Control: public, max-age=60
        ├─ GET /api/test/:tid/run/:rid     → calls persist.LoadAll → full cell detail (with Output)
        │                                    Cache-Control: public, max-age=86400 (the (tid,rid) pair is immutable)
        └─ GET /*                          → SPA assets (embedded), with SPA fallback

Browser
  │
  ├─ initial load: GET /api/tests → render <Grid />
  ├─ click cell: push URL ?run=&test= → GET /api/test/:tid/run/:rid → <RunDetailSheet />
  └─ tab reload re-fetches /api/tests (subject to browser cache throttle)
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

1. Builds the `http.Handler` via `serve.New(persistOpts, Assets)`. The handler closes over `persistOpts` and re-reads the data branch on each API request (no startup load, no in-memory cache).
2. Starts `http.Server` on `127.0.0.1:<port>`. On `listen` error (e.g., port in use), logs `port <N> already in use; pass --port to override` and returns 1.
3. Prints the URL and blocks.

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

Defines the request-scoped `Dataset` and the loader:

```go
type Dataset struct {
    Runs        []persist.RunRecord       // newest first, capped at 50 — these are the grid's columns
    TestEntries map[string][]persist.Entry // testID → entries that ran in one of Runs (with Output retained)
}

func Load(opts persist.Options) (Dataset, error)
```

`Load` is called per-request by the HTTP handlers. It calls `persist.LoadAll`, sorts runs by `Timestamp` descending, caps `Runs` to 50, then keeps only entries whose `RunID` is in that capped set. Test name comes from `Entry.TestName`. There is no in-memory cache; throttling lives in HTTP `Cache-Control` headers and the browser HTTP cache.

Grid cell semantics: a cell at `(testID, runID)` is *missing* if that test did not run in that run. Missing cells render as a dimmed empty square — distinct from green/red/yellow.

#### `internal/serve/server.go`

```go
func New(opts persist.Options, assets fs.FS) http.Handler
```

The handler closes over `opts` and calls `Load(opts)` on each API request. Wires three handlers on a `http.ServeMux`:

- `GET /api/tests` → calls `Load(opts)`. Sets `Cache-Control: public, max-age=60`. Returns:
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
- `GET /api/test/{tid}/run/{rid}` → calls `Load(opts)`. Sets `Cache-Control: public, max-age=86400` (a fixed `(tid, rid)` pair is immutable — once a test has run, that result doesn't change). Returns `{test: Entry, run: RunRecord}` (full, with `Output`). Returns 404 with `{"error":"unknown test or run"}` if either ID is unknown.
- `GET /*` → serves SPA assets from `assets`. Unknown paths fall back to `index.html` so React Router handles routing.

The handler depends only on `(opts, fs.FS)` — opts is value-typed, no global state. For tests, `Load` is replaced via a test-only seam (a package-level `loaderFn` variable defaulting to `Load`, swappable in `_test.go`).

#### `internal/persist/persist.go`

Add one new public function:

```go
func LoadAll(opts Options) ([]RunRecord, map[string][]Entry, error)
```

Reads every `runs/<run_id>.json` and every `tests/<test_id>.ndjson` from the data branch. Returns runs unsorted and entries grouped by test ID. The existing `History()` stays as-is — it remains the implementation backing `defrost history`.

`ErrNoOrigin` is returned with the same semantics as `History()`.

### Web side

#### Stack

Vite + React 18 + TypeScript + React Router v6 + Tailwind + shadcn/ui + recharts (via shadcn's `<Chart>` wrapper) + TanStack Query v5 (`@tanstack/react-query`) + Vitest + @testing-library/react + jsdom.

#### Routes

- `/` — grid view. Optional `?run=<rid>&test=<tid>` query params select a cell and open the side panel. Deep-linkable; back button closes the panel.

#### Components (`web/src/components/`)

| Component | Role | shadcn primitives |
|---|---|---|
| `App` | Wraps router in `<QueryClientProvider>`; mounts global `<Toaster />` | — |
| `Grid` | `useQuery` against `/api/tests`, renders one `TestRow` per test | — |
| `TestRow` | Test name on left + `RunCell` strip on right | `Tooltip` on cell hover (timestamp + status) |
| `RunCell` | Colored 14×14 square. `onClick` updates URL search params | — |
| `RunDetailSheet` | Reads `?run=&test=` from URL, `useQuery` for detail, renders side panel | `Sheet` |
| `DurationSparkline` | Line chart of selected test's duration across recent runs; selected run highlighted | shadcn `ChartContainer` + recharts `LineChart` |
| `StatusBadge` | green/red/yellow status pill | `Badge` |

#### Status → color

- `pass` → green-500
- `fail` → red-500
- `skip` → neutral-300
- not run in this run → neutral-100 (empty cell, slightly dimmed border)

#### Data fetching

TanStack Query v5. A single `QueryClient` lives at the root of `App`, configured with conservative defaults that complement the HTTP `Cache-Control` story:

```ts
new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60_000,         // matches /api/tests Cache-Control: max-age=60
      gcTime: 5 * 60_000,
      retry: 1,
      refetchOnWindowFocus: false, // explicit user action only — no surprise refetches
    },
  },
})
```

Typed fetch wrappers in `web/src/api.ts` (`getTests()`, `getTestRun(tid, rid)`) return `Promise<T>` and throw on non-2xx responses. Components consume them via `useQuery` with explicit query keys:

- `Grid` → `useQuery({ queryKey: ['tests'], queryFn: getTests })`
- `RunDetailSheet` → `useQuery({ queryKey: ['test', tid, 'run', rid], queryFn: () => getTestRun(tid, rid), enabled: Boolean(tid && rid), staleTime: Infinity })` — `(tid, rid)` is immutable, so once fetched it never goes stale within a session.

`isError` states render an inline message in-place (toast via shadcn `Toaster` for transient fetch failures, retry button calls `refetch()`).

This is a small step up from plain `fetch` (one library, ~13kB gzipped), but pays for itself: automatic dedupe of concurrent fetches (e.g., two cells clicked in quick succession), no manual loading-state plumbing, and a clean `refetch()` if a future "Refresh" button is added.

## Data flow

1. **Server startup:** `HandleServe` builds the handler and listens. No data is loaded yet.
2. **Initial page load:** SPA mounts, `Grid` fetches `/api/tests`. Handler calls `serve.Load` → `persist.LoadAll` → sorts and caps → serializes (minus `Output`). Server attaches `Cache-Control: public, max-age=60`.
3. **Click cell:** `RunCell.onClick` updates `?run=&test=` via `useSearchParams`. `RunDetailSheet` observes the change; its `useQuery` (with `staleTime: Infinity`) issues `GET /api/test/:tid/run/:rid` if not already cached in TanStack Query's store. Handler calls `Load`, looks up the requested entry, returns it with `Cache-Control: public, max-age=86400`. Repeated clicks on the same cell within a session never re-fetch (TanStack Query cache hit); after navigation, browser HTTP cache also avoids re-hitting the server.
4. **Seeing new runs:** user reloads the browser tab. If the browser's cached `/api/tests` is fresher than 60s, it serves from cache (no server hit, no git op). Otherwise it re-requests, the handler re-loads from the data branch, and the grid re-renders with whatever is now there. There is no in-process cache, no server-side TTL, no auto-refresh.

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
├── .github/
│   └── workflows/
│       └── web-dist.yml   (new: auto-rebuild web/dist on web/ source changes)
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
    │   ├── queryClient.ts     (TanStack Query client config)
    │   ├── test-utils.tsx     (renderWithProviders helper)
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

### CI: rebuild `web/dist/` automatically

A GitHub Action keeps the committed `web/dist/` in sync with `web/src/` (and friends) so contributors don't have to remember to rebuild before pushing.

**Workflow file:** `.github/workflows/web-dist.yml`

**Trigger:**

```yaml
on:
  push:
    paths:
      - 'web/src/**'
      - 'web/index.html'
      - 'web/package.json'
      - 'web/package-lock.json'
      - 'web/vite.config.ts'
      - 'web/tailwind.config.ts'
      - 'web/tsconfig.json'
```

Note: `web/dist/**` is *not* in the path filter — that prevents the workflow from re-triggering itself when it pushes the rebuilt dist.

**Steps:**

1. `actions/checkout@v4` (with `token: ${{ secrets.GITHUB_TOKEN }}`).
2. `actions/setup-node@v4` (Node 20, cache `web/package-lock.json`).
3. `cd web && npm ci`.
4. `cd web && npm run build`.
5. If `git diff --quiet web/dist`, exit 0 (nothing to commit).
6. Otherwise, configure git as `github-actions[bot]`, `git add web/dist`, commit with message `chore: rebuild web/dist [skip ci]`, push to the same branch.

**Permissions:** the workflow needs `permissions: contents: write` so the default `GITHUB_TOKEN` can push. Pushes made via `GITHUB_TOKEN` do not trigger further workflow runs by default — additional safety against loops on top of the path filter.

**Branch protection:** if `main` requires PRs, the action will fail to push directly to `main`. The action only runs on PR branches in that case (where direct push is allowed). Document this in the README.

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
| `internal/serve/server_test.go` | `httptest.NewServer(New(...))` with the `loaderFn` seam stubbed — `/api/tests` returns the expected JSON shape and 200 with `Cache-Control: public, max-age=60`; `/api/test/:tid/run/:rid` returns 200 with `Cache-Control: public, max-age=86400` on a known pair and 404 on an unknown one. SPA fallback returns `index.html` for unknown paths. |
| `web/src/components/Grid.test.tsx` | Wraps in `QueryClientProvider`; stubs `getTests`; asserts rows + cells render with correct status classes. |
| `web/src/components/RunCell.test.tsx` | Status → CSS class mapping; click updates URL search params (uses `MemoryRouter`). |
| `web/src/components/RunDetailSheet.test.tsx` | Wraps in `QueryClientProvider`; stubs `getTestRun`; with URL `?run=&test=`, asserts output and sparkline render. |
| `web/src/components/DurationSparkline.test.tsx` | Smoke: renders given sample data without throwing. |
| `web/src/api.test.ts` | Fetch wrappers parse JSON shape; throw on non-2xx. |

A small `web/src/test-utils.tsx` exports a `renderWithProviders(ui)` helper that wraps in a fresh `QueryClient` (with `retry: false` and `gcTime: Infinity` for deterministic tests) and `MemoryRouter`. All component tests use it.

The SPA tests exist deliberately — once `defrost exec` gains TypeScript/Vitest support, defrost will display its own test history. Dogfooding hook.

## Out of scope for v1

Documented so they're not accidentally built and so they're easy follow-ups:

- Auto-refresh without user action (polling on a timer, SSE, websockets). Browser-tab reload is the v1 refresh mechanism.
- Filters / search / sort.
- Pagination — fixed cap of last 50 runs.
- Run-diff view.
- Per-test detail page with full history.
- Auth — `127.0.0.1` only.
- Multi-repo / project picker.
- Auto-open browser.
- Dark mode toggle.
- TypeScript / pytest / jest result ingestion.
