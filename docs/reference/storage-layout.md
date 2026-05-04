---
title: 'Storage layout'
---

What defrost writes to the data branch (default: `_defrost`).

## Branch initialisation

The first `defrost exec` against a fresh repo creates the data branch as
an orphan branch (no parent). The seed commit contains:

- `.gitattributes` — declares `traces/**` and `metrics/**` files as
  `merge=union` so concurrent runs from parallel CI jobs can be merged
  without conflicts.
- `README.md` — a short pointer back to the defrost project explaining
  what the branch is for.

## Run files

For each `defrost exec` invocation that records data, defrost writes up
to two files:

```text
traces/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst
metrics/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst
```

- `<YYYY>/<MM>/<DD>` is the run start time in **UTC**.
- `<trace-id>` is `sha256(run-id)[:16]` rendered as 16 lower-case hex
  characters, where `run-id` is `<16-hex-of-UnixNano>-<8-hex-of-crypto/rand>`.
  Trace IDs sort by run start time within a millisecond and are
  collision-resistant across parallel jobs.
- `<file>.otlp.pb.zst` is a [zstd](https://facebook.github.io/zstd/)-compressed
  OTLP protobuf message:
  - `traces/...`: one `ResourceSpans` containing the `defrost.run` root
    span and one child span per test result.
  - `metrics/...`: one `ResourceMetrics` containing every metric the
    child OTel SDK exported during the run.
- The metrics file is omitted entirely if the child exported no metrics.

Files are written atomically: defrost writes to `<path>.tmp`, fsyncs,
renames onto the final path, and fsyncs the parent directory. A crash
mid-write leaves at most a stray `.tmp` file, never a partial result.

## `suppressions.json`

Stored at the **root** of the data branch:

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
- Unlike trace and metrics files, `suppressions.json` is **not**
  declared `merge=union`. Concurrent mutations (two parallel
  `defrost suppress add` calls) resolve via fetch-rebase-retry rather
  than line-level union, since order matters here.

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

`defrost drop history` rewrites the branch as a single orphan commit
containing the keep set (`suppressions.json`, plus whichever of
`traces/` / `metrics/` was not dropped) and force-pushes. Old git
objects become unreachable and are eventually garbage-collected by the
remote — the data is gone, not just hidden.
