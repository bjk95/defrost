# Defrost Wrapper MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `defrost exec <cmd...>` — a Go CLI that runs a child command (typically `go test ... -json`), parses each stdout line as a `TestEvent`, and re-emits parsed events as JSON while passing non-JSON lines through verbatim.

**Architecture:** Single Go module at the repo root. `package main` lives at the root and contains `main.go` (entry) plus `cli.go` (arg parsing + child orchestration). Subpackage `internal/resultcollector` owns the `TestEvent` schema and the `Stream(r, out)` parse-and-re-emit loop. Stdlib only.

**Tech Stack:** Go 1.23 stdlib (`bufio`, `encoding/json`, `io`, `os`, `os/exec`, `time`, `bytes`, `strings`, `testing`). No external dependencies.

**Spec:** [docs/superpowers/specs/2026-04-29-defrost-wrapper-mvp-design.md](../specs/2026-04-29-defrost-wrapper-mvp-design.md)

---

## Task 1: TestEvent struct + Stream — JSON round-trip

Replace the placeholder `GetResults()` with the `TestEvent` schema and a `Stream` function that decodes one JSON line per `TestEvent` and re-emits it. This task only handles the success path; non-JSON input causes `Stream` to return an error. Task 2 adds the passthrough fallback.

**Files:**
- Modify (full rewrite): `internal/resultcollector/golang.go`
- Modify (full rewrite): `internal/resultcollector/golang_test.go`

- [ ] **Step 1: Write the failing test**

Replace the entire contents of `internal/resultcollector/golang_test.go` with:

```go
package resultcollector

import (
	"bytes"
	"strings"
	"testing"
)

func TestStream_ValidJSONRoundTrips(t *testing.T) {
	in := `{"Action":"pass","Package":"p","Test":"TestX","Elapsed":0.001}` + "\n"
	want := `{"Time":"0001-01-01T00:00:00Z","Action":"pass","Package":"p","Test":"TestX","Elapsed":0.001,"Output":""}` + "\n"

	var out bytes.Buffer
	if err := Stream(strings.NewReader(in), &out); err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if got := out.String(); got != want {
		t.Errorf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}
```

Note on the expected output: the input JSON omits `Time` and `Output`. After decoding into `TestEvent` and re-marshalling, those fields appear with their Go zero values (`time.Time{}` serialises as `"0001-01-01T00:00:00Z"`; `string` zero is `""`). Field order is the struct's declaration order. This is intentional — re-marshalling proves we parsed the line.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/brad/dev/defrost/go && go test ./result_collector/
```

Expected: compile failure (`undefined: Stream`, `undefined: TestEvent` from the test, plus `GetResults` no longer being referenced is fine).

- [ ] **Step 3: Write the minimal implementation**

Replace the entire contents of `internal/resultcollector/golang.go` with:

```go
package resultcollector

import (
	"bufio"
	"encoding/json"
	"io"
	"time"
)

type TestEvent struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Elapsed float64
	Output  string
}

