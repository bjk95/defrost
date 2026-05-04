// Command defrost-ci is the slim, CI-focused defrost binary. Same
// write-path subcommands as `cmd/defrost` (exec, history, suppress,
// drop) but no serve handler — the dashboard's DuckDB cgo and embedded
// web bundle add ~30MB to the binary and require a Node toolchain to
// rebuild, neither of which is ever needed in CI.
//
// `defrost-ci serve` prints an install-the-full-binary hint and exits 1
// rather than silently no-op'ing, so a misuse surfaces immediately.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/bjk95/defrost/internal/cli"
)

func main() {
	var c cli.CLI
	parsed := kong.Parse(&c)
	cmd := parsed.Command()

	switch {
	case strings.HasPrefix(cmd, "exec"):
		os.Exit(cli.HandleExec(c.Exec))
	case strings.HasPrefix(cmd, "history"):
		os.Exit(cli.HandleHistory(c.History))
	case strings.HasPrefix(cmd, "suppress add"):
		os.Exit(cli.HandleSuppressAdd(c.Suppress.Add))
	case strings.HasPrefix(cmd, "suppress remove"):
		os.Exit(cli.HandleSuppressRemove(c.Suppress.Remove))
	case strings.HasPrefix(cmd, "suppress list"):
		os.Exit(cli.HandleSuppressList(c.Suppress.List))
	case strings.HasPrefix(cmd, "drop history"):
		os.Exit(cli.HandleDropHistory(c.Drop.History))
	case strings.HasPrefix(cmd, "reset"):
		os.Exit(cli.HandleReset(c.Reset))
	case strings.HasPrefix(cmd, "serve"):
		fmt.Fprintln(os.Stderr,
			"serve requires the full defrost binary. Reinstall with:")
		fmt.Fprintln(os.Stderr,
			"  go install github.com/bjk95/defrost/cmd/defrost@latest")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}
