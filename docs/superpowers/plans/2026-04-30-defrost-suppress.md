# Defrost Suppress Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `defrost suppress {add,remove,list}` so failing tests can be marked as suppressed; when every failing test in an `exec` run is on the list, defrost rewrites the non-zero exit code to zero.

**Architecture:** Suppression list is a single JSON file (`suppressions.json`) at the root of the existing `_defrost` data branch. The persistence layer grows two new methods (`GetSuppressions`, `UpdateSuppressions`); `UpdateSuppressions` takes a mutation closure so the gitBackend can re-apply the user's intent against the latest tip during push retries. `exec` calls `GetSuppressions` only on the failure path, only when there are test-level (not build-level) failures, and only consults its result to potentially rewrite a non-zero exit code to zero.

**Tech Stack:** Go (1.x as per `go.mod`), `kong` for CLI parsing, plain `git` CLI invoked via `os/exec`, standard library `encoding/json`.

---

## File Structure

**Created:**
- `internal/persist/suppress.go` — suppression file IO helpers + closure helpers shared by both backends.
- `suppress.go` — top-level handlers `HandleSuppressAdd`, `HandleSuppressRemove`, `HandleSuppressList`.
- `internal/persist/suppress_test.go` — unit tests for the helpers, the fileBackend implementation, and the gitBackend implementation (uses the existing `makeFixture` / `cloneDataBranch` test fixtures from `persist_test.go`).
- `exec_test.go` — exit-code rewrite tests that drive `HandleExecution` with a stub adapter.

**Modified:**
- `cli.go` — adds the `Suppress` command group with `Add`, `Remove`, `List` subcommands.
- `main.go` — three new dispatch arms next to `exec` and `history`.
- `internal/persist/persist.go` — adds two methods to the `Backend` interface and implements them on both `gitBackend` and `fileBackend`.
- `exec.go` — rewrites the exit code after the adapter returns, when all failing tests are suppressed.

Each task below produces a self-contained commit. The plan follows TDD: write the failing test first, run it, write the minimal code, run the test, commit.

---

## Task 1: Suppression file IO helpers

Pure functions that read and write `suppressions.json` from a given directory. No git involved. Both backends will reuse these.

**Files:**
- Create: `internal/persist/suppress.go`
- Create: `internal/persist/suppress_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/persist/suppress_test.go`:

```go
package persist

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestReadSuppressionsFile_AbsentReturnsEmpty(t *testing.T) {
	got, err := readSuppressionsFile(t.TempDir())
	if err != nil {
		t.Fatalf("readSuppressionsFile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestWriteSuppressionsFile_SortsAndDedupes(t *testing.T) {
	dir := t.TempDir()
	if err := writeSuppressionsFile(dir, []string{"b", "a", "a", "c"}); err != nil {
		t.Fatalf("writeSuppressionsFile: %v", err)
	}

	got, err := readSuppressionsFile(dir)
	if err != nil {
		t.Fatalf("readSuppressionsFile: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip: want %v got %v", want, got)
	}
}

func TestWriteSuppressionsFile_StableSortAcrossWrites(t *testing.T) {
	dir := t.TempDir()
	if err := writeSuppressionsFile(dir, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "suppressions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSuppressionsFile(dir, []string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "suppressions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("write order should not affect bytes:\n first:  %s\n second: %s", first, second)
	}
}

func TestReadSuppressionsFile_MalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "suppressions.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSuppressionsFile(dir); err == nil {
		t.Errorf("expected error reading malformed file, got nil")
	}
}

func TestSortAndDedupe(t *testing.T) {
	got := sortAndDedupe([]string{"b", "a", "b", "c", "a"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("not sorted: %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/persist/ -run TestReadSuppressionsFile -v`

Expected: FAIL — `readSuppressionsFile` and `writeSuppressionsFile` are undefined.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/persist/suppress.go`:

```go
package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const suppressionsFile = "suppressions.json"

// suppressionsDoc is the on-disk shape of suppressions.json. The schema
// field future-proofs the move to richer entries (reason/expiry/etc.) if
// a later iteration needs them.
type suppressionsDoc struct {
	Schema  int      `json:"schema"`
	TestIDs []string `json:"test_ids"`
}

const suppressionsSchema = 1

