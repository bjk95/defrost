# JavaScript/TypeScript (jest) Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Implemented (2026-04-30).** During Task 7's smoke test, the parser was found to reference fields from the wrong jest documentation surface (`testResultsProcessor` config) instead of the actual `--json --outputFile` CLI output. The fixtures in this plan (Task 1) and the parser snippet (Task 3) reflect the original incorrect shape. The merged code at `internal/javascript/jest/parser.go` plus the testdata fixtures shipped with commit `0d8b5a1` are the source of truth. See the spec's "Errata" section for the four shape corrections (`testFilePath`→`name`, nested `testResults`→`assertionResults`, no `testExecError`, no `startAt`).

**Goal:** Add jest support to defrost so `defrost exec jest tests/` (and the common JS/TS invocation forms — including `npm test` / `yarn test` / `pnpm test`) wraps a jest run and emits parsed `models.TestResult` records, with no extra setup beyond defrost + jest already installed.

**Architecture:** Slot a third adapter into the existing `internal/runner` registry. The adapter auto-injects `--json --outputFile=<tempfile>`, parses jest's JSON document with `encoding/json`, and emits `[]models.TestResult`. For `npm test` / `yarn test` / `pnpm test` style invocations (form (d)), the matcher reads `./package.json` and accepts only when `scripts.<name>` is strict-jest-shaped (allowing leading `KEY=value` env-var tokens, rejecting wrappers and composite shell commands).

**Tech Stack:** Go 1.24 (matches `go.mod`; relies on `t.Chdir` for matcher tests), `encoding/json` (stdlib), `os/exec` (stdlib), `regexp` (stdlib), `path/filepath` (stdlib). No new third-party Go dependencies. Examples use `jest@^29` and `ts-jest@^29`.

**Spec:** [docs/superpowers/specs/2026-04-30-javascript-jest-support-design.md](docs/superpowers/specs/2026-04-30-javascript-jest-support-design.md)

---

## File Structure

| Path | Status | Responsibility |
|---|---|---|
| `internal/javascript/jest/parser.go` | new | Jest JSON → `[]models.TestResult` |
| `internal/javascript/jest/parser_test.go` | new | Table-driven parser tests over fixtures |
| `internal/javascript/jest/adapter.go` | new | Matcher (pure-argv + form-(d) `package.json`) + Run flow + helpers |
| `internal/javascript/jest/adapter_test.go` | new | Table-driven matcher + script-shape tests |
| `internal/javascript/jest/testdata/pass.json` | new | Single-passing-assertion fixture |
| `internal/javascript/jest/testdata/fail.json` | new | Single-failing-assertion with `failureMessages` |
| `internal/javascript/jest/testdata/pending.json` | new | `status="pending"` (skipped) |
| `internal/javascript/jest/testdata/todo.json` | new | `status="todo"` |
| `internal/javascript/jest/testdata/ancestors.json` | new | Nested `describe` → `ancestorTitles` |
| `internal/javascript/jest/testdata/multi-file.json` | new | Two files, mixed pass/fail |
| `internal/javascript/jest/testdata/exec-error.json` | new | File with `testExecError`, no assertions |
| `internal/javascript/jest/testdata/null-duration.json` | new | `duration: null`, `startAt: null` |
| `internal/javascript/jest/testdata/empty.json` | new | `testResults: []` |
| `exec.go` | edit | Register `&jest.Adapter{}` alongside existing two adapters |
| `examples/javascript/package.json` | new | Pins `jest@^29` for the JS example |
| `examples/javascript/jest.config.js` | new | Minimal jest config |
| `examples/javascript/.gitignore` | new | Ignores `node_modules/` |
| `examples/javascript/basics.test.js` | new | 4 tests: pass, fail, async pass, async fail |
| `examples/javascript/describe.test.js` | new | 2 tests inside nested describes |
| `examples/javascript/each.test.js` | new | 3 tests via `test.each` (one fails) |
| `examples/javascript/skip.test.js` | new | 3 skipped/todo (skip, todo, xit) |
| `examples/javascript/README.md` | new | Short note |
| `examples/typescript/package.json` | new | Pins `jest@^29`, `ts-jest@^29`, `typescript@^5` |
| `examples/typescript/jest.config.js` | new | `ts-jest` preset |
| `examples/typescript/tsconfig.json` | new | Minimal TS config |
| `examples/typescript/.gitignore` | new | Ignores `node_modules/` |
| `examples/typescript/basics.test.ts` | new | Same 4 tests as JS, with type annotations |
| `examples/typescript/describe.test.ts` | new | Same 2 tests as JS |
| `examples/typescript/each.test.ts` | new | Same 3 tests as JS |
| `examples/typescript/skip.test.ts` | new | Same 3 tests as JS |
| `examples/typescript/README.md` | new | Short note |
| `.github/workflows/integration.yml` | edit | Add `javascript` and `typescript` jobs |

---

## Task 1: Jest JSON fixtures

**Files:**
- Create: `internal/javascript/jest/testdata/pass.json`
- Create: `internal/javascript/jest/testdata/fail.json`
- Create: `internal/javascript/jest/testdata/pending.json`
- Create: `internal/javascript/jest/testdata/todo.json`
- Create: `internal/javascript/jest/testdata/ancestors.json`
- Create: `internal/javascript/jest/testdata/multi-file.json`
- Create: `internal/javascript/jest/testdata/exec-error.json`
- Create: `internal/javascript/jest/testdata/null-duration.json`
- Create: `internal/javascript/jest/testdata/empty.json`