func Stream(r io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		var ev TestEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return err
		}
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := out.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return sc.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/brad/dev/defrost/go && go test ./result_collector/
```

Expected: `ok  github.com/bjk95/defrost/internal/resultcollector ...`

- [ ] **Step 5: Commit**

```bash
cd /Users/brad/dev/defrost
git add internal/resultcollector/golang.go internal/resultcollector/golang_test.go
git commit -m "feat(result_collector): add TestEvent + Stream JSON round-trip"
```

---

## Task 2: Non-JSON passthrough

Lines that are not valid `TestEvent` JSON (build errors, panics, framing output) must be written through verbatim instead of returning an error.

**Files:**
- Modify: `internal/resultcollector/golang.go` (change the JSON-error branch in `Stream`)
- Modify: `internal/resultcollector/golang_test.go` (add a test)

- [ ] **Step 1: Write the failing test**

Append this test to `internal/resultcollector/golang_test.go`:

```go
func TestStream_NonJSONPassesThrough(t *testing.T) {
	in := "FAIL\tgithub.com/example/pkg [build failed]\n"
	want := in

	var out bytes.Buffer
	if err := Stream(strings.NewReader(in), &out); err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if got := out.String(); got != want {
		t.Errorf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/brad/dev/defrost/go && go test ./result_collector/ -run TestStream_NonJSONPassesThrough
```

Expected: FAIL — `Stream returned error: invalid character 'F' looking for beginning of value`

- [ ] **Step 3: Update Stream to fall through on decode error**

Replace the body of `Stream` in `internal/resultcollector/golang.go` with:

```go
func Stream(r io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Bytes()
		var ev TestEvent
		if err := json.Unmarshal(line, &ev); err == nil {
			b, err := json.Marshal(ev)
			if err != nil {
				return err
			}
			line = b
		}
		if _, err := out.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return sc.Err()
}
```

(The change: the `json.Unmarshal` error branch no longer returns; on success `line` is replaced with the re-marshalled bytes; on failure `line` remains the original input and is written as-is.)

- [ ] **Step 4: Run all tests to verify both pass**

```bash
cd /Users/brad/dev/defrost/go && go test ./result_collector/ -v
```

Expected: `--- PASS: TestStream_ValidJSONRoundTrips` and `--- PASS: TestStream_NonJSONPassesThrough`.

- [ ] **Step 5: Commit**

```bash
cd /Users/brad/dev/defrost
git add internal/resultcollector/golang.go internal/resultcollector/golang_test.go
git commit -m "feat(result_collector): pass non-JSON lines through verbatim"
```

---

## Task 3: Mixed input preserves order

Add a regression test that feeds `Stream` a sequence of valid + invalid + valid lines and asserts the output preserves their order. The implementation from Task 2 should already pass this; the test exists so the contract is documented and won't silently regress.

**Files:**
- Modify: `internal/resultcollector/golang_test.go` (add a test)

- [ ] **Step 1: Add the test**

Append to `internal/resultcollector/golang_test.go`:

```go
func TestStream_MixedInputPreservesOrder(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"run","Package":"p","Test":"A"}`,
		`FAIL\tp [build failed]`,
		`{"Action":"pass","Package":"p","Test":"A","Elapsed":0.5}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := Stream(strings.NewReader(in), &out); err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 output lines, got %d: %q", len(lines), out.String())
	}

	var first, third TestEvent
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Errorf("line 0 not valid TestEvent JSON: %v (%q)", err, lines[0])
	}
	if first.Test != "A" || first.Action != "run" {
		t.Errorf("line 0 fields wrong: got %+v", first)
	}
	if lines[1] != `FAIL\tp [build failed]` {
		t.Errorf("line 1 not passed through verbatim: %q", lines[1])
	}
	if err := json.Unmarshal([]byte(lines[2]), &third); err != nil {
		t.Errorf("line 2 not valid TestEvent JSON: %v (%q)", err, lines[2])
	}
	if third.Test != "A" || third.Action != "pass" || third.Elapsed != 0.5 {
		t.Errorf("line 2 fields wrong: got %+v", third)
	}
}
```

This test needs `encoding/json` in the test file's imports. Update the import block at the top of `internal/resultcollector/golang_test.go` to:

```go
import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run all tests**

```bash
cd /Users/brad/dev/defrost/go && go test ./result_collector/ -v
```

Expected: all three tests pass. No source changes needed in `golang.go`.

- [ ] **Step 3: Commit**

```bash
cd /Users/brad/dev/defrost
git add internal/resultcollector/golang_test.go
git commit -m "test(result_collector): assert mixed input order is preserved"
```

---

## Task 4: CLI wiring (main.go + cli.go)

Replace the placeholder `main.go` and create `cli.go`. `main.go` is the entrypoint that calls `run(os.Args)` and propagates the exit code. `cli.go` validates `defrost exec <cmd...>`, spawns the child, pipes child stdout into `Stream`, wires child stderr to the wrapper's stderr, waits for the child, and returns the child's exit code.

No unit tests in this task per the spec — the verification step is a manual smoke test against the wrapper's own tests.

**Files:**
- Modify (full rewrite): `go/main.go`
- Create: `go/cli.go`

- [ ] **Step 1: Replace `go/main.go`**

Replace the entire contents of `go/main.go` with:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	code, err := run(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}
```

- [ ] **Step 2: Create `go/cli.go`**

Create `go/cli.go` with:

```go
package main

