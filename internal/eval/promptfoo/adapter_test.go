package promptfoo

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
		{"direct", []string{"promptfoo", "eval"}, true},
		{"direct with config", []string{"promptfoo", "eval", "-c", "promptfooconfig.yaml"}, true},
		{"npx", []string{"npx", "promptfoo", "eval"}, true},
		{"npx with -y", []string{"npx", "-y", "promptfoo", "eval"}, true},
		{"npx with @latest", []string{"npx", "promptfoo@latest", "eval"}, true},
		{"pnpm", []string{"pnpm", "promptfoo", "eval"}, true},
		{"pnpm dlx", []string{"pnpm", "dlx", "promptfoo", "eval"}, true},
		{"yarn", []string{"yarn", "promptfoo", "eval"}, true},
		{"node_modules bin", []string{"./node_modules/.bin/promptfoo", "eval"}, true},
		{"npx --package", []string{"npx", "--package", "promptfoo", "--", "promptfoo", "eval"}, true},
		{"npx --package no exec", []string{"npx", "--package", "promptfoo"}, false},
		{"missing eval subcommand", []string{"promptfoo"}, false},
		{"different subcommand", []string{"promptfoo", "view"}, false},
		{"different subcommand between promptfoo and eval", []string{"promptfoo", "init", "eval"}, false},
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

func TestBuildArgsInjectsOutputFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "-c", "promptfooconfig.yaml"}, "/tmp/results.json")
	want := []string{"eval", "-c", "promptfooconfig.yaml", "--output", "/tmp/results.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsUserLongFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "--output", "user.json"}, "/tmp/results.json")
	want := []string{"eval", "--output", "user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsUserShortFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "-o", "user.json"}, "/tmp/results.json")
	want := []string{"eval", "-o", "user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsLongFlagWithEquals(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "--output=user.json"}, "/tmp/results.json")
	want := []string{"eval", "--output=user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestUserOutputPath(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"eval", "--output", "user.json"}, "user.json"},
		{[]string{"eval", "-o", "user.json"}, "user.json"},
		{[]string{"eval", "--output=user.json"}, "user.json"},
		{[]string{"eval", "-c", "x.yaml"}, ""},
	}
	for _, tc := range cases {
		got := userOutputPath(tc.args)
		if got != tc.want {
			t.Fatalf("userOutputPath(%v) = %q, want %q", tc.args, got, tc.want)
		}
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

// fakeChildScript writes a small shell script that copies a fixture to the
// path given after `--output`. Used to stub out `promptfoo eval` in Run tests.
func fakeChildScript(t *testing.T, fixture string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-promptfoo")
	fixtureSrc := filepath.Join("testdata", fixture)
	body := `#!/usr/bin/env bash
set -e
out=""
for ((i=1;i<=$#;i++)); do
    a="${!i}"
    if [[ "$a" == "--output" || "$a" == "-o" ]]; then
        n=$((i+1))
        out="${!n}"
        break
    elif [[ "$a" == --output=* ]]; then
        out="${a#*=}"
        break
    fi
done
if [[ -z "$out" ]]; then
    echo "fake-promptfoo: no --output flag" >&2
    exit 2
fi
cp "` + fixtureSrc + `" "$out"
`
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake-promptfoo: %v", err)
	}
	abs, err := filepath.Abs(scriptPath)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func TestRunHappyPath(t *testing.T) {
	bin := fakeChildScript(t, "single_assertion.json")
	a := &Adapter{}
	tests, metrics, code := a.Run([]string{bin, "eval", "-c", "x.yaml"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if len(tests) != 1 || !tests[0].Passed {
		t.Fatalf("expected 1 passing test, got %v", tests)
	}
	if len(metrics) != 1 || metrics[0].Name != "eval.contains" {
		t.Fatalf("expected eval.contains metric, got %v", metrics)
	}
}

func TestRunFailingChildPropagatesExit(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-fail")
	body := `#!/usr/bin/env bash
exit 7
`
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake-fail: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)
	a := &Adapter{}
	_, _, code := a.Run([]string{abs, "eval"})
	if code != 7 {
		t.Fatalf("expected exit 7, got %d", code)
	}
}

func TestRunUserSuppliedOutputClearsStaleContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	// Pre-existing file at the user-supplied path with stale content.
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.json")
	stale := []byte(`{"results":{"results":[{"success":true,"vars":{"stale":"data"},"gradingResult":{"pass":true,"score":1,"componentResults":[{"pass":true,"score":1,"assertion":{"type":"contains"}}]}}]}}`)
	if err := os.WriteFile(userPath, stale, 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	// Fake child that exits 0 but writes nothing — so any stale content
	// would survive and be parsed if defrost didn't clear it pre-run.
	scriptPath := filepath.Join(dir, "fake-noop")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)

	a := &Adapter{}
	tests, _, code := a.Run([]string{abs, "eval", "--output", userPath})
	// Defrost must surface this as failure, not parse the stale data.
	if code == 0 {
		t.Fatalf("expected non-zero exit (stale file should not be ingested), got 0")
	}
	if len(tests) != 0 {
		t.Fatalf("expected zero tests (stale data must not be ingested), got %d: %+v", len(tests), tests)
	}
}

func TestRunPassthroughOnNonJSONUserOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	// Fake child that exits 0 — we only care that defrost ran it as
	// passthrough rather than trying to parse the user's HTML output.
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-child")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)

	a := &Adapter{}
	tests, metrics, code := a.Run([]string{abs, "eval", "--output", "report.html"})
	if code != 0 {
		t.Fatalf("expected exit 0 (passthrough propagates child exit), got %d", code)
	}
	if len(tests) != 0 {
		t.Fatalf("expected no test results in passthrough mode, got %d", len(tests))
	}
	if len(metrics) != 0 {
		t.Fatalf("expected no metrics in passthrough mode, got %d", len(metrics))
	}
}

func TestRunChildSucceededButNoOutputFileBumpsExit(t *testing.T) {
	// Fake child that exits 0 but never writes the output file. Defrost
	// must surface this as a non-zero exit so a CI run can't silently
	// report success when defrost itself failed to read results.
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-noop")
	body := `#!/usr/bin/env bash
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake-noop: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)
	a := &Adapter{}
	_, _, code := a.Run([]string{abs, "eval"})
	if code != 1 {
		t.Fatalf("expected exit 1 (defrost-internal floor), got %d", code)
	}
}
