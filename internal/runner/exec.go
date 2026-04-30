package runner

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// ForwardSignals installs a forwarder that re-sends SIGINT, SIGTERM, SIGHUP
// and SIGQUIT received by the parent to c.Process. The returned stop func
// MUST be called after c.Wait() returns; it tears down the signal handler
// and the goroutine it owns.
//
// SIGINT has two-stage semantics, matching the convention of bash, npm,
// pytest, and most language runtimes: the first Ctrl+C forwards to the
// child for graceful shutdown; the second SIGKILLs the child and exits
// the parent with 130 (128 + SIGINT). Without the second-press escape
// hatch, a child that ignores or hangs on SIGINT would trap the user —
// once we call signal.Notify on SIGINT the Go runtime stops killing the
// parent on it, so we have to provide the way out ourselves.
//
// Other terminating signals are forwarded once and don't trigger the
// force-quit path; those are typically `kill <pid>` and the operator
// already chose the signal level.
//
// Without this forwarder a `kill -TERM` against the defrost process
// would leave the child orphaned and still running. The default
// behaviour for terminal SIGINT also reaches the child via the
// foreground process group, but explicit forwarding makes shutdown
// deterministic regardless of how the parent is signalled.
func ForwardSignals(c *exec.Cmd) func() {
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		intCount := 0
		for {
			select {
			case sig := <-sigCh:
				if sig == syscall.SIGINT {
					intCount++
					if intCount >= 2 {
						fmt.Fprintln(os.Stderr,
							"defrost: second SIGINT received; killing child and force-quitting")
						if c.Process != nil {
							_ = c.Process.Kill()
						}
						os.Exit(130)
					}
				}
				if c.Process != nil {
					_ = c.Process.Signal(sig)
				}
			case <-stop:
				return
			}
		}
	}()
	return func() {
		signal.Stop(sigCh)
		close(stop)
		<-done
	}
}

// RunChild starts c with stdio wired to the parent's, forwards relevant
// termination signals, and waits for it to exit. Returns the child's
// exit code. On a non-exit error (binary not found, fork failure) returns
// -1 and the error.
//
// Adapters that need to capture the child's stdout for parsing (e.g.
// `go test -json`) should not use this helper — they construct the pipe
// themselves and call ForwardSignals directly.
func RunChild(c *exec.Cmd) (int, error) {
	if c.Stdin == nil {
		c.Stdin = os.Stdin
	}
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
	if err := c.Start(); err != nil {
		return -1, err
	}
	stop := ForwardSignals(c)
	defer stop()

	err := c.Wait()
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}
