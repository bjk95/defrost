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
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/query/duckdb"
	"github.com/bjk95/defrost/internal/serve"
	"github.com/bjk95/defrost/internal/serve/assets"
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
	case strings.HasPrefix(cmd, "serve"):
		os.Exit(handleServe(c.Serve))
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}

func handleServe(c cli.ServeCmd) int {
	pOpts := persist.Options{
		RepoDir:    c.RepoDir,
		DataBranch: c.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        c.Dev,
	}

	q, err := duckdb.New(pOpts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "serve: open cache:", err)
		return 1
	}
	defer q.Close()

	addr := "127.0.0.1:" + strconv.Itoa(c.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			fmt.Fprintf(os.Stderr, "port %d already in use; pass --port to override\n", c.Port)
			return 1
		}
		fmt.Fprintln(os.Stderr, "serve:", err)
		return 1
	}

	handler := serve.New(serve.Deps{
		Querier: q,
		Persist: pOpts,
		Assets:  assets.FS,
	})
	srv := &http.Server{Handler: handler}

	fmt.Printf("→ http://localhost:%d\n", c.Port)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "serve:", err)
		return 1
	}
	return 0
}