These are minimal JSON documents that mirror what jest writes when invoked with `--json --outputFile=<path>`. Only the fields the parser reads are populated. The placeholder token `__CWD__` appears in `testFilePath` values and is rewritten to the test's `t.TempDir()` at parse time so `filepath.Rel` produces predictable output.

- [ ] **Step 1: Create `pass.json`**

```json
{
  "testResults": [
    {
      "testFilePath": "__CWD__/basics.test.js",
      "testResults": [
        {
          "title": "adds correctly",
          "status": "passed",
          "ancestorTitles": [],
          "failureMessages": [],
          "duration": 1.0,
          "startAt": 1714435200000
        }
      ]
    }
  ]
}
```

- [ ] **Step 2: Create `fail.json`**

```json
{
  "testResults": [
    {
      "testFilePath": "__CWD__/basics.test.js",
      "testResults": [
        {
          "title": "adds correctly",
          "status": "failed",
          "ancestorTitles": [],
          "failureMessages": ["Error: expected 2 but got 3", "    at Object.<anonymous>"],
          "duration": 2.0,
          "startAt": 1714435200000
        }
      ]
    }
  ]
}
```

- [ ] **Step 3: Create `pending.json`**

```json
{
  "testResults": [
    {
      "testFilePath": "__CWD__/skip.test.js",
      "testResults": [
        {
          "title": "skipped test",
          "status": "pending",
          "ancestorTitles": [],
          "failureMessages": [],
          "duration": null,
          "startAt": null
        }
      ]
    }
  ]
}
```

- [ ] **Step 4: Create `todo.json`**

```json
{
  "testResults": [
    {
      "testFilePath": "__CWD__/skip.test.js",
      "testResults": [
        {
          "title": "todo test",
          "status": "todo",
          "ancestorTitles": [],
          "failureMessages": [],
          "duration": null,
          "startAt": null
        }
      ]
    }
  ]
}
```

- [ ] **Step 5: Create `ancestors.json`**

```json
{
  "testResults": [
    {
      "testFilePath": "__CWD__/describe.test.js",
      "testResults": [
        {
          "title": "adds correctly",
          "status": "passed",
          "ancestorTitles": ["math", "addition"],
          "failureMessages": [],
          "duration": 1.0,
          "startAt": 1714435200000
        }
      ]
    }
  ]
}
```

- [ ] **Step 6: Create `multi-file.json`**

```json
{
  "testResults": [
    {
      "testFilePath": "__CWD__/a.test.js",
      "testResults": [
        {
          "title": "passes",
          "status": "passed",
          "ancestorTitles": [],
          "failureMessages": [],
          "duration": 1.0,
          "startAt": 1714435200000
        }
      ]
    },
    {
      "testFilePath": "__CWD__/sub/b.test.js",
      "testResults": [
        {
          "title": "fails",
          "status": "failed",
          "ancestorTitles": [],
          "failureMessages": ["boom"],
          "duration": 2.0,
          "startAt": 1714435200000
        },
        {
          "title": "skipped",
          "status": "pending",
          "ancestorTitles": [],
          "failureMessages": [],
          "duration": null,
          "startAt": null
        }
      ]
    }
  ]
}
```

- [ ] **Step 7: Create `exec-error.json`**

```json
{
  "testResults": [
    {
      "testFilePath": "__CWD__/broken.test.js",
      "testExecError": {
        "message": "SyntaxError: Unexpected token",
        "stack": "    at Module._compile"
      },
      "testResults": []
    }
  ]
}
```

- [ ] **Step 8: Create `null-duration.json`**

```json
{
  "testResults": [
    {
      "testFilePath": "__CWD__/x.test.js",
      "testResults": [
        {
          "title": "no timing info",
          "status": "passed",
          "ancestorTitles": [],
          "failureMessages": [],
          "duration": null,
          "startAt": null
        }
      ]
    }
  ]
}
```

- [ ] **Step 9: Create `empty.json`**

```json
{
  "testResults": []
}
```

- [ ] **Step 10: Commit**

```bash
git add internal/javascript/jest/testdata/
git commit -m "test: add jest JSON fixtures for parser tests"
```

---

## Task 2: Parser tests

**Files:**
- Test: `internal/javascript/jest/parser_test.go`

The test loads each fixture, rewrites `__CWD__` to a per-test temp dir (so `filepath.Rel(cwd, …)` produces stable output), parses, and asserts on the resulting `[]models.TestResult`. Tests are written before the parser exists, so they will fail to compile — that's the intended TDD red state.

- [ ] **Step 1: Write the failing tests**

Create `internal/javascript/jest/parser_test.go`:

