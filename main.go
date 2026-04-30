package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
)

func main() {
	parsed := kong.Parse(&CLI)
	cmd := parsed.Command()

	switch {
	case strings.HasPrefix(cmd, "exec"):
		os.Exit(HandleExecution(CLI.Exec.Cmd, ExecOpts{
			RepoDir:    CLI.Exec.RepoDir,
			DataBranch: CLI.Exec.DataBranch,
			Persist:    !CLI.Exec.NoPersist,
			NoRemote:   CLI.Exec.NoRemote,
		}))
	case strings.HasPrefix(cmd, "history"):
		os.Exit(HandleHistory(CLI.History.Test, CLI.History.RepoDir, CLI.History.DataBranch, CLI.History.NoRemote))
	case strings.HasPrefix(cmd, "serve"):
		os.Exit(HandleServe(ServeOpts{
			Port:       CLI.Serve.Port,
			RepoDir:    CLI.Serve.RepoDir,
			DataBranch: CLI.Serve.DataBranch,
			NoRemote:   CLI.Serve.NoRemote,
		}))
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}