// readSuppressionsFile returns the suppression list at dir/suppressions.json.
// An absent file is not an error: it returns an empty slice. A present-but-
// malformed file IS an error — defrost must not silently treat corruption as
// "no suppressions" (which would un-suppress everything).
func readSuppressionsFile(dir string) ([]string, error) {
	path := filepath.Join(dir, suppressionsFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	var doc suppressionsDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return doc.TestIDs, nil
}

// writeSuppressionsFile sorts and dedupes ids, then writes the JSON
// document to dir/suppressions.json with two-space indent and a trailing
// newline. Sorting on every write keeps diffs minimal regardless of input
// order.
func writeSuppressionsFile(dir string, ids []string) error {
	doc := suppressionsDoc{Schema: suppressionsSchema, TestIDs: sortAndDedupe(ids)}
	if doc.TestIDs == nil {
		doc.TestIDs = []string{}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal suppressions: %w", err)
	}
	path := filepath.Join(dir, suppressionsFile)
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func sortAndDedupe(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/persist/ -run "TestReadSuppressionsFile|TestWriteSuppressionsFile|TestSortAndDedupe" -v`

Expected: PASS for all five subtests above.

- [ ] **Step 5: Commit**

```bash
git add internal/persist/suppress.go internal/persist/suppress_test.go
git commit -m "feat: add suppressions.json read/write helpers"
```

---

## Task 2: Add `GetSuppressions` and `UpdateSuppressions` to the `Backend` interface and implement on `fileBackend`

The `fileBackend` (used in `--dev` mode) just calls the helpers from Task 1. Adding the two interface methods is a compile-time fence that forces the gitBackend to implement them too — keep this task focused on `fileBackend` only and let the gitBackend test in Task 3 break the build until that task lands.

**Files:**
- Modify: `internal/persist/persist.go` — extend `Backend` interface; add methods to `fileBackend` and a stub on `gitBackend` that returns `errors.New("not implemented")`.
- Modify: `internal/persist/suppress_test.go` — fileBackend tests.

- [ ] **Step 1: Write the failing test**

Append to `internal/persist/suppress_test.go`:

```go
func TestFileBackend_SuppressionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	b := &fileBackend{dir: filepath.Join(dir, "scratch")}

	got, err := b.GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions on empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}

	addX := func(cur []string) []string { return append(cur, "x") }
	if err := b.UpdateSuppressions(addX, "ignored"); err != nil {
		t.Fatalf("UpdateSuppressions add x: %v", err)
	}
	addY := func(cur []string) []string { return append(cur, "y") }
	if err := b.UpdateSuppressions(addY, "ignored"); err != nil {
		t.Fatalf("UpdateSuppressions add y: %v", err)
	}

	got, err = b.GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions after writes: %v", err)
	}
	want := []string{"x", "y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/persist/ -run TestFileBackend_SuppressionsRoundTrip -v`

Expected: FAIL — `b.GetSuppressions` undefined.

- [ ] **Step 3: Extend the interface and implement on both backends**

Edit `internal/persist/persist.go`. Find the `Backend` interface declaration (around line 121) and add two methods:

```go
type Backend interface {
	InitialisePersistence() error
	InsertNewTestResults(run RunRecord, results []models.TestResult) error
	GetTestHistory(testName string) ([]HistoricalEntry, error)

	// GetSuppressions returns the current list of suppressed test IDs,
	// or an empty slice if none have been recorded. A missing storage
	// (no data branch yet, no scratch dir yet) is not an error.
	GetSuppressions() ([]string, error)

	// UpdateSuppressions reads the current list, applies mutate, and
	// writes the result. The closure form lets the git backend re-apply
	// the caller's intent against the latest tip during push retry, so
	// concurrent add calls for different IDs both end up in the final
	// list. msg is used as the commit message by backends that commit;
	// other backends ignore it.
	UpdateSuppressions(mutate func([]string) []string, msg string) error
}
```

Append to `internal/persist/persist.go` (or to `internal/persist/suppress.go` — wherever feels least cluttered; prefer `suppress.go` for grouping):

```go
// fileBackend implementation of the suppression methods.

func (b *fileBackend) GetSuppressions() ([]string, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	return readSuppressionsFile(b.dir)
}

func (b *fileBackend) UpdateSuppressions(mutate func([]string) []string, _ string) error {
	if err := b.InitialisePersistence(); err != nil {
		return err
	}
	cur, err := readSuppressionsFile(b.dir)
	if err != nil {
		return err
	}
	return writeSuppressionsFile(b.dir, mutate(cur))
}
```

Add the stub on `gitBackend` so the package still compiles. The real implementation lands in Task 3:

```go
func (b *gitBackend) GetSuppressions() ([]string, error) {
	return nil, errors.New("gitBackend.GetSuppressions: not implemented")
}

func (b *gitBackend) UpdateSuppressions(mutate func([]string) []string, msg string) error {
	return errors.New("gitBackend.UpdateSuppressions: not implemented")
}
```

If the file already imports `errors`, no import change is needed. If not, add it.

- [ ] **Step 4: Run tests to verify**

Run: `go test ./internal/persist/ -run TestFileBackend_SuppressionsRoundTrip -v`

Expected: PASS.

Run: `go build ./...`

Expected: success — the gitBackend stubs satisfy the interface.

Run: `go test ./internal/persist/ -v`

Expected: all existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/persist/persist.go internal/persist/suppress.go internal/persist/suppress_test.go
git commit -m "feat(persist): add suppressions interface + fileBackend impl"
```

---

## Task 3: Implement `GetSuppressions` and `UpdateSuppressions` on `gitBackend`

The git path clones (or initialises) the data branch, reads/mutates/writes the file, commits with the caller-supplied message, and pushes via the existing retry-on-non-fast-forward loop. The closure runs once before the first push attempt and again before each retry, against the freshly fetched tip — so two concurrent `add` calls for different IDs both make it into the final list.

`suppressions.json` is **not** covered by `merge=union` in `.gitattributes` — a real conflict on simultaneous writes is the correct behavior, and the closure-replay approach handles the common "different IDs" case without needing union-merge.

**Files:**
- Modify: `internal/persist/persist.go` (or `internal/persist/suppress.go`) — replace the stubs.
- Modify: `internal/persist/suppress_test.go` — gitBackend tests.

- [ ] **Step 1: Write the failing tests**

Append to `internal/persist/suppress_test.go`:

```go
func TestGitBackend_Suppressions_EmptyWhenBranchAbsent(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	got, err := New(Options{RepoDir: repoDir}).GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty (no data branch yet), got %v", got)
	}
}

func TestGitBackend_Suppressions_RoundTrip(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	addX := func(cur []string) []string { return append(cur, "github.com/x/p/TestX") }
	if err := New(Options{RepoDir: repoDir}).UpdateSuppressions(addX, "suppress: add X"); err != nil {
		t.Fatalf("UpdateSuppressions add X: %v", err)
	}

	got, err := New(Options{RepoDir: repoDir}).GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions: %v", err)
	}
	want := []string{"github.com/x/p/TestX"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}

	// Verify the file landed on the data branch.
	verify := cloneDataBranch(t, originURL)
	b, err := os.ReadFile(filepath.Join(verify, "suppressions.json"))
	if err != nil {
		t.Fatalf("read suppressions.json: %v", err)
	}
	if !strings.Contains(string(b), "github.com/x/p/TestX") {
		t.Errorf("file does not contain test id:\n%s", b)
	}
}

