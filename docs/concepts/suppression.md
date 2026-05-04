---
title: 'Suppression'
---

Suppression marks a test as known-failing **without skipping it in
source**. The test still runs, results are still recorded, but a
failure does not turn CI red.

## Suppress, not skip

The distinction matters:

|  | Skipped (in source) | Suppressed (in defrost) |
|---|---|---|
| Test runs? | No | Yes |
| Result recorded? | No | Yes — every run, with output |
| Failure visible in dashboard? | No | Yes |
| Effect on CI exit code? | None | Failure is rewritten to `0` only when **every** failing test is suppressed |
| Stored in source? | Yes (decorator / `t.Skip`) | No — stored on the `_defrost` data branch as `suppressions.json` |
| Survives a code change? | Until someone deletes the marker | Until someone runs `defrost suppress remove` |

A skipped test is invisible. A suppressed test keeps generating data —
you can see at a glance how often it actually fails, whether the failure
shape is changing, and whether it has started passing on its own.

## When to use it

- **Known-flaky tests** you haven't had time to fix but don't want
  blocking PRs.
- **Aspirational evals** — quality benchmarks the model isn't passing
  yet but you want to track over time.
- **Migration windows** — a refactor temporarily fails some assertions;
  suppress them, ship the refactor, fix them next.

The right answer for a test that is genuinely meant to be off is to
delete it or mark it skipped in source. Suppression is for tests that
should still be running.

## How the rewrite works

After the child exits with a non-zero code, `defrost exec`:

1. Collects the IDs of the failing tests from the adapter.
2. Reads `suppressions.json` from `<repo>/.defrost/` (the persistent
   worktree of the data branch — defrost keeps it in sync).
3. If **every** failing test ID is in the list, rewrites the exit code
   to `0` and prints a one-line stderr note. CI sees green.
4. Otherwise, preserves the original exit code. CI sees red.

File-level errors (the test runner itself failing — bad import, syntax
error, broken fixture) carry the special suffix `::<file-error>` and
are never suppressible. If your suite can't load, that always fails.

A persist failure does **not** disable suppression. The test command's
exit signal is what matters; suppression rewriting still applies. The
unpushed run is gone (see [troubleshooting](../../guides/troubleshooting/persist-failed/))
but the build outcome reflects the actual test result.

## Where suppressions live

On the `_defrost` data branch as `suppressions.json` at the branch
root, alongside `traces/`, `metrics/`, and `logs/`:

```json
{
  "schema": 1,
  "test_ids": ["github.com/x/p.TestFlaky", "tests/eval.py::test_quality"]
}
```

`defrost suppress add | remove` writes via a temp clone of the data
branch, commits with the `defrost[bot]` identity, and pushes — same
conflict-resolution logic as run writes (fetch + replay-mutation on
non-FF, up to 5 attempts). Reads use the persistent worktree at
`<repo>/.defrost/suppressions.json` for speed; first-time CI jobs
that have no worktree fall back to a temp clone.

`git log` on the data branch tells you who suppressed what and when.

There is no expiry, watcher, or "auto-unsuppress when passing"
behaviour — suppressions stay until removed. That's intentional: an
automatic unsuppress would create a flake (test fails → suppression
auto-removes → next run fails CI for "no reason"), and silent removal
would defeat the audit trail.

To remove a suppression manually:

```sh
defrost suppress remove "tests/eval.py::test_quality"
```

To see what's currently suppressed:

```sh
defrost suppress list
```
