<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-light-128.png">
    <img alt="defrost" src="assets/logo-128.png" width="96" height="96">
  </picture>
</p>

# defrost

> Track AI evals, metrics, and tests with Git as the database.

Every team wants a record of how their evals, benchmarks, and tests have changed
over time. Almost nobody wants to host a database for it. **defrost** records
each run as commits on a `_defrost` branch in the same repo, so the history
travels with the code — no database, no SaaS, no API keys.

## What you get

- **Persisted history** — every test run, eval, and metric is recorded automatically. Clone the repo, get the history.
- **Universal instrumentation** — push evals and metrics from any language using a standard OpenTelemetry SDK. No defrost client library to install.
- **Suppression** — mark known-failing tests as suppressed so red CI goes green without skipping them in source.
- **Local dashboard** — `defrost serve` opens your testing, evals, and metrics dashboard at `http://localhost:6969`.

## Install

```sh
# Full install: includes the dashboard and embedded DuckDB read engine.
go install github.com/bjk95/defrost/cmd/defrost@latest
```

For CI workflows that only need the write path (`exec`, `history`,
`suppress`, `drop`) — no dashboard, no DuckDB cgo, no embedded web
bundle — install the slim `defrost-ci` binary instead. About 1/3 the
size and faster to install:

```sh
go install github.com/bjk95/defrost/cmd/defrost-ci@latest
```

`defrost-ci serve` is a stub that prints the install hint and exits 1,
so a misuse surfaces immediately rather than silently no-op'ing.

## Quickstart

From inside any Git repo with an `origin`:

```sh
# Run your tests through defrost. Results, evals, and metrics are saved automatically.
defrost exec go test ./...

# Inspect a single test's history.
defrost history github.com/you/pkg.TestThing

# Open the dashboard.
defrost serve
```

## Supported runners

| Language | Runner | Invocation |
|---|---|---|
| Go | `go test` | `defrost exec go test ./...` |
| Python | pytest (JUnit XML) | `defrost exec pytest path/` |
| JavaScript / TypeScript | jest | `defrost exec npm test` |

## Pushing evals and metrics

Use your normal OpenTelemetry SDK. `defrost exec` exports the standard OTLP
environment variables to the child process, so a default-configured SDK
exports to defrost with no extra wiring:

```python
# Record an eval score
score_gauge = meter.create_gauge("eval.score")
score_gauge.set(0.87, {"model": "claude-opus-4-7", "suite": "summarization"})

# Record a perf metric
latency = meter.create_histogram("request.latency_ms")
latency.record(elapsed_ms, {"endpoint": "/api/search"})
```

Counters, gauges, sums, and histograms are all captured. Same SDK pattern
works in Go, Node, Rust, Java, and every other OTel-supported language.

## Commands

### `defrost exec <cmd...>`

Runs the test command, captures any evals or metrics emitted during the run,
and records everything. Exit code:

- Normally the child's exit code.
- If every failing test is on the suppression list, the exit is rewritten to `0`.
- If persisting to the `_defrost` branch fails (e.g. push rejected,
  no `origin`), the exit is forced to `1` even when the test command itself
  succeeded — so CI never silently loses data.

### `defrost history <test_id>`

Prints the recorded history for a single test as NDJSON, oldest first.

### `defrost suppress add | remove | list`

Manage the suppression list. When every failing test in a run is suppressed,
defrost rewrites the exit code to 0 — anything outside the list (a new
failure, a build error, a panic) still exits non-zero.

### `defrost drop history`

Defrost is append-only — every run adds one trace and one metrics file to
the data branch. Over months that branch grows. `defrost drop history`
permanently deletes persisted traces and metrics and rewrites the branch
so git can actually reclaim the space.

```sh
# Drop everything. Shows an inventory and asks before touching anything.
defrost drop history

# Drop only one signal.
defrost drop history --traces-only
defrost drop history --metrics-only

# Skip the prompt (CI / scripts).
defrost drop history --yes

# Wipe just the local <repo>/.defrost/ tree (no remote operations).
defrost drop history --dev
```

You'll see:

```
About to drop history on branch _defrost (origin: git@github.com:you/repo.git):
  traces:  142 files, 18.4 MB  (2024-09-12 → 2026-04-29)
  metrics: 142 files,  4.2 MB  (2024-09-12 → 2026-04-29)

This rewrites the branch via orphan commit + force-push and is irreversible.
Preserved: 37 suppressions, README.md.

Type "drop" to confirm:
```

