package golang

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/bjk95/defrost/internal/models"
)

// ExecuteGoTest runs cmd, parses test events from its stdout, and returns
// the collected results plus the child's exit code. Stderr is wired to the
// parent's stderr unchanged. Caller decides what to do with the results
// (print, persist, etc.) and is responsible for exiting.
func ExecuteGoTest(cmd []string) ([]models.TestResult, int) {
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
