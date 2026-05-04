---
title: '`defrost suppress`'
---

Manage the suppression list. When every failing test in a `defrost exec`
run is suppressed, the exit code is rewritten to `0` so CI can stay
green without skipping the test in source. Suppressions are stored in
your working tree at `<repo>/.defrost/suppressions.json` — committed
alongside your source so changes flow through normal PR review. See
[storage layout](../../storage-layout/#suppressionsjson) for the file
format.

`defrost suppress` has three subcommands: `add`, `remove`, `list`. All
mutations are idempotent. Mutations write the file directly — defrost
does **not** auto-commit. Run `git add .defrost/suppressions.json &&
git commit` afterwards to share the change with your team.

## `defrost suppress add`

Add one or more test IDs to the suppression list in a single commit.

```text
defrost suppress add [flags] <test-id> [test-id...]
```

```sh
defrost suppress add github.com/bjk95/defrost.TestFlaky
defrost suppress add \
  "tests/test_evals.py::test_quality" \
  "tests/test_evals.py::test_latency"
```

| Flag | Type | Default | Description |
|---|---|---|---|
| `--repo-dir` | string | `.` | Path to the git repo. |
| `--data-branch` | string | `_defrost` | Branch where suppressions are stored. |
| `--dev`, `-d` | bool | `false` | Read/write only the local `<repo>/.defrost/` tree (no remote operations). Same effect as the prod path here, since suppressions are local-file-only either way; the flag is preserved for symmetry. |

**Exit codes:** `0` on success (including no-op when every ID was
already on the list). `1` on file-write failure. `2` if no test IDs
were provided.

## `defrost suppress remove`

Remove a single test ID from the suppression list.

```text
defrost suppress remove [flags] <test-id>
```

| Flag | Type | Default | Description |
|---|---|---|---|
| `--repo-dir` | string | `.` | Path to the git repo. |
| `--data-branch` | string | `_defrost` | Branch where suppressions are stored. |
| `--dev`, `-d` | bool | `false` | Read/write only the local `<repo>/.defrost/` tree (no remote operations). Same effect as the prod path here, since suppressions are local-file-only either way; the flag is preserved for symmetry. |

**Exit codes:** `0` on success (including no-op when the ID was already
absent). `1` on file-write failure.

## `defrost suppress list`

Print suppressed test IDs, sorted, one per line.

```text
defrost suppress list [flags]
```

| Flag | Type | Default | Description |
|---|---|---|---|
| `--repo-dir` | string | `.` | Path to the git repo. |
| `--data-branch` | string | `_defrost` | Branch to read from. |
| `--dev`, `-d` | bool | `false` | Read only from the local `<repo>/.defrost/` tree (no remote operations). |

**Exit codes:** `0` on success. `1` on read failure.

## Interaction with `defrost exec`

After a child exits with a non-zero code, `defrost exec` reads
`suppressions.json` and:

- If **every** failing test ID is in the list, rewrites the exit code to
  `0` and prints `defrost: all N failing test(s) suppressed; rewriting
  exit <code> → 0` to stderr.
- If any failing test ID is absent from the list, the original exit code
  is preserved.
- File-level errors (test IDs ending in `::<file-error>`) are never
  suppressed — these indicate the test runner itself failed and should
  not be hidden.
- If persistence failed, suppression is skipped entirely and the exit
  code is rewritten to `1`.

See also [`defrost exec` exit codes](../exec/#exit-codes).
