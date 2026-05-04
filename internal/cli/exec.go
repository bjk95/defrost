package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/bjk95/defrost/internal/eval/inspect"
	"github.com/bjk95/defrost/internal/eval/promptfoo"
	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/javascript/jest"
	"github.com/bjk95/defrost/internal/javascript/vitest"
	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/otlp"
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
	"github.com/bjk95/defrost/internal/runner/passthrough"
)

// drainGrace is the window we give SDK background flushes after the
// child exits before tearing down the OTLP receiver.
const drainGrace = 2 * time.Second

// HandleExec is the `defrost exec` subcommand handler. Returns the
// process exit code.
func HandleExec(c ExecCmd) int {
	if len(c.Cmd) == 0 {
		fmt.Fprintln(os.Stderr, "exec: no command provided")
		return 2
	}

	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(&inspect.Adapter{})
	reg.Register(pytest.Adapter{})
	reg.Register(&promptfoo.Adapter{})
	reg.Register(&vitest.Adapter{})
	reg.Register(&jest.Adapter{})
	reg.Register(passthrough.Adapter{})

	a := reg.Find(c.Cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "exec: unsupported test command: %q\n", c.Cmd[0])
		return 2
	}
	return execWith(a, c)
}

// execWith runs a known adapter and applies persistence + suppression
// rewrite. Split out so tests can drive it with a stub adapter.
func execWith(a runner.Adapter, c ExecCmd) int {
	pOpts := persist.Options{
		RepoDir:    c.RepoDir,
		DataBranch: c.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        c.Dev,
	}
	persistEnabled := !c.NoPersist

	var run models.RunContext
	var receiver *otlp.Receiver
	var sink *otlp.Sink
	restoreEnv := func() {}
	persistFailed := false

	if persistEnabled {
		var runErr error
		run, runErr = persist.DetectRunContext(pOpts, c.Cmd, DefrostVersion)
		if runErr != nil {
			fmt.Fprintln(os.Stderr, "exec: detect run context, skipping persist:", runErr)
			persistFailed = true
		} else {
			receiver, sink, restoreEnv = startReceiver()
		}
	}
	defer restoreEnv()

	results, adapterMetrics, code := a.Run(c.Cmd, run)

	if td, mt, lg := drainSink(sink, receiver); persistEnabled && !persistFailed {
		// Merge in-process adapter output into the sink's accumulator
		// so the gitExporter sees a single set of bytes per signal.
		td.ResourceSpans().MoveAndAppendTo(results.ResourceSpans())
		mt.ResourceMetrics().MoveAndAppendTo(adapterMetrics.ResourceMetrics())
		lg.ResourceLogs() // unused but referenced
		_ = lg
	}

	pass, fail, skip := tallyResults(results)
	if pass+fail+skip > 0 {
		fmt.Fprintf(os.Stderr, "defrost: results: %d pass, %d fail, %d skip\n", pass, fail, skip)
	}

	if persistEnabled && !persistFailed {
		// Add the synthetic root span and run-duration metric, then
		// flush everything as canonical OTLP bytes.
		runEnd := time.Now()
		root := runner.NewRootSpan(run)
		runner.FinaliseRoot(root, runEnd, code)
		root.ResourceSpans().MoveAndAppendTo(results.ResourceSpans())

		dur := runner.RunDurationMetric(run, c.Cmd, persist.RepoPrefix(c.RepoDir), runEnd)
		dur.ResourceMetrics().MoveAndAppendTo(adapterMetrics.ResourceMetrics())

		// Drain the sink one more time in case the SDK was still
		// flushing during our spans/metrics build above.
		_, _, _ = drainSink(sink, nil)

		if err := persistRun(pOpts, run, results, adapterMetrics, plog.NewLogs()); err != nil {
			persistFailed = true
			logPersistFailure(err)
		}
	}

	// Persist failure does NOT affect the exit code — the run already
	// happened, the test command's signal is what matters. The user's
	// terminal got a clear warning above; suppression rewriting still
	// applies. See docs/guides/troubleshooting/persist-failed.md.
	code = maybeRewriteExitCode(code, results, pOpts)
	return code
}

// persistFailuresDocURL is printed in the warning banner so users can
// click straight through to the troubleshooting page. Update both
// here and the page itself if the canonical URL changes.
const persistFailuresDocURL = "https://bjk95.github.io/defrost/guides/troubleshooting/persist-failed/"

// logPersistFailure prints a visible, terminal-clickable warning to
// stderr when defrost couldn't push a run to origin. The exit code is
// unchanged (the test command's result wins). The link points at the
// docs page that walks through likely causes (auth, network, branch
// protection) and how to recover the missing run.
func logPersistFailure(err error) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "⚠️  defrost: persist failed — this run was NOT pushed to origin.")
	fmt.Fprintln(os.Stderr, "   The test command's exit code is preserved.")
	fmt.Fprintln(os.Stderr, "   Cause:", err)
	fmt.Fprintln(os.Stderr, "   What to do:", persistFailuresDocURL)
	fmt.Fprintln(os.Stderr)
}

// drainSink returns the sink's accumulated pdata. Callers MUST have
// already shut down the receiver. When the receiver argument is non-nil
// we shut it down first and wait drainGrace for stragglers.
func drainSink(sink *otlp.Sink, receiver *otlp.Receiver) (ptrace.Traces, pmetric.Metrics, plog.Logs) {
	if sink == nil {
		return ptrace.NewTraces(), pmetric.NewMetrics(), plog.NewLogs()
	}
	if receiver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), drainGrace)
		_ = receiver.Shutdown(ctx)
		cancel()
	}
	return sink.Drain()
}

