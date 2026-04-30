# Defrost Suppress — Design

**Date:** 2026-04-30
**Status:** Draft, pending implementation

## Purpose

Let users mark known-failing tests as "suppressed" so the failures stop breaking
CI without requiring the test to be deleted, skipped, or commented out in
source. When every failing test in a run is on the suppression list, defrost
rewrites a non-zero exit code to zero. Any failure outside the list — a new
test failure, a build error, a panic the adapter could not attribute to a
single test — still exits non-zero.

Out of scope: per-suppression reason/owner/expiry metadata, retries on
suppressed tests, classification of why a test is failing, alerting on
suppressed tests passing.

## User-facing behavior

Three new subcommands, all sharing the existing `--repo-dir`, `--data-branch`,
`--no-remote`, and `--dev` flags:

```
defrost suppress add <test_id>
defrost suppress remove <test_id>
defrost suppress list
```

`<test_id>` is the same string `defrost history` accepts — the full
package-plus-test identifier the adapters emit (e.g.
`github.com/x/p/TestFoo`, `src/foo.test.ts > Foo > does the thing`,
`tests/test_x.py::TestY::test_z`).

`add` and `remove` are idempotent: adding an existing ID or removing a missing
one is a no-op (exit 0, no commit, no push). `list` prints the IDs in sort
order, one per line, with no other formatting.

`defrost exec` is unchanged on the happy path. When the adapter returns a
non-zero exit code, defrost reads the suppression list, compares it to the IDs
of the tests that ran and failed, and:

- If the set of failing-test IDs is non-empty and is a subset of the
  suppression list, defrost writes one line to stderr —
  `defrost: suppressed N failing test(s); rewriting exit 1 → 0` — and returns 0.
- Otherwise it returns the original exit code.

If the failures are not test-level (build error, panic with no test name,
adapter could not attribute the failure), suppressions do not apply. The exit
code is preserved.

## Storage

A single file `suppressions.json` at the root of the `_defrost` data branch:

```json
{
  "schema": 1,
  "test_ids": [
    "github.com/x/p/TestFoo",
    "src/foo.test.ts > Foo > does the thing"
  ]
}
```

`test_ids` is sorted alphabetically on every write so diffs stay minimal. The
`schema` field future-proofs the move to richer entries (reason, expiry, etc.)
if a later iteration needs them.

History of who suppressed what comes from `git log` on the data branch — the
commit message identifies the change (e.g. `suppress: add <id>` /
`suppress: remove <id>`) and the bot identity records the author of the
defrost invocation.

## Components

### `cli.go`

Add a `Suppress` command group with three subcommands:

```go
Suppress struct {
    Add    struct { Test string `arg:"" name:"test"` ; … flags … } `cmd:""`
    Remove struct { Test string `arg:"" name:"test"` ; … flags … } `cmd:""`
    List   struct { … flags … }                                    `cmd:""`
} `cmd:"" help:"Manage the suppression list."`
```

