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
	os.Exit(run())
}

func run() int {
	startDir, _ := os.Getwd()
	resolver, cfgWarnings, cfgErr := cli.LoadConfigResolver(startDir)
	if cfgErr != nil {
		fmt.Fprintln(os.Stderr, cfgErr)
		return 2
	}

	var c cli.CLI

	helpFn := func(opts kong.HelpOptions, ctx *kong.Context) error {
		p := cli.NewPrinter(&cli.CLI{
			NoColor: hasArg("--no-color"),
		})
		return cli.MakeHelpPrinter(p)(opts, ctx)
	}

	parsed := kong.Parse(&c,
		kong.Name("defrost-ci"),
		kong.Description("Slim CI-focused defrost binary (no serve)."),
		kong.UsageOnError(),
		kong.Vars{"version": cli.VersionString()},
		kong.Help(helpFn),
		kong.Resolvers(resolver),
		kong.Groups{
			"core":       "CORE COMMANDS",
			"inspection": "INSPECTION COMMANDS",
			"management": "MANAGEMENT COMMANDS",
		},
	)

	if msg, code := cli.ValidateGlobalFlags(&c); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		return code
	}

	out := cli.NewPrinter(&c)
	for _, w := range cfgWarnings {
		out.Warn(w)
	}

	root := c.Root()
	cmd := parsed.Command()

	switch {
	case strings.HasPrefix(cmd, "exec"):
		return cli.HandleExec(c.Exec, root, out)
	case strings.HasPrefix(cmd, "history"):
		return cli.HandleHistory(c.History, root, out)
	case strings.HasPrefix(cmd, "suppress add"):
		return cli.HandleSuppressAdd(c.Suppress.Add, root, out)
	case strings.HasPrefix(cmd, "suppress remove"):
		return cli.HandleSuppressRemove(c.Suppress.Remove, root, out)
	case strings.HasPrefix(cmd, "suppress list"):
		return cli.HandleSuppressList(c.Suppress.List, root, out)
	case strings.HasPrefix(cmd, "drop history"):
		return cli.HandleDropHistory(c.Drop.History, root, out)
	case strings.HasPrefix(cmd, "reset"):
		return cli.HandleReset(c.Reset, root, out)
	case strings.HasPrefix(cmd, "serve"):
		out.Fail("serve requires the full defrost binary. Reinstall with:")
		out.Fail("  go install github.com/bjk95/defrost/cmd/defrost@latest")
		return 1
	default:
		out.Failf("unknown command: %s", cmd)
		return 2
	}
}

func hasArg(name string) bool {
	for _, a := range os.Args[1:] {
		if a == name {
			return true
		}
	}
	return false
}