// startReceiver binds the OTLP listener and exports the OTEL_EXPORTER_OTLP_*
// env vars for the child. On bind failure we log and return (nil, nil,
// no-op) so the run continues without metric collection.
//
// The new pipeline captures all three signals — traces, metrics, and
// logs — so we override the GENERIC OTEL_EXPORTER_OTLP_* vars (not
// the per-signal vars). Today's homegrown receiver was metrics-only so
// it scoped to OTEL_EXPORTER_OTLP_METRICS_*; the upstream
// otlpreceiver speaks all three signals so the generic override is
// safe and ensures the user's traces/logs don't 404 silently.
func startReceiver() (*otlp.Receiver, *otlp.Sink, func()) {
	sink := otlp.NewSink()
	r, port, err := otlp.Start(context.Background(), sink)
	if err != nil {
		fmt.Fprintln(os.Stderr, "exec: otlp receiver bind failed, continuing without telemetry:", err)
		return nil, sink, func() {}
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)

	type envVar struct {
		key       string
		prevValue string
		hadPrev   bool
	}
	overrides := []envVar{
		{key: "OTEL_EXPORTER_OTLP_ENDPOINT"},
		{key: "OTEL_EXPORTER_OTLP_PROTOCOL"},
	}
	values := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": endpoint,
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
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
	return r, sink, restore
}

// persistRun serializes pdata to canonical OTLP bytes and hands them
// to the persist.Backend. Empty signals produce empty byte slices; the
// backend skips empty files.
func persistRun(pOpts persist.Options, run models.RunContext, td ptrace.Traces, md pmetric.Metrics, ld plog.Logs) error {
	traceBytes, err := marshalTraces(td)
	if err != nil {
		return fmt.Errorf("marshal traces: %w", err)
	}
	metricBytes, err := marshalMetrics(md)
	if err != nil {
		return fmt.Errorf("marshal metrics: %w", err)
	}
	logBytes, err := marshalLogs(ld)
	if err != nil {
		return fmt.Errorf("marshal logs: %w", err)
	}

	r := persist.Run{
		TraceID:      run.TraceID,
		RunStartUTC:  time.Unix(0, run.StartTimeUnixNano).UTC(),
		TraceBytes:   traceBytes,
		MetricsBytes: metricBytes,
		LogsBytes:    logBytes,
	}
	if err := persist.New(pOpts).InsertNewRun(r); err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			return errors.New("no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --dev to write to a local scratch dir")
		}
		return err
	}
	return nil
}

func marshalTraces(td ptrace.Traces) ([]byte, error) {
	if td.ResourceSpans().Len() == 0 {
		return nil, nil
	}
	return ptraceotlp.NewExportRequestFromTraces(td).MarshalProto()
}

func marshalMetrics(md pmetric.Metrics) ([]byte, error) {
	if md.ResourceMetrics().Len() == 0 {
		return nil, nil
	}
	return pmetricotlp.NewExportRequestFromMetrics(md).MarshalProto()
}

func marshalLogs(ld plog.Logs) ([]byte, error) {
	if ld.ResourceLogs().Len() == 0 {
		return nil, nil
	}
	return plogotlp.NewExportRequestFromLogs(ld).MarshalProto()
}

func tallyResults(td ptrace.Traces) (pass, fail, skip int) {
	rs := td.ResourceSpans()
	for i := 0; i < rs.Len(); i++ {
		ss := rs.At(i).ScopeSpans()
		for j := 0; j < ss.Len(); j++ {
			spans := ss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if span.Name() == runner.RootSpanName {
					continue
				}
				v, ok := span.Attributes().Get("test.case.result.status")
				if !ok {
					continue
				}
				switch v.AsString() {
				case "passed":
					pass++
				case "failed", "aborted":
					fail++
				case "skipped":
					skip++
				}
			}
		}
	}
	return
}

// maybeRewriteExitCode rewrites a non-zero exit to 0 when every failing
// test (excluding file-level errors, which we never suppress) is on
// the suppression list.
func maybeRewriteExitCode(code int, td ptrace.Traces, pOpts persist.Options) int {
	failing, hasFileError := failingAndFileError(td)
	if len(failing) == 0 {
		return code
	}
	if hasFileError {
		fmt.Fprintf(os.Stderr,
			"defrost: file-level error present; exit %d preserved\n", code)
		return code
	}
	fmt.Fprintf(os.Stderr, "defrost: checking suppression list for %d failing test(s)\n", len(failing))
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
	for _, id := range failing {
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
		len(failing), code)
	return 0
}

// failingAndFileError walks td and returns the IDs of failing test
// spans (excluding root spans). hasFileError is true when any failing
// test ID carries the file-error suffix — those are infrastructure
// failures and must never be suppressed.
func failingAndFileError(td ptrace.Traces) (failing []string, hasFileError bool) {
	rs := td.ResourceSpans()
	for i := 0; i < rs.Len(); i++ {
		ss := rs.At(i).ScopeSpans()
		for j := 0; j < ss.Len(); j++ {
			spans := ss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				s := spans.At(k)
				if s.Name() == runner.RootSpanName {
					continue
				}
				v, ok := s.Attributes().Get("test.case.result.status")
				if !ok {
					continue
				}
				status := v.AsString()
				if status == "passed" || status == "skipped" {
					continue
				}
				name := s.Name()
				if isFileError(name) {
					hasFileError = true
					continue
				}
				failing = append(failing, name)
			}
		}
	}
	return
}

func isFileError(id string) bool {
	const suffix = models.FileErrorSuffix
	if len(id) < len(suffix) {
		return false
	}
	return id[len(id)-len(suffix):] == suffix
}
