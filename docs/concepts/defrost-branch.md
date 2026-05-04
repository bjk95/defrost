---
title: 'The `_defrost` branch'
---

defrost writes to a single git branch — by default named `_defrost` —
that lives alongside `main`. This page covers its lifecycle and how to
inspect it by hand.

## Lifecycle

1. **Created on first use.** The first `defrost exec` against a repo
   creates the branch as an **orphan** branch (no shared history with
   `main`). The seed commit contains a `.gitattributes` declaring
   `traces/**` and `metrics/**` as `merge=union`, plus a small
   `README.md` pointing back to defrost.
2. **Append-only by default.** Every recorded run appends one commit.
   Suppression mutations append one commit each. The data branch grows
   monotonically.
3. **Force-rewritten only by `defrost drop history`.** That command
   creates a new orphan commit containing only the keep set and
   force-pushes it. Old objects become unreachable and are
   garbage-collected by the remote.

## What's in it

```text
.gitattributes
README.md
suppressions.json
traces/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst
metrics/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst
```

See [storage layout](../reference/storage-layout.md) for the exact
file format and naming rules.

## Inspecting the branch by hand

The branch is just git, so anything that works on git works on it:

```sh
# Switch to the data branch in a separate worktree (recommended).
git worktree add ../defrost-data _defrost
cd ../defrost-data
git log --oneline | head

# Decode a single trace file with zstd + protoc, or use defrost itself:
defrost history "tests/test_basics.py::test_pass" | head -1 | jq

# See who suppressed what.
git blame suppressions.json
```

## Why a separate branch instead of a separate repo

Two reasons:

- **Atomic correlation with code.** A trace file recorded against
  commit `abc123` lives in the same git object database as `abc123`.
  Cloning the repo gives you both halves. There is no way for the data
  to outlive (or be lost from) the code.
- **No second access-control surface.** Whoever can push to the repo
  can record runs; whoever can read the repo can read the history.
  There is no separate ACL to keep in sync.

The cost is repo size. See the [trade-offs in the Git-as-database
page](./git-as-database.md#trade-offs).

## Why orphan

If `_defrost` shared history with `main`, every `git fetch` on `main`
would pull down trace data nobody cares about, and `git log` on `main`
would have to ignore data-branch commits to produce useful output.
Orphan branches sidestep both problems: nothing on `main` references
them, and they're invisible until you ask for them.
