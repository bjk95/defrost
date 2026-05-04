package pytest

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
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

func (Adapter) Run(cmd []string, run models.RunContext) (ptrace.Traces, pmetric.Metrics, int) {
	if hasUserJunitxml(cmd) {
		// User-controlled --junitxml means we can't inject our own without
		// silently overriding their config. Run their command unchanged
		// and skip per-test parsing rather than refusing to run.
		fmt.Fprintln(os.Stderr,
			"defrost: pytest --junitxml present in argv; running passthrough (no per-test results will be recorded)")
		c := exec.Command(cmd[0], cmd[1:]...)
		code, err := runner.RunChild(c)
		if err != nil {
			fmt.Fprintln(os.Stderr, "defrost:", err)
			return ptrace.NewTraces(), pmetric.NewMetrics(), 1
		}
		return ptrace.NewTraces(), pmetric.NewMetrics(), code
	}

	f, err := os.CreateTemp("", "defrost-pytest-*.xml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return ptrace.NewTraces(), pmetric.NewMetrics(), 1
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
	exitCode, err := runner.RunChild(child)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return ptrace.NewTraces(), pmetric.NewMetrics(), 1
	}

	return parseOrPreserve(path, run, exitCode)
}

// parseOrPreserve reads pytest's JUnit XML and returns the parsed test
// results paired with the child's exit code. Pytest exits 5 when no
// tests were collected and exits non-zero on internal errors (config
// error, plugin crash); in both cases the XML may be missing or empty.
// Preserve pytest's exit code rather than overwriting with a synthetic
// 1 — that's the only signal the user has and we mustn't overwrite it.
func parseOrPreserve(path string, run models.RunContext, exitCode int) (ptrace.Traces, pmetric.Metrics, int) {
	if !fileNonEmpty(path) {
		fmt.Fprintf(os.Stderr,
			"defrost: pytest exited %d without writing JUnit XML; recording run with no per-test results\n",
			exitCode)
		return ptrace.NewTraces(), pmetric.NewMetrics(), exitCode
	}
	results, err := ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"defrost: parse pytest output: %v; recording run with no per-test results\n",
			err)
		return ptrace.NewTraces(), pmetric.NewMetrics(), exitCode
	}
	runner.ApplyRepoPrefix(results)
	return runner.TestResultsToTraces(results, run), pmetric.NewMetrics(), exitCode
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
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
