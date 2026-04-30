package inspect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMatches(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"direct", []string{"inspect", "eval", "task.py"}, true},
		{"direct with model", []string{"inspect", "eval", "task.py", "--model", "openai/gpt-4o"}, true},
		{"absolute path", []string{"/usr/local/bin/inspect", "eval", "task.py"}, true},
		{"venv path", []string{"./.venv/bin/inspect", "eval", "task.py"}, true},
		{"missing eval subcommand", []string{"inspect"}, false},
		{"different subcommand", []string{"inspect", "view"}, false},
		{"different subcommand at index 1", []string{"inspect", "init"}, false},
		{"python module form", []string{"python", "-m", "inspect_ai", "eval", "task.py"}, true},
		{"python3 module form", []string{"python3", "-m", "inspect_ai", "eval", "task.py"}, true},
		{"python3.12 module form", []string{"python3.12", "-m", "inspect_ai", "eval", "task.py"}, true},
		{"python -m wrong module", []string{"python", "-m", "inspect", "eval", "task.py"}, false},
		{"poetry run", []string{"poetry", "run", "inspect", "eval", "task.py"}, true},
		{"uv run", []string{"uv", "run", "inspect", "eval", "task.py"}, true},
		{"pipenv run", []string{"pipenv", "run", "inspect", "eval", "task.py"}, true},
		{"poetry run wrong tool", []string{"poetry", "run", "pytest", "eval", "task.py"}, false},
		{"poetry run inspect missing eval", []string{"poetry", "run", "inspect", "view"}, false},
		{"echo arg-mistake", []string{"echo", "inspect", "eval"}, false},
		{"unrelated cmd", []string{"jest"}, false},
		{"empty", []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adapter{}
			got := a.Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestHasUserLogDir(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"eval", "task.py"}, false},
		{[]string{"eval", "task.py", "--log-dir", "/tmp/x"}, true},
		{[]string{"eval", "task.py", "--log-dir=/tmp/x"}, true},
		{[]string{"eval", "task.py", "--log-format", "json"}, false},
	}
	for _, tc := range cases {
		got := hasUserLogDir(tc.args)
		if got != tc.want {
			t.Fatalf("hasUserLogDir(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestHasUserLogFormat(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"eval", "task.py"}, false},
		{[]string{"eval", "task.py", "--log-format", "json"}, true},
		{[]string{"eval", "task.py", "--log-format=eval"}, true},
		{[]string{"eval", "task.py", "--log-dir", "/tmp/x"}, false},
	}
	for _, tc := range cases {
		got := hasUserLogFormat(tc.args)
		if got != tc.want {
			t.Fatalf("hasUserLogFormat(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestBuildArgsInjectsLogDirAndFormat(t *testing.T) {
	args := buildArgs([]string{"inspect", "eval", "task.py", "--limit", "10"}, "/tmp/inspect-out")
	want := []string{"eval", "task.py", "--limit", "10", "--log-dir", "/tmp/inspect-out", "--log-format", "json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fakeChildScript writes a small shell script that finds --log-dir in argv,
// then copies a fixture into that directory as <basename>. Used to stub
// out `inspect eval` in Run tests.
func fakeChildScript(t *testing.T, fixture, outName string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-inspect")
	fixtureSrc, err := filepath.Abs(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("abs fixture: %v", err)
	}
	body := `#!/usr/bin/env bash
set -e
log_dir=""
for ((i=1;i<=$#;i++)); do
    a="${!i}"
    if [[ "$a" == "--log-dir" ]]; then
        n=$((i+1))
        log_dir="${!n}"
        break
    elif [[ "$a" == --log-dir=* ]]; then
        log_dir="${a#*=}"
        break
    fi
done
if [[ -z "$log_dir" ]]; then
    echo "fake-inspect: no --log-dir flag" >&2
    exit 2
fi
mkdir -p "$log_dir"
cp "` + fixtureSrc + `" "$log_dir/` + outName + `"
`
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake-inspect: %v", err)
	}
	abs, err := filepath.Abs(scriptPath)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func TestRunHappyPath(t *testing.T) {
	bin := fakeChildScript(t, "smoke.json", "task_2026.json")
	a := &Adapter{}
	tests, metrics, code := a.Run([]string{bin, "eval", "task.py"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if !tests[0].Passed || tests[1].Passed {
		t.Fatalf("expected sample_1 pass / sample_2 fail, got %v / %v", tests[0].Passed, tests[1].Passed)
	}
}

func TestRunPassthroughOnUserLogDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-noop")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)

	a := &Adapter{}
	tests, metrics, code := a.Run([]string{abs, "eval", "task.py", "--log-dir", "/tmp/user-controlled"})
	if code != 0 {
		t.Fatalf("expected exit 0 (passthrough propagates child exit), got %d", code)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected zero results in passthrough mode, got %d tests / %d metrics", len(tests), len(metrics))
	}
}

func TestRunPassthroughOnUserLogFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-noop")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)

	a := &Adapter{}
	tests, metrics, code := a.Run([]string{abs, "eval", "task.py", "--log-format=eval"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected zero results in passthrough mode, got %d tests / %d metrics", len(tests), len(metrics))
	}
}

func TestRunFailingChildPropagatesExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-fail")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 7\n"), 0o755); err != nil {
		t.Fatalf("write fake-fail: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)
	a := &Adapter{}
	_, _, code := a.Run([]string{abs, "eval", "task.py"})
	if code != 7 {
		t.Fatalf("expected exit 7, got %d", code)
	}
}

func TestRunNoOutputFilesPreservesExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	// Fake child exits 0 but writes nothing into the log dir. The adapter
	// should preserve the exit code (the child's own signal) and emit a
	// stderr warning, returning no per-test results.
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-noop")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)
	a := &Adapter{}
	tests, metrics, code := a.Run([]string{abs, "eval", "task.py"})
	if code != 0 {
		t.Fatalf("expected exit 0 preserved, got %d", code)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected zero results, got %d / %d", len(tests), len(metrics))
	}
}

func TestRunMultipleJSONFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	// Inspect can write multiple JSON files when given multiple task files.
	// The adapter must aggregate samples across all files.
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-inspect")
	smoke, _ := filepath.Abs(filepath.Join("testdata", "smoke.json"))
	stringID, _ := filepath.Abs(filepath.Join("testdata", "string_id.json"))
	body := `#!/usr/bin/env bash
set -e
log_dir=""
for ((i=1;i<=$#;i++)); do
    a="${!i}"
    if [[ "$a" == "--log-dir" ]]; then
        n=$((i+1))
        log_dir="${!n}"
        break
    fi
done
mkdir -p "$log_dir"
cp "` + smoke + `" "$log_dir/task_a.json"
cp "` + stringID + `" "$log_dir/task_b.json"
`
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)
	a := &Adapter{}
	tests, metrics, code := a.Run([]string{abs, "eval", "a.py", "b.py"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	// 2 samples from smoke + 2 from string_id = 4 total
	if len(tests) != 4 {
		t.Fatalf("expected 4 tests aggregated, got %d", len(tests))
	}
	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics aggregated, got %d", len(metrics))
	}
}