func TestGitBackend_Suppressions_IdempotentAdd(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	addX := func(cur []string) []string { return append(cur, "X") }
	be := New(Options{RepoDir: repoDir})
	if err := be.UpdateSuppressions(addX, "suppress: add X"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := be.UpdateSuppressions(addX, "suppress: add X"); err != nil {
		t.Fatalf("second add: %v", err)
	}

	// Two adds of the same ID should produce one suppression entry...
	got, err := be.GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"X"}) {
		t.Errorf("want [X], got %v", got)
	}

	// ...and only ONE commit on the data branch (the second should be a
	// no-op because the file content didn't change).
	verify := cloneDataBranch(t, originURL)
	out, err := exec.Command("git", "-C", verify, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v: %s", err, out)
	}
	commits := strings.Count(strings.TrimSpace(string(out)), "\n") + 1
	// Expected: seed commit (initial branch creation) + one suppress commit = 2.
	// If second add added a commit, we'd see 3.
	if commits > 2 {
		t.Errorf("expected at most 2 commits on data branch, got %d:\n%s", commits, out)
	}
}

func TestGitBackend_Suppressions_DevModeUsesScratchDir(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	be := New(Options{RepoDir: repoDir, Dev: true})
	add := func(cur []string) []string { return append(cur, "id1") }
	if err := be.UpdateSuppressions(add, "n/a"); err != nil {
		t.Fatalf("UpdateSuppressions: %v", err)
	}
	scratchPath := filepath.Join(repoDir, DevDir, "suppressions.json")
	if _, err := os.Stat(scratchPath); err != nil {
		t.Errorf("expected %s to exist: %v", scratchPath, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/persist/ -run TestGitBackend_Suppressions -v`

Expected: FAIL — the gitBackend methods return "not implemented".

- [ ] **Step 3: Replace the stubs with the real implementation**

Edit `internal/persist/suppress.go` (or the gitBackend section of `persist.go`). Replace the two stub methods:

```go
func (b *gitBackend) GetSuppressions() ([]string, error) {
	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}

	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return nil, err
	}

	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []string{}, nil
	}

	workDir, err := os.MkdirTemp("", "defrost-suppress-read-")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)
	_ = os.Remove(workDir) // clone wants the path missing

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, fmt.Errorf("clone data branch: %w", err)
	}
	return readSuppressionsFile(workDir)
}

