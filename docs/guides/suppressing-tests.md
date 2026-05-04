---
title: 'Suppressing known-failing tests'
---

Suppression marks a test as known-failing **without skipping it in
source**. The test still runs, results are still recorded, but its
failure does not turn CI red — as long as every failing test in the
run is suppressed.

For the why and the trade-offs vs. skipping in source, see
[the suppression concept page](../../concepts/suppression/).

## Suppress a test

Find the test ID — easiest way is from the dashboard or from a previous
run's output:

```sh
defrost suppress add "tests/test_evals.py::test_quality"
```

You can suppress several IDs in a single commit:

```sh
defrost suppress add \
  "tests/test_evals.py::test_quality" \
  "tests/test_evals.py::test_latency"
```

Next time the suite runs under `defrost exec`, if every failing test is
in the suppression list, the exit code is rewritten to `0`. CI sees green.

## See what's suppressed

```sh
defrost suppress list
```

Or in the dashboard: there's a Suppressions panel that lists current
entries and lets you add or remove from the UI.

## Unsuppress

```sh
defrost suppress remove "tests/test_evals.py::test_quality"
```

There is no auto-unsuppress when a test starts passing — that would
create a flake (test passes → suppression auto-removes → next failure
turns CI red unexpectedly). Take the test off the list explicitly when
you've fixed it.

## When suppression doesn't help

Two cases where the rewrite does **not** apply:

- **Any failing test is not on the list.** Even one un-suppressed
  failure preserves the original non-zero exit code.
- **The test runner itself failed.** Errors with the test ID suffix
  `::<file-error>` (bad import, syntax error, broken fixture) cannot
  be suppressed. If your suite can't load, that always fails.

## Audit trail

`suppressions.json` lives in your working tree at
`<repo>/.defrost/suppressions.json` and is committed alongside your
source code. That means every suppression change is a reviewable diff
in your normal PR workflow — no separate audit trail to consult.

```sh
git log --follow .defrost/suppressions.json
git blame .defrost/suppressions.json
```

Use this for periodic reviews — long-lived suppressions usually mean a
test that should be either fixed or deleted.

### Why a working-tree file (not the data branch)?

Earlier versions stored `suppressions.json` on the `_defrost` data
branch. Two problems with that:

1. **Reviews skipped them.** `defrost suppress add` would commit-and-
   push to the data branch silently, bypassing the team's PR review
   process.
2. **Reads were expensive.** Every `defrost exec` cloned the data
   branch just to check whether each failing test was suppressed.

Putting the file in the working tree fixes both: changes show up as
diffs in PRs, and reads are a local file open.

## Doing it from CI scripts

The dashboard exposes the same operations as HTTP endpoints (used by
the SPA). If you have programmatic suppression management — e.g. a
bot that opens a PR adding suppressions when a flake threshold is
crossed — you can either shell out to `defrost suppress add` or use
the [Serve HTTP API](../../reference/serve-api/#suppressions) directly.

When called from CI, both paths write to the runner's checked-out
`<repo>/.defrost/suppressions.json` — that's a local file in the CI
job's workspace, **not** an automatic push. To make the change
durable, the bot needs to commit and push that diff back to the
source branch (typically as a PR).
