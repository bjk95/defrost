package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/serve"
)

type ServeOpts struct {
	Port       int
	RepoDir    string
	DataBranch string
	NoRemote   bool
}

func HandleServe(opts ServeOpts) int {
	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
	}

	addr := "127.0.0.1:" + strconv.Itoa(opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			fmt.Fprintf(os.Stderr, "port %d already in use; pass --port to override\n", opts.Port)
			return 1
		}
		fmt.Fprintln(os.Stderr, "serve:", err)
		return 1
	}

	handler := serve.New(pOpts, Assets)
	srv := &http.Server{Handler: handler}

	fmt.Printf("→ http://localhost:%d\n", opts.Port)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "serve:", err)
		return 1
	}
	return 0
}
