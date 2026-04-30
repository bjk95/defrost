package jest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)

// Adapter implements runner.Adapter for jest invocations. Forms supported:
//
//   - direct: `jest …`
//   - npx / bunx: `npx jest …`, `npx -y jest …`, `bunx jest …`
//   - package-runner direct: `yarn jest …`, `pnpm jest …`
//   - node_modules binary: any cmd[0] ending in node_modules/.bin/jest
//   - package-runner script (form (d)): `npm test`, `yarn test`, `pnpm test`,
//     `<runner> run <name>`, `yarn <name>` — these read ./package.json and
//     accept iff scripts.<name> has strict jest shape (env-var prefix +
//     leading "jest" token, no shell composition).
//
// Form (d) is the only place in the registry where Matches touches the
// filesystem. The script-resolution result is cached on the adapter so Run
// can surface a helpful error when scripts.<name> exists but isn't
// jest-shaped.
type Adapter struct {
	formD       bool
	scriptOK    bool
	scriptName  string
	scriptValue string
}

var envVarRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

func (a *Adapter) Matches(cmd []string) bool {
	a.reset()
	if len(cmd) == 0 {
		return false
	}
	first := cmd[0]

	if first == "jest" {
		return true
	}
	if strings.HasSuffix(first, "node_modules/.bin/jest") {
		return true
	}

	if first == "npx" || first == "bunx" {
		// Some npx flags take their value as the next token (`--package jest`
		// installs the jest package). When we see one of those flags, the next
		// token is the flag's argument, not the executable — don't match on it.
		// The `--flag=value` form keeps the value attached to the flag token,
		// so it doesn't need this skip.
		skipNext := false
		for _, tok := range cmd[1:] {
			if skipNext {
				skipNext = false
				continue
			}
			if tok == "jest" {
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
		if len(cmd) >= 2 && cmd[1] == "jest" {
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
		if len(cmd) >= 2 && cmd[1] == "jest" {
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
	a.scriptName = ""
	a.scriptValue = ""
}

// matchScript reads ./package.json and looks up scripts.<name>. Returns
// true iff that script entry exists. scriptOK reflects whether the script
// value passes the strict-jest-shape check; if false, Run surfaces a
// helpful error rather than running a wrong command.
func (a *Adapter) matchScript(name string) bool {
	value, ok := readPackageScript(name)
	if !ok {
		return false
	}
	a.formD = true
	a.scriptName = name
	a.scriptValue = value
	a.scriptOK = looksLikeJestScript(value)
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

// looksLikeJestScript returns true iff value is a strict jest invocation:
// optional leading KEY=value env-var tokens, then "jest", then any args
// that don't contain shell-composition operators.
func looksLikeJestScript(value string) bool {
	tokens := strings.Fields(value)
	i := 0
	for i < len(tokens) && envVarRe.MatchString(tokens[i]) {
		i++
	}
	if i >= len(tokens) || tokens[i] != "jest" {
		return false
	}
	// npm runs scripts through a shell, so shell-composition characters are
	// significant even when glued onto another token (`jest --foo&&attack`).
	// Reject any token containing one of these chars rather than only the
	// standalone `&&` / `||` / `;` / etc. forms. `&` covers `&&` and bare
	// background; `|` covers `||` and pipe; `(` covers subshells and `$(...)`.
	for _, t := range tokens[i:] {
		if strings.ContainsAny(t, "`<>|&;(") {
			return false
		}
	}
	return true
}

func (a *Adapter) Run(cmd []string) ([]models.TestResult, int) {
	if hasUserJSONFlag(cmd) {
		fmt.Fprintln(os.Stderr, "defrost: jest adapter requires control of --json and --outputFile; remove those flags")
		return nil, 2
	}
	if hasUserWatchFlag(cmd) {
		fmt.Fprintln(os.Stderr, "defrost: jest adapter is not compatible with --watch / --watchAll")
		return nil, 2
	}
	if a.formD && !a.scriptOK {
		fmt.Fprintf(os.Stderr,
			"defrost: scripts.%s in package.json doesn't look like a direct jest invocation; run jest via 'npx jest …' or rewrite the script\n",
			a.scriptName)
		return nil, 2
	}

	f, err := os.CreateTemp("", "defrost-jest-*.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, 1
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	args := buildArgs(cmd, path)

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
		return nil, 1
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost:", err)
		return nil, 1
	}

	results, parseErr := ParseFile(path, cwd)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return nil, 1
	}
	runner.ApplyRepoPrefix(results)

	return results, exitCode
}

// buildArgs returns the args to pass to cmd[0], with --json and
// --outputFile=<path> appended. For package-runner script invocations
// (`npm test`, `pnpm test`, `pnpm run X`), a "--" separator is inserted
// before the injected flags if not already present so the script-runner
// forwards them to jest. Direct binary execution (`pnpm jest …`) and yarn
// (any form) forward args as-is and do not need the separator — adding
// one would make jest see "--" as a positional terminator and treat
// `--json` / `--outputFile` as positional args.
func buildArgs(cmd []string, jsonPath string) []string {
	rest := append([]string{}, cmd[1:]...)
	jsonFlags := []string{"--json", "--outputFile=" + jsonPath}

	if !needsSeparator(cmd) {
		return append(rest, jsonFlags...)
	}

	for _, t := range rest {
		if t == "--" {
			return append(rest, jsonFlags...)
		}
	}
	return append(rest, append([]string{"--"}, jsonFlags...)...)
}

// needsSeparator returns true when cmd is a package-runner script
// invocation that requires a `--` token before user-injected flags so the
// runner forwards them to the underlying script. npm only enters this
// adapter via script forms (npm test / npm run X), so it always needs the
// separator. pnpm enters either via direct binary exec (`pnpm jest …`,
// no separator) or via script forms (`pnpm test` / `pnpm run X`,
// separator). yarn forwards args directly in all forms.
func needsSeparator(cmd []string) bool {
	switch cmd[0] {
	case "npm":
		return true
	case "pnpm":
		return len(cmd) < 2 || cmd[1] != "jest"
	}
	return false
}

func hasUserJSONFlag(cmd []string) bool {
	for _, a := range cmd {
		if a == "--json" || a == "--outputFile" {
			return true
		}
		if strings.HasPrefix(a, "--json=") || strings.HasPrefix(a, "--outputFile=") {
			return true
		}
	}
	return false
}

func hasUserWatchFlag(cmd []string) bool {
	for _, a := range cmd {
		if a == "--watch" || a == "--watchAll" {
			return true
		}
		if strings.HasPrefix(a, "--watch=") || strings.HasPrefix(a, "--watchAll=") {
			return true
		}
	}
	return false
}
