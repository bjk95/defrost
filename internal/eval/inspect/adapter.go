package inspect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)

// Adapter implements runner.Adapter for `inspect eval` invocations. The
// adapter recognises the direct binary, the `python -m inspect_ai eval` form,
// and tool-runner forms (poetry/uv/pipenv run inspect eval).
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
	// Direct: inspect eval ... or /path/to/inspect eval ...
	if filepath.Base(cmd[0]) == "inspect" {
		return len(cmd) >= 2 && cmd[1] == "eval"
	}
	// python -m inspect_ai eval ...
	if pythonRe.MatchString(cmd[0]) && len(cmd) >= 4 &&
		cmd[1] == "-m" && cmd[2] == "inspect_ai" && cmd[3] == "eval" {
		return true
	}
	// poetry run inspect eval ... / uv run inspect eval ...
	if toolRunners[cmd[0]] && len(cmd) >= 4 &&
		cmd[1] == "run" && filepath.Base(cmd[2]) == "inspect" && cmd[3] == "eval" {
		return true
	}
	return false
}

// hasFlag reports whether args contains either `--flag <v>` or
// `--flag=<v>`. Used to detect user-supplied --log-dir / --log-format that
// would conflict with defrost's auto-injection.
func hasFlag(args []string, flag string) bool {
	prefix := flag + "="
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// buildArgs returns the args slice (excluding cmd[0]) with `--log-dir
// <tempDir> --log-format json` appended. Caller must have verified the user
// did not already supply these flags.
func buildArgs(cmd []string, tempDir string) []string {
	rest := append([]string{}, cmd[1:]...)
	return append(rest, "--log-dir", tempDir, "--log-format", "json")
}

// passthroughRun executes cmd verbatim with stdio and signals wired through,
// returning the child exit code without parsing any results. Used when the
// user supplied --log-dir or --log-format and defrost can't safely override.
func passthroughRun(cmd []string) (ptrace.Traces, pmetric.Metrics, int) {
	c := exec.Command(cmd[0], cmd[1:]...)
	code, err := runner.RunChild(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return ptrace.NewTraces(), pmetric.NewMetrics(), 1
	}
	return ptrace.NewTraces(), pmetric.NewMetrics(), code
}

func (a *Adapter) Run(cmd []string, run models.RunContext) (ptrace.Traces, pmetric.Metrics, int) {
	if len(cmd) == 0 {
		return ptrace.NewTraces(), pmetric.NewMetrics(), 2
	}

	if hasFlag(cmd[1:], "--log-dir") || hasFlag(cmd[1:], "--log-format") {
		fmt.Fprintln(os.Stderr,
			"defrost: inspect --log-dir / --log-format present in argv; running passthrough (no per-test results will be recorded)")
		return passthroughRun(cmd)
	}

	tempDir, err := os.MkdirTemp("", "defrost-inspect-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return ptrace.NewTraces(), pmetric.NewMetrics(), 1
	}
	defer os.RemoveAll(tempDir)

	args := buildArgs(cmd, tempDir)
	child := exec.Command(cmd[0], args...)
	exitCode, err := runner.RunChild(child)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return ptrace.NewTraces(), pmetric.NewMetrics(), 1
	}

	results, metrics, parseErr := parseLogDir(tempDir, runner.RepoRelCwd(), run)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		if exitCode == 0 {
			exitCode = 1
		}
		return ptrace.NewTraces(), pmetric.NewMetrics(), exitCode
	}
	if len(results) == 0 {
		fmt.Fprintln(os.Stderr,
			"defrost: inspect produced no JSON log files in", tempDir,
			"(child exit", exitCode, ")")
		if exitCode == 0 {
			exitCode = 1
		}
		return ptrace.NewTraces(), pmetric.NewMetrics(), exitCode
	}

	runner.ApplyRepoPrefix(results)
	return runner.TestResultsToTraces(results, run), metrics, exitCode
}

// parseLogDir scans dir for *.json files (Inspect's --log-format=json output)
// and parses each one. Per-file decode failures are logged and skipped so a
// single corrupt file doesn't drop the whole run. repoRelCwd is forwarded
// to ParseFile, which combines it with each log's `eval.task_file` field
// to form a per-file metric-name prefix. Returns error only on directory
// listing failure.
func parseLogDir(dir, repoRelCwd string, run models.RunContext) ([]models.TestResult, pmetric.Metrics, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, pmetric.NewMetrics(), fmt.Errorf("glob inspect log dir: %w", err)
	}
	var allTests []models.TestResult
	allMetrics := runner.NewEvalMetrics(run)
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "defrost: inspect: open", path, ":", err)
			continue
		}
		tests, metrics, parseErr := ParseFile(f, repoRelCwd, run)
		f.Close()
		if parseErr != nil {
			fmt.Fprintln(os.Stderr, "defrost: inspect: parse", path, ":", parseErr)
			continue
		}
		allTests = append(allTests, tests...)
		mergeMetrics(allMetrics, metrics)
	}
	return allTests, allMetrics, nil
}

// mergeMetrics moves every Metric from src.ScopeMetrics[0] into
// dst.ScopeMetrics[0]. dst and src must both have been produced by
// runner.NewEvalMetrics so the resource and scope shapes already match.
func mergeMetrics(dst, src pmetric.Metrics) {
	if src.ResourceMetrics().Len() == 0 || dst.ResourceMetrics().Len() == 0 {
		return
	}
	srcRM := src.ResourceMetrics().At(0)
	dstRM := dst.ResourceMetrics().At(0)
	if srcRM.ScopeMetrics().Len() == 0 || dstRM.ScopeMetrics().Len() == 0 {
		return
	}
	srcRM.ScopeMetrics().At(0).Metrics().MoveAndAppendTo(dstRM.ScopeMetrics().At(0).Metrics())
}