import (
	"fmt"
	"os"
	"os/exec"

	resultcollector "github.com/bjk95/defrost/internal/resultcollector"
)

func run(args []string) (int, error) {
	if len(args) < 3 || args[1] != "exec" {
		return 2, fmt.Errorf("usage: defrost exec <command> [args...]")
	}

	cmd := exec.Command(args[2], args[3:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 1, err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 1, err
	}

	streamErr := resultcollector.Stream(stdout, os.Stdout)
	waitErr := cmd.Wait()

	if streamErr != nil {
		return 1, streamErr
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	if waitErr != nil {
		return 1, waitErr
	}
	return 0, nil
}
```

Notes for the engineer:
- `cmd.StdoutPipe()` must be called *before* `cmd.Start()`; calling `Wait()` after `Start()` closes the pipe.
- `Stream` reads until EOF on the child's stdout. The child closes stdout when it exits, so `Stream` returns first; `Wait()` then collects the exit status.
- `*exec.ExitError` is the error type returned by `Wait()` when the child exited non-zero. Its `ExitCode()` method returns the child's exit code, which we propagate unchanged.

- [ ] **Step 3: Build the binary**

```bash
cd /Users/brad/dev/defrost/go && go build -o defrost
```

Expected: no output, `defrost` binary appears in `go/`.

- [ ] **Step 4: Smoke test — run the wrapper against its own tests**

```bash
cd /Users/brad/dev/defrost/go && ./defrost exec go test ./result_collector/ -json
```

Expected: a stream of one-line JSON objects on stdout, each containing fields like `Time`, `Action`, `Package`, `Test`, `Elapsed`, `Output`. Final exit code: `0` (verify with `echo $?` after the command).

Sanity check that the first event line is valid JSON with all six fields:

```bash
cd /Users/brad/dev/defrost/go && ./defrost exec go test ./result_collector/ -json | head -1
```

Expected: a single line containing `"Time"`, `"Action"`, `"Package"`, `"Test"`, `"Elapsed"`, and `"Output"`. The line should start with `{` and end with `}`. If any field is missing or the line is not valid JSON, `Stream` is emitting malformed output.

- [ ] **Step 5: Smoke test — exit code propagation**

Confirm the wrapper preserves a non-zero exit code. Run the wrapper against a deliberately failing command:

```bash
cd /Users/brad/dev/defrost/go && ./defrost exec false; echo "exit=$?"
```

Expected: `exit=1`.

- [ ] **Step 6: Smoke test — usage error**

```bash
cd /Users/brad/dev/defrost/go && ./defrost; echo "exit=$?"
```

Expected: `usage: defrost exec <command> [args...]` on stderr, `exit=2`.

- [ ] **Step 7: Commit**

```bash
cd /Users/brad/dev/defrost
git add go/main.go go/cli.go
git commit -m "feat(cli): add 'defrost exec' wrapper around child commands"
```

- [ ] **Step 8: Add the built binary to .gitignore (housekeeping)**

If `go/defrost` is now tracked or shows in `git status`, add it to `.gitignore`:

```bash
cd /Users/brad/dev/defrost
[ -f .gitignore ] || touch .gitignore
grep -qxF 'go/defrost' .gitignore || echo 'go/defrost' >> .gitignore
git add .gitignore
git status
```

If `.gitignore` was created or modified:

```bash
git commit -m "chore: ignore built defrost binary"
```

If `.gitignore` already had the entry, skip the commit.

---

## Verification summary

After all four tasks the repo state should be:

- `go/main.go` — entrypoint, calls `run`.
- `go/cli.go` — `run(args)` with `exec` subcommand.
- `internal/resultcollector/golang.go` — `TestEvent` struct + `Stream` function.
- `internal/resultcollector/golang_test.go` — three tests, all passing.
- `go/defrost` — buildable, gitignored.
- `docs/superpowers/specs/2026-04-29-defrost-wrapper-mvp-design.md` — already committed.

Final verification: from `/Users/brad/dev/defrost/go`, `go test ./...` returns 0, and `./defrost exec go test ./result_collector/ -json` emits parsed JSON events on stdout.