**What's preserved.** `suppressions.json`, `README.md`, the data-
branch `.gitignore`, and whichever signal you didn't drop (e.g.
metrics, when you used `--traces-only`).

**How space gets reclaimed.** Defrost replaces the data branch with a
single new commit containing only the kept files. Old commits become
unreachable and the remote (GitHub, GitLab, …) garbage-collects them over
time. The next `defrost serve` notices the force-push (via a cheap
`ls-remote` against the persistent worktree at `<repo>/.defrost/`),
wipes its local DuckDB cache, and re-hydrates against the new history.

**Concurrency.** If someone else's `defrost exec` lands between the
confirmation and the push, defrost aborts with a clear error rather than
overwriting their data. Re-run `defrost drop history` and the new run
shows up in the inventory.

**Nothing to drop.** If the data branch doesn't exist yet, or there are
zero files for the selectors you chose, defrost prints a one-line
explanation and exits 0 without prompting.

### `defrost serve`

Serves the dashboard at `127.0.0.1:6969` (override with `--port`).

## Storage

Every run is committed to a `_defrost` branch (configurable with
`--data-branch`) in your repo. Clone the repo to get the history. Push the
repo to share it. Concurrent runs from different machines never conflict
because each writer owns a unique filename (one file per run, named by its
OTel `trace_id`).

Defrost keeps a local worktree of the data branch at
`<repo>/.defrost/` — same model as `.git/`, defrost-managed and
excluded from your main repo's commits (the entry is auto-added to
your `.gitignore` on first run):

```
<your-repo>/.defrost/                      ← clone of _defrost
├── .git/                                  worktree's git directory
├── .gitignore                             committed; ignores cache.duckdb
├── README.md                              committed
├── traces/<YYYY>/<MM>/<DD>/*.otlp.pb.zst  committed
├── metrics/<YYYY>/<MM>/<DD>/*.otlp.pb.zst committed
├── logs/<YYYY>/<MM>/<DD>/*.otlp.pb.zst    committed
├── suppressions.json                      committed
└── cache.duckdb                           local-only, gitignored
```

The persistent worktree means `defrost serve` does a single `git fetch`
on subsequent loads instead of re-cloning, and a `git ls-remote` short-
circuit skips even that when no new commits exist.

To experiment without pushing to origin, pass `--no-persist` (don't store
anything) or `--dev` (write locally to `.defrost/` only — same paths as
prod mode, just no push).

If a push fails, defrost prints a clear warning with a link to
[troubleshooting](https://bjk95.github.io/defrost/guides/troubleshooting/persist-failed/)
and **does not** fail the test run — the test command's exit code is
preserved.

When the branch grows large enough to matter, use
[`defrost drop history`](#defrost-drop-history) to rewrite it and reclaim
space. Suppressions, README, and the data-branch `.gitignore` are
preserved.

### When to graduate from git

The git-as-storage model is sized for projects with up to a couple of
gigabytes of run history. Past that, push frequency, fetch cost, and
dashboard load all start hitting the limits of the git protocol. For
this stage, defrost is the right tool. Beyond that, plan to graduate
to a hosted offering — but that's a problem you only encounter once
defrost has been working for you for a while.

### Under the hood

For the curious — you do not need to know any of this to use defrost.

The data branch is shaped like OpenTelemetry. One `defrost exec` invocation
is one trace; the run is the root span; each test is a child span. Evals,
metrics, and structured log records emitted during the run are persisted
as OTel data points and log records. Each file is the canonical
OTLP/Protobuf payload (produced by upstream `pdata` serializers, same as
an OTel Collector exporter would emit), zstd-compressed.

```
_defrost branch
├── traces/<YYYY>/<MM>/<DD>/<trace_id>.otlp.pb.zst    # one ExportTraceServiceRequest per run
├── metrics/<YYYY>/<MM>/<DD>/<trace_id>.otlp.pb.zst   # one ExportMetricsServiceRequest per run
└── logs/<YYYY>/<MM>/<DD>/<trace_id>.otlp.pb.zst      # one ExportLogsServiceRequest per run (when emitted)
```

Defrost runs the upstream `otlpreceiver` from the OpenTelemetry Collector
in library mode during `exec`, so any OTel SDK pointed at
`OTEL_EXPORTER_OTLP_ENDPOINT` works for traces, metrics, AND logs out of
the box. Run-scoped attributes (commit, branch, author, command, OS/arch,
run id, …) live exactly once on each file's `Resource`. The OTel span
`name` is the canonical fully qualified test name — no lossy projections
are stored alongside it.
