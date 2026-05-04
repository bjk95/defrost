package golang

import (
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)

// Adapter implements runner.Adapter for `go test ...` invocations.
//
// Matches the literal form `go test [args...]`. Tighter than a prefix-only
// match: `go run`, `go build`, etc. fall through to no-match.
type Adapter struct{}

func (Adapter) Matches(cmd []string) bool {
	return len(cmd) >= 2 && cmd[0] == "go" && cmd[1] == "test"
}

func (Adapter) Run(cmd []string, run models.RunContext) (ptrace.Traces, pmetric.Metrics, int) {
	results, exitCode := ExecuteGoTest(cmd)
	return runner.TestResultsToTraces(results, run), pmetric.NewMetrics(), exitCode
}
