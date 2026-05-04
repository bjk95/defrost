package cli

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/bjk95/defrost/internal/cliout"
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

// drainTimeout is the upper bound on how long we'll wait for the
// receiver's in-flight HTTP handlers to complete after the child
// process exits. Hit this only if a handler is stuck (the child
// disconnected mid-request, the proxy is broken, …) — under normal
// conditions Shutdown returns near-instantly because there's no live
// SDK left to push records once the child has exited.
//
// Generous on purpose: the eventual git commit + push is the slow
// step in `defrost exec`, often 5–30s. Spending an extra few seconds
// waiting for the OTel SDK to finish flushing costs nothing relative
// to that, and avoids the prior 2-second hard cap that occasionally
// truncated late records when CI runners were under load.
const drainTimeout = 60 * time.Second

// HandleExec is the `defrost exec` subcommand handler. Returns the
// process exit code.
func HandleExec(c ExecCmd, root RootOpts, out *cliout.Printer) int {
	if len(c.Cmd) == 0 {
		out.Fail("exec: no command provided")
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
		out.Failf("exec: unsupported test command: %q", c.Cmd[0])
		return 2
	}
	return execWith(a, c, root, out)
}

// execWith runs a known adapter and applies persistence + suppression
// rewrite. Split out so tests can drive it with a stub adapter.
func execWith(a runner.Adapter, c ExecCmd, root RootOpts, out *cliout.Printer) int {
	pOpts := persist.Options{
		RepoDir:    root.RepoDir,
		DataBranch: root.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        root.Dev,
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
			out.Warnf("exec: detect run context, skipping persist: %v", runErr)
			persistFailed = true
		} else {
			receiver, sink, restoreEnv = startReceiver(out)
		}
	}
	defer restoreEnv()

	results, adapterMetrics, code := a.Run(c.Cmd, run)

	// Drain the sink once after the child exits; this also shuts the
	// receiver down. Merge the three signals into the local pdata so
	// downstream code sees one set of structures per signal.
	receiverLogs := plog.NewLogs()
	if td, mt, lg := drainSink(sink, receiver); persistEnabled && !persistFailed {
		td.ResourceSpans().MoveAndAppendTo(results.ResourceSpans())
		mt.ResourceMetrics().MoveAndAppendTo(adapterMetrics.ResourceMetrics())
		lg.ResourceLogs().MoveAndAppendTo(receiverLogs.ResourceLogs())
	}

	// Load the suppression set BEFORE the summary so suppressed
	// failures are reported in their own column rather than mislabeled
	// as "failed". A read failure (no origin, transient git error)
	// degrades to suppSet=nil — the summary will then show the raw
	// failure count and the exit code is preserved later.
	var suppSet map[string]struct{}
	if hasUnSuppressibleFailures(results) {
		var err error
		suppSet, err = loadSuppressionSet(pOpts)
		if err != nil {
			out.Warnf("suppression read failed: %v", err)
			suppSet = nil
		}
	}

	pass, fail, skip, suppressed := tallyResults(results, suppSet)
	if pass+fail+skip+suppressed > 0 {
		out.Stepf("%s passed   %s %s failed   %s %s skipped   %s %s suppressed",
			out.Bold(out.Green(strconv.Itoa(pass))),
			out.Red("✗"), out.Bold(out.Red(strconv.Itoa(fail))),
			out.Yellow("⊘"), out.Bold(out.Dim(strconv.Itoa(skip))),
			out.Dim("✱"), out.Bold(out.Dim(strconv.Itoa(suppressed))),
		)
	}
	printFailedTests(out, results, suppSet)

	if persistEnabled && !persistFailed {
		// Add the synthetic root span and run-duration metric, then
		// flush everything as canonical OTLP bytes.
		runEnd := time.Now()
		root2 := runner.NewRootSpan(run)
		runner.FinaliseRoot(root2, runEnd, code)
		root2.ResourceSpans().MoveAndAppendTo(results.ResourceSpans())

		dur := runner.RunDurationMetric(run, c.Cmd, persist.RepoPrefix(root.RepoDir), runEnd)
		dur.ResourceMetrics().MoveAndAppendTo(adapterMetrics.ResourceMetrics())

		// Drain the sink one more time in case the SDK was still
		// flushing during our spans/metrics build above. Merge any
		// late arrivals across all three signals.
		td2, mt2, lg2 := drainSink(sink, nil)
		td2.ResourceSpans().MoveAndAppendTo(results.ResourceSpans())
		mt2.ResourceMetrics().MoveAndAppendTo(adapterMetrics.ResourceMetrics())
		lg2.ResourceLogs().MoveAndAppendTo(receiverLogs.ResourceLogs())

		if err := persistRun(pOpts, run, results, adapterMetrics, receiverLogs); err != nil {
			persistFailed = true
			logPersistFailure(err)
		} else {
			// Including trace_id in the summary lets readback checks
			// (in CI and `defrost inspect`) find the just-pushed files
			// on the data branch by exact filename match — much more
			// robust than counting committed files since the last
			// fetch.
			out.Passf("persisted: trace_id=%s, spans=%d, metric_points=%d, log_records=%d",
				hex.EncodeToString(run.TraceID[:]),
				results.SpanCount(), adapterMetrics.DataPointCount(), receiverLogs.LogRecordCount())
		}
	}

	// Persist failure does NOT affect the exit code — the run already
	// happened, the test command's signal is what matters. The user's
	// terminal got a clear warning above; suppression rewriting still
	// applies. See docs/guides/troubleshooting/persist-failed.md.
	code = maybeRewriteExitCode(code, results, suppSet, out)
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
// we shut it down first and wait for in-flight requests to drain
// (bounded by drainTimeout as a deadlock backstop).
func drainSink(sink *otlp.Sink, receiver *otlp.Receiver) (ptrace.Traces, pmetric.Metrics, plog.Logs) {
	if sink == nil {
		return ptrace.NewTraces(), pmetric.NewMetrics(), plog.NewLogs()
	}
	if receiver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
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
func startReceiver(out *cliout.Printer) (*otlp.Receiver, *otlp.Sink, func()) {
	sink := otlp.NewSink()
	r, port, err := otlp.Start(context.Background(), sink)
	if err != nil {
		out.Warnf("exec: otlp receiver bind failed, continuing without telemetry: %v", err)
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

// tallyResults counts results by category. A failing test that is on
// the suppression list (and not a file-level error) is counted as
// suppressed rather than failed, so the summary the user sees matches
// the eventual exit-code rewrite. suppSet may be nil — in that case
// nothing is reclassified.
func tallyResults(td ptrace.Traces, suppSet map[string]struct{}) (pass, fail, skip, suppressed int) {
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
				case "skipped":
					skip++
				case "failed", "aborted":
					if !isFileError(span.Name()) && suppSet != nil {
						if _, ok := suppSet[span.Name()]; ok {
							suppressed++
							continue
						}
					}
					fail++
				}
			}
		}
	}
	return
}

