package golang

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bjk95/defrost/internal/models"
)

// ExecuteGoTest runs cmd, parses test events from its stdout, and returns
// the collected results plus the child's exit code. Stderr is wired to the
// parent's stderr unchanged. Caller decides what to do with the results
// (print, persist, etc.) and is responsible for exiting.
//
// If cmd lacks -json (or any -json= variant), it's inserted right after
// `go test` so callers can write `defrost exec go test ./...` instead of
// remembering the flag. The parser requires testjson-formatted output.
func ExecuteGoTest(cmd []string) ([]models.TestResult, int) {
	cmd = ensureJSONFlag(cmd)
	c := exec.Command(cmd[0], cmd[1:]...)
	stdout, err := c.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, 1
	}
	c.Stderr = os.Stderr

	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, 1
	}

	results, parseErr := Parse(stdout)
	waitErr := c.Wait()

	if parseErr != nil {
		fmt.Fprintln(os.Stderr, parseErr)
	}

	if e, ok := waitErr.(*exec.ExitError); ok {
		return results, e.ExitCode()
	}
	if waitErr != nil {
		return results, 1
	}
	return results, 0
}

func ensureJSONFlag(cmd []string) []string {
	for _, a := range cmd[2:] {
		if a == "-json" || a == "--json" || strings.HasPrefix(a, "-json=") || strings.HasPrefix(a, "--json=") {
			return cmd
		}
	}
	out := make([]string, 0, len(cmd)+1)
	out = append(out, cmd[0], cmd[1], "-json")
	out = append(out, cmd[2:]...)
	return out
}
