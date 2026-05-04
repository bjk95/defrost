package passthrough

import (
	"fmt"
	"os"
	"os/exec"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)

// Adapter is the universal fallback. It matches any non-empty argv and
// executes cmd verbatim with stdio + signals wired through, returning the
// child's exit code and no test results.
//
// Register last in the registry so framework-specific adapters win when
// they recognise the invocation. The point is that `defrost exec <cmd>`
// always runs the user's command — when no specialised adapter matches,
// per-test results are missing but the run itself still happens, and the
// run-level record (commit, exit code, OTel metrics emitted by the child)
// is still persisted.
type Adapter struct{}

func (Adapter) Matches(cmd []string) bool { return len(cmd) > 0 }

func (Adapter) Run(cmd []string, _ models.RunContext) (ptrace.Traces, pmetric.Metrics, int) {
	fmt.Fprintf(os.Stderr,
		"defrost: no test-framework adapter matched %q; running passthrough (no per-test results will be recorded)\n",
		cmd[0])
	c := exec.Command(cmd[0], cmd[1:]...)
	code, err := runner.RunChild(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return ptrace.NewTraces(), pmetric.NewMetrics(), 1
	}
	return ptrace.NewTraces(), pmetric.NewMetrics(), code
}