// maybeRewriteExitCode returns 0 if every failing test is on the
// suppression list, else preserves code. suppSet may be nil — that
// signals the suppression read failed earlier and we can't make a
// confident decision, so the original exit code stands.
func maybeRewriteExitCode(code int, td ptrace.Traces, suppSet map[string]struct{}, out *cliout.Printer) int {
	failing, hasFileError := failingAndFileError(td)
	if len(failing) == 0 {
		return code
	}
	if hasFileError {
		out.Warnf("file-level error present; exit %d preserved", code)
		return code
	}
	if suppSet == nil {
		// Read failed earlier — we already warned the user; preserve
		// the exit code rather than guessing.
		return code
	}
	for _, id := range failing {
		if _, ok := suppSet[id]; !ok {
			return code
		}
	}
	out.Infof("all %d failing test(s) suppressed; rewriting exit %d → 0", len(failing), code)
	return 0
}

// loadSuppressionSet reads the suppression list from the data branch
// and returns it as a set. Caller decides what to do on error.
func loadSuppressionSet(pOpts persist.Options) (map[string]struct{}, error) {
	ids, err := persist.New(pOpts).GetSuppressions()
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
}

// hasUnSuppressibleFailures reports whether any test in td is a
// real (non-file-level) failure — i.e. the kind that suppression
// could potentially mask. Used to short-circuit the suppression read
// when it can't possibly affect the outcome.
func hasUnSuppressibleFailures(td ptrace.Traces) bool {
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
				if (status == "failed" || status == "aborted") && !isFileError(s.Name()) {
					return true
				}
			}
		}
	}
	return false
}

// printFailedTests appends a "Failures:" block under the result
// summary listing each failed test ID (un-suppressed only). At -v
// (Info verbosity) it also dumps the first few lines of each test's
// captured failure output (from the test span's status.message
// attribute, or the most recent event whose name is "exception").
// File-level errors are excluded — they're printed separately by
// the maybeRewriteExitCode flow when present.
func printFailedTests(out *cliout.Printer, td ptrace.Traces, suppSet map[string]struct{}) {
	type failedSpan struct {
		name     string
		duration time.Duration
		message  string
	}
	var failures []failedSpan

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
				if status != "failed" && status != "aborted" {
					continue
				}
				if isFileError(s.Name()) {
					continue
				}
				if suppSet != nil {
					if _, ok := suppSet[s.Name()]; ok {
						continue
					}
				}
				failures = append(failures, failedSpan{
					name:     s.Name(),
					duration: time.Duration(s.EndTimestamp() - s.StartTimestamp()),
					message:  spanFailureMessage(s),
				})
			}
		}
	}

	if len(failures) == 0 {
		return
	}
	out.Failf("%s:", out.Bold("Failures"))
	for _, f := range failures {
		fmt.Fprintf(os.Stderr, "  %s  %s\n",
			out.Bold(f.name),
			out.Dim(fmt.Sprintf("(%s)", f.duration.Round(time.Millisecond))),
		)
		if out.Verbose() && f.message != "" {
			for _, line := range failureMessageLines(f.message, 4) {
				fmt.Fprintf(os.Stderr, "    %s\n", out.Dim(line))
			}
		}
	}
}

// spanFailureMessage extracts the most informative failure message
// from a failed test span. Tries in order:
//  1. The span's Status.Message
//  2. The most recent event whose name is "exception" (its
//     "exception.message" attribute)
//  3. Any attribute named "test.case.failure_message"
//
// Returns "" if nothing useful is found.
func spanFailureMessage(s ptrace.Span) string {
	if msg := s.Status().Message(); msg != "" {
		return msg
	}
	events := s.Events()
	for i := events.Len() - 1; i >= 0; i-- {
		e := events.At(i)
		if e.Name() == "exception" {
			if v, ok := e.Attributes().Get("exception.message"); ok {
				return v.AsString()
			}
		}
	}
	if v, ok := s.Attributes().Get("test.case.failure_message"); ok {
		return v.AsString()
	}
	return ""
}

// failureMessageLines returns up to maxLines non-empty lines from
// a failure message, trimmed of leading whitespace.
func failureMessageLines(msg string, maxLines int) []string {
	if msg == "" {
		return nil
	}
	var lines []string
	for _, raw := range strings.Split(msg, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
		if len(lines) >= maxLines {
			break
		}
	}
	return lines
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
