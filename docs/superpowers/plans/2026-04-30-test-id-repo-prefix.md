# Repo-Relative Test ID Prefix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Jest and Pytest test IDs globally unique within a repository by prefixing each ID with the cwd-relative-to-repo-root, separated by `¬`. Lock in current correct handling of nested Jest describes via a regression test.

**Architecture:** Add two helpers in `internal/runner` — `RepoRelCwd()` (queries `git rev-parse --show-toplevel`) and `ApplyRepoPrefix()` (decorates a `[]models.TestResult`). Jest and Pytest adapters call `runner.ApplyRepoPrefix(results)` after parsing. Go adapter is unchanged. Jest parser is unchanged for nested describes — only a regression fixture/test is added.

**Tech Stack:** Go 1.24, `os/exec` (for `git rev-parse`), `path/filepath`, `gotest.tools/gotestsum` (already in go.mod, not directly used by this change).

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `internal/runner/prefix.go` | Create | `RepoRelCwd()` and `ApplyRepoPrefix()` helpers |
| `internal/runner/prefix_test.go` | Create | Unit tests for both helpers |
| `internal/javascript/jest/testdata/nested-describes.json` | Create | Regression fixture: same `title` under different inner `describe`s |
| `internal/javascript/jest/parser_test.go` | Modify | Add a case for the new fixture |
| `internal/javascript/jest/adapter.go` | Modify | Call `runner.ApplyRepoPrefix(results)` after `ParseFile` succeeds |
| `internal/python/pytest/adapter.go` | Modify | Call `runner.ApplyRepoPrefix(results)` after `ParseFile` succeeds |

The Go adapter, the Adapter interface, and `exec.go` are intentionally untouched. The decoration concern lives entirely in the two adapters that need it.

---

## Task 1: Add `RepoRelCwd` and `ApplyRepoPrefix` helpers

**Files:**
- Create: `internal/runner/prefix.go`
- Test: `internal/runner/prefix_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/prefix_test.go`:

```go
package runner

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bjk95/defrost/internal/models"
)

func TestRepoRelCwdAtRepoRoot(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	t.Chdir(dir)

	if got := RepoRelCwd(); got != "" {
		t.Errorf("RepoRelCwd at repo root: got %q, want %q", got, "")
	}
}

func TestRepoRelCwdInSubdir(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	sub := filepath.Join(root, "examples", "typescript")
	mkdirAll(t, sub)
	t.Chdir(sub)

	want := filepath.Join("examples", "typescript")
	if got := RepoRelCwd(); got != want {
		t.Errorf("RepoRelCwd in subdir: got %q, want %q", got, want)
	}
}

func TestRepoRelCwdNotInRepo(t *testing.T) {
	dir := t.TempDir()
	// Block git from walking above dir's parent. With no .git in dir or
	// its (test-temp) parent, `git rev-parse --show-toplevel` will fail
	// and RepoRelCwd must return "".
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)

	if got := RepoRelCwd(); got != "" {
		t.Errorf("RepoRelCwd outside repo: got %q, want %q", got, "")
	}
}

func TestApplyRepoPrefixEmptyPrefix(t *testing.T) {
	// Use the "not in repo" setup so RepoRelCwd returns "".
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)

	results := []models.TestResult{{Id: "a"}, {Id: "b"}}
	ApplyRepoPrefix(results)

	if results[0].Id != "a" || results[1].Id != "b" {
		t.Errorf("ApplyRepoPrefix with empty prefix mutated IDs: %+v", results)
	}
}

func TestApplyRepoPrefixDecorates(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	sub := filepath.Join(root, "examples", "typescript")
	mkdirAll(t, sub)
	t.Chdir(sub)

	results := []models.TestResult{
		{Id: "basics.test.ts::adds correctly"},
		{Id: "basics.test.ts::subtracts"},
	}
	ApplyRepoPrefix(results)

	prefix := filepath.Join("examples", "typescript") + "¬"
	if results[0].Id != prefix+"basics.test.ts::adds correctly" {
		t.Errorf("results[0].Id = %q, want %q", results[0].Id, prefix+"basics.test.ts::adds correctly")
	}
	if results[1].Id != prefix+"basics.test.ts::subtracts" {
		t.Errorf("results[1].Id = %q, want %q", results[1].Id, prefix+"basics.test.ts::subtracts")
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "--quiet", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := exec.Command("mkdir", "-p", dir).Run(); err != nil {
		t.Fatalf("mkdir -p %s: %v", dir, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runner/... -run 'RepoRelCwd|ApplyRepoPrefix' -v`
