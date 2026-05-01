package vitest

import (
	"strings"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

// Adapter implements runner.Adapter for vitest invocations. Forms supported:
//
//   - direct: `vitest …`, `vitest run …`
//   - npx / bunx: `npx vitest …`, `npx -y vitest …`, `bunx vitest …`
//   - package-runner direct: `yarn vitest …`, `pnpm vitest …`
//   - node_modules binary: any cmd[0] ending in node_modules/.bin/vitest
//   - package-runner script (form D): `npm test`, `yarn test`, `pnpm test`,
//     `<runner> run <name>`, `yarn <name>` — these read ./package.json and
//     accept iff scripts.<name> has strict vitest shape (env-var prefix +
//     leading "vitest" token, no shell composition, no watch trigger).
type Adapter struct {
	formD         bool
	scriptOK      bool
	watchInScript bool
	scriptName    string
	scriptValue   string
}

func (a *Adapter) Matches(cmd []string) bool {
	a.reset()
	if len(cmd) == 0 {
		return false
	}
	first := cmd[0]

	if first == "vitest" {
		return true
	}
	if strings.HasSuffix(first, "node_modules/.bin/vitest") {
		return true
	}

	if first == "npx" || first == "bunx" {
		// Some npx flags take their value as the next token (`--package vitest`
		// installs the vitest package). When we see one of those flags, the
		// next token is the flag's argument, not the executable — don't match
		// on it. The `--flag=value` form keeps the value attached to the flag
		// token, so it doesn't need this skip.
		skipNext := false
		for _, tok := range cmd[1:] {
			if skipNext {
				skipNext = false
				continue
			}
			if tok == "vitest" {
				return true
			}
			if !strings.HasPrefix(tok, "-") {
				return false
			}
			if tok == "-p" || tok == "--package" || tok == "-c" || tok == "--call" {
				skipNext = true
			}
		}
		return false
	}

	switch first {
	case "yarn":
		if len(cmd) >= 2 && cmd[1] == "vitest" {
			return true
		}
	case "pnpm":
		if len(cmd) >= 2 && cmd[1] == "vitest" {
			return true
		}
	}

	return false
}

func (a *Adapter) reset() {
	a.formD = false
	a.scriptOK = false
	a.watchInScript = false
	a.scriptName = ""
	a.scriptValue = ""
}

// Run is a placeholder returning passthrough behavior; the real implementation
// arrives in later tasks. This stub exists so the adapter satisfies
// runner.Adapter at the package boundary.
func (a *Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	return nil, nil, 0
}
