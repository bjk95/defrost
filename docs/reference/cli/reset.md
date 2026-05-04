---
title: '`defrost reset`'
---

Wipe the local `<repo>/.defrost/` cache and immediately re-clone the
data branch from origin. Use this when the local worktree is in a state
that `defrost serve` or other read commands cannot recover from (e.g.
a corrupted DuckDB cache, a botched manual edit, or a stuck git lock).

The data branch on **origin is untouched** — only the local worktree is
affected.

## Synopsis

```text
defrost [global-flags] reset [flags]
```

## Examples

```sh
# Interactive: asks you to type "reset" to confirm.
defrost reset

# Skip the prompt (scripts / CI).
defrost reset --yes

# Reset a repo at a specific path.
defrost --repo-dir=/path/to/repo reset --yes
```

**Inherits global flags** `--repo-dir`, `--data-branch`, `--dev`,
plus `--no-color`, `-v/--verbose`, `-q/--quiet`, `-V/--version`.
See [Configuration](../configuration/).

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--yes`, `-y` | bool | `false` | Skip the interactive confirmation prompt. |

## What it does

1. Checks whether `<repo-dir>/.defrost/` exists. If it does not,
   skips the wipe and goes straight to step 3.
2. Unless `--yes` is set, prints the absolute path of the directory and
   asks you to type `reset` to confirm. Any other input cancels.
3. Deletes `<repo-dir>/.defrost/` entirely (including `cache.duckdb`
   and the embedded git worktree).
4. Re-clones the data branch from `origin` into `<repo-dir>/.defrost/`
   so the next command has a fresh, usable worktree. If the data branch
   does not exist on origin yet (a brand-new repo), the re-clone is a
   no-op and a note is printed.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Reset completed (or cancelled by the user at the prompt). |
| `1` | Could not stat or remove the directory, or the re-clone failed. |

## Notes

- `defrost reset` is the escape hatch of last resort. Normal usage never
  requires it — `defrost serve` keeps the worktree healthy automatically.
- If the re-clone fails (e.g. no network), the local cache is already
  wiped. The next read command will retry the clone automatically.
- `defrost reset` does **not** affect the data branch on origin.
  Use [`defrost drop history`](../drop/) to permanently remove data from
  the remote.