func (b *gitBackend) UpdateSuppressions(mutate func([]string) []string, msg string) error {
	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}

	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "defrost-suppress-write-")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)

	branchExisted, err := openOrInitDataRepo(workDir, remoteURL, branch)
	if err != nil {
		return err
	}
	if !branchExisted {
		if err := writeSeed(workDir); err != nil {
			return err
		}
	}

	apply := func() (changed bool, err error) {
		cur, err := readSuppressionsFile(workDir)
		if err != nil {
			return false, err
		}
		next := mutate(cur)
		next = sortAndDedupe(next)

		// Compute current bytes for the change check.
		prevBytes, _ := os.ReadFile(filepath.Join(workDir, suppressionsFile))
		if err := writeSuppressionsFile(workDir, next); err != nil {
			return false, err
		}
		newBytes, err := os.ReadFile(filepath.Join(workDir, suppressionsFile))
		if err != nil {
			return false, err
		}
		return string(prevBytes) != string(newBytes), nil
	}

	changed, err := apply()
	if err != nil {
		return err
	}
	if !changed && branchExisted {
		// No-op: file content unchanged and the branch already exists,
		// so there is nothing to commit or push.
		return nil
	}

	if err := commitAll(workDir, msg); err != nil {
		return err
	}

	// pushBranch + retry, re-running the mutation closure on each retry
	// against the rebased tip.
	var lastErr error
	for attempt := 1; attempt <= maxPushAttempts; attempt++ {
		err := pushBranch(workDir, branch)
		if err == nil {
			return nil
		}
		lastErr = err
		if !branchExisted || !isNonFastForward(err) {
			return err
		}
		if rebErr := pullRebase(workDir, branch); rebErr != nil {
			return fmt.Errorf("rebase after push conflict (attempt %d): %w", attempt, rebErr)
		}
		// Re-apply the mutation on top of the rebased tip and amend the
		// pending commit. The rebase moved HEAD; we now need to re-do
		// the suppression edit and the commit on top of that.
		if _, err := runGit(workDir, "reset", "--soft", "HEAD~1"); err != nil {
			return fmt.Errorf("reset for retry: %w", err)
		}
		if _, err := apply(); err != nil {
			return err
		}
		if err := commitAll(workDir, msg); err != nil {
			return err
		}
	}
	return fmt.Errorf("push failed after %d retries: %w", maxPushAttempts, lastErr)
}
```

Make sure these imports exist at the top of whichever file holds the
implementation: `encoding/json`, `errors`, `fmt`, `os`, `os/exec`, `path/filepath`, `sort`. Import what you use; let `goimports` / `go vet` guide you.

Add to the test file imports if not already present: `os/exec`, `strings`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/persist/ -run TestGitBackend_Suppressions -v`

Expected: PASS for all four subtests.

Run: `go test ./internal/persist/ -v`

Expected: all existing persist tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/persist/suppress.go internal/persist/persist.go internal/persist/suppress_test.go
git commit -m "feat(persist): gitBackend suppression read/write with retry"
```

---

## Task 4: Add the `Suppress` command group to `cli.go`

Top-level `kong` struct gains a `Suppress` field with three subcommands. Each subcommand re-declares `RepoDir`, `DataBranch`, `NoRemote`, and `Dev` so they show up on the leaf command line.

**Files:**
- Modify: `cli.go`

- [ ] **Step 1: Edit `cli.go`**

Replace the file body (keeping the `package main` header) with:

```go
package main

