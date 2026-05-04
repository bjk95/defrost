---
title: 'Storage layout'
---

defrost stores everything on a dedicated git branch (default
`_defrost`). Locally, that branch's worktree lives at
`<repo>/.defrost/`. There's no separate database, no SaaS — the data
branch IS the storage, and `<repo>/.defrost/` is its on-disk view.

## `<repo>/.defrost/` (the data branch worktree)

```text
<your-repo>/.defrost/
├── .git/                worktree's git directory (clone of _defrost)
├── .gitignore           committed; ignores cache.duckdb*, pending/
├── README.md            committed
├── traces/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst    committed
├── metrics/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst   committed
├── logs/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst      committed
├── suppressions.json    committed
├── cache.duckdb         local-only, gitignored on the data branch
└── pending/             local-only, holds runs that failed to push
```

Same model as `.git/` itself: a defrost-managed directory inside the
user's repo. The user's main-repo `.gitignore` adds `/.defrost/` to
keep the worktree out of code commits — defrost auto-adds that line on
first run if it isn't already there.

The `.gitignore` committed at the data branch root keeps per-machine
artefacts (`cache.duckdb`, `cache.duckdb.wal`, `cache.duckdb.tmp`,
`pending/`) out of pushes while letting them share the worktree
directory.

## Run files

For each `defrost exec` invocation that records data, defrost writes
up to three files (one per signal that the run produced):

```text
traces/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst
metrics/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst
logs/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst
```

- `<YYYY>/<MM>/<DD>` is the run start time in **UTC**.
- `<trace-id>` is `sha256(run-id)[:16]` rendered as 16 lower-case hex
  characters, where `run-id` is `<16-hex-of-UnixNano>-<8-hex-of-crypto/rand>`.
  Trace IDs sort by run start time within a millisecond and are
  collision-resistant across parallel jobs.
- `<file>.otlp.pb.zst` is a [zstd](https://facebook.github.io/zstd/)-compressed
  canonical OTLP protobuf message, produced by upstream
  `pdata.{ptraceotlp,pmetricotlp,plogotlp}.MarshalProto` — the same
  serializers an OTel Collector exporter would use. Downstream
  readers (the local DuckDB hydrator, future hosted ClickHouse)
  decode without translation:
  - `traces/...`: one `ExportTraceServiceRequest` containing the
    `defrost.run` root span and one child span per test result.
  - `metrics/...`: one `ExportMetricsServiceRequest` containing every
    metric the child OTel SDK exported during the run.
  - `logs/...`: one `ExportLogsServiceRequest` containing every log
    record the child OTel SDK exported.
- A signal's file is omitted entirely if the run emitted nothing for
  that signal.

Files are written atomically: defrost writes to `<path>.tmp`, fsyncs,
renames onto the final path, and fsyncs the parent directory. A crash
mid-write leaves at most a stray `.tmp` file, never a partial result.

Concurrent writers from parallel CI jobs never collide — each run
produces a unique filename keyed by trace_id. If a push hits a
non-fast-forward race, defrost fetches, rebases, and retries (up to 5
attempts). After that the run is dropped with a visible warning;
`defrost exec` does **not** fail the build over a persist failure.
See [troubleshooting persist failures](../../guides/troubleshooting/persist-failed/).

## Branch initialisation

The first `defrost exec` against a fresh repo creates the data branch
as an orphan branch (no parent). The seed commit contains:

- `README.md` — short pointer back to defrost.
- `.gitignore` — excludes `cache.duckdb*` and `pending/` so per-
  machine artefacts share the worktree without being committed.

## `suppressions.json`

Sits at the data branch root, alongside `traces/`, `metrics/`, and
`logs/`:

```json
{
  "schema": 1,
  "test_ids": [
    "github.com/bjk95/defrost.TestFlaky",
    "tests/test_evals.py::test_quality"
  ]
}
```

- `test_ids` is sorted alphabetically and de-duplicated on every write.
- `schema` is the file format version.
- `defrost suppress add | remove` writes via a temp clone, commits
  with the `defrost[bot]` identity, pushes with conflict-resolution
  (fetch + replay-mutation on non-FF). Same conflict model as run
  writes.

## Run-span resource attributes

Every recorded `ResourceSpans` carries OTel resource attributes
identifying the run:

| Attribute | Source | Notes |
|---|---|---|
| `service.name` | constant `"defrost"` | |
| `defrost.version` | binary build info | |
| `vcs.repository.commit.sha` | `git rev-parse HEAD` | |
| `vcs.repository.ref.name` | `git rev-parse --abbrev-ref HEAD`, falling back to `GITHUB_HEAD_REF` then `GITHUB_REF_NAME` | Source branch even on detached-HEAD CI checkouts. |
| `vcs.repository.change.id` | `GITHUB_PR_NUMBER` | Set only if the env var is present. |
| `vcs.repository.dirty` | `git status --porcelain` non-empty | `true` / `false`. |
| `host.os` / `host.arch` | Go `runtime` | |

## Drop semantics

`defrost drop history` rewrites the data branch as a single orphan
commit containing the keep set (whichever of `traces/` / `metrics/` /
`logs/` was not dropped, plus `suppressions.json`, `README.md`, and
`.gitignore`) and force-pushes with `--force-with-lease` against the
SHA we cloned. If a concurrent writer pushed between our clone and
our push, the lease check fails and we abort cleanly rather than
silently destroying their data.

Old git objects on the remote become unreachable and are eventually
garbage-collected — the data is gone, not just hidden.

After a successful drop, the next `defrost serve` notices the new
remote tip via `ls-remote`, force-resets the persistent worktree at
`<repo>/.defrost/`, drops every materialised row from `cache.duckdb`,
and re-hydrates against the new history.

## Scale guidance

The git-as-storage model is sized for projects with up to a few
gigabytes of run history. Beyond that, push frequency, fetch cost,
and dashboard load times all start to feel the limits of the git
protocol. The mitigations are mostly mechanical (compaction,
windowed fetch) — see the project roadmap. For now, treat ~1–2 GB
of accumulated history as the comfort zone.