```go
package jest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

// loadFixture reads a fixture file, rewrites the __CWD__ placeholder to
// the given cwd, and returns the resulting bytes.
func loadFixture(t *testing.T, name, cwd string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return bytes.ReplaceAll(raw, []byte("__CWD__"), []byte(cwd))
}

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    []models.TestResult
	}{
		{
			name:    "single passing test",
			fixture: "pass.json",
			want: []models.TestResult{
				{
					Id:        "basics.test.js::adds correctly",
					Ran:       true,
					Passed:    true,
					Duration:  time.Millisecond,
					StartTime: time.UnixMilli(1714435200000),
				},
			},
		},
		{
			name:    "single failing test",
			fixture: "fail.json",
			want: []models.TestResult{
				{
					Id:        "basics.test.js::adds correctly",
					Ran:       true,
					Passed:    false,
					Duration:  2 * time.Millisecond,
					StartTime: time.UnixMilli(1714435200000),
					Output:    "Error: expected 2 but got 3\n    at Object.<anonymous>",
				},
			},
		},
		{
			name:    "pending status (skip)",
			fixture: "pending.json",
			want: []models.TestResult{
				{Id: "skip.test.js::skipped test", Ran: false, Passed: false},
			},
		},
		{
			name:    "todo status",
			fixture: "todo.json",
			want: []models.TestResult{
				{Id: "skip.test.js::todo test", Ran: false, Passed: false},
			},
		},
		{
			name:    "ancestor titles joined with ' > '",
			fixture: "ancestors.json",
			want: []models.TestResult{
				{
					Id:        "describe.test.js::math > addition > adds correctly",
					Ran:       true,
					Passed:    true,
					Duration:  time.Millisecond,
					StartTime: time.UnixMilli(1714435200000),
				},
			},
		},
		{
			name:    "multi-file mixed results",
			fixture: "multi-file.json",
			want: []models.TestResult{
				{
					Id:        "a.test.js::passes",
					Ran:       true,
					Passed:    true,
					Duration:  time.Millisecond,
					StartTime: time.UnixMilli(1714435200000),
				},
				{
					Id:        "sub/b.test.js::fails",
					Ran:       true,
					Passed:    false,
					Duration:  2 * time.Millisecond,
					StartTime: time.UnixMilli(1714435200000),
					Output:    "boom",
				},
				{
					Id:     "sub/b.test.js::skipped",
					Ran:    false,
					Passed: false,
				},
			},
		},
		{
			name:    "testExecError emits synthetic file-error result",
			fixture: "exec-error.json",
			want: []models.TestResult{
				{
					Id:     "broken.test.js::<file-error>",
					Ran:    true,
					Passed: false,
					Output: "SyntaxError: Unexpected token\n    at Module._compile",
				},
			},
		},
		{
			name:    "null duration / startAt yield zero values",
			fixture: "null-duration.json",
			want: []models.TestResult{
				{Id: "x.test.js::no timing info", Ran: true, Passed: true},
			},
		},
		{
			name:    "empty results",
			fixture: "empty.json",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			data := loadFixture(t, tc.fixture, cwd)

			got, err := Parse(bytes.NewReader(data), cwd)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d, len(want)=%d\ngot: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i].Id != tc.want[i].Id {
					t.Errorf("[%d] Id: got %q, want %q", i, got[i].Id, tc.want[i].Id)
				}
				if got[i].Ran != tc.want[i].Ran {
					t.Errorf("[%d] Ran: got %v, want %v", i, got[i].Ran, tc.want[i].Ran)
				}
				if got[i].Passed != tc.want[i].Passed {
					t.Errorf("[%d] Passed: got %v, want %v", i, got[i].Passed, tc.want[i].Passed)
				}
				if got[i].Duration != tc.want[i].Duration {
					t.Errorf("[%d] Duration: got %v, want %v", i, got[i].Duration, tc.want[i].Duration)
				}
				if !got[i].StartTime.Equal(tc.want[i].StartTime) {
					t.Errorf("[%d] StartTime: got %v, want %v", i, got[i].StartTime, tc.want[i].StartTime)
				}
				if strings.TrimSpace(got[i].Output) != strings.TrimSpace(tc.want[i].Output) {
					t.Errorf("[%d] Output: got %q, want %q", i, got[i].Output, tc.want[i].Output)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/javascript/jest/...`
