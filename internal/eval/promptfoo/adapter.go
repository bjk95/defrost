package promptfoo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)

// Adapter implements runner.Adapter for `promptfoo eval` invocations.
// Recognised forms:
//
//   - direct:    promptfoo eval ...
//   - npx:       npx promptfoo eval ..., npx -y promptfoo eval ..., npx promptfoo@latest eval ...
//   - pnpm:      pnpm promptfoo eval ..., pnpm dlx promptfoo eval ...
//   - yarn:      yarn promptfoo eval ...
//
// All forms must contain the literal `promptfoo` (or `promptfoo@<ver>`)
// token followed by `eval`. Other subcommands (`promptfoo view`,
// `promptfoo init`) are rejected.
type Adapter struct{}

func (a *Adapter) Matches(cmd []string) bool {
	if len(cmd) == 0 {
		return false
	}

	// Direct: promptfoo eval ... or /path/to/promptfoo eval ... or promptfoo@ver eval ...
	if parseExecBase(cmd[0]) == "promptfoo" {
		return len(cmd) >= 2 && cmd[1] == "eval"
	}

	switch cmd[0] {
	case "npx", "bunx":
		// Skip flags; a few flags take their value as the next token.
		skipNext := false
		for i := 1; i < len(cmd); i++ {
			tok := cmd[i]
			if skipNext {
				skipNext = false
				continue
			}
			if strings.HasPrefix(tok, "-") {
				if tok == "-p" || tok == "--package" || tok == "-c" || tok == "--call" {
					skipNext = true
				}
				continue
			}
			// First positional after flags must be promptfoo.
			if parseExecBase(tok) != "promptfoo" {
				return false
			}
			return i+1 < len(cmd) && cmd[i+1] == "eval"
		}
		return false
	case "pnpm":
		i := 1
		if i < len(cmd) && cmd[i] == "dlx" {
			i++
		}
		if i >= len(cmd) {
			return false
		}
		if parseExecBase(cmd[i]) != "promptfoo" {
			return false
		}
		return i+1 < len(cmd) && cmd[i+1] == "eval"
	case "yarn":
		i := 1
		if i < len(cmd) && cmd[i] == "run" {
			i++
		}
		if i >= len(cmd) {
			return false
		}
		if parseExecBase(cmd[i]) != "promptfoo" {
			return false
		}
		return i+1 < len(cmd) && cmd[i+1] == "eval"
	}

	return false
}

// parseExecBase returns the basename of an exec token with any `@<ver>`
// suffix stripped. e.g. `./node_modules/.bin/promptfoo@1.0.0` → `promptfoo`.
func parseExecBase(tok string) string {
	base := filepath.Base(tok)
	if at := strings.Index(base, "@"); at > 0 {
		base = base[:at]
	}
	return base
}

// userOutputPaths returns every `--output` / `-o` / `--output=<v>` value
// in args, in order. Promptfoo accepts multiple output flags producing
// different formats simultaneously (json + html + csv etc.).
func userOutputPaths(args []string) []string {
	var paths []string
	for i, a := range args {
		switch {
		case a == "--output" || a == "-o":
			if i+1 < len(args) {
				paths = append(paths, args[i+1])
			}
		case strings.HasPrefix(a, "--output="):
			paths = append(paths, strings.TrimPrefix(a, "--output="))
		}
	}
	return paths
}

// userJSONOutput returns the first user-supplied --output value that
// targets a .json file (case-insensitive), or "" if none.
func userJSONOutput(args []string) string {
	for _, p := range userOutputPaths(args) {
		if strings.HasSuffix(strings.ToLower(p), ".json") {
			return p
		}
	}
	return ""
}

// buildArgs strips the executable token from cmd[0] and returns the args
// to pass to it, with --output <path> appended unless the user already
// supplied an output flag. The first token of the returned slice is the
// `eval` subcommand (or whatever the user wrote there); cmd[0] itself is
// dropped because the caller invokes exec.Command(cmd[0], buildArgs(...)).
func buildArgs(cmd []string, jsonPath string) []string {
	rest := append([]string{}, cmd[1:]...)
	if len(userOutputPaths(rest)) > 0 {
		return rest
	}
	return append(rest, "--output", jsonPath)
}

// passthroughRun executes cmd verbatim with stdio + signals wired,
// returning the child exit code and no test results / metrics. Used when
// the promptfoo adapter recognises the invocation but can't safely
// capture results (user-supplied non-JSON --output target).
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

	userOutputs := userOutputPaths(cmd[1:])
	jsonPath := userJSONOutput(cmd[1:])

	if len(userOutputs) > 0 && jsonPath == "" {
		// User supplied --output flags but none target JSON. Defrost
		// can't extract per-test results; run passthrough so the user's
		// command still executes.
		fmt.Fprintln(os.Stderr,
			"defrost: promptfoo --output flags target non-JSON formats only; running passthrough (no per-test results will be recorded)")
		return passthroughRun(cmd)
	}

	tempPath := jsonPath
	if tempPath == "" {
		f, err := os.CreateTemp("", "defrost-promptfoo-*.json")
		if err != nil {
			fmt.Fprintln(os.Stderr, "defrost:", err)
			return nil, nil, 1
		}
		tempPath = f.Name()
		f.Close()
		defer os.Remove(tempPath)
	} else {
		// User-supplied path: clear any pre-existing content so a failed
		// child run can't be silently parsed as the current run's results.
		if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "defrost: cannot clear", tempPath, ":", err)
			return nil, nil, 1
		}
	}

	args := buildArgs(cmd, tempPath)

	child := exec.Command(cmd[0], args...)
	exitCode, err := runner.RunChild(child)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, nil, 1
	}

	f, err := os.Open(tempPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost: promptfoo output not found at", tempPath, ":", err)
		if exitCode == 0 {
			exitCode = 1
		}
		return nil, nil, exitCode
	}
	defer f.Close()

	tests, metrics, parseErr := Parse(f)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		if exitCode == 0 {
			exitCode = 1
		}
		return nil, nil, exitCode
	}
	runner.ApplyRepoPrefix(tests)

	return tests, metrics, exitCode
}
