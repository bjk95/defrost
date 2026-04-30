package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/javascript/jest"
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
	reg.Register(&jest.Adapter{})

	a := reg.Find(cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "exec: unsupported test command: %q\n", cmd[0])
		return 2
	}

	return execWith(a, cmd, opts)
}

// execWith runs a known adapter and applies persistence + suppression
// rewrite. Split out from HandleExecution so tests can drive it with a
// stub adapter.
func execWith(a runner.Adapter, cmd []string, opts ExecOpts) int {
	results, code := a.Run(cmd)

	for _, r := range results {
		fmt.Printf("%+v\n", r)
	}

	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
		Dev:        opts.Dev,
	}

	persistFailed := false
	if opts.Persist && len(results) > 0 {
		if err := persistResults(pOpts, cmd, results); err != nil {
			fmt.Fprintln(os.Stderr, "persist: failed:", err)
			// A persist failure should surface even when the test command
			// itself succeeded — otherwise CI silently loses data and no
			// one notices. If tests already failed, keep that exit code
			// (it's the more important signal).
			persistFailed = true
			if code == 0 {
				code = 1
			}
		}
	}

	// Don't rewrite the exit code to 0 when persistence failed: doing so
	// would let CI report success on a run where historical data was lost
	// (e.g. transient push/auth failure with all failing tests suppressed).
	if code != 0 && !persistFailed {
		code = maybeRewriteExitCode(code, results, pOpts)
	}
	return code
}

func maybeRewriteExitCode(code int, results []models.TestResult, pOpts persist.Options) int {
	failingIDs := collectFailingTestIDs(results)
	if len(failingIDs) == 0 {
		return code
	}
	for _, r := range results {
		if r.IsFileError() {
			fmt.Fprintf(os.Stderr,
				"defrost: file-level error present (%s); exit %d preserved\n",
				r.Id, code)
			return code
		}
	}

	pass, fail, skip := tallyResults(results)
	fmt.Fprintf(os.Stderr, "defrost: results: %d pass, %d fail, %d skip\n", pass, fail, skip)
	fmt.Fprintf(os.Stderr, "defrost: checking suppression list for %d failing test(s)\n", len(failingIDs))

	suppressed, err := persist.New(pOpts).GetSuppressions()
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost: suppression read failed (exit code unchanged):", err)
		return code
	}
	suppSet := make(map[string]struct{}, len(suppressed))
	for _, s := range suppressed {
		suppSet[s] = struct{}{}
	}

	allSuppressed := true
	for _, id := range failingIDs {
		if _, ok := suppSet[id]; ok {
			fmt.Fprintf(os.Stderr, "defrost:   %s in suppression list -> ignoring\n", id)
		} else {
			fmt.Fprintf(os.Stderr, "defrost:   %s not in suppression list -> failing build\n", id)
			allSuppressed = false
		}
	}

	if !allSuppressed {
		fmt.Fprintf(os.Stderr, "defrost: not all failures suppressed; exit %d preserved\n", code)
		return code
	}
	fmt.Fprintf(os.Stderr,
		"defrost: all %d failing test(s) suppressed; rewriting exit %d → 0\n",
		len(failingIDs), code)
	return 0
}

func collectFailingTestIDs(results []models.TestResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		// Only count actual test-level failures. Synthetic file-error
		// results (jest's "could not load file" et al.) carry Ran=true
		// but represent infrastructure failures, not test failures —
		// suppressing those would let a broken test file mask itself.
		if r.Ran && !r.Passed && !r.IsFileError() {
			out = append(out, r.Id)
		}
	}
	return out
}

func tallyResults(results []models.TestResult) (pass, fail, skip int) {
	for _, r := range results {
		switch {
		case !r.Ran:
			skip++
		case r.Passed:
			pass++
		default:
			fail++
		}
	}
	return
}

func persistResults(pOpts persist.Options, cmd []string, results []models.TestResult) error {
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