Expected: build error — `Parse` undefined (the parser file doesn't exist yet).

- [ ] **Step 3: Commit**

```bash
git add internal/javascript/jest/parser_test.go
git commit -m "test: add jest parser tests (failing — parser not yet written)"
```

---

## Task 3: Parser implementation

**Files:**
- Create: `internal/javascript/jest/parser.go`

- [ ] **Step 1: Write the parser**

Create `internal/javascript/jest/parser.go`:

```go
package jest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

// jestDoc is the top-level JSON shape jest writes when invoked with
// --json --outputFile=<path>. We decode only the fields we actually use.
type jestDoc struct {
	TestResults []jestFileResult `json:"testResults"`
}

type jestFileResult struct {
	TestFilePath     string          `json:"testFilePath"`
	TestExecError    *jestExecError  `json:"testExecError"`
	AssertionResults []jestAssertion `json:"testResults"`
}

type jestExecError struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

type jestAssertion struct {
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	AncestorTitles  []string `json:"ancestorTitles"`
	FailureMessages []string `json:"failureMessages"`
	Duration        *float64 `json:"duration"`
	StartAt         *int64   `json:"startAt"`
}

// Parse reads a jest --json --outputFile document from r and returns one
// models.TestResult per assertion. Test file paths are made relative to
// cwd. Returns nil and an error only on JSON decode failure.
func Parse(r io.Reader, cwd string) ([]models.TestResult, error) {
	var doc jestDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse jest json: %w", err)
	}
	var out []models.TestResult
	for _, f := range doc.TestResults {
		rel := relPath(f.TestFilePath, cwd)
		if len(f.AssertionResults) == 0 {
			if f.TestExecError != nil {
				out = append(out, fileExecError(rel, *f.TestExecError))
			}
			continue
		}
		for _, a := range f.AssertionResults {
			out = append(out, mapAssertion(rel, a))
		}
	}
	return out, nil
}

// ParseFile is a convenience wrapper around Parse for a file on disk.
func ParseFile(path, cwd string) ([]models.TestResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, cwd)
}

func relPath(abs, cwd string) string {
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return abs
	}
	return rel
}

func mapAssertion(relFile string, a jestAssertion) models.TestResult {
	id := relFile + "::"
	if len(a.AncestorTitles) > 0 {
		id += strings.Join(a.AncestorTitles, " > ") + " > " + a.Title
	} else {
		id += a.Title
	}
	ran := a.Status == "passed" || a.Status == "failed"
	passed := a.Status == "passed"

	var duration time.Duration
	if a.Duration != nil {
		duration = time.Duration(*a.Duration * float64(time.Millisecond))
	}
	var startTime time.Time
	if a.StartAt != nil {
		startTime = time.UnixMilli(*a.StartAt)
	}

	output := ""
	if a.Status == "failed" {
		output = strings.Join(a.FailureMessages, "\n")
	}

	return models.TestResult{
		Id:        id,
		Ran:       ran,
		Passed:    passed,
		StartTime: startTime,
		Duration:  duration,
		Output:    output,
	}
}

func fileExecError(relFile string, e jestExecError) models.TestResult {
	var parts []string
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Stack != "" {
		parts = append(parts, e.Stack)
	}
	return models.TestResult{
		Id:     relFile + "::<file-error>",
		Ran:    true,
		Passed: false,
		Output: strings.Join(parts, "\n"),
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/javascript/jest/...`
Expected: PASS — all 9 parser test cases green.

- [ ] **Step 3: Commit**

```bash
git add internal/javascript/jest/parser.go
git commit -m "feat: add jest JSON parser"
```

---

## Task 4: Adapter matcher tests

**Files:**
- Test: `internal/javascript/jest/adapter_test.go`

The matcher tests cover three categories:

1. Pure-argv forms (no filesystem touch).
2. Form (d) — `npm test` / `yarn test` / `pnpm test` / `<runner> run X` — which read `./package.json`. These cases use `t.TempDir()` + `t.Chdir(dir)` to create an isolated working directory with a per-test `package.json`, and rely on Go 1.24's `t.Chdir` for automatic cleanup.
3. The `looksLikeJestScript` helper in isolation (so future debugging of strict-shape rules doesn't have to round-trip through filesystem fixtures).

- [ ] **Step 1: Write the failing tests**

Create `internal/javascript/jest/adapter_test.go`:

```go
package jest

import (
	"os"
	"path/filepath"
	"testing"
)

// writePackageJSON drops a package.json with the given scripts map into
// the current directory.
func writePackageJSON(t *testing.T, scripts map[string]string) {
	t.Helper()
	body := `{"scripts": {`
	first := true
	for k, v := range scripts {
		if !first {
			body += ", "
		}
		first = false
		// crude JSON-string escaping is fine here: tests control inputs
		body += `"` + k + `": "` + v + `"`
	}
	body += `}}`
	if err := os.WriteFile("package.json", []byte(body), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
}

func TestAdapterMatchesPureArgv(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"bare jest", []string{"jest"}, true},
		{"jest with args", []string{"jest", "tests/"}, true},
		{"jest with --json (collision handled in Run)", []string{"jest", "--json"}, true},
		{"jest with --watch (collision handled in Run)", []string{"jest", "--watch"}, true},
		{"npx jest", []string{"npx", "jest", "tests/"}, true},
		{"npx with flags before jest", []string{"npx", "-y", "jest"}, true},
		{"npx --no-install jest", []string{"npx", "--no-install", "jest"}, true},
		{"bunx jest", []string{"bunx", "jest"}, true},
		{"yarn jest", []string{"yarn", "jest", "tests/"}, true},
		{"pnpm jest", []string{"pnpm", "jest", "tests/"}, true},
		{"node_modules/.bin/jest", []string{"node_modules/.bin/jest"}, true},
		{"./node_modules/.bin/jest", []string{"./node_modules/.bin/jest"}, true},
		{"/abs/path/node_modules/.bin/jest", []string{"/repo/node_modules/.bin/jest"}, true},

		{"empty", []string{}, false},
		{"go test", []string{"go", "test", "./..."}, false},
		{"pytest", []string{"pytest", "tests/"}, false},
		{"npx other", []string{"npx", "vitest"}, false},
		{"bunx other", []string{"bunx", "vitest"}, false},
		{"yarn alone", []string{"yarn"}, false},
		{"pnpm alone", []string{"pnpm"}, false},
		{"npm alone", []string{"npm"}, false},
		{"node alone", []string{"node", "script.js"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (&Adapter{}).Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestAdapterMatchesFormD(t *testing.T) {
	cases := []struct {
		name      string
		scripts   map[string]string
		cmd       []string
		want      bool
		wantOK    bool   // value of scriptOK if matched
		wantName  string // value of scriptName if matched
	}{
		{
			name:     "npm test with scripts.test=jest",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "npm test with scripts.test=jest --config=foo.js",
			scripts:  map[string]string{"test": "jest --config=foo.js"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "env-var prefix accepted",
			scripts:  map[string]string{"test": "NODE_ENV=test jest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "multiple env-var prefix tokens accepted",
			scripts:  map[string]string{"test": "FOO=1 BAR=2 jest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "cross-env wrapper rejected by shape (matches form, scriptOK=false)",
			scripts:  map[string]string{"test": "cross-env NODE_ENV=test jest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:     "composite && rejected",
			scripts:  map[string]string{"test": "jest && eslint ."},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:     "react-scripts rejected (no jest token in head)",
			scripts:  map[string]string{"test": "react-scripts test"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:     "vitest rejected",
			scripts:  map[string]string{"test": "vitest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:     "node escape form rejected",
			scripts:  map[string]string{"test": "node --experimental-vm-modules node_modules/jest/bin/jest.js"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:    "scripts.test missing",
			scripts: map[string]string{"build": "tsc"},
			cmd:     []string{"npm", "test"},
			want:    false,
		},
		{
			name:     "npm run jest with scripts.jest=jest",
			scripts:  map[string]string{"jest": "jest"},
			cmd:      []string{"npm", "run", "jest"},
			want:     true,
			wantOK:   true,
			wantName: "jest",
		},
		{
			name:    "npm run lint with non-jest script",
			scripts: map[string]string{"lint": "eslint ."},
			cmd:     []string{"npm", "run", "lint"},
			want:    true, // form recognized; script shape failure surfaced in Run
			wantOK:  false,
			wantName: "lint",
		},
		{
			name:     "yarn test",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"yarn", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "yarn run test",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"yarn", "run", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "pnpm test",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"pnpm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "pnpm run test",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"pnpm", "run", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writePackageJSON(t, tc.scripts)

			a := &Adapter{}
			got := a.Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
			if got {
				if a.scriptName != tc.wantName {
					t.Errorf("scriptName: got %q, want %q", a.scriptName, tc.wantName)
				}
				if a.scriptOK != tc.wantOK {
					t.Errorf("scriptOK: got %v, want %v", a.scriptOK, tc.wantOK)
				}
			}
		})
	}
}

func TestAdapterMatchesFormDMissingPackageJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// no package.json written

	if (&Adapter{}).Matches([]string{"npm", "test"}) {
		t.Fatal("expected no match when package.json is missing")
	}
}

func TestAdapterMatchesFormDInvalidPackageJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	if (&Adapter{}).Matches([]string{"npm", "test"}) {
		t.Fatal("expected no match when package.json is invalid")
	}
}

func TestLooksLikeJestScript(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"jest", true},
		{"jest --config=foo.js", true},
		{"NODE_ENV=test jest", true},
		{"FOO=1 BAR=2 jest --watch=false", true},
		{"cross-env NODE_ENV=test jest", false},
		{"jest && eslint .", false},
		{"jest || true", false},
		{"jest ; true", false},
		{"jest | tee out.txt", false},
		{"jest > out.txt", false},
		{"jest < in.txt", false},
		{"jest &", false},
		{"jest `whoami`", false},
		{"jest $(whoami)", false},
		{"jest (subshell)", false},
		{"react-scripts test", false},
		{"vitest", false},
		{"node node_modules/jest/bin/jest.js", false},
		{"", false},
		{"   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if got := looksLikeJestScript(tc.value); got != tc.want {
				t.Errorf("looksLikeJestScript(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/javascript/jest/...`
Expected: build error — `Adapter`, `looksLikeJestScript` undefined.

- [ ] **Step 3: Commit**

```bash
git add internal/javascript/jest/adapter_test.go
git commit -m "test: add jest adapter matcher tests (failing — adapter not yet written)"
```

---

## Task 5: Adapter implementation

**Files:**
- Create: `internal/javascript/jest/adapter.go`

The adapter is registered as `*Adapter` (pointer) so `Matches` can stash form-(d) script-resolution state for `Run` to consume. `Matches` resets all state on every call so reuse across invocations is safe.

- [ ] **Step 1: Write the adapter**

Create `internal/javascript/jest/adapter.go`:

```go
package jest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/bjk95/defrost/internal/models"
)

// Adapter implements runner.Adapter for jest invocations. Forms supported:
//
//   - direct: `jest …`
//   - npx / bunx: `npx jest …`, `npx -y jest …`, `bunx jest …`
//   - package-runner direct: `yarn jest …`, `pnpm jest …`
//   - node_modules binary: any cmd[0] ending in node_modules/.bin/jest
//   - package-runner script (form (d)): `npm test`, `yarn test`, `pnpm test`,
//     `<runner> run <name>`, `yarn <name>` — these read ./package.json and
//     accept iff scripts.<name> has strict jest shape (env-var prefix +
//     leading "jest" token, no shell composition).
//
// Form (d) is the only place in the registry where Matches touches the
// filesystem. The script-resolution result is cached on the adapter so Run
// can surface a helpful error when scripts.<name> exists but isn't
// jest-shaped.
type Adapter struct {
	formD       bool
	scriptOK    bool
	scriptName  string
	scriptValue string
}

var envVarRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

func (a *Adapter) Matches(cmd []string) bool {
	a.reset()
	if len(cmd) == 0 {
		return false
	}
	first := cmd[0]

	if first == "jest" {
		return true
	}
	if strings.HasSuffix(first, "node_modules/.bin/jest") {
		return true
	}

	if first == "npx" || first == "bunx" {
		for _, tok := range cmd[1:] {
			if tok == "jest" {
				return true
			}
			if !strings.HasPrefix(tok, "-") {
				return false
			}
		}
		return false
	}

	switch first {
	case "yarn":
		if len(cmd) >= 2 && cmd[1] == "jest" {
			return true
		}
		var name string
		if len(cmd) >= 3 && cmd[1] == "run" {
			name = cmd[2]
		} else if len(cmd) >= 2 {
			name = cmd[1]
		}
		if name == "" {
			return false
		}
		return a.matchScript(name)
	case "pnpm":
		if len(cmd) >= 2 && cmd[1] == "jest" {
			return true
		}
		var name string
		if len(cmd) >= 2 && cmd[1] == "test" {
			name = "test"
		} else if len(cmd) >= 3 && cmd[1] == "run" {
			name = cmd[2]
		}
		if name == "" {
			return false
		}
		return a.matchScript(name)
	case "npm":
		var name string
		if len(cmd) >= 2 && cmd[1] == "test" {
			name = "test"
		} else if len(cmd) >= 3 && cmd[1] == "run" {
			name = cmd[2]
		}
		if name == "" {
			return false
		}
		return a.matchScript(name)
	}

	return false
}

func (a *Adapter) reset() {
	a.formD = false
	a.scriptOK = false
	a.scriptName = ""
	a.scriptValue = ""
}

// matchScript reads ./package.json and looks up scripts.<name>. Returns
// true iff that script entry exists. scriptOK reflects whether the script
// value passes the strict-jest-shape check; if false, Run surfaces a
// helpful error rather than running a wrong command.
func (a *Adapter) matchScript(name string) bool {
	value, ok := readPackageScript(name)
	if !ok {
		return false
	}
	a.formD = true
	a.scriptName = name
	a.scriptValue = value
	a.scriptOK = looksLikeJestScript(value)
	return true
}

func readPackageScript(name string) (string, bool) {
	data, err := os.ReadFile("package.json")
	if err != nil {
		return "", false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", false
	}
	v, ok := pkg.Scripts[name]
	return v, ok
}

// looksLikeJestScript returns true iff value is a strict jest invocation:
// optional leading KEY=value env-var tokens, then "jest", then any args
// that don't contain shell-composition operators.
func looksLikeJestScript(value string) bool {
	tokens := strings.Fields(value)
	i := 0
	for i < len(tokens) && envVarRe.MatchString(tokens[i]) {
		i++
	}
	if i >= len(tokens) || tokens[i] != "jest" {
		return false
	}
	for _, t := range tokens[i:] {
		switch t {
		case "&&", "||", ";", "|", ">", "<", "&":
			return false
		}
		if strings.ContainsAny(t, "`(") || strings.Contains(t, "$(") {
			return false
		}
	}
	return true
}

func (a *Adapter) Run(cmd []string) ([]models.TestResult, int) {
	if hasUserJSONFlag(cmd) {
		fmt.Fprintln(os.Stderr, "defrost: jest adapter requires control of --json and --outputFile; remove those flags")
		return nil, 2
	}
	if hasUserWatchFlag(cmd) {
		fmt.Fprintln(os.Stderr, "defrost: jest adapter is not compatible with --watch / --watchAll")
		return nil, 2
	}
	if a.formD && !a.scriptOK {
		fmt.Fprintf(os.Stderr,
			"defrost: scripts.%s in package.json doesn't look like a direct jest invocation; run jest via 'npx jest …' or rewrite the script\n",
			a.scriptName)
		return nil, 2
	}

	f, err := os.CreateTemp("", "defrost-jest-*.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, 1
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	args := buildArgs(cmd, path)

	child := exec.Command(cmd[0], args...)
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	runErr := child.Run()
	exitCode := 0
	switch e := runErr.(type) {
	case nil:
		// success
	case *exec.ExitError:
		exitCode = e.ExitCode()
	default:
		fmt.Fprintln(os.Stderr, "defrost:", runErr)
		return nil, 1
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, 1
	}

	results, parseErr := ParseFile(path, cwd)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return nil, 1
	}

	return results, exitCode
}

// buildArgs returns the args to pass to cmd[0], with --json and
// --outputFile=<path> appended. For npm/pnpm invocations, a "--" separator
// is inserted before the injected flags if not already present.
func buildArgs(cmd []string, jsonPath string) []string {
	rest := append([]string{}, cmd[1:]...)
	jsonFlags := []string{"--json", "--outputFile=" + jsonPath}

	needsSeparator := cmd[0] == "npm" || cmd[0] == "pnpm"
	if !needsSeparator {
		return append(rest, jsonFlags...)
	}

	for _, t := range rest {
		if t == "--" {
			return append(rest, jsonFlags...)
		}
	}
	return append(rest, append([]string{"--"}, jsonFlags...)...)
}

func hasUserJSONFlag(cmd []string) bool {
	for _, a := range cmd {
		if a == "--json" || a == "--outputFile" {
			return true
		}
		if strings.HasPrefix(a, "--outputFile=") {
			return true
		}
	}
	return false
}

func hasUserWatchFlag(cmd []string) bool {
	for _, a := range cmd {
		if a == "--watch" || a == "--watchAll" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/javascript/jest/...`
Expected: PASS — both matcher test functions and `TestLooksLikeJestScript` green.

- [ ] **Step 3: Commit**

```bash
git add internal/javascript/jest/adapter.go
git commit -m "feat: add jest adapter (matcher + Run flow)"
```

---

## Task 6: Register jest adapter in exec.go

**Files:**
- Modify: `exec.go`

- [ ] **Step 1: Add the import and the Register call**

Edit `exec.go`. The existing imports already include `golang`, `pytest`, and `runner`. Add a `jest` import and a `Register` call.

Current imports block (around lines 3–13):

```go
import (
	"errors"
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
)
```

Update to:

```go
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
```

Current registration block (around lines 28–30):

```go
	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(pytest.Adapter{})
```

Update to:

```go
	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(pytest.Adapter{})
	reg.Register(&jest.Adapter{})
```

- [ ] **Step 2: Run all tests + build to verify nothing regressed**

Run: `go build ./... && go test ./...`
Expected: PASS — all packages compile, all existing tests still green, jest tests green.

- [ ] **Step 3: Commit**

```bash
git add exec.go
git commit -m "feat: register jest adapter in exec.go"
```

---

## Task 7: JavaScript example + smoke verification

**Files:**
- Create: `examples/javascript/package.json`
- Create: `examples/javascript/jest.config.js`
- Create: `examples/javascript/.gitignore`
- Create: `examples/javascript/basics.test.js`
- Create: `examples/javascript/describe.test.js`
- Create: `examples/javascript/each.test.js`
- Create: `examples/javascript/skip.test.js`
- Create: `examples/javascript/README.md`

The example exists primarily as input for the integration CI job. It mirrors the shape of `examples/python/`. Total assertion count: **12** (4 + 2 + 3 + 3) — the CI job will assert this exact count.

- [ ] **Step 1: Create `examples/javascript/package.json`**

```json
{
  "name": "defrost-jest-example-js",
  "private": true,
  "scripts": {
    "test": "jest"
  },
  "devDependencies": {
    "jest": "^29.7.0"
  }
}
```

- [ ] **Step 2: Create `examples/javascript/jest.config.js`**

```javascript
module.exports = {
  testMatch: ["**/*.test.js"],
};
```

- [ ] **Step 3: Create `examples/javascript/.gitignore`**

```
node_modules/
package-lock.json
```

- [ ] **Step 4: Create `examples/javascript/basics.test.js`**

```javascript
test("adds correctly", () => {
  expect(1 + 1).toBe(2);
});

test("intentional failure", () => {
  expect(1 + 1).toBe(3);
});

test("async pass", async () => {
  await new Promise((r) => setTimeout(r, 1));
  expect(true).toBe(true);
});

test("async fail", async () => {
  await new Promise((r) => setTimeout(r, 1));
  throw new Error("intentional async failure");
});
```

- [ ] **Step 5: Create `examples/javascript/describe.test.js`**

```javascript
describe("math", () => {
  describe("addition", () => {
    test("adds correctly", () => {
      expect(1 + 1).toBe(2);
    });

    test("intentional failure", () => {
      expect(1 + 1).toBe(3);
    });
  });
});
```

- [ ] **Step 6: Create `examples/javascript/each.test.js`**

```javascript
test.each([
  [1, 1, 2],
  [2, 2, 4],
  [3, 3, 7], // intentional failure
])("adds %i + %i = %i", (a, b, expected) => {
  expect(a + b).toBe(expected);
});
```

- [ ] **Step 7: Create `examples/javascript/skip.test.js`**

```javascript
test.skip("skipped via test.skip", () => {
  expect(true).toBe(false);
});

test.todo("todo placeholder");

xit("skipped via xit", () => {
  expect(true).toBe(false);
});
```

- [ ] **Step 8: Create `examples/javascript/README.md`**

```markdown
# JavaScript jest example

Used by defrost's integration CI job to verify the jest adapter end-to-end.

12 assertions total: 4 in `basics.test.js`, 2 in `describe.test.js`, 3 in
`each.test.js`, 3 in `skip.test.js`. Several are intentional failures.

To run locally:

    cd examples/javascript
    npm install
    ../../defrost exec --no-persist npm test
```

- [ ] **Step 9: Smoke test locally (if `npm` and `node` are available)**

Run from the repo root:

```bash
go build -o defrost .
cd examples/javascript
npm install
../../defrost exec --no-persist npm test
```

Expected: stderr shows jest's progress output. Stdout shows 12 lines starting with `{Id:`. The defrost process exits non-zero (intentional failures). Total exit happens cleanly — no orphaned tempfiles in `$TMPDIR`.

If `npm`/`node` aren't installed locally, skip this step — CI will catch regressions in Task 9.

- [ ] **Step 10: Commit**

```bash
git add examples/javascript/
git commit -m "test: add JavaScript jest example fixtures"
```

---

## Task 8: TypeScript example + smoke verification

**Files:**
- Create: `examples/typescript/package.json`
- Create: `examples/typescript/jest.config.js`
- Create: `examples/typescript/tsconfig.json`
- Create: `examples/typescript/.gitignore`
- Create: `examples/typescript/basics.test.ts`
- Create: `examples/typescript/describe.test.ts`
- Create: `examples/typescript/each.test.ts`
- Create: `examples/typescript/skip.test.ts`
- Create: `examples/typescript/README.md`

Same shape as the JS example, but with `ts-jest` so the TS files type-check before running. Total assertions: **12** (matches JS).

- [ ] **Step 1: Create `examples/typescript/package.json`**

```json
{
  "name": "defrost-jest-example-ts",
  "private": true,
  "scripts": {
    "test": "jest"
  },
  "devDependencies": {
    "@types/jest": "^29.5.0",
    "jest": "^29.7.0",
    "ts-jest": "^29.1.0",
    "typescript": "^5.4.0"
  }
}
```

- [ ] **Step 2: Create `examples/typescript/jest.config.js`**

```javascript
module.exports = {
  preset: "ts-jest",
  testMatch: ["**/*.test.ts"],
};
```

- [ ] **Step 3: Create `examples/typescript/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "es2020",
    "module": "commonjs",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "types": ["jest", "node"]
  }
}
```

- [ ] **Step 4: Create `examples/typescript/.gitignore`**

```
node_modules/
package-lock.json
```

- [ ] **Step 5: Create `examples/typescript/basics.test.ts`**

```typescript
test("adds correctly", () => {
  const result: number = 1 + 1;
  expect(result).toBe(2);
});

test("intentional failure", () => {
  const result: number = 1 + 1;
  expect(result).toBe(3);
});

test("async pass", async () => {
  await new Promise<void>((r) => setTimeout(r, 1));
  expect(true).toBe(true);
});

test("async fail", async () => {
  await new Promise<void>((r) => setTimeout(r, 1));
  throw new Error("intentional async failure");
});
```

- [ ] **Step 6: Create `examples/typescript/describe.test.ts`**

```typescript
describe("math", () => {
  describe("addition", () => {
    test("adds correctly", () => {
      expect(1 + 1).toBe(2);
    });

    test("intentional failure", () => {
      expect(1 + 1).toBe(3);
    });
  });
});
```

- [ ] **Step 7: Create `examples/typescript/each.test.ts`**

```typescript
test.each<[number, number, number]>([
  [1, 1, 2],
  [2, 2, 4],
  [3, 3, 7], // intentional failure
])("adds %i + %i = %i", (a, b, expected) => {
  expect(a + b).toBe(expected);
});
```

- [ ] **Step 8: Create `examples/typescript/skip.test.ts`**

```typescript
test.skip("skipped via test.skip", () => {
  expect(true).toBe(false);
});

test.todo("todo placeholder");

xit("skipped via xit", () => {
  expect(true).toBe(false);
});
```

- [ ] **Step 9: Create `examples/typescript/README.md`**

```markdown
# TypeScript jest example

Used by defrost's integration CI job to verify the jest adapter against
TypeScript test files via `ts-jest`.

12 assertions total: 4 in `basics.test.ts`, 2 in `describe.test.ts`, 3 in
`each.test.ts`, 3 in `skip.test.ts`. Several are intentional failures.

To run locally:

    cd examples/typescript
    npm install
    ../../defrost exec --no-persist npm test
```

- [ ] **Step 10: Smoke test locally (if `npm` and `node` are available)**

Run from the repo root:

```bash
cd examples/typescript
npm install
../../defrost exec --no-persist npm test
```

Expected: stderr shows jest's progress output (incl. ts-jest's compile output). Stdout shows 12 lines starting with `{Id:`. The defrost process exits non-zero (intentional failures).

If `npm`/`node` aren't available locally, skip — CI catches regressions.

- [ ] **Step 11: Commit**

```bash
git add examples/typescript/
git commit -m "test: add TypeScript jest example fixtures"
```

---

## Task 9: CI integration jobs

**Files:**
- Modify: `.github/workflows/integration.yml`

Add two new jobs alongside the existing `python` job. Both `cd` into their example directory before invoking defrost so `./package.json` resolves correctly for the form-(d) matcher.

- [ ] **Step 1: Add `javascript` and `typescript` jobs**

Edit `.github/workflows/integration.yml`. Append the two new jobs after the existing `python` job, indented under the existing top-level `jobs:` key:

```yaml
  javascript:
    name: jest example (js)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: "20"

      - name: Install jest
        run: npm install
        working-directory: examples/javascript

      - name: Build defrost
        run: go build -o defrost .

      - name: Run defrost against examples/javascript
        id: run
        run: |
          set +e
          cd examples/javascript
          ../../defrost exec --no-persist npm test > out.txt 2>&1
          echo "exit=$?" >> "$GITHUB_OUTPUT"
          exit 0

      - name: Assert exit code is non-zero (intentional failures)
        run: |
          code="${{ steps.run.outputs.exit }}"
          echo "defrost exit code: $code"
          if [ "$code" -eq 0 ]; then
            echo "FAIL: expected non-zero exit (intentional failures present)"
            cat examples/javascript/out.txt
            exit 1
          fi

      - name: Assert result count matches expected
        run: |
          count=$(grep -c '^{Id:' examples/javascript/out.txt || true)
          echo "TestResult lines emitted: $count"
          if [ "$count" -ne 12 ]; then
            echo "FAIL: expected 12 TestResult lines, got $count"
            cat examples/javascript/out.txt
            exit 1
          fi

      - name: Upload defrost output for debugging
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: javascript-out
          path: examples/javascript/out.txt

  typescript:
    name: jest example (ts)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: actions/setup-node@v4
        with:
          node-version: "20"

      - name: Install jest + ts-jest
        run: npm install
        working-directory: examples/typescript

      - name: Build defrost
        run: go build -o defrost .

      - name: Run defrost against examples/typescript
        id: run
        run: |
          set +e
          cd examples/typescript
          ../../defrost exec --no-persist npm test > out.txt 2>&1
          echo "exit=$?" >> "$GITHUB_OUTPUT"
          exit 0

      - name: Assert exit code is non-zero (intentional failures)
        run: |
          code="${{ steps.run.outputs.exit }}"
          echo "defrost exit code: $code"
          if [ "$code" -eq 0 ]; then
            echo "FAIL: expected non-zero exit (intentional failures present)"
            cat examples/typescript/out.txt
            exit 1
          fi

      - name: Assert result count matches expected
        run: |
          count=$(grep -c '^{Id:' examples/typescript/out.txt || true)
          echo "TestResult lines emitted: $count"
          if [ "$count" -ne 12 ]; then
            echo "FAIL: expected 12 TestResult lines, got $count"
            cat examples/typescript/out.txt
            exit 1
          fi

      - name: Upload defrost output for debugging
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: typescript-out
          path: examples/typescript/out.txt
```

- [ ] **Step 2: Validate the YAML locally**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/integration.yml'))"`
Expected: no output (valid YAML). If `pyyaml` isn't installed, skip — GitHub will reject malformed YAML on push.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/integration.yml
git commit -m "ci: add jest integration jobs (javascript + typescript)"
```

---

## Final verification

After all tasks land:

- [ ] **Step 1: Full test pass**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 2: Build defrost**

Run: `go build -o defrost .`
Expected: success, `defrost` binary produced.

- [ ] **Step 3: Push and watch CI**

Push the branch and open a PR. The `python`, `javascript`, and `typescript` jobs should all go green (each with a non-zero defrost exit and the expected `TestResult` line count).

- [ ] **Step 4: Optional manual sanity check**

If you have `npx jest` available in some unrelated repo:

```bash
defrost exec --no-persist npx jest
```

Expected: TestResult lines emitted to stdout, non-zero defrost exit if any tests failed.
