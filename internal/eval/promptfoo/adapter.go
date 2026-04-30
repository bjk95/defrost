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
	for i, tok := range cmd {
		base := filepath.Base(tok)
		if at := strings.Index(base, "@"); at > 0 {
			base = base[:at]
		}
		if base != "promptfoo" {
			continue
		}
		// Need `eval` as the next positional token.
		if i+1 < len(cmd) && cmd[i+1] == "eval" {
			return true
		}
		return false
	}
	return false
}

// userOutputPath returns the value of --output / -o / --output=<v> in args,
// or "" if not present.
func userOutputPath(args []string) string {
	for i, a := range args {
		switch {
		case a == "--output" || a == "-o":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--output="):
			return strings.TrimPrefix(a, "--output=")
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
	if userOutputPath(rest) != "" {
		return rest
	}
	return append(rest, "--output", jsonPath)
}

func (a *Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	if len(cmd) == 0 {
		return nil, nil, 2
	}
	userPath := userOutputPath(cmd[1:])
	tempPath := userPath
	if tempPath == "" {
		f, err := os.CreateTemp("", "defrost-promptfoo-*.json")
		if err != nil {
			fmt.Fprintln(os.Stderr, "defrost:", err)
			return nil, nil, 1
		}
		tempPath = f.Name()
		f.Close()
		defer os.Remove(tempPath)
	}

	args := buildArgs(cmd, tempPath)

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
