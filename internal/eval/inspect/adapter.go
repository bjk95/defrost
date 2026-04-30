package inspect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)

// Adapter implements runner.Adapter for `inspect eval` invocations.
// Recognised forms (see spec §3):
//
//   - direct:       inspect eval task.py
//   - python -m:    python(3.x)? -m inspect_ai eval task.py
//   - tool runner:  {poetry,uv,pipenv} run inspect eval task.py
type Adapter struct{}

var pythonRe = regexp.MustCompile(`^python(3(\.\d+)?)?$`)

var toolRunners = map[string]bool{
	"poetry": true,
	"uv":     true,
	"pipenv": true,
}

func (a *Adapter) Matches(cmd []string) bool {
	if len(cmd) == 0 {
		return false
	}
	if filepath.Base(cmd[0]) == "inspect" {
		return len(cmd) >= 2 && cmd[1] == "eval"
	}
	if pythonRe.MatchString(cmd[0]) && len(cmd) >= 4 &&
		cmd[1] == "-m" && cmd[2] == "inspect_ai" && cmd[3] == "eval" {
		return true
	}
	if toolRunners[cmd[0]] && len(cmd) >= 4 &&
		cmd[1] == "run" && filepath.Base(cmd[2]) == "inspect" && cmd[3] == "eval" {
		return true
	}
	return false
}

// hasUserLogDir reports whether the user has supplied --log-dir or
// --log-dir=<v>. When set, defrost can't safely override it; we run
// passthrough instead.
func hasUserLogDir(args []string) bool {
	for _, a := range args {
		if a == "--log-dir" || strings.HasPrefix(a, "--log-dir=") {
			return true
		}
	}
	return false
}

// hasUserLogFormat reports whether the user has supplied --log-format or
// --log-format=<v>. The parser needs JSON; defrost won't override the
// user's choice silently.
func hasUserLogFormat(args []string) bool {
	for _, a := range args {
		if a == "--log-format" || strings.HasPrefix(a, "--log-format=") {
			return true
		}
	}
	return false
}

// buildArgs strips cmd[0] (the executable token) and appends the
// auto-injection flags so Inspect writes parseable JSON into a
// defrost-controlled directory.
func buildArgs(cmd []string, tempDir string) []string {
	rest := append([]string{}, cmd[1:]...)
	return append(rest, "--log-dir", tempDir, "--log-format", "json")
}

// passthroughRun executes cmd verbatim with stdio + signals wired,
// returning the child exit code and no test results / metrics. Used when
// the inspect adapter recognises the invocation but can't safely capture
// results (user already supplied --log-dir or --log-format).
func passthroughRun(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	c := exec.Command(cmd[0], cmd[1:]...)
	code, err := runner.RunChild(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, nil, 1
	}
	return nil, nil, code
}

func (a *Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	if len(cmd) == 0 {
		return nil, nil, 2
	}

	if hasUserLogDir(cmd[1:]) || hasUserLogFormat(cmd[1:]) {
		fmt.Fprintln(os.Stderr,
			"defrost: inspect --log-dir / --log-format present in argv; running passthrough (no per-test results will be recorded)")
		return passthroughRun(cmd)
	}

	tempDir, err := os.MkdirTemp("", "defrost-inspect-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, nil, 1
	}
	defer os.RemoveAll(tempDir)

	args := buildArgs(cmd, tempDir)
	child := exec.Command(cmd[0], args...)
	exitCode, err := runner.RunChild(child)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, nil, 1
	}

	files, err := filepath.Glob(filepath.Join(tempDir, "*.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost: inspect glob log dir:", err)
		if exitCode == 0 {
			exitCode = 1
		}
		return nil, nil, exitCode
	}
	if len(files) == 0 {
		// Inspect may have failed before writing logs (e.g. import error
		// in the task file). Preserve the child's exit code rather than
		// rewriting to 1 — the user needs the original signal.
		fmt.Fprintf(os.Stderr,
			"defrost: inspect exited %d without writing JSON logs to %s; recording run with no per-test results\n",
			exitCode, tempDir)
		return nil, nil, exitCode
	}

	var (
		tests   []models.TestResult
		metrics []*metricspb.Metric
	)
	for _, f := range files {
		rd, err := os.Open(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "defrost: inspect open log file:", err)
			continue
		}
		ts, ms, perr := ParseFile(rd)
		rd.Close()
		if perr != nil {
			fmt.Fprintln(os.Stderr, "defrost:", perr)
			continue
		}
		tests = append(tests, ts...)
		metrics = append(metrics, ms...)
	}

	runner.ApplyRepoPrefix(tests)
	return tests, metrics, exitCode
}
