package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
)

type ExecOpts struct {
	RepoDir    string
	DataBranch string
	Persist    bool
	NoRemote   bool
	Dev        bool
}

func HandleExecution(cmd []string, opts ExecOpts) int {
	if len(cmd) == 0 {
		fmt.Fprintln(os.Stderr, "exec: no command provided")
		return 2
	}

	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(pytest.Adapter{})

	a := reg.Find(cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "exec: unsupported test command: %q\n", cmd[0])
		return 2
	}

	results, code := a.Run(cmd)

	for _, r := range results {
		fmt.Printf("%+v\n", r)
	}

	if opts.Persist && len(results) > 0 {
		if err := persistResults(opts, cmd, results); err != nil {
			fmt.Fprintln(os.Stderr, "persist: failed:", err)
			// A persist failure should surface even when the test command
			// itself succeeded — otherwise CI silently loses data and no
			// one notices. If tests already failed, keep that exit code
			// (it's the more important signal).
			if code == 0 {
				code = 1
			}
		}
	}
	return code
}

func persistResults(opts ExecOpts, cmd []string, results []models.TestResult) error {
	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
		Dev:        opts.Dev,
	}
	run, err := persist.DetectRun(pOpts, cmd)
	if err != nil {
		return fmt.Errorf("detect run: %w", err)
	}
	if err := persist.New(pOpts).InsertNewTestResults(run, results); err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			return errors.New("no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --no-remote to persist locally only")
		}
		return err
	}
	return nil
}
