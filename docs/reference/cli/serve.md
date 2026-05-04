---
title: '`defrost serve`'
---

Serve a local web dashboard for browsing recorded test history.

## Synopsis

```text
defrost serve [flags]
```

```sh
defrost serve                # http://127.0.0.1:6969
defrost serve --port 8080
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--port` | int | `6969` | Port to bind on `127.0.0.1`. |
| `--repo-dir` | string | `.` | Path to the git repo. |
| `--data-branch` | string | `_defrost` | Branch name to read from. |
| `--dev`, `-d` | bool | `false` | Dev mode: read only from the local `<repo>/.defrost/` tree (no `git fetch`). |

The server binds only to `127.0.0.1` — never to `0.0.0.0`. There is no
flag to expose it on a public interface.

This subcommand is only available on the full `defrost` binary.
`defrost-ci serve` is a stub that prints the install hint and exits
1 — see [installation](../../../guides/quickstart/#1-install).

## How it serves data

Reads happen against an embedded DuckDB instance at
`<repo>/.defrost/cache.duckdb`, hydrated from canonical OTLP files in
`<repo>/.defrost/` (a persistent worktree of the data branch).

Steady-state cost of refreshing the dashboard is one `git ls-remote`
per request:

1. **`git ls-remote origin <branch>`** (~50ms typical). If the SHA
   matches `cache_meta.last_sha` in DuckDB, return immediately —
   nothing to do.
2. Otherwise, fetch + `git reset --hard` against the persistent
   worktree. If the remote was force-rewritten (e.g. by `defrost drop
   history`), the materialised tables get truncated and re-hydrated
   from scratch.
3. Walk new files since the last hydrate, decode the OTLP bytes, and
   `INSERT` into `traces` / `metrics` / `logs`. Files already in
   `hydration_state` are skipped.
4. Update `cache_meta.last_sha` so the next request can short-circuit.

First-time cost (cold cache, full clone): proportional to the size of
the data branch. Subsequent loads are sub-second on unchanged remotes.

## What you get

A single-page app at `http://127.0.0.1:<port>/` showing:

- **Heatmap grid** — rows are test IDs, columns are recent runs, each
  cell coloured by pass / fail / skip / suppressed.
- **Run detail panel** — click a cell to see the recorded output, span
  duration, commit SHA, branch, PR number, and any captured stdout /
  stderr.
- **Metrics charts** — recorded OTel metrics over time, grouped by
  metric name.
- **Suppression management** — view, add, and remove suppressed test IDs.
- **Drop preview** — dry-run what `defrost drop history` would remove
  before running it.

URLs include run / test query parameters (`?run=<rid>&test=<tid>`) so
deep links and the browser back button work.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Clean shutdown (Ctrl+C). |
| `1` | Port already in use, or server failed to start. |

## HTTP API

The same server exposes a JSON API used by the SPA. See
[Serve HTTP API](../../serve-api/) for the full endpoint list, request /
response shapes, and cache headers.
