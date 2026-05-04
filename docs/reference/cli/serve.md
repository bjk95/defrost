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
| `--dev`, `-d` | bool | `false` | Dev mode: read the local scratch dir instead of the data branch. |

The server binds only to `127.0.0.1` — never to `0.0.0.0`. There is no
flag to expose it on a public interface.

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
