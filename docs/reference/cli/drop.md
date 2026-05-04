# `defrost drop`

Destructively drop persisted traces and/or metrics. Used to garbage-collect
the data branch when it grows too large or accumulates noise from
abandoned experiments. Suppressions are preserved.

## `defrost drop history`

Rewrite the data branch as a single orphan commit containing only the
preserved data, then force-push so old git objects can be garbage-collected
by the remote.

```text
defrost drop history [flags]
```

| Flag | Type | Default | Description |
|---|---|---|---|
| `--traces-only` | bool | `false` | Drop only traces; keep metrics. |
| `--metrics-only` | bool | `false` | Drop only metrics; keep traces. |
| `--yes`, `-y` | bool | `false` | Skip the interactive confirmation prompt. |
| `--repo-dir` | string | `.` | Path to the git repo. |
| `--data-branch` | string | `_defrost` | Branch name to rewrite. |
| `--dev`, `-d` | bool | `false` | Drop from the local scratch dir instead of the data branch. |

`--traces-only` and `--metrics-only` are mutually exclusive. With neither
set, both are dropped.

## What it does

1. Reads the current data branch and partitions files into "keep" and
   "drop" sets according to the flags. `suppressions.json` is always in
   the keep set.
2. Prints a one-line summary of what will be removed and prompts for
   confirmation (`y` to proceed) unless `--yes` is set.
3. Creates an **orphan commit** containing only the keep set. This
   commit has no parent, so the previous history is no longer reachable.
4. Force-pushes the new commit to the data branch on the remote (if
   configured). The remote's git GC will eventually reclaim the
   unreachable objects.

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
