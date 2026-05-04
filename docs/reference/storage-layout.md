---
title: 'Storage layout'
---

defrost stores state in two places: a remote **data branch** in your
repo (default name `_defrost`) for the run history, and a local
**`<repo>/.defrost/`** tree in your working copy for committed config
plus the read-side cache.

## `<repo>/.defrost/` (in your working tree)

Created on first defrost invocation:

```text
<your-repo>/.defrost/
├── .gitignore           # auto-generated; ignores data/ and cache.duckdb
├── data/                # persistent worktree of the data branch (gitignored)
├── cache.duckdb         # DuckDB read cache (gitignored)
└── suppressions.json    # committed: shared list of suppressed test IDs
```

- `.gitignore` — auto-written on first init, idempotent (only created
  when missing, so user additions are preserved). Contents:

  ```gitignore
  /data
  /cache.duckdb
  /cache.duckdb.wal
  ```

- `data/` — In normal mode it's a clone of the `_defrost` branch.
  Subsequent `defrost serve` calls fetch incrementally rather than
  re-cloning. In `--dev` mode it's a plain directory of OTLP files
  (no `.git` inside; no remote operations). Same path in both modes.

- `cache.duckdb` — DuckDB file populated incrementally from `data/`.
  Holds materialised tables (`traces`, `metrics`, `logs`,
  `hydration_state`, `cache_meta`) the dashboard queries against.
  `cache_meta.last_sha` is the SHA we hydrated against; it's compared
  against `git ls-remote` on the next read so unchanged remotes don't
  trigger any work.

- `suppressions.json` — committed, version-controlled with the rest of
  your code. See [the suppression concept page](../../concepts/suppression/)
  for why this lives here rather than on the data branch.

## Data branch initialisation

The first `defrost exec` against a fresh repo creates the data branch
as an orphan branch (no parent). The seed commit contains a short
`README.md` pointing back to defrost. There is no `.gitattributes` —
the per-run write path produces unique filenames (one file per run,
named by trace_id), so concurrent writers never contend on shared
paths.

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
  serializers an OTel Collector exporter would use. That means
  downstream readers (the local DuckDB hydrator, future hosted
  ClickHouse) decode without translation:
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

## `suppressions.json`

Stored in the working tree at `<repo>/.defrost/suppressions.json`,
**not** on the data branch:

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
- `schema` is the file format version. Future schema bumps will be
  documented here.
- The user is responsible for `git add` + `git commit` after running
  `defrost suppress add | remove`. We deliberately don't auto-commit so
  suppression changes are reviewable in PRs.

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
`logs/` was not dropped) and force-pushes with a `--force-with-lease`
on the cloned tip so a concurrent writer's push isn't silently
clobbered.

Old git objects on the remote become unreachable and are eventually
garbage-collected — the data is gone, not just hidden.

The local cache (`<repo>/.defrost/data/` and `cache.duckdb`) doesn't
need to be invalidated by drop directly. The next `defrost serve` run
detects the rewritten remote via `ls-remote` (the new SHA isn't a
fast-forward from `cache_meta.last_sha`), force-resets the persistent
worktree, drops every materialised row + `hydration_state` entry, and
re-hydrates against the new history.

`suppressions.json` is in your working tree, so drop never touches it.
