---
title: 'Git as the database'
---

defrost stores test, eval, and metric history as commits on a branch
inside the same repo as the code under test. There is no separate
database, no SaaS endpoint, no API key.

## Why

The data structure is append-only, branch-shaped, and naturally
correlated with code state — exactly what git is for. Storing it
anywhere else would mean re-implementing things git already gives you:

- **Persistence and replication.** Anyone with `git clone` access has
  the full history. There is nothing to back up separately.
- **Authentication.** If a developer can already push to the repo, they
  can already record results. No second credential to manage.
- **Code correlation.** Every recorded run carries the commit SHA it
  ran against. There is no possibility of drift between "the test
  result" and "the code that produced it" because they live in the same
  object database.
- **History is a feature.** `git log` on the data branch is run history.
  `git diff` between two trace files is a regression diff. `git blame`
  on `suppressions.json` says who suppressed what.

## How runs are stored

Each recorded run becomes a commit on the data branch (default
`_defrost`) carrying one trace file and, when the run produced them,
one metrics file and one logs file. Suppressions live on the same
branch as `suppressions.json`. Files are named by trace ID and
partitioned by UTC date. See
[storage layout](../../reference/storage-layout/) for the exact paths.

The branch is **orphan** — it shares no history with the main branch.
That keeps the working tree clean (you never have trace files appearing
in normal `git log`) and means CI checkouts of `main` never download
the data branch unless they ask for it.

## Concurrency

Two CI jobs can record runs in parallel without coordinating. Each
write produces a unique filename (one file per run, named by its OTel
`trace_id`), so parallel commits never contend on a shared path. If a
push hits a non-fast-forward race, defrost fetches, rebases, and
retries (up to 5 attempts). After that the run is dropped with a
loud warning, but the test command's exit code is preserved — see
[troubleshooting persist failures](../../guides/troubleshooting/persist-failed/).

## Read-side cache

Reads happen against a local DuckDB at `<repo>/.defrost/cache.duckdb`,
hydrated incrementally from the data branch worktree itself —
`<repo>/.defrost/` IS the worktree. The first `defrost serve` clones
the data branch there; every subsequent load runs `git ls-remote` (one
HTTPS round-trip, ~50ms) and short-circuits when the SHA hasn't
changed. So "git as a database" on the read side actually behaves
like a database: queries hit local indexed storage, not a fresh clone
per request.

## Trade-offs

This model assumes you are willing to:

- **Add weight to your repo.** Trace, metric, and log files are zstd-
  compressed protobuf and average a few KB each per run, but a year of
  CI on a busy project will add tens to hundreds of MB. `defrost drop
  history` exists for when you want to reclaim it.
- **Live without server-side query.** The data branch is read by
  cloning. `defrost serve` keeps a persistent local worktree and
  hydrates a DuckDB from it for queries — but the source of truth is
  still the git branch. There is no remote query API; cross-repo
  dashboards aren't a thing here.
- **Stay under a few gigabytes of run history.** Past that, push
  frequency, fetch cost, and dashboard load all start hitting the
  limits of the git protocol. Plan for `defrost drop history` to be
  part of regular hygiene at scale.

If you want history that travels with the code and works in five years
without anyone paying a bill, you probably want this.
