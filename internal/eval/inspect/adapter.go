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

// joinScope joins non-empty path segments with "." for use as the
// repo-unique prefix in metric names emitted by this adapter.
func joinScope(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ".")
}

// userTaskFile returns the first non-flag positional argument immediately
// after the `eval` subcommand — Inspect AI's CLI requires the task file
// there. Returns "" when not found, or when the slot is occupied by a
// flag (defensive: we don't try to skip past arbitrary flag values).
func userTaskFile(cmd []string) string {
	for i, a := range cmd {
		if a != "eval" || i+1 >= len(cmd) {
			continue
		}
		next := cmd[i+1]
		if strings.HasPrefix(next, "-") {
			return ""
		}
		return next
	}
	return ""
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

	if hasFlag(cmd[1:], "--log-dir") || hasFlag(cmd[1:], "--log-format") {
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

	scope := joinScope(runner.RepoRelCwd(), userTaskFile(cmd))

	results, metrics, parseErr := parseLogDir(tempDir, scope)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		if exitCode == 0 {
			exitCode = 1
		}
		return nil, nil, exitCode
	}
	if len(results) == 0 {
		fmt.Fprintln(os.Stderr,
			"defrost: inspect produced no JSON log files in", tempDir,
			"(child exit", exitCode, ")")
		if exitCode == 0 {
			exitCode = 1
		}
		return nil, nil, exitCode
	}

	runner.ApplyRepoPrefix(results)
	return results, metrics, exitCode
}

// parseLogDir scans dir for *.json files (Inspect's --log-format=json output)
// and parses each one. Per-file decode failures are logged and skipped so a
// single corrupt file doesn't drop the whole run. scope is forwarded to
// ParseFile and becomes the repo-unique prefix on emitted metric names.
// Returns error only on directory listing failure.
func parseLogDir(dir, scope string) ([]models.TestResult, []*metricspb.Metric, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("glob inspect log dir: %w", err)
	}
	var (
		allTests   []models.TestResult
		allMetrics []*metricspb.Metric
	)
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "defrost: inspect: open", path, ":", err)
			continue
		}
		tests, metrics, parseErr := ParseFile(f, scope)
		f.Close()
		if parseErr != nil {
			fmt.Fprintln(os.Stderr, "defrost: inspect: parse", path, ":", parseErr)
			continue
		}
		allTests = append(allTests, tests...)
		allMetrics = append(allMetrics, metrics...)
	}
	return allTests, allMetrics, nil
}
