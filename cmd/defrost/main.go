// Command defrost is the full defrost CLI: write path (exec, history,
// suppress, drop) plus the read path (serve dashboard, DuckDB-backed
// Querier, embedded web bundle).
//
// CI installs that don't need the dashboard should use
// `cmd/defrost-ci` instead — same write-path subcommands but without
// DuckDB cgo or the web bundle, so the binary is smaller and the
// install matrix is simpler.
package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/bjk95/defrost/internal/cli"
	"github.com/bjk95/defrost/internal/cliout"
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/query/duckdb"
	"github.com/bjk95/defrost/internal/serve"
	"github.com/bjk95/defrost/internal/serve/assets"
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

	// helpFn captures the printer at render time. Kong fires the help
	// callback during Parse, which is BEFORE the resolver/parser fully
	// populate c — we re-derive verbosity/color from os.Args + env so
	// help renders correctly even at that early point.
	helpFn := func(opts kong.HelpOptions, ctx *kong.Context) error {
		p := cli.NewPrinter(&cli.CLI{
			NoColor: hasArg("--no-color"),
		})
		return cli.MakeHelpPrinter(p)(opts, ctx)
	}

	parsed := kong.Parse(&c,
		kong.Name("defrost"),
		kong.Description("Track AI evals, metrics, and tests with Git as the database."),
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
		return handleServe(c.Serve, root, out)
	default:
		out.Failf("unknown command: %s", cmd)
		return 2
	}
}

// hasArg returns true if name appears anywhere in os.Args[1:].
// Used by the help callback because Kong fires it before c is
// populated; we need to know --no-color status to colorize headings
// correctly even in the help output.
func hasArg(name string) bool {
	for _, a := range os.Args[1:] {
		if a == name {
			return true
		}
	}
	return false
}

// handleServe is the full binary's serve handler. The slim CI binary
// prints an install hint instead.
func handleServe(c cli.ServeCmd, root cli.RootOpts, out *cliout.Printer) int {
	pOpts := persist.Options{
		RepoDir:    root.RepoDir,
		DataBranch: root.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        root.Dev,
	}

	q, err := duckdb.New(pOpts)
	if err != nil {
		out.Failf("serve: open cache: %v", err)
		return 1
	}
	defer q.Close()

	addr := "127.0.0.1:" + strconv.Itoa(c.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			out.Failf("port %d already in use; pass --port to override", c.Port)
			return 1
		}
		out.Failf("serve: %v", err)
		return 1
	}

	handler := serve.New(serve.Deps{
		Querier: q,
		Persist: pOpts,
		Assets:  assets.FS,
	})
	srv := &http.Server{Handler: handler}

	out.Stepf("%s", out.URL(fmt.Sprintf("http://localhost:%d", c.Port)))
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		out.Failf("serve: %v", err)
		return 1
	}
	return 0
}