var CLI struct {
	Exec struct {
		Cmd        []string `arg:"" name:"cmd" passthrough:"" help:"Test command to run."`
		RepoDir    string   `name:"repo-dir" default:"." help:"Path to the git repo to persist into."`
		DataBranch string   `name:"data-branch" default:"_defrost" help:"Branch name where results are stored."`
		NoPersist  bool     `name:"no-persist" help:"Run tests without persisting results."`
		NoRemote   bool     `name:"no-remote" help:"Persist locally only — store the data branch in the local repo and do not push."`
		Dev        bool     `name:"dev" short:"d" help:"Dev mode: write results to <repo-dir>/.defrost-dev (gitignored scratch dir) instead of committing/pushing. For developing defrost itself."`
	} `cmd:"" help:"Execute test command and persist results."`

	History struct {
		Test       string `arg:"" name:"test" help:"Full test name (package + test, e.g. github.com/x/p/TestA)."`
		RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
		DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
		NoRemote   bool   `name:"no-remote" help:"Read from the local repo only — do not consult origin."`
	} `cmd:"" help:"Print recorded history for a single test as NDJSON."`

	Suppress struct {
		Add struct {
			Test       string `arg:"" name:"test" help:"Full test ID to suppress (same form as 'defrost history')."`
			RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name where suppressions are stored."`
			NoRemote   bool   `name:"no-remote" help:"Write to the local repo only — do not push."`
			Dev        bool   `name:"dev" short:"d" help:"Dev mode: read/write the local scratch dir instead of the data branch."`
		} `cmd:"" help:"Add a test to the suppression list."`

		Remove struct {
			Test       string `arg:"" name:"test" help:"Full test ID to remove from the suppression list."`
			RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name where suppressions are stored."`
			NoRemote   bool   `name:"no-remote" help:"Write to the local repo only — do not push."`
			Dev        bool   `name:"dev" short:"d" help:"Dev mode: read/write the local scratch dir instead of the data branch."`
		} `cmd:"" help:"Remove a test from the suppression list."`

		List struct {
			RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
			NoRemote   bool   `name:"no-remote" help:"Read from the local repo only — do not consult origin."`
			Dev        bool   `name:"dev" short:"d" help:"Dev mode: read the local scratch dir instead of the data branch."`
		} `cmd:"" help:"List the suppressed test IDs, one per line."`
	} `cmd:"" help:"Manage the suppression list. When every failing test in 'defrost exec' is suppressed, the exit code is rewritten to 0."`
}
```

- [ ] **Step 2: Verify the package compiles**

Run: `go build ./...`

Expected: success.

- [ ] **Step 3: Sanity-check the help output**

Run: `go run . suppress --help`

Expected: the three subcommands show up in the usage banner.

- [ ] **Step 4: Commit**

```bash
git add cli.go
git commit -m "feat(cli): add 'suppress' command group with add/remove/list"
```

---

## Task 5: Implement the `suppress` handlers

Three thin handlers: read flags, build `persist.Options`, call into the backend with the right closure.

**Files:**
- Create: `suppress.go`

- [ ] **Step 1: Create `suppress.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/persist"
)

type SuppressOpts struct {
	RepoDir    string
	DataBranch string
	NoRemote   bool
	Dev        bool
}

func (s SuppressOpts) toPersist() persist.Options {
	return persist.Options{
		RepoDir:    s.RepoDir,
		DataBranch: s.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   s.NoRemote,
		Dev:        s.Dev,
	}
}

func HandleSuppressAdd(testID string, opts SuppressOpts) int {
	if testID == "" {
		fmt.Fprintln(os.Stderr, "suppress add: empty test id")
		return 2
	}
	be := persist.New(opts.toPersist())
	mutate := func(cur []string) []string { return append(cur, testID) }
	if err := be.UpdateSuppressions(mutate, "suppress: add "+testID); err != nil {
		fmt.Fprintln(os.Stderr, "suppress add:", err)
		return 1
	}
	return 0
}

