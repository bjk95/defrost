---
title: '`defrost drop`'
---

Destructively drop persisted traces, metrics, and/or logs. Used to
garbage-collect the data branch when it grows too large or accumulates
noise from abandoned experiments. Suppressions are unaffected — they
live in your working tree at `<repo>/.defrost/suppressions.json`, not
on the data branch.

## `defrost drop history`

Rewrite the data branch as a single orphan commit containing only the
preserved data, then force-push so old git objects can be garbage-collected
by the remote.

```text
defrost drop history [flags]
```

| Flag | Type | Default | Description |
|---|---|---|---|
| `--traces-only` | bool | `false` | Drop only traces; keep metrics and logs. |
| `--metrics-only` | bool | `false` | Drop only metrics; keep traces and logs. |
| `--yes`, `-y` | bool | `false` | Skip the interactive confirmation prompt. |
| `--repo-dir` | string | `.` | Path to the git repo. |
| `--data-branch` | string | `_defrost` | Branch name to rewrite. |
| `--dev`, `-d` | bool | `false` | Drop only the local `<repo-dir>/.defrost/data/` tree (no remote operations). |

`--traces-only` and `--metrics-only` are mutually exclusive. With neither
set, every signal is dropped.

## What it does

1. Reads the current data branch and partitions files into "keep" and
   "drop" sets according to the flags.
2. Prints a one-line summary of what will be removed and prompts for
   confirmation (type `drop` to proceed) unless `--yes` is set. The
   prompt also reports the suppression count from your working tree
   so you can confirm those are unaffected.
3. Creates an **orphan commit** containing only the keep set. This
   commit has no parent, so the previous history is no longer reachable.
4. Force-pushes the new commit to the data branch on the remote with
   `--force-with-lease` against the SHA we cloned. If a concurrent
   writer pushed between our clone and our push, the lease check fails
   and we abort cleanly rather than silently destroying their data.

After a successful drop, the next `defrost serve` notices the new
remote tip via `ls-remote`, force-resets the local persistent worktree
at `<repo>/.defrost/data/`, drops every materialised row from
`cache.duckdb`, and re-hydrates against the new history. No manual
cache invalidation needed.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Drop completed (or nothing to drop). |
| `1` | Drop failed, or the user answered `n` at the confirmation prompt. |
| `2` | Both `--traces-only` and `--metrics-only` were set. |

## Caveats

- This is a force-push. Anyone with a local clone of the data branch
  will need to re-fetch to see the rewritten history; their old objects
  will dangle locally until the next `git gc`.
- There is no built-in undo. Recover by force-pushing the previous tip
  back if you have it locally (`git push origin <old-sha>:_defrost
  --force`).
