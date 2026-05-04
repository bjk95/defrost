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
  on `.defrost/suppressions.json` (which lives in the working tree, not
  on the data branch) says who suppressed what.

## How runs are stored

Each recorded run becomes a commit on the data branch (default
`_defrost`) carrying one trace file and, when the run produced them,
one metrics file and one logs file. Files are named by trace ID and
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
retries (up to 5 attempts).

## Read-side cache

Reads happen against a local DuckDB at `<repo>/.defrost/cache.duckdb`,
hydrated incrementally from a persistent worktree at
`<repo>/.defrost/data/`. The first `defrost serve` clones the data
branch there; every subsequent load runs `git ls-remote` (one HTTPS
round-trip, ~50ms) and short-circuits when the SHA hasn't changed. So
"git as a database" on the read side actually behaves like a database:
queries hit local indexed storage, not a fresh clone per request.

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
  dashboards aren't a thing here. For most teams, on most repos, this
  is fine; for some it isn't, and defrost is not the right tool then.

If you want a hosted service with cross-repo dashboards and a query
API, you want a different product. If you want history that travels
with the code and works in five years without anyone paying a bill,
you probably want this.