Expected: FAIL with "undefined: RepoRelCwd" / "undefined: ApplyRepoPrefix".

- [ ] **Step 3: Implement the helpers**

Create `internal/runner/prefix.go`:

```go
package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bjk95/defrost/internal/models"
)

// RepoRelCwd returns the path from the git repo root to the current
// working directory, suitable for prefixing test IDs to make them
// globally unique within the repository. Returns "" when:
//   - not inside a git repo,
//   - cwd is the repo root,
//   - any git or filesystem call fails.
//
// The empty return is the "no decoration" signal — callers must treat
// it as such, not as an error.
//
// Both cwd and the git toplevel are passed through filepath.EvalSymlinks
// so callers don't see spurious "../" segments on systems where one
// path is symlinked and the other isn't (notably macOS, where the
// default temp dir is /var/folders/... → /private/var/folders/...).
func RepoRelCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return ""
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ""
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || rel == "." {
		return ""
	}
	return rel
}

// ApplyRepoPrefix decorates each result's Id with RepoRelCwd() + "¬".
// No-op when RepoRelCwd returns "" (run from repo root, or not in a
// repo). Mutates results in place.
//
// `¬` (U+00AC) is used as the separator because it never appears in
// path separators, Python module names, JS test descriptions, or shell
// metacharacters defrost touches — collision-free as a sentinel.
func ApplyRepoPrefix(results []models.TestResult) {
	prefix := RepoRelCwd()
	if prefix == "" {
		return
	}
	for i := range results {
		results[i].Id = prefix + "¬" + results[i].Id
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runner/... -run 'RepoRelCwd|ApplyRepoPrefix' -v`
Expected: PASS for all five tests.

- [ ] **Step 5: Run the full test suite to confirm no regressions**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/prefix.go internal/runner/prefix_test.go
git commit -m "feat(runner): add RepoRelCwd and ApplyRepoPrefix helpers"
```

---

## Task 2: Add Jest nested-describes regression fixture and test

**Goal:** Lock in the current correct behavior — two tests sharing the same `title` under different inner `describe`s produce distinct IDs. No production code change.

**Files:**
- Create: `internal/javascript/jest/testdata/nested-describes.json`
- Modify: `internal/javascript/jest/parser_test.go` (add one case to the `TestParse` table)

- [ ] **Step 1: Write the failing test**

Create `internal/javascript/jest/testdata/nested-describes.json`:

```json
{
  "testResults": [
    {
      "name": "__CWD__/nested.test.js",
      "status": "passed",
      "message": "",
      "assertionResults": [
        {
          "title": "same",
          "status": "passed",
          "ancestorTitles": ["outer", "A"],
          "failureMessages": [],
          "duration": 1.0
        },
        {
          "title": "same",
          "status": "passed",
          "ancestorTitles": ["outer", "B"],
          "failureMessages": [],
          "duration": 2.0
        }
      ]
    }
  ]
}
```

Modify `internal/javascript/jest/parser_test.go` — add this case to the `cases` table in `TestParse` (insert it after the existing "ancestor titles joined with ' > '" case at line 70-81):

```go
		{
			name:    "nested describes with same title produce distinct IDs",
			fixture: "nested-describes.json",
			want: []models.TestResult{
				{
					Id:       "nested.test.js::outer > A > same",
					Ran:      true,
					Passed:   true,
					Duration: time.Millisecond,
				},
				{
					Id:       "nested.test.js::outer > B > same",
					Ran:      true,
					Passed:   true,
					Duration: 2 * time.Millisecond,
				},
			},
		},
