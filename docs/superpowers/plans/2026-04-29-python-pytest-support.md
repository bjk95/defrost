# Python (pytest) Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add pytest support to defrost so `defrost exec pytest tests/` (and the common Python invocation forms) wraps a pytest run and emits parsed `models.TestResult` records — with no extra setup beyond defrost + pytest already installed.

**Architecture:** Introduce a small adapter registry under `internal/runner/`. Refactor the existing Go path into a registered adapter. Add a new pytest adapter that auto-injects `--junitxml=<tempfile> -o junit_family=xunit2 -o junit_logging=system-out`, parses the resulting JUnit XML with `encoding/xml`, and emits `[]models.TestResult`.

**Tech Stack:** Go 1.22+, `encoding/xml` (stdlib), `os/exec` (stdlib), `regexp` (stdlib). No new third-party dependencies.

**Spec:** [docs/superpowers/specs/2026-04-29-python-pytest-support-design.md](docs/superpowers/specs/2026-04-29-python-pytest-support-design.md)

---

## File Structure

| Path | Status | Responsibility |
|---|---|---|
| `internal/runner/adapter.go` | new | `Adapter` interface + `Registry` type with `Register` / `Find` |
| `internal/runner/registry_test.go` | new | Registry behavior tests |
| `internal/golang/adapter.go` | new | `Adapter` impl wrapping existing Go logic |
| `internal/golang/executor.go` | edit | `ExecuteGoTest` returns `int` exit code instead of calling `os.Exit` |
| `internal/python/pytest/adapter.go` | new | `Adapter` impl: matcher + run flow + collision check |
| `internal/python/pytest/parser.go` | new | JUnit XML → `[]models.TestResult` |
| `internal/python/pytest/parser_test.go` | new | Table-driven parser tests over fixtures |
| `internal/python/pytest/adapter_test.go` | new | Table-driven matcher tests |
| `internal/python/pytest/testdata/pass.xml` | new | Single-passing-test fixture |
| `internal/python/pytest/testdata/fail.xml` | new | Single-failing-test fixture |
| `internal/python/pytest/testdata/error.xml` | new | Single-errored-test fixture |
| `internal/python/pytest/testdata/skip.xml` | new | Single-skipped-test fixture |
| `internal/python/pytest/testdata/mixed.xml` | new | Mixed suite with `<system-out>` / `<system-err>` |
| `internal/python/pytest/testdata/empty.xml` | new | Empty suite |
| `exec.go` | edit | Replace `switch` with `runner.Find`; explicit registration of both adapters |

---

## Task 1: Runner registry

**Files:**
- Create: `internal/runner/adapter.go`
- Test: `internal/runner/registry_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/registry_test.go`:

```go
package runner

import "testing"

type fakeAdapter struct {
	name    string
	matchOn string
	exit    int
}

func (a fakeAdapter) Matches(cmd []string) bool {
	return len(cmd) > 0 && cmd[0] == a.matchOn
}

func (a fakeAdapter) Run(cmd []string) int { return a.exit }

func TestFindReturnsNilWhenEmpty(t *testing.T) {
	r := NewRegistry()
	if got := r.Find([]string{"go", "test"}); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestFindReturnsNilWhenNoMatch(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeAdapter{name: "go", matchOn: "go"})
	if got := r.Find([]string{"pytest"}); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestFindReturnsFirstMatch(t *testing.T) {
	r := NewRegistry()
	first := fakeAdapter{name: "first", matchOn: "go", exit: 1}
	second := fakeAdapter{name: "second", matchOn: "go", exit: 2}
	r.Register(first)
	r.Register(second)

	got := r.Find([]string{"go", "test"})
	if got == nil {
		t.Fatal("expected match, got nil")
	}
	if got.Run(nil) != 1 {
		t.Fatalf("expected first adapter (exit=1), got exit=%d", got.Run(nil))
	}
}

func TestFindMatchesByCmd(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeAdapter{name: "go", matchOn: "go"})
	r.Register(fakeAdapter{name: "pytest", matchOn: "pytest"})

	got := r.Find([]string{"pytest", "tests/"})
	if got == nil {
		t.Fatal("expected match, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/...`
Expected: FAIL — package `runner` does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/runner/adapter.go`:

```go
package runner

