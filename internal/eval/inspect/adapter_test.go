package inspect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

func TestMatches(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"direct", []string{"inspect", "eval", "task.py"}, true},
		{"direct with model", []string{"inspect", "eval", "task.py", "--model", "openai/gpt-4o"}, true},
		{"direct with extra flags", []string{"inspect", "eval", "task.py", "--limit", "10", "--epochs", "2"}, true},
		{"absolute path inspect", []string{"/usr/local/bin/inspect", "eval", "task.py"}, true},
		{"python -m inspect_ai eval", []string{"python", "-m", "inspect_ai", "eval", "task.py"}, true},
		{"python3 -m inspect_ai eval", []string{"python3", "-m", "inspect_ai", "eval", "task.py"}, true},
		{"python3.12 -m inspect_ai eval", []string{"python3.12", "-m", "inspect_ai", "eval", "task.py"}, true},
		{"poetry run inspect eval", []string{"poetry", "run", "inspect", "eval", "task.py"}, true},
		{"uv run inspect eval", []string{"uv", "run", "inspect", "eval", "task.py"}, true},
		{"pipenv run inspect eval", []string{"pipenv", "run", "inspect", "eval", "task.py"}, true},
		{"poetry run absolute inspect", []string{"poetry", "run", "/usr/bin/inspect", "eval", "task.py"}, true},
		{"missing eval subcommand", []string{"inspect"}, false},
		{"different subcommand", []string{"inspect", "view"}, false},
		{"different subcommand inspect score", []string{"inspect", "score"}, false},
		{"unrelated cmd", []string{"pytest"}, false},
		{"empty", []string{}, false},
		{"echo arg-mistake", []string{"echo", "inspect", "eval"}, false},
		{"sh -c inspect eval", []string{"sh", "-c", "inspect eval task.py"}, false},
		{"python -m wrong module", []string{"python", "-m", "pytest", "eval"}, false},
		{"python -m inspect_ai wrong subcommand", []string{"python", "-m", "inspect_ai", "view"}, false},
		{"poetry run wrong tool", []string{"poetry", "run", "pytest"}, false},
		{"poetry run inspect missing eval", []string{"poetry", "run", "inspect"}, false},
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

func TestHasFlag(t *testing.T) {
	cases := []struct {
		args []string
		flag string
		want bool
	}{
		{[]string{"eval", "task.py"}, "--log-dir", false},
		{[]string{"eval", "task.py", "--log-dir", "/tmp/x"}, "--log-dir", true},
		{[]string{"eval", "task.py", "--log-dir=/tmp/x"}, "--log-dir", true},
		{[]string{"eval", "task.py", "--log-format", "json"}, "--log-format", true},
		{[]string{"eval", "task.py", "--log-format=json"}, "--log-format", true},
		{[]string{"eval", "task.py", "--model", "gpt-4o"}, "--log-dir", false},
	}
	for _, tc := range cases {
		got := hasFlag(tc.args, tc.flag)
		if got != tc.want {
			t.Errorf("hasFlag(%v, %q) = %v, want %v", tc.args, tc.flag, got, tc.want)
		}
	}
}

func TestBuildArgs(t *testing.T) {
	args := buildArgs([]string{"inspect", "eval", "task.py", "--model", "openai/gpt-4o"}, "/tmp/logs")
	want := []string{"eval", "task.py", "--model", "openai/gpt-4o", "--log-dir", "/tmp/logs", "--log-format", "json"}
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

// fakeChildScript writes a small bash script that reads --log-dir from its
// args and copies the named fixture there as <task>.json. Used to stub out
// `inspect eval` in Run tests.
func fakeChildScript(t *testing.T, fixtures ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-inspect")
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
`
	for i, fx := range fixtures {
		fixtureSrc, err := filepath.Abs(filepath.Join("testdata", fx))
		if err != nil {
			t.Fatalf("abs: %v", err)
		}
		body += "cp \"" + fixtureSrc + "\" \"$log_dir/log_" + itoa(i) + ".json\"\n"
	}
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake-inspect: %v", err)
	}
	abs, err := filepath.Abs(scriptPath)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

func TestRunHappyPath(t *testing.T) {
	bin := fakeChildScript(t, "smoke.json")

	// Pin cwd outside any git repo so RepoRelCwd() returns "" and the
	// metric scope is driven entirely by the user-supplied task file.
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)

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
	// Task file comes from the JSON's `eval.task_file`, not the cmd-line.
	want := "eval.tasks/capitals.py.capital_cities.match"
	if metrics[0].Name != want {
		t.Fatalf("expected %s, got %q", want, metrics[0].Name)
	}
}

func TestRunMultipleLogFiles(t *testing.T) {
	// Inspect can write multiple log files when given multiple tasks; the
	// adapter must aggregate results across all *.json files in the log dir
	// AND attribute each log's metrics to that log's own task_file (not the
	// first task file from cmd args).
	bin := fakeChildScript(t, "smoke.json", "multi_scorer.json")

	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)

	a := &Adapter{}
	tests, metrics, code := a.Run([]string{bin, "eval", "task1.py", "task2.py"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if len(tests) != 3 {
		t.Fatalf("expected 3 tests (2 + 1), got %d", len(tests))
	}
	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics (2 + 2), got %d", len(metrics))
	}

	// Each metric must carry the task_file from its own JSON log — not the
	// first task path from cmd args. smoke.json -> tasks/capitals.py;
	// multi_scorer.json -> tasks/qa.py.
	want := map[string]bool{
		"eval.tasks/capitals.py.capital_cities.match": false,
		"eval.tasks/qa.py.qa_eval.accuracy":           false,
		"eval.tasks/qa.py.qa_eval.f1_score":           false,
	}
	for _, m := range metrics {
		if _, ok := want[m.Name]; ok {
			want[m.Name] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Fatalf("missing per-file-attributed metric %q in %v", k, metricNames(metrics))
		}
	}
}

func metricNames(ms []*metricspb.Metric) []string {
	names := make([]string, 0, len(ms))
	for _, m := range ms {
		names = append(names, m.Name)
	}
	return names
}

func TestRunFailingChildPropagatesExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
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
	_, _, code := a.Run([]string{abs, "eval", "task.py"})
	if code != 7 {
		t.Fatalf("expected exit 7, got %d", code)
	}
}

func TestRunPassthroughOnUserLogDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	// Fake child that exits 0 immediately. With user-supplied --log-dir
	// defrost must not parse anything and must propagate the child exit.
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-noop")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)

	a := &Adapter{}
	tests, metrics, code := a.Run([]string{abs, "eval", "task.py", "--log-dir", "/tmp/userlogs"})
	if code != 0 {
		t.Fatalf("expected exit 0 (passthrough propagates child exit), got %d", code)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected no results in passthrough mode, got %d tests / %d metrics", len(tests), len(metrics))
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
		t.Fatalf("expected exit 0 (passthrough), got %d", code)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected no results in passthrough mode, got %d tests / %d metrics", len(tests), len(metrics))
	}
}

func TestRunChildSucceededButNoLogsBumpsExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	// Child exits 0 but writes no JSON. Defrost must surface this as
	// non-zero so CI can't silently report success on a broken run.
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-noop")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake-noop: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)
	a := &Adapter{}
	_, _, code := a.Run([]string{abs, "eval", "task.py"})
	if code != 1 {
		t.Fatalf("expected exit 1 (defrost-internal floor), got %d", code)
	}
}

func TestRunEmpty(t *testing.T) {
	a := &Adapter{}
	_, _, code := a.Run(nil)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
}
