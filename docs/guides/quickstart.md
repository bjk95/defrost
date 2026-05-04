---
title: 'Quickstart'
---

Install defrost, record your first run, see the result. About five
minutes.

## 1. Install

```sh
go install github.com/bjk95/defrost@latest
```

This drops a `defrost` binary in `$GOBIN` (usually `~/go/bin`). Make
sure that's on your `PATH`.

## 2. Run your tests under defrost

From inside any git repo with a test command, wrap it:

```sh
defrost exec go test ./...
# or
defrost exec pytest tests/
# or
defrost exec npm test
```

Output streams through to your terminal exactly as before. The exit
code is your test runner's exit code (with one well-defined exception
— see [suppression](../concepts/suppression.md)).

## 3. See what was recorded

```sh
defrost serve
```

Opens a dashboard at `http://127.0.0.1:6969`. You'll see a heatmap
with one row per test and one column for the run you just made. Click
a cell to see captured output, duration, and commit metadata.

For programmatic access:

```sh
defrost history "tests/test_basics.py::test_pass"
```

Prints recorded history as NDJSON, one OTel `ResourceSpans` per line.

## 4. Look at the data branch

defrost recorded results to a new git branch called `_defrost`:

```sh
git fetch origin _defrost            # if you have a remote
git log _defrost --oneline | head    # one commit per run
```

A separate worktree makes it easy to inspect by hand:

```sh
git worktree add ../defrost-data _defrost
ls ../defrost-data/traces
```

See [the `_defrost` branch](../concepts/defrost-branch.md) for the
on-disk layout.

## What's next

- **Wire it into CI.** See [CI setup](./ci-setup.md).
- **Suppress a flaky test** so CI stays green without skipping it. See
  [Suppressing known-failing tests](./suppressing-tests.md).
- **Record eval metrics**, not just test pass/fail. See
  [Recording evals](./recording-evals.md).
