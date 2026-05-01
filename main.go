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
			Dev:        CLI.Exec.Dev,
		}))
	case strings.HasPrefix(cmd, "history"):
		os.Exit(HandleHistory(CLI.History.Test, CLI.History.RepoDir, CLI.History.DataBranch, CLI.History.Dev))
	case strings.HasPrefix(cmd, "suppress add"):
		os.Exit(HandleSuppressAdd(CLI.Suppress.Add.Tests, SuppressOpts{
			RepoDir:    CLI.Suppress.Add.RepoDir,
			DataBranch: CLI.Suppress.Add.DataBranch,
			Dev:        CLI.Suppress.Add.Dev,
		}))
	case strings.HasPrefix(cmd, "suppress remove"):
		os.Exit(HandleSuppressRemove(CLI.Suppress.Remove.Test, SuppressOpts{
			RepoDir:    CLI.Suppress.Remove.RepoDir,
			DataBranch: CLI.Suppress.Remove.DataBranch,
			Dev:        CLI.Suppress.Remove.Dev,
		}))
	case strings.HasPrefix(cmd, "suppress list"):
		os.Exit(HandleSuppressList(SuppressOpts{
			RepoDir:    CLI.Suppress.List.RepoDir,
			DataBranch: CLI.Suppress.List.DataBranch,
			Dev:        CLI.Suppress.List.Dev,
		}))
	case strings.HasPrefix(cmd, "drop history"):
		os.Exit(HandleDropHistory(DropOpts{
			RepoDir:     CLI.Drop.History.RepoDir,
			DataBranch:  CLI.Drop.History.DataBranch,
			TracesOnly:  CLI.Drop.History.TracesOnly,
			MetricsOnly: CLI.Drop.History.MetricsOnly,
			Yes:         CLI.Drop.History.Yes,
			Dev:         CLI.Drop.History.Dev,
		}))
	case strings.HasPrefix(cmd, "serve"):
		os.Exit(HandleServe(ServeOpts{
			Port:       CLI.Serve.Port,
			RepoDir:    CLI.Serve.RepoDir,
			DataBranch: CLI.Serve.DataBranch,
			Dev:        CLI.Serve.Dev,
		}))
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}
