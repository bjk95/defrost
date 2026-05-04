---
title: 'Suppressing known-failing tests'
---

Suppression marks a test as known-failing **without skipping it in
source**. The test still runs, results are still recorded, but its
failure does not turn CI red — as long as every failing test in the
run is suppressed.

For the why and the trade-offs vs. skipping in source, see
[the suppression concept page](../concepts/suppression.md).

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

`suppressions.json` lives at the root of the data branch. `git blame`
on it shows who suppressed what and when:

```sh
git fetch origin _defrost
git -C $(git rev-parse --show-toplevel) blame _defrost -- suppressions.json
```

Use this for periodic reviews — long-lived suppressions usually mean a
test that should be either fixed or deleted.

## Doing it from CI scripts

The dashboard exposes the same operations as HTTP endpoints (used by
the SPA). If you have programmatic suppression management — e.g. a
bot that opens a PR adding suppressions when a flake threshold is
crossed — you can either shell out to `defrost suppress add` or use
the [Serve HTTP API](../reference/serve-api.md#suppressions) directly.
