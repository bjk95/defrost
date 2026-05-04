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
`_defrost`) containing one trace file and optionally one metrics file,
named by trace ID and partitioned by UTC date. See
[storage layout](../reference/storage-layout.md) for the exact paths.

The branch is **orphan** — it shares no history with the main branch.
That keeps the working tree clean (you never have trace files appearing
in normal `git log`) and means CI checkouts of `main` never download
the data branch unless they ask for it.

## Concurrency

Two CI jobs can record runs in parallel without coordinating. defrost's
`.gitattributes` declares trace and metrics files as `merge=union`, so
parallel commits land in different paths and any merge resolves
mechanically. `suppressions.json` does not use `merge=union` (order
matters there) and instead resolves concurrent writes by fetch-rebase-retry.

## Trade-offs

This model assumes you are willing to:

- **Add weight to your repo.** Trace files are zstd-compressed protobuf
  and average a few KB per run, but a year of CI on a busy project will
  add tens to hundreds of MB. `defrost drop history` exists for when
  you want to reclaim it.
- **Live without server-side query.** The data branch is read by
  cloning. `defrost serve` clones the branch and reads it locally.
  There is no way to ask "give me all evals where accuracy < 0.8" via
  HTTP without first cloning. For most teams, on most repos, this is
  fine; for some it isn't, and defrost is not the right tool then.

If you want a hosted service with cross-repo dashboards and a query
API, you want a different product. If you want history that travels
with the code and works in five years without anyone paying a bill,
you probably want this.