func HandleSuppressRemove(testID string, opts SuppressOpts) int {
	if testID == "" {
		fmt.Fprintln(os.Stderr, "suppress remove: empty test id")
		return 2
	}
	be := persist.New(opts.toPersist())
	mutate := func(cur []string) []string {
		out := make([]string, 0, len(cur))
		for _, id := range cur {
			if id != testID {
				out = append(out, id)
			}
		}
		return out
	}
	if err := be.UpdateSuppressions(mutate, "suppress: remove "+testID); err != nil {
		fmt.Fprintln(os.Stderr, "suppress remove:", err)
		return 1
	}
	return 0
}

func HandleSuppressList(opts SuppressOpts) int {
	be := persist.New(opts.toPersist())
	ids, err := be.GetSuppressions()
	if err != nil {
		fmt.Fprintln(os.Stderr, "suppress list:", err)
		return 1
	}
	for _, id := range ids {
		fmt.Println(id)
	}
	return 0
}
```

- [ ] **Step 2: Verify the package compiles**

Run: `go build ./...`

Expected: success — `main.go` does not yet dispatch to these handlers, so they are flagged as unused. Compilation passes because Go does not consider exported functions in `package main` unused.

- [ ] **Step 3: Commit**

```bash
git add suppress.go
git commit -m "feat: handlers for suppress add/remove/list"
```

---

## Task 6: Wire `main.go` dispatch

Three new arms next to `exec` and `history`.

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Edit `main.go`**

Replace the `switch` block. The new contents:

```go
func main() {
	parsed := kong.Parse(&CLI)
	cmd := parsed.Command()

	switch {
	case strings.HasPrefix(cmd, "exec"):
		os.Exit(HandleExecution(CLI.Exec.Cmd, ExecOpts{
			RepoDir:    CLI.Exec.RepoDir,
			DataBranch: CLI.Exec.DataBranch,
			Persist:    !CLI.Exec.NoPersist,
			NoRemote:   CLI.Exec.NoRemote,
			Dev:        CLI.Exec.Dev,
		}))
	case strings.HasPrefix(cmd, "history"):
		os.Exit(HandleHistory(CLI.History.Test, CLI.History.RepoDir, CLI.History.DataBranch, CLI.History.NoRemote))
	case strings.HasPrefix(cmd, "suppress add"):
		os.Exit(HandleSuppressAdd(CLI.Suppress.Add.Test, SuppressOpts{
			RepoDir:    CLI.Suppress.Add.RepoDir,
			DataBranch: CLI.Suppress.Add.DataBranch,
			NoRemote:   CLI.Suppress.Add.NoRemote,
			Dev:        CLI.Suppress.Add.Dev,
		}))
	case strings.HasPrefix(cmd, "suppress remove"):
		os.Exit(HandleSuppressRemove(CLI.Suppress.Remove.Test, SuppressOpts{
			RepoDir:    CLI.Suppress.Remove.RepoDir,
			DataBranch: CLI.Suppress.Remove.DataBranch,
			NoRemote:   CLI.Suppress.Remove.NoRemote,
			Dev:        CLI.Suppress.Remove.Dev,
		}))
	case strings.HasPrefix(cmd, "suppress list"):
		os.Exit(HandleSuppressList(SuppressOpts{
			RepoDir:    CLI.Suppress.List.RepoDir,
			DataBranch: CLI.Suppress.List.DataBranch,
			NoRemote:   CLI.Suppress.List.NoRemote,
			Dev:        CLI.Suppress.List.Dev,
		}))
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}
```

- [ ] **Step 2: Verify the build**

Run: `go build ./...`

Expected: success.

- [ ] **Step 3: End-to-end smoke test in dev mode**

Run:
```bash
mkdir -p /tmp/defrost-suppress-smoke && cd /tmp/defrost-suppress-smoke && git init -q && git commit --allow-empty -m init -q
go run github.com/bjk95/defrost suppress add --dev --repo-dir /tmp/defrost-suppress-smoke github.com/x/p/TestFoo
go run github.com/bjk95/defrost suppress list --dev --repo-dir /tmp/defrost-suppress-smoke
go run github.com/bjk95/defrost suppress remove --dev --repo-dir /tmp/defrost-suppress-smoke github.com/x/p/TestFoo
go run github.com/bjk95/defrost suppress list --dev --repo-dir /tmp/defrost-suppress-smoke
```

(Run the `go run` lines from the defrost worktree, with `--repo-dir` pointing at the smoke dir.)

Expected output:
- After add: `list` prints `github.com/x/p/TestFoo`.
- After remove: `list` prints nothing.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: dispatch suppress add/remove/list in main"
```

---

## Task 7: `exec` rewrites the exit code when every failing test is suppressed

When the adapter returns a non-zero exit code, defrost asks the backend for the suppression list, builds the set of failing test IDs (only `r.Ran && !r.Passed` results), and rewrites the exit code to zero only when every failing ID is on the list and there is at least one such ID. Build-only failures (no `r.Ran && !r.Passed` result) never trigger a rewrite.

A `GetSuppressions` error on this path is logged to stderr but does not change the exit code. Defrost must not turn a green build red because the data branch was unreachable, and equally must not turn a red build green by guessing.

**Files:**
- Modify: `exec.go`
- Create: `exec_test.go`

- [ ] **Step 1: Write the failing test**

Create `exec_test.go`:

```go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// stubAdapter lets us drive HandleExecution with a known set of results
// and a known exit code without invoking go test / pytest / jest.
type stubAdapter struct {
	results []models.TestResult
	code    int
}

func (s stubAdapter) Matches(cmd []string) bool { return cmd[0] == "stub" }
func (s stubAdapter) Run(cmd []string) ([]models.TestResult, int) {
	return s.results, s.code
}

func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "commit", "--allow-empty", "-q", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func writeDevSuppressions(t *testing.T, repoDir string, ids []string) {
	t.Helper()
	dir := filepath.Join(repoDir, persist.DevDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	be := persist.New(persist.Options{RepoDir: repoDir, Dev: true})
	if err := be.UpdateSuppressions(func([]string) []string { return ids }, ""); err != nil {
		t.Fatal(err)
	}
}

func TestExec_SuppressedSingleFailure_ExitsZero(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA"})

	stub := stubAdapter{
		results: []models.TestResult{{Id: "pkg/TestA", Ran: true, Passed: false}},
		code:    1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 0 {
		t.Errorf("want exit 0, got %d", got)
	}
}

func TestExec_AllFailuresSuppressed_ExitsZero(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA", "pkg/TestB"})

	stub := stubAdapter{
		results: []models.TestResult{
			{Id: "pkg/TestA", Ran: true, Passed: false},
			{Id: "pkg/TestB", Ran: true, Passed: false},
		},
		code: 1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 0 {
		t.Errorf("want exit 0, got %d", got)
	}
}

func TestExec_PartialSuppression_ExitUnchanged(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA"})

	stub := stubAdapter{
		results: []models.TestResult{
			{Id: "pkg/TestA", Ran: true, Passed: false},
			{Id: "pkg/TestB", Ran: true, Passed: false},
		},
		code: 1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("want exit 1, got %d", got)
	}
}

func TestExec_NoFailures_ExitUnchanged(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA"})

	stub := stubAdapter{
		results: []models.TestResult{{Id: "pkg/TestA", Ran: true, Passed: true}},
		code:    0,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 0 {
		t.Errorf("want exit 0, got %d", got)
	}
}

func TestExec_SuppressionsReadError_ExitUnchanged(t *testing.T) {
	repoDir := makeRepo(t)
	// Plant a malformed suppressions.json in the dev scratch dir. The
	// fileBackend should error on read; exec should log and preserve the
	// original exit code rather than guessing the build is green.
	dir := filepath.Join(repoDir, persist.DevDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "suppressions.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	stub := stubAdapter{
		results: []models.TestResult{{Id: "pkg/TestA", Ran: true, Passed: false}},
		code:    1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("read error must preserve exit code: want 1, got %d", got)
	}
}

func TestExec_BuildOnlyFailure_NotSuppressed(t *testing.T) {
	repoDir := makeRepo(t)
	// "buildErr" is the only ID present; suppress it. We must NOT rewrite
	// the exit code because the failure isn't a test-level failure
	// (Ran == false → the test never executed).
	writeDevSuppressions(t, repoDir, []string{"buildErr"})

	stub := stubAdapter{
		results: []models.TestResult{{Id: "buildErr", Ran: false, Passed: false}},
		code:    1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("build failure should not be suppressible: want exit 1, got %d", got)
	}
}

// runExecWithAdapter invokes the same code path HandleExecution uses, but
// with a stub adapter installed in a fresh registry. It pulls the
// "registry build + run + suppression check" logic out of HandleExecution
// for testing — see Task 7 step 3.
func runExecWithAdapter(t *testing.T, a stubAdapter, repoDir string) int {
	t.Helper()
	return execWith(a, []string{"stub", "..."}, ExecOpts{
		RepoDir: repoDir,
		Persist: false,
		Dev:     true,
	})
}

// Silence "declared but not used" if we add helpers later.
var _ = strings.Contains
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run TestExec_ -v`

Expected: FAIL — `execWith` is undefined.

- [ ] **Step 3: Refactor `exec.go` to expose the seam and add the suppression check**

Replace the body of `internal/.../exec.go` (the top-level `exec.go`) with:

```go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/javascript/jest"
	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
)

type ExecOpts struct {
	RepoDir    string
	DataBranch string
	Persist    bool
	NoRemote   bool
	Dev        bool
}

func HandleExecution(cmd []string, opts ExecOpts) int {
	if len(cmd) == 0 {
		fmt.Fprintln(os.Stderr, "exec: no command provided")
		return 2
	}

	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(pytest.Adapter{})
	reg.Register(&jest.Adapter{})

	a := reg.Find(cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "exec: unsupported test command: %q\n", cmd[0])
		return 2
	}

	return execWith(a, cmd, opts)
}

// execWith runs a known adapter and applies persistence + suppression
// rewrite. Split out from HandleExecution so tests can drive it with a
// stub adapter.
func execWith(a runner.Adapter, cmd []string, opts ExecOpts) int {
	results, code := a.Run(cmd)

	for _, r := range results {
		fmt.Printf("%+v\n", r)
	}

	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
		Dev:        opts.Dev,
	}

	if opts.Persist && len(results) > 0 {
		if err := persistResults(opts, cmd, results); err != nil {
			fmt.Fprintln(os.Stderr, "persist: failed:", err)
			if code == 0 {
				code = 1
			}
		}
	}

	if code != 0 {
		code = maybeRewriteExitCode(code, results, pOpts)
	}
	return code
}

func maybeRewriteExitCode(code int, results []models.TestResult, pOpts persist.Options) int {
	failingIDs := collectFailingTestIDs(results)
	if len(failingIDs) == 0 {
		return code
	}
	suppressed, err := persist.New(pOpts).GetSuppressions()
	if err != nil {
		fmt.Fprintln(os.Stderr, "suppress: read failed (exit code unchanged):", err)
		return code
	}
	suppSet := make(map[string]struct{}, len(suppressed))
	for _, s := range suppressed {
		suppSet[s] = struct{}{}
	}
	for _, id := range failingIDs {
		if _, ok := suppSet[id]; !ok {
			return code
		}
	}
	fmt.Fprintf(os.Stderr,
		"defrost: suppressed %d failing test(s); rewriting exit %d → 0\n",
		len(failingIDs), code)
	return 0
}

func collectFailingTestIDs(results []models.TestResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		if r.Ran && !r.Passed {
			out = append(out, r.Id)
		}
	}
	return out
}

func persistResults(opts ExecOpts, cmd []string, results []models.TestResult) error {
	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
		Dev:        opts.Dev,
	}
	run, err := persist.DetectRun(pOpts, cmd)
	if err != nil {
		return fmt.Errorf("detect run: %w", err)
	}
	if err := persist.New(pOpts).InsertNewTestResults(run, results); err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			return errors.New("no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --no-remote to persist locally only")
		}
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify**

Run: `go test . -run TestExec_ -v`

Expected: PASS — all five subtests.

Run: `go test ./...`

Expected: every test in the repo passes.

- [ ] **Step 5: Commit**

```bash
git add exec.go exec_test.go
git commit -m "feat(exec): rewrite exit code to 0 when all failing tests are suppressed"
```

---

## Final verification

- [ ] Run the full suite once more from a clean state:

```bash
go build ./...
go test ./...
```

Expected: build succeeds, every package's tests pass.

- [ ] Confirm `go run . suppress --help` shows the three subcommands.

- [ ] Spot-check the smoke flow from Task 6 once more end-to-end against `--dev` to confirm the dev-mode round-trip.

- [ ] Skim `git log --oneline` — there should be one commit per task above.
