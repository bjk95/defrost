package vitest

import (
	"encoding/json"
	"os"
	"regexp"
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
		var name string
		if len(cmd) >= 3 && cmd[1] == "run" {
			name = cmd[2]
		} else if len(cmd) >= 2 {
			name = cmd[1]
		}
		if name == "" {
			return false
		}
		return a.matchScript(name)
	case "pnpm":
		if len(cmd) >= 2 && cmd[1] == "vitest" {
			return true
		}
		var name string
		if len(cmd) >= 2 && cmd[1] == "test" {
			name = "test"
		} else if len(cmd) >= 3 && cmd[1] == "run" {
			name = cmd[2]
		}
		if name == "" {
			return false
		}
		return a.matchScript(name)
	case "npm":
		var name string
		if len(cmd) >= 2 && cmd[1] == "test" {
			name = "test"
		} else if len(cmd) >= 3 && cmd[1] == "run" {
			name = cmd[2]
		}
		if name == "" {
			return false
		}
		return a.matchScript(name)
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

var envVarRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// matchScript reads ./package.json and looks up scripts.<name>.
// Returns true iff that script entry exists. scriptOK and watchInScript
// reflect the result of the strict-vitest-shape check; if scriptOK is
// false, Run uses watchInScript to choose between exit-2 (watch) and
// passthrough (other shape failures).
func (a *Adapter) matchScript(name string) bool {
	value, ok := readPackageScript(name)
	if !ok {
		return false
	}
	tokens := strings.Fields(value)
	i := 0
	for i < len(tokens) && envVarRe.MatchString(tokens[i]) {
		i++
	}
	if i >= len(tokens) || tokens[i] != "vitest" {
		return false
	}
	a.formD = true
	a.scriptName = name
	a.scriptValue = value

	// Composite shell rejection (script matched form D but Run will surface
	// a passthrough warning).
	for _, t := range tokens[i:] {
		if strings.ContainsAny(t, "`<>|&;(") {
			a.scriptOK = false
			return true
		}
	}
	// Watch-trigger detection in script value.
	for _, t := range tokens[i+1:] {
		if t == "watch" || t == "--watch" || t == "--watchAll" || t == "--ui" {
			a.scriptOK = false
			a.watchInScript = true
			return true
		}
		if strings.HasPrefix(t, "--watch=") || strings.HasPrefix(t, "--watchAll=") || strings.HasPrefix(t, "--ui=") {
			a.scriptOK = false
			a.watchInScript = true
			return true
		}
	}
	a.scriptOK = true
	return true
}

func readPackageScript(name string) (string, bool) {
	data, err := os.ReadFile("package.json")
	if err != nil {
		return "", false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", false
	}
	v, ok := pkg.Scripts[name]
	return v, ok
}

// Run is a placeholder returning passthrough behavior; the real implementation
// arrives in later tasks. This stub exists so the adapter satisfies
// runner.Adapter at the package boundary.
func (a *Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	return nil, nil, 0
}