Each subcommand re-declares `RepoDir`, `DataBranch`, `NoRemote`, `Dev` so kong
sees them on the leaf command. (Kong does not inherit flags upward by default
in this project's setup.)

### `main.go`

Three new dispatch arms next to the existing `exec` and `history` cases. Each
constructs a `persist.Options` and calls into a new `suppress.go` handler.

### `suppress.go`

Three handlers — `HandleSuppressAdd`, `HandleSuppressRemove`, `HandleSuppressList`
— each:

1. Builds `persist.Options` from the parsed flags.
2. Calls `persist.New(opts)` to get a `Backend`.
3. For `list`: calls `GetSuppressions()` and prints. For `add`/`remove`:
   calls `UpdateSuppressions` with a closure that inserts or strips the given
   ID, sorts, and dedupes.

The handlers carry no git knowledge; the backend handles persistence.

### `exec.go`

Add a single block after `a.Run(cmd)` returns:

```go
if code != 0 {
    failingIDs := collectFailingTestIDs(results)
    if len(failingIDs) > 0 {
        suppressed, err := persist.New(pOpts).GetSuppressions()
        if err == nil && allIn(failingIDs, suppressed) {
            fmt.Fprintf(os.Stderr,
                "defrost: suppressed %d failing test(s); rewriting exit %d → 0\n",
                len(failingIDs), code)
            code = 0
        }
    }
}
```

`collectFailingTestIDs` returns the IDs of results where `r.Ran && !r.Passed`.
`allIn` does a small-set membership check; treat the suppression list as a
`map[string]struct{}` internally.

A `GetSuppressions` failure on the rewrite path is logged to stderr and
treated as "no suppressions" — the original exit code is preserved. Defrost
must not turn a green build red because the data branch was unreachable.

### `internal/persist/persist.go`

Two new methods on `Backend`:

```go
GetSuppressions() ([]string, error)
UpdateSuppressions(mutate func(current []string) []string) error
```

`UpdateSuppressions` takes a mutation closure rather than a finished list so
the gitBackend can re-apply it against the latest tip on push retry. The
fileBackend just calls the closure once. `add`/`remove` handlers pass small
closures that insert or strip a single ID; a future `set` operation could
pass a closure that ignores `current` entirely.

A new `gitErr`-aware code path on the read side: when the data branch does not
exist, `GetSuppressions` returns an empty slice and a nil error. Same when the
branch exists but has no `suppressions.json` file yet.

`fileBackend` (dev mode) reads/writes `<DevDir>/suppressions.json` directly.

`gitBackend.UpdateSuppressions`:

1. Resolve target URL (origin or local `.git`, same as `InsertNewTestResults`).
2. Clone the data branch into a temp workdir, or initialise a fresh one if the
   branch does not yet exist (same `openOrInitDataRepo` flow).
3. Read the current `suppressions.json` (empty list if absent), call the
   caller's mutation closure on it, then marshal the result to
   `suppressions.json` with two-space indent and a trailing newline.
4. If the file content is byte-identical to what was already on disk, return
   without committing.
5. Commit with a short message (the caller passes one — typically
   `suppress: add <id>` or `suppress: remove <id>`), then push with the same
   retry/rebase logic `InsertNewTestResults` uses.

The shared `pushWithRetry` is reused as-is. `suppressions.json` is **not**
covered by `merge=union` in `.gitattributes` — it is a single canonical file
with one writer's intent per commit, so a real merge conflict on simultaneous
writes is the correct behavior. The rebase loop in `pushWithRetry` re-reads
the file from the rebased tip before retrying, so the second writer's change
is preserved (the read-modify-write happens against the latest list each
attempt).

The push retry path re-runs the closure against the rebased tip, so two
concurrent `add` calls for different IDs both end up in the final list.
`UpdateSuppressions` therefore takes a `(mutate, commitMessage)` pair; the
exact signature is left to implementation but conceptually:

```go
UpdateSuppressions(mutate func([]string) []string, message string) error
```

### `internal/persist/persist_test.go`

New tests covering:

- `fileBackend.UpdateSuppressions` writes the expected JSON, sorted and
  deduplicated.
- `fileBackend.GetSuppressions` returns empty when the file is absent.
- `gitBackend.GetSuppressions` returns empty when the branch is absent
  (uses the existing test harness that fakes `ls-remote`).
- `gitBackend.UpdateSuppressions` round-trips through clone-commit-push
  (uses the existing local-bare-repo fixture).
- Concurrent `UpdateSuppressions` calls reconcile correctly: writer A and
  writer B both add different IDs; the final list contains both.

### `exec_test.go` (new or extended)

Tests that exercise the rewrite path with stub adapters:

- One failing test, suppressed → exit 0, stderr line emitted.
- Two failing tests, both suppressed → exit 0.
- Two failing tests, only one suppressed → exit unchanged.
- One failing test, none suppressed → exit unchanged.
- Build-only failure (`Ran == false` for all results, or no results at all
  but `code != 0`) → exit unchanged even when the file-error pseudo-ID is
  on the list.
- `GetSuppressions` returns an error → exit unchanged, error logged.

## Data flow

```
defrost suppress add X
  → cli parses
  → suppress.HandleSuppressAdd
  → persist.Backend.UpdateSuppressions(addClosure(X), "suppress: add X")
  → (gitBackend) clone → read → mutate → write → commit → push (with retry)

defrost exec go test ./...
  → adapter.Run → (results, code)
  → if code != 0 and any test-level failures:
      persist.Backend.GetSuppressions
      → (gitBackend) ls-remote, then ephemeral clone read
      → check subset
      → maybe rewrite code to 0
  → if Persist: persist results (existing flow)
  → return code
```

## Error handling

- Unreachable data branch on `suppress add/remove/list`: surface the underlying
  git error to stderr; exit non-zero. These are explicit user actions; failing
  loudly is correct.
- Unreachable data branch on `exec`: log to stderr, do not rewrite the exit
  code. The test result is the more important signal.
- Concurrent writes: reconcile via push retry + closure re-application. After
  `maxPushAttempts` failures, surface the error. Tested via
  `updateSuppressionsInWorkDir`, which takes a pre-staged workdir so the
  test can simulate a clone whose tip predates a competing push.
- Malformed `suppressions.json`: hard error on read. Defrost should not silently
  treat a corrupt list as empty (which would un-suppress everything).
- `--no-remote` on a repo with no origin: works the same as for `exec` —
  read/write the local `.git` directly.
- `--dev` mode: read/write `.defrost-dev/suppressions.json`. No git involved.

## Out of scope (deferred)

- Per-entry reason / author / expiry metadata.
- A subcommand to prune suppressions whose tests have been passing for N runs.
- Warnings when a suppressed test starts passing again.
- A `--reason` flag (would require schema bump; revisit when there's a
  concrete need).
