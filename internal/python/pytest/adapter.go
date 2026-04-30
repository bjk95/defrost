package pytest

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

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

func (Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	if hasUserJunitxml(cmd) {
		fmt.Fprintln(os.Stderr, "defrost: pytest adapter requires control of --junitxml; remove your --junitxml flag")
		return nil, nil, 2
	}

	f, err := os.CreateTemp("", "defrost-pytest-*.xml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, nil, 1
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

	results, parseErr := ParseFile(path)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return nil, nil, 1
	}
	runner.ApplyRepoPrefix(results)

	return results, nil, exitCode
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
