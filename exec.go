package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/javascript/jest"
	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/otlp"
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
)

// defrostVersion is stamped into the Resource attribute service.version.
// Bump when cutting a release; build-time injection can replace this
// constant later.
const defrostVersion = "0.0.0-dev"

// drainGrace is the window we give SDK background flushes after the child
// exits before tearing down the OTLP receiver. See spec defaults table.
const drainGrace = 2 * time.Second

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
	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
		Dev:        opts.Dev,
	}

	// When Persist is off, no run artifacts hit disk, so the OTLP receiver
	// and the env-var override that points the child at it are pointless
	// side effects — and they actively interfere with users who have
	// OTEL_EXPORTER_OTLP_* set for their own observability. Skip the
	// whole metrics path in that mode.
	//
	// When Persist is on but DetectRunContext fails (non-git workdir,
	// transient git error), tests must still run — defrost is a wrapper,
	// not a precondition. Skip the metrics+persist path, log the error,
	// and treat it as a persist failure for exit-code purposes so the
	// signal isn't masked by passing tests.
	var run models.RunContext
	var receiver *otlp.Receiver
	restoreEnv := func() {}
	persistFailed := false
	if opts.Persist {
		var runErr error
		run, runErr = persist.DetectRunContext(pOpts, cmd, defrostVersion)
		if runErr != nil {
			fmt.Fprintln(os.Stderr, "exec: detect run context, skipping persist:", runErr)
			persistFailed = true
		} else {
			receiver, restoreEnv = startReceiver()
		}
	}
	defer restoreEnv()

	// The adapter is responsible for piping child stdout/stderr through
	// to the user (pytest and jest do; the Go adapter consumes stdout
	// because go test -json is for the parser). Defrost does not layer
	// its own per-test print on top — we only emit a summary line below.
	results, code := a.Run(cmd)

	if len(results) > 0 {
		pass, fail, skip := tallyResults(results)
		fmt.Fprintf(os.Stderr, "defrost: results: %d pass, %d fail, %d skip\n", pass, fail, skip)
	}

	var metrics []models.MetricEntry
	if receiver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), drainGrace)
		buffered, err := receiver.Shutdown(ctx)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "exec: otlp receiver shutdown:", err)
		}
		for _, req := range buffered {
			metrics = append(metrics, otlp.MetricsToEntries(req, run)...)
		}
	}

	// Always persist when --persist is on AND a run context was built,
	// even with zero results and zero metrics. Schema 3 models the run
	// as the `defrost.run` root span; a compile/setup failure that
	// produces no test cases still needs a history entry recording who,
	// when, what commit, what exit code — the runs you most want to
	// debug are precisely the ones with no per-test data.
	if opts.Persist && !persistFailed {
		if err := persistRun(pOpts, run, results, metrics, code); err != nil {
			fmt.Fprintln(os.Stderr, "persist: failed:", err)
			// A persist failure should surface even when the test command
			// itself succeeded — otherwise CI silently loses data and no
			// one notices. If tests already failed, keep that exit code
			// (it's the more important signal).
			persistFailed = true
		}
	}
	if persistFailed && code == 0 {
		code = 1
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

// startReceiver binds the OTLP listener and exports the metrics-signal
// env vars for the child. On bind failure we log and return (nil, no-op)
// so the run continues without metric collection. Returns a restore
// function the caller MUST call to clear the exported env vars regardless
// of outcome.
//
// Only the OTEL_EXPORTER_OTLP_METRICS_* per-signal vars are overridden —
// not the generic OTEL_EXPORTER_OTLP_* — because the receiver implements
// /v1/metrics only. Redirecting the generic endpoint would point the
// child's trace and log exporters at our localhost, where they'd 404 on
// /v1/traces and /v1/logs and silently drop unrelated telemetry. Per OTel
// precedence rules the per-signal var wins for the metrics signal even
// when a user has the generic var set, so this scoping is safe.
func startReceiver() (*otlp.Receiver, func()) {
	r := otlp.New()
	port, err := r.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "exec: otlp receiver bind failed, continuing without metrics:", err)
		return nil, func() {}
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)

	type envVar struct {
		key       string
		prevValue string
		hadPrev   bool
	}
	overrides := []envVar{
		{key: "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"},
		{key: "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL"},
	}
	values := map[string]string{
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": endpoint + "/v1/metrics",
		"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "http/protobuf",
	}
	for i := range overrides {
		overrides[i].prevValue, overrides[i].hadPrev = os.LookupEnv(overrides[i].key)
		os.Setenv(overrides[i].key, values[overrides[i].key])
	}
	restore := func() {
		for _, e := range overrides {
			if e.hadPrev {
				os.Setenv(e.key, e.prevValue)
			} else {
				os.Unsetenv(e.key)
			}
		}
	}
	return r, restore
}

func persistRun(pOpts persist.Options, run models.RunContext, results []models.TestResult, metrics []models.MetricEntry, exitCode int) error {
	testSpans := otlp.TestResultsToSpans(results, run)

	root := persist.NewRootSpan(run)
	root.EndTimeUnixNano = time.Now().UnixNano()
	root.Status = rootStatusFromExit(exitCode)

	if err := persist.New(pOpts).InsertNewRun(root, testSpans, metrics); err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			return errors.New("no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --no-remote to persist locally only")
		}
		return err
	}
	return nil
}

func rootStatusFromExit(code int) models.SpanStatus {
	if code == 0 {
		return models.SpanStatus{Code: "OK"}
	}
	return models.SpanStatus{Code: "ERROR", Message: fmt.Sprintf("exit code %d", code)}
}