// Adapter wraps a single language/test-framework integration. Implementations
// inspect a defrost-exec argv and decide whether they handle it (Matches),
// then run the underlying child command and return its exit code (Run).
type Adapter interface {
	Matches(cmd []string) bool
	Run(cmd []string) int
}

// Registry holds an ordered list of adapters. The first adapter whose Matches
// returns true wins. Order is registration order.
type Registry struct {
	adapters []Adapter
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(a Adapter) {
	r.adapters = append(r.adapters, a)
}

func (r *Registry) Find(cmd []string) Adapter {
	for _, a := range r.adapters {
		if a.Matches(cmd) {
			return a
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runner/...`
Expected: PASS — all four tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/adapter.go internal/runner/registry_test.go
git commit -m "feat: add runner adapter registry"
```

---

## Task 2: Refactor Go executor to return exit code

**Files:**
- Modify: `internal/golang/executor.go`

The existing `ExecuteGoTest` calls `os.Exit` directly. To slot into `Adapter.Run`, it needs to return an `int`. Caller (`exec.go`) is updated in Task 4; in this task we change the signature only — the call site simply ignores the return value, keeping the build green.

- [ ] **Step 1: Replace executor body**

Replace the entire contents of `internal/golang/executor.go` with:

```go
package golang

import (
	"fmt"
	"os"
	"os/exec"
)

func ExecuteGoTest(cmd []string) int {
	c := exec.Command(cmd[0], cmd[1:]...)
	stdout, err := c.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	c.Stderr = os.Stderr

	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	results, parseErr := Parse(stdout)
	waitErr := c.Wait()

	for _, r := range results {
		fmt.Printf("%+v\n", r)
	}

	if parseErr != nil {
		fmt.Fprintln(os.Stderr, parseErr)
	}
	if e, ok := waitErr.(*exec.ExitError); ok {
		return e.ExitCode()
	}
	if waitErr != nil {
		return 1
	}
	return 0
}
```

- [ ] **Step 2: Verify the build still compiles**

Run: `go build ./...`
Expected: success. (`exec.go` still calls `golang.ExecuteGoTest(cmd)` — Go silently allows discarding a return value, so the call site keeps working.)

- [ ] **Step 3: Verify Go-side tests still pass**

Run: `go test ./...`
Expected: pass (the new registry tests from Task 1 still pass; nothing else changed in test surface).

- [ ] **Step 4: Commit**

```bash
git add internal/golang/executor.go
git commit -m "refactor: ExecuteGoTest returns exit code instead of calling os.Exit"
```

---

## Task 3: Go adapter struct

**Files:**
- Create: `internal/golang/adapter.go`

- [ ] **Step 1: Create the adapter file**

Create `internal/golang/adapter.go`:

```go
package golang

// Adapter implements runner.Adapter for `go test ...` invocations.
//
// Matches the literal form `go test [args...]`. Tighter than a prefix-only
// match: `go run`, `go build`, etc. fall through to no-match.
type Adapter struct{}

func (Adapter) Matches(cmd []string) bool {
	return len(cmd) >= 2 && cmd[0] == "go" && cmd[1] == "test"
}

func (Adapter) Run(cmd []string) int {
	return ExecuteGoTest(cmd)
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/golang/adapter.go
git commit -m "feat: wrap Go executor as runner.Adapter"
```

---

## Task 4: Wire registry into exec.go

**Files:**
- Modify: `exec.go`

- [ ] **Step 1: Replace `exec.go` contents**

Replace the entire contents of `exec.go` with:

```go
package main

import (
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/runner"
)

func HandleExecution(cmd []string) {
	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})

	a := reg.Find(cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "defrost: no adapter for %q\n", cmd)
		os.Exit(2)
	}
	os.Exit(a.Run(cmd))
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Smoke-test that the Go path still works**

Run: `go run . exec go test ./internal/runner/... -json`
Expected: registry tests run, output includes a `{Id:... Ran:true Passed:true ...}` line per test, exit code 0. (`-json` is required because the existing Go parser consumes `go test`'s JSON event stream.)

- [ ] **Step 4: Commit**

```bash
git add exec.go
git commit -m "refactor: dispatch via runner registry instead of switch"
```

---

## Task 5: JUnit XML fixtures

**Files:**
- Create: `internal/python/pytest/testdata/pass.xml`
- Create: `internal/python/pytest/testdata/fail.xml`
- Create: `internal/python/pytest/testdata/error.xml`
- Create: `internal/python/pytest/testdata/skip.xml`
- Create: `internal/python/pytest/testdata/mixed.xml`
- Create: `internal/python/pytest/testdata/empty.xml`

These are minimal `xunit2`-family JUnit XML documents that mirror what pytest writes when invoked with `-o junit_family=xunit2 -o junit_logging=system-out`. Keep them small and self-contained; they're the authoritative input for the parser tests.

- [ ] **Step 1: Create `pass.xml`**

```xml
<?xml version="1.0" encoding="utf-8"?>
<testsuites>
  <testsuite name="pytest" errors="0" failures="0" skipped="0" tests="1" time="0.001" timestamp="2026-04-29T00:00:00.000000" hostname="x">
    <testcase classname="tests.test_a" name="test_pass" time="0.001"/>
  </testsuite>
</testsuites>
```

- [ ] **Step 2: Create `fail.xml`**

```xml
<?xml version="1.0" encoding="utf-8"?>
<testsuites>
  <testsuite name="pytest" errors="0" failures="1" skipped="0" tests="1" time="0.002" timestamp="2026-04-29T00:00:00.000000" hostname="x">
    <testcase classname="tests.test_a" name="test_fail" time="0.002">
      <failure message="assert 1 == 2" type="AssertionError">Traceback line</failure>
    </testcase>
  </testsuite>
</testsuites>
```

- [ ] **Step 3: Create `error.xml`**

```xml
<?xml version="1.0" encoding="utf-8"?>
<testsuites>
  <testsuite name="pytest" errors="1" failures="0" skipped="0" tests="1" time="0.003" timestamp="2026-04-29T00:00:00.000000" hostname="x">
    <testcase classname="tests.test_a" name="test_error" time="0.003">
      <error message="setup failure" type="ValueError">boom</error>
    </testcase>
  </testsuite>
</testsuites>
```

- [ ] **Step 4: Create `skip.xml`**

```xml
<?xml version="1.0" encoding="utf-8"?>
<testsuites>
  <testsuite name="pytest" errors="0" failures="0" skipped="1" tests="1" time="0.000" timestamp="2026-04-29T00:00:00.000000" hostname="x">
    <testcase classname="tests.test_a" name="test_skip" time="0.000">
      <skipped message="not relevant" type="pytest.skip"/>
    </testcase>
  </testsuite>
</testsuites>
```

- [ ] **Step 5: Create `mixed.xml`**

```xml
<?xml version="1.0" encoding="utf-8"?>
<testsuites>
  <testsuite name="pytest" errors="0" failures="1" skipped="1" tests="3" time="0.005" timestamp="2026-04-29T00:00:00.000000" hostname="x">
    <testcase classname="tests.test_a" name="test_pass" time="0.001">
      <system-out>hello stdout</system-out>
    </testcase>
    <testcase classname="tests.test_a" name="test_fail" time="0.002">
      <failure message="assert 1 == 2" type="AssertionError">stack</failure>
      <system-out>captured stdout</system-out>
      <system-err>captured stderr</system-err>
    </testcase>
    <testcase classname="tests.test_a" name="test_skip" time="0.000">
      <skipped message="not today" type="pytest.skip"/>
    </testcase>
  </testsuite>
</testsuites>
```

- [ ] **Step 6: Create `empty.xml`**

```xml
<?xml version="1.0" encoding="utf-8"?>
<testsuites>
  <testsuite name="pytest" errors="0" failures="0" skipped="0" tests="0" time="0.000" timestamp="2026-04-29T00:00:00.000000" hostname="x">
  </testsuite>
</testsuites>
```

- [ ] **Step 7: Commit**

```bash
git add internal/python/pytest/testdata/
git commit -m "test: add JUnit XML fixtures for pytest parser"
```

---

## Task 6: Parser tests

**Files:**
- Test: `internal/python/pytest/parser_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/python/pytest/parser_test.go`:

```go
package pytest

import (
	"strings"
	"testing"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    []models.TestResult
	}{
		{
			name:    "single passing test",
			fixture: "testdata/pass.xml",
			want: []models.TestResult{
				{Id: "tests.test_a::test_pass", Ran: true, Passed: true, Duration: time.Millisecond},
			},
		},
		{
			name:    "single failing test",
			fixture: "testdata/fail.xml",
			want: []models.TestResult{
				{
					Id: "tests.test_a::test_fail", Ran: true, Passed: false,
					Duration: 2 * time.Millisecond,
					Output:   "assert 1 == 2\nTraceback line",
				},
			},
		},
		{
			name:    "single errored test",
			fixture: "testdata/error.xml",
			want: []models.TestResult{
				{
					Id: "tests.test_a::test_error", Ran: true, Passed: false,
					Duration: 3 * time.Millisecond,
					Output:   "setup failure\nboom",
				},
			},
		},
		{
			name:    "single skipped test",
			fixture: "testdata/skip.xml",
			want: []models.TestResult{
				{Id: "tests.test_a::test_skip", Ran: false, Passed: false, Duration: 0},
			},
		},
		{
			name:    "mixed suite with system-out and system-err",
			fixture: "testdata/mixed.xml",
			want: []models.TestResult{
				{
					Id: "tests.test_a::test_pass", Ran: true, Passed: true,
					Duration: time.Millisecond,
					Output:   "hello stdout",
				},
				{
					Id: "tests.test_a::test_fail", Ran: true, Passed: false,
					Duration: 2 * time.Millisecond,
					Output:   "assert 1 == 2\nstack\ncaptured stdout\ncaptured stderr",
				},
				{
					Id: "tests.test_a::test_skip", Ran: false, Passed: false, Duration: 0,
				},
			},
		},
		{
			name:    "empty suite",
			fixture: "testdata/empty.xml",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseFile(tc.fixture)
			if err != nil {
				t.Fatalf("ParseFile(%s) error: %v", tc.fixture, err)
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
				if strings.TrimSpace(got[i].Output) != strings.TrimSpace(tc.want[i].Output) {
					t.Errorf("[%d] Output: got %q, want %q", i, got[i].Output, tc.want[i].Output)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/python/pytest/...`
Expected: FAIL — package `pytest` does not exist (or `Parse`/`ParseFile` undefined).

- [ ] **Step 3: Skip — implementation lives in Task 7**

Move on to Task 7 to make these pass.

---

## Task 7: Parser implementation

**Files:**
- Create: `internal/python/pytest/parser.go`

- [ ] **Step 1: Write the implementation**

Create `internal/python/pytest/parser.go`:

```go
package pytest

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

type junitDoc struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Cases []junitCase `xml:"testcase"`
}

type junitCase struct {
	ClassName string         `xml:"classname,attr"`
	Name      string         `xml:"name,attr"`
	Time      float64        `xml:"time,attr"`
	Failure   *junitMessage  `xml:"failure"`
	Error     *junitMessage  `xml:"error"`
	Skipped   *junitMessage  `xml:"skipped"`
	SystemOut string         `xml:"system-out"`
	SystemErr string         `xml:"system-err"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// Parse reads a JUnit XML document (xunit2 family) from r and returns one
// models.TestResult per <testcase>. Returns nil and an error only on XML decode
// failure.
func Parse(r io.Reader) ([]models.TestResult, error) {
	var doc junitDoc
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse junit xml: %w", err)
	}
	var out []models.TestResult
	for _, s := range doc.Suites {
		for _, c := range s.Cases {
			out = append(out, mapCase(c))
		}
	}
	return out, nil
}

// ParseFile is a convenience wrapper around Parse for a file on disk.
func ParseFile(path string) ([]models.TestResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

func mapCase(c junitCase) models.TestResult {
	id := c.ClassName + "::" + c.Name
	ran := c.Skipped == nil
	passed := c.Failure == nil && c.Error == nil && c.Skipped == nil
	duration := time.Duration(c.Time * float64(time.Second))

	var parts []string
	if c.Failure != nil {
		parts = append(parts, formatMessage(*c.Failure))
	}
	if c.Error != nil {
		parts = append(parts, formatMessage(*c.Error))
	}
	if c.SystemOut != "" {
		parts = append(parts, c.SystemOut)
	}
	if c.SystemErr != "" {
		parts = append(parts, c.SystemErr)
	}

	return models.TestResult{
		Id:       id,
		Ran:      ran,
		Passed:   passed,
		Duration: duration,
		Output:   strings.Join(parts, "\n"),
	}
}

func formatMessage(m junitMessage) string {
	body := strings.TrimSpace(m.Body)
	switch {
	case m.Message == "" && body == "":
		return ""
	case m.Message == "":
		return body
	case body == "":
		return m.Message
	default:
		return m.Message + "\n" + body
	}
}
```

- [ ] **Step 2: Run parser tests**

Run: `go test ./internal/python/pytest/...`
Expected: all six sub-cases of `TestParse` pass.

- [ ] **Step 3: Commit**

```bash
git add internal/python/pytest/parser.go internal/python/pytest/parser_test.go
git commit -m "feat: parse JUnit XML into models.TestResult"
```

---

## Task 8: Pytest matcher tests

**Files:**
- Test: `internal/python/pytest/adapter_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/python/pytest/adapter_test.go`:

```go
package pytest

import "testing"

func TestAdapterMatches(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"bare pytest", []string{"pytest"}, true},
		{"pytest with args", []string{"pytest", "tests/"}, true},
		{"python -m pytest", []string{"python", "-m", "pytest", "tests/"}, true},
		{"python3 -m pytest", []string{"python3", "-m", "pytest", "tests/"}, true},
		{"python3.12 -m pytest", []string{"python3.12", "-m", "pytest", "tests/"}, true},
		{"poetry run pytest", []string{"poetry", "run", "pytest", "tests/"}, true},
		{"uv run pytest", []string{"uv", "run", "pytest", "tests/"}, true},
		{"pipenv run pytest", []string{"pipenv", "run", "pytest", "tests/"}, true},
		{"pytest with junitxml (collision handled in Run)", []string{"pytest", "--junitxml=foo.xml"}, true},

		{"go test", []string{"go", "test", "./..."}, false},
		{"empty", []string{}, false},
		{"python alone", []string{"python"}, false},
		{"python -m other", []string{"python", "-m", "unittest"}, false},
		{"poetry run other", []string{"poetry", "run", "ruff"}, false},
		{"poetry without run", []string{"poetry", "install"}, false},
		{"python4", []string{"python4", "-m", "pytest"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Adapter{}.Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/python/pytest/...`
Expected: FAIL — `Adapter` undefined.

---

## Task 9: Pytest adapter implementation

**Files:**
- Create: `internal/python/pytest/adapter.go`

- [ ] **Step 1: Write the implementation**

Create `internal/python/pytest/adapter.go`:

```go
package pytest

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Adapter implements runner.Adapter for pytest invocations in any of three
// forms: direct (`pytest ...`), module (`python(3.x)? -m pytest ...`), or
// tool-runner (`{poetry,uv,pipenv} run pytest ...`).
type Adapter struct{}

var pythonRe = regexp.MustCompile(`^python(3(\.\d+)?)?$`)

var toolRunners = map[string]bool{
	"poetry": true,
	"uv":     true,
	"pipenv": true,
}

func (Adapter) Matches(cmd []string) bool {
	if len(cmd) == 0 {
		return false
	}
	if cmd[0] == "pytest" {
		return true
	}
	if pythonRe.MatchString(cmd[0]) && len(cmd) >= 3 && cmd[1] == "-m" && cmd[2] == "pytest" {
		return true
	}
	if toolRunners[cmd[0]] && len(cmd) >= 3 && cmd[1] == "run" && cmd[2] == "pytest" {
		return true
	}
	return false
}

func (Adapter) Run(cmd []string) int {
	if hasUserJunitxml(cmd) {
		fmt.Fprintln(os.Stderr, "defrost: pytest adapter requires control of --junitxml; remove your --junitxml flag")
		return 2
	}

	f, err := os.CreateTemp("", "defrost-pytest-*.xml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return 1
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	args := append([]string{}, cmd[1:]...)
	args = append(args,
		"--junitxml="+path,
		"-o", "junit_family=xunit2",
		"-o", "junit_logging=system-out",
	)

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
		return 1
	}

	results, parseErr := ParseFile(path)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return 1
	}

	for _, r := range results {
		fmt.Printf("%+v\n", r)
	}

	return exitCode
}

func hasUserJunitxml(cmd []string) bool {
	for _, a := range cmd {
		if a == "--junitxml" || a == "--junit-xml" {
			return true
		}
		if strings.HasPrefix(a, "--junitxml=") || strings.HasPrefix(a, "--junit-xml=") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run matcher tests**

Run: `go test ./internal/python/pytest/...`
Expected: both `TestParse` and `TestAdapterMatches` pass.

- [ ] **Step 3: Commit**

```bash
git add internal/python/pytest/adapter.go internal/python/pytest/adapter_test.go
git commit -m "feat: add pytest adapter (matcher + junitxml run flow)"
```

---

## Task 10: Register pytest adapter in exec.go

**Files:**
- Modify: `exec.go`

- [ ] **Step 1: Add the pytest registration**

Replace `exec.go` contents with:

```go
package main

import (
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
)

func HandleExecution(cmd []string) {
	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(pytest.Adapter{})

	a := reg.Find(cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "defrost: no adapter for %q\n", cmd)
		os.Exit(2)
	}
	os.Exit(a.Run(cmd))
}
```

- [ ] **Step 2: Verify all tests still pass**

Run: `go test ./...`
Expected: all tests pass (registry, parser, matcher).

- [ ] **Step 3: Commit**

```bash
git add exec.go
git commit -m "feat: register pytest adapter alongside Go adapter"
```

---

## Task 11: End-to-end smoke test

**Files:** none (verification only)

This task confirms the wired-up pipeline works against a real pytest invocation. It does not modify code; if the smoke surfaces a bug, capture a fix as its own task.

- [ ] **Step 1: Verify pytest is on PATH**

Run: `pytest --version`
Expected: prints a version like `pytest 8.x.x` (or similar). If pytest is not installed, install it (`pip install pytest` or `uv pip install pytest`) before continuing.

- [ ] **Step 2: Create a tiny throwaway pytest project**

Run:

```bash
mkdir -p /tmp/defrost-smoke && cd /tmp/defrost-smoke
cat > test_demo.py <<'EOF'
def test_pass():
    assert 1 == 1

def test_fail():
    assert 1 == 2

import pytest
@pytest.mark.skip(reason="demo skip")
def test_skip():
    pass
EOF
```

- [ ] **Step 3: Run defrost against pytest**

From the worktree root:

```bash
go run . exec pytest /tmp/defrost-smoke/test_demo.py
```

Expected:
- pytest's normal terminal output streams live to your terminal.
- After pytest finishes, three `{Id:... Ran:... Passed:... ...}` lines print, one per test:
  - `tests.test_demo::test_pass` (or similar; pytest may use just `test_demo`) — `Ran:true Passed:true`
  - `tests.test_demo::test_fail` — `Ran:true Passed:false`, `Output` containing the assertion error
  - `tests.test_demo::test_skip` — `Ran:false Passed:false`
- Exit code matches pytest's (non-zero, because `test_fail` failed).

- [ ] **Step 4: Run defrost with the `python -m pytest` form**

```bash
go run . exec python -m pytest /tmp/defrost-smoke/test_demo.py
```

Expected: same output shape as Step 3.

- [ ] **Step 5: Run defrost with a colliding `--junitxml`**

```bash
go run . exec pytest /tmp/defrost-smoke/test_demo.py --junitxml=/tmp/oops.xml
```

Expected: stderr says `defrost: pytest adapter requires control of --junitxml; remove your --junitxml flag`. Exit code 2. No `{Id:...}` lines.

- [ ] **Step 6: Confirm Go path still works**

```bash
go run . exec go test ./... -json
```

Expected: defrost still parses and prints Go test results as before. (`-json` is required for the Go parser to consume the JSON event stream.)

- [ ] **Step 7: Clean up smoke artefacts**

```bash
rm -rf /tmp/defrost-smoke
```

- [ ] **Step 8: Final commit (if any tweaks were needed)**

If steps 3–6 surfaced no issues, no commit is needed for Task 11 — the work has already been committed. If a tweak was required to make the smoke pass, commit it with a focused message describing the fix.
