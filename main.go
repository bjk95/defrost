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
			NoHistory:  CLI.Exec.NoHistory,
		}))
	case strings.HasPrefix(cmd, "history"):
		os.Exit(HandleHistory(CLI.History.Test, CLI.History.RepoDir, CLI.History.DataBranch, CLI.History.NoRemote))
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}
