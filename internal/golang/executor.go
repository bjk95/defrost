package golang

import (
	"fmt"
	"os"
	"os/exec"
)

func ExecuteGoTest(cmd []string) {
	c := exec.Command(cmd[0], cmd[1:]...)
	stdout, err := c.StdoutPipe()
	if err != nil {
		os.Exit(1)
	}
	c.Stderr = os.Stderr

	if err := c.Start(); err != nil {
		os.Exit(1)
	}

	results, parseErr := Parse(stdout)
	waitErr := c.Wait()

	for _, r := range results {
		fmt.Printf("%+v\n", r)
	}

	if parseErr != nil {
		fmt.Fprintln(os.Stderr, parseErr)
	}
	if e, ok := waitErr.(*exec.ExitError); ok {
		os.Exit(e.ExitCode())
	}
	if waitErr != nil {
		os.Exit(1)
	}
}
