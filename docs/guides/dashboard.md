---
title: 'Running the dashboard'
---

`defrost serve` opens a local web UI for browsing recorded test history.

## Start it

```sh
defrost serve
```

Opens `http://127.0.0.1:6969`. Override the port:

```sh
defrost serve --port 8080
```

The server only ever binds to `127.0.0.1`. There is no flag to expose
it on a public interface — if you need to share a dashboard, host it
behind your own reverse proxy or VPN.

## What you can do

- **Heatmap.** Rows are tests, columns are recent runs. Cells are
  coloured pass / fail / skip / suppressed. Pick out flakes by eye —
  they look like dotted columns rather than solid ones.
- **Run detail.** Click any cell to see captured stdout/stderr,
  duration, the commit SHA the run was made against, the branch, and
  the PR number (if recorded).
- **Metrics.** A separate view groups recorded OTel metrics by name and
  plots them over time. Useful for tracking eval scores across commits.
- **Suppression management.** Add or remove suppressions without
  leaving the UI. Equivalent to `defrost suppress add` /
  `defrost suppress remove`.
- **Drop preview.** See the size of `traces/` and `metrics/` and what
  `defrost drop history` would remove before running it.
- **Deep-linkable URLs.** `?run=<rid>&test=<tid>` query parameters
  survive page reloads and the back button.

## Refreshing data

The data branch is re-read on every API request. Cell data has a
60-second cache header (the `/api/tests` endpoint), so a hard browser
reload bypasses it. Run-detail responses (`/api/test/:tid/run/:rid`)
are cached for 24 hours since a specific (test, run) pair is
immutable.

There is no auto-refresh button. If you want to watch CI in
near-real-time, reload the page periodically.

## On a CI host

`defrost serve` is intended for local use. Running it on a long-lived
server is possible (clone the repo, fetch `_defrost`, run the
command), but the server has no auth layer of its own — pair it with
your own access control if you do this. Most teams find that "run it
locally when you need it" is enough.

## Reading data without the dashboard

For programmatic access, the same read endpoints are documented in the
[Serve HTTP API](../../reference/serve-api/), and `defrost history`
gives NDJSON for a single test from the command line.
