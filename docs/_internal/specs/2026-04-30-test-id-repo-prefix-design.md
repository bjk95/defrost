# Repo-Relative Test ID Prefix — Design

**Date:** 2026-04-30
**Status:** Approved, not yet implemented.

## Purpose

Make persisted test IDs globally unique within a repository by prefixing them with the path from the repo root to the working directory where the test runner was invoked. Today, two test files with the same name in different subprojects (e.g. `examples/typescript/basics.test.ts` and `examples/javascript/basics.test.js`) collide in persisted history because their IDs are computed relative to cwd, not the repo root.

Out of scope: changing Go test IDs (Go's `<modulePath>/<TestName>` is already globally unique within a repo, even across multiple `go.mod` files), changing the persistence file format, or changing `models.TestResult.Id` schema.

## High-level flow

```
defrost exec <runner> ─► adapter.Run(cmd)
                          │
                          ├─► runs the underlying test binary
                          ├─► parses results into []TestResult (cwd-relative IDs)
                          │
                          └─► (jest/pytest only) prepend repoRelCwd + "¬" to each
                              result.Id, where repoRelCwd is filepath.Rel(repoRoot, cwd)
                              and repoRoot comes from `git rev-parse --show-toplevel`.
                              Empty when not in a git repo or cwd == repoRoot.
```

## Components

### `internal/runner/prefix.go` (new)

Single exported function:

```go
// RepoRelCwd returns the path from the git repo root to the current
// working directory, suitable for prefixing test IDs to make them
// globally unique within the repository. Returns "" when:
//   - not inside a git repo,
//   - cwd is the repo root,
//   - any git invocation fails.
//
// The empty return is the "no decoration" signal — callers must treat
// it as such, not as an error.
func RepoRelCwd() string
```

Implementation: `git rev-parse --show-toplevel` → `filepath.Rel(root, cwd)` → return `""` if result is `.` or any step errors. Uses `os/exec` directly (the `persist` package's `runGit` is private; duplicating ~10 lines is fine — keeps the runner package free of a dependency on persist).

### `internal/javascript/jest/adapter.go`

In `(*Adapter).Run`, after `ParseFile` succeeds and before returning, decorate IDs:

```go
if prefix := runner.RepoRelCwd(); prefix != "" {
    for i := range results {
        results[i].Id = prefix + "¬" + results[i].Id
    }
}
```

The Jest parser (`parser.go`) is **not** modified — it keeps emitting cwd-relative file paths. The decoration is a thin layer on top.

### `internal/python/pytest/adapter.go`

Same shape: prepend `prefix + "¬"` to each result's `Id` after `ParseFile` succeeds.

### `internal/golang/adapter.go`

Unchanged. Go's package paths are already globally unique within a repo.

### `internal/runner/adapter.go`

Unchanged. The Adapter interface stays at two methods; the prefix concern lives inside the Jest and Pytest implementations.

## Format

`<repoRelCwd>¬<existing test id>`, where `¬` is U+00AC (NOT SIGN).

| Cwd (relative to repo root) | Runner | Result ID |
|---|---|---|
| `.` (repo root) | jest | `basics.test.ts::adds correctly` (unchanged) |
| `examples/typescript` | jest | `examples/typescript¬basics.test.ts::adds correctly` |
| `examples/python` | pytest | `examples/python¬test_basics::test_foo` |
| any | go | `github.com/bjk95/defrost/internal/x/TestFoo` (unchanged) |

`¬` was chosen because:
- It does not appear in any path separator, Python module name, JS test description, or shell character set we care about — collision-free as a sentinel.
- It's a single Unicode character, visually distinct, and easy to grep for in persisted ndjson.
- `url.PathEscape` (used in `persist.EncodeTestID`) escapes it cleanly to `%C2%AC`, so filesystem-safe IDs still work.

## Nested describes (Jest/TypeScript)

**No behavior change required.** `parser.go:78-85` already joins the full `ancestorTitles` slice with ` > `, so:

```js
describe("outer", () => {
  describe("A", () => { it("same", ...) })
  describe("B", () => { it("same", ...) })
})
```

produces two distinct IDs: `outer > A > same` and `outer > B > same`.

This spec adds a regression fixture (`testdata/nested-describes.json`) and a parser-level test case to lock the behavior in. No production code change.

## Testing

1. **`internal/runner/prefix_test.go`** (new) — three cases:
   - cwd == repo root → returns `""`.
   - cwd in a subdir → returns the rel path.
   - not a git repo → returns `""` (no panic, no error surfaced to caller).

2. **`internal/javascript/jest/adapter_test.go`** (extend) — assert that IDs returned from `(*Adapter).Run` are decorated with `<prefix>¬` when invoked from a subdir of a git repo, and undecorated when invoked from the repo root.

3. **`internal/python/pytest/adapter_test.go`** (extend) — same as above for pytest.

4. **`internal/javascript/jest/testdata/nested-describes.json`** (new) + parser test case — two assertions sharing the same `title` under different inner describes. Asserts both are emitted with distinct IDs.

5. **No new test for the Go path** — Go's adapter is untouched.

## Migration / persisted-data implications

Test IDs change shape for any user running defrost from a subdirectory. Persisted history under the old IDs becomes orphaned (still readable, but won't match new entries). This is acceptable for the current state of the project (defrost is pre-1.0 and the data branch format is explicitly versioned via `SchemaVersion`). No schema bump is needed because the on-disk format is unchanged — only the values stored in `test_id` / `test_name` differ.

If a user wants their old history to merge with new history, they can either:
- Always run defrost from the same directory (typically repo root or always the subproject dir), or
- Manually rename `tests/<old-encoded-id>.ndjson` → `tests/<new-encoded-id>.ndjson` on the data branch.

Documenting this is out of scope for this change; we'll add a README note if/when users hit it.
