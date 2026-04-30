package promptfoo

import (
	"strings"
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
		base := tok
		if at := strings.Index(tok, "@"); at > 0 {
			base = tok[:at]
		}
		if base != "promptfoo" {
			continue
		}
		// Need an `eval` subcommand somewhere after this token.
		for j := i + 1; j < len(cmd); j++ {
			if cmd[j] == "eval" {
				return true
			}
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
