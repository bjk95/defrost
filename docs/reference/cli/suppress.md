---
title: '`defrost suppress`'
---

Manage the suppression list. When every failing test in a `defrost exec`
run is suppressed, the exit code is rewritten to `0` so CI can stay
green without skipping the test in source. Suppressions are stored at
the root of the `_defrost` data branch as `suppressions.json`. See
[storage layout](../../storage-layout/#suppressionsjson) for the file
format.

`defrost suppress` has three subcommands: `add`, `remove`, `list`. All
mutations are idempotent. Mutations push to the data branch
immediately (no manual commit needed); same conflict-resolution model
as run writes (fetch + replay-mutation on non-FF, up to 5 retries).

**Inherits global flags** `--repo-dir`, `--data-branch`, `--dev`,
plus `--no-color`, `-v/--verbose`, `-q/--quiet`, `-V/--version`.
See [Configuration](../configuration/).

## `defrost suppress add`

Add one or more test IDs to the suppression list in a single commit.

```text
defrost [global-flags] suppress add [flags] <test-id> [test-id...]
```

```sh
defrost suppress add github.com/bjk95/defrost.TestFlaky
defrost suppress add \
  "tests/test_evals.py::test_quality" \
  "tests/test_evals.py::test_latency"
```

This subcommand has no additional flags beyond the global flags.

**Exit codes:** `0` on success (including no-op when every ID was
already on the list). `1` on commit/push failure. `2` if no test IDs
were provided.

## `defrost suppress remove`

Remove a single test ID from the suppression list.

```text
defrost [global-flags] suppress remove [flags] <test-id>
```

This subcommand has no additional flags beyond the global flags.

**Exit codes:** `0` on success (including no-op when the ID was already
absent). `1` on commit/push failure.

## `defrost suppress list`

Print suppressed test IDs, sorted, one per line.

```text
defrost [global-flags] suppress list [flags]
```

This subcommand has no additional flags beyond the global flags.

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

A persist failure does **not** disable suppression. The test command's
exit signal is what matters; suppression rewriting still applies. See
[`defrost exec` exit codes](../exec/#exit-codes) and
[troubleshooting persist failures](../../../guides/troubleshooting/persist-failed/).