```

- [ ] **Step 2: Run the test to verify it passes immediately**

Run: `go test ./internal/javascript/jest/... -run TestParse -v`
Expected: PASS, including the new case. The fixture is consumed unchanged because the existing parser already joins all `ancestorTitles` with ` > ` ([parser.go:80-84](internal/javascript/jest/parser.go:80)).

If it fails, **stop** — that means the parser does not in fact handle deep nesting correctly, which contradicts the spec's "no behavior change" assumption. Investigate before proceeding.

- [ ] **Step 3: Commit**

```bash
git add internal/javascript/jest/testdata/nested-describes.json internal/javascript/jest/parser_test.go
git commit -m "test(jest): regression test for nested describes with same title"
```

---

## Task 3: Wire `ApplyRepoPrefix` into the Jest adapter

**Files:**
- Modify: `internal/javascript/jest/adapter.go` — one new import + one new call after `ParseFile` succeeds.

- [ ] **Step 1: Add the import and the call**

In `internal/javascript/jest/adapter.go`, the imports block currently reads (lines 3-12):

```go
import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/bjk95/defrost/internal/models"
)
```

Replace it with (adds `runner`):

```go
import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)
```

Then in `(*Adapter).Run`, the parse block currently reads (lines 234-238):

```go
	results, parseErr := ParseFile(path, cwd)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return nil, 1
	}
```

Replace with:

```go
	results, parseErr := ParseFile(path, cwd)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return nil, 1
	}
	runner.ApplyRepoPrefix(results)
```

The unused `models` import is unaffected — `models.TestResult` is the return type. The new `runner` import sits next to it.

- [ ] **Step 2: Run the jest package tests**

Run: `go test ./internal/javascript/jest/... -v`
Expected: PASS — none of the existing tests exercise `Run()` end-to-end, so the new call is exercised only at integration time. Tests for `Matches`, `buildArgs`, `looksLikeJestScript`, `hasUserJSONFlag`, `hasUserWatchFlag`, and `Parse` continue to pass unchanged.

- [ ] **Step 3: Build to confirm imports resolve**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/javascript/jest/adapter.go
git commit -m "feat(jest): prefix test IDs with repo-relative cwd"
```

---

## Task 4: Wire `ApplyRepoPrefix` into the Pytest adapter

**Files:**
- Modify: `internal/python/pytest/adapter.go` — one new import + one new call after `ParseFile` succeeds.

- [ ] **Step 1: Add the import and the call**

In `internal/python/pytest/adapter.go`, the imports block currently reads (lines 3-11):

```go
import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/bjk95/defrost/internal/models"
)
```

Replace with (adds `runner`):

```go
import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)
```

Then in `Adapter.Run`, the parse block currently reads (lines 80-84):

```go
	results, parseErr := ParseFile(path)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return nil, 1
	}
```

Replace with:

```go
	results, parseErr := ParseFile(path)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return nil, 1
	}
	runner.ApplyRepoPrefix(results)
```

- [ ] **Step 2: Run the pytest package tests**

Run: `go test ./internal/python/pytest/... -v`
Expected: PASS — existing tests (`TestAdapterMatches`, `TestParse` cases) all pass unchanged; the new call sits in the un-tested `Run()` path.

- [ ] **Step 3: Build to confirm imports resolve**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/python/pytest/adapter.go
git commit -m "feat(pytest): prefix test IDs with repo-relative cwd"
```

---

## Verification Checklist (run after Task 4)

- [ ] **Full test suite passes:** `go test ./...` exits 0.
- [ ] **Build is clean:** `go build ./...` exits 0.
- [ ] **Spec coverage confirmed:**
  - `RepoRelCwd()` exists and is tested (Task 1).
  - `ApplyRepoPrefix()` exists, uses `¬`, and is tested (Task 1).
  - Jest adapter calls `runner.ApplyRepoPrefix` after parsing (Task 3).
  - Pytest adapter calls `runner.ApplyRepoPrefix` after parsing (Task 4).
  - Go adapter is unchanged.
  - Nested-describes regression test in place (Task 2).
- [ ] **Commit log shows four atomic commits**, one per task:
  ```
  feat(pytest): prefix test IDs with repo-relative cwd
  feat(jest): prefix test IDs with repo-relative cwd
  test(jest): regression test for nested describes with same title
  feat(runner): add RepoRelCwd and ApplyRepoPrefix helpers
  ```
