// Package runner: spans.go contains the shared []TestResult →
// ptrace.Traces helper used by every runner adapter (golang, pytest,
// jest, vitest, inspect, promptfoo, passthrough). Each adapter parses
// non-OTLP runner output and emits pdata via this helper — making
// adapters in-process scrape receivers in the OTel sense.
package runner

import (
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/bjk95/defrost/internal/models"
)

// TestCaseScopeName is the scope.name value defrost stamps on every
// adapter-emitted ScopeSpans. Constant so the read path can identify
// adapter-emitted spans uniformly.
const TestCaseScopeName = "defrost"

// RootSpanName is the OTel span.Name used for every defrost.run root
// span. Exported so the read path can find root spans by name.
const RootSpanName = "defrost.run"

// TestResultsToTraces converts a slice of runner-adapter test results
// into a single ptrace.Traces holding one ResourceSpans with one
// ScopeSpans named "defrost". The Resource is populated from run.Attrs.
//
// Each result becomes one INTERNAL span parented to run.RootSpanID.
// Result statuses follow the OTel test-case semconv: passed/failed/
// skipped/aborted, written to the `test.case.result.status` attr.
func TestResultsToTraces(results []models.TestResult, run models.RunContext) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	ApplyAttrs(rs.Resource().Attributes(), run.Attrs)
	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName(TestCaseScopeName)
	for _, r := range results {
		appendTestSpan(ss.Spans(), r, run)
	}
	return td
}

// AppendTestResultSpans appends test-case spans into an existing
// ScopeSpans. Useful for merging adapter output into traces that
// already have a root span built.
func AppendTestResultSpans(spans ptrace.SpanSlice, results []models.TestResult, run models.RunContext) {
	for _, r := range results {
		appendTestSpan(spans, r, run)
	}
}

func appendTestSpan(target ptrace.SpanSlice, r models.TestResult, run models.RunContext) {
	span := target.AppendEmpty()
	span.SetTraceID(pcommon.TraceID(run.TraceID))
	span.SetSpanID(pcommon.SpanID(models.NewSpanID()))
	span.SetParentSpanID(pcommon.SpanID(run.RootSpanID))
	span.SetName(r.Id)
	span.SetKind(ptrace.SpanKindInternal)

	start := r.StartTime
	if start.IsZero() {
		start = time.Now()
	}
	startNs := uint64(start.UnixNano())
	endNs := startNs + uint64(r.Duration.Nanoseconds())
	span.SetStartTimestamp(pcommon.Timestamp(startNs))
	span.SetEndTimestamp(pcommon.Timestamp(endNs))

	resultStatus := mapResultStatus(r)
	span.Attributes().PutStr("test.case.result.status", resultStatus)
	switch resultStatus {
	case "passed":
		span.Status().SetCode(ptrace.StatusCodeOk)
	case "failed", "aborted":
		span.Status().SetCode(ptrace.StatusCodeError)
		span.Status().SetMessage(firstLine(r.Output))
	default:
		span.Status().SetCode(ptrace.StatusCodeUnset)
	}

	if r.Output != "" {
		ev := span.Events().AppendEmpty()
		ev.SetTimestamp(pcommon.Timestamp(endNs))
		ev.SetName("test.output")
		ev.Attributes().PutStr("body", r.Output)
	}
}

// NewRootSpan returns a ptrace.Traces with the synthetic `defrost.run`
// root span already attached. End time and status are filled in by the
// caller after the child exits and persistence either succeeds or fails.
func NewRootSpan(run models.RunContext) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	ApplyAttrs(rs.Resource().Attributes(), run.Attrs)
	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName(TestCaseScopeName)
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID(run.TraceID))
	span.SetSpanID(pcommon.SpanID(run.RootSpanID))
	span.SetName(RootSpanName)
	span.SetKind(ptrace.SpanKindInternal)
	span.SetStartTimestamp(pcommon.Timestamp(run.StartTimeUnixNano))
	span.Status().SetCode(ptrace.StatusCodeUnset)
	return td
}

// FinaliseRoot fills in the root span's end time and status based on
// the child's exit code, mutating td in place. td must have been
// returned from NewRootSpan.
func FinaliseRoot(td ptrace.Traces, end time.Time, exitCode int) {
	if td.ResourceSpans().Len() == 0 {
		return
	}
	rs := td.ResourceSpans().At(0)
	if rs.ScopeSpans().Len() == 0 {
		return
	}
	ss := rs.ScopeSpans().At(0)
	if ss.Spans().Len() == 0 {
		return
	}
	root := ss.Spans().At(0)
	root.SetEndTimestamp(pcommon.Timestamp(end.UnixNano()))
	if exitCode == 0 {
		root.Status().SetCode(ptrace.StatusCodeOk)
		root.Status().SetMessage("")
	} else {
		root.Status().SetCode(ptrace.StatusCodeError)
		root.Status().SetMessage("exit code " + itoa(exitCode))
	}
}

// MergeAppendTraces moves every ResourceSpans from src into dst.
// src is left empty. Used to combine multiple builders' output.
func MergeAppendTraces(dst, src ptrace.Traces) {
	src.ResourceSpans().MoveAndAppendTo(dst.ResourceSpans())
}

// ApplyAttrs copies a primitive []models.Attr into a pcommon.Map. The
// boundary that converts our minimal RunContext.Attrs representation
// into pdata is intentionally narrow.
func ApplyAttrs(target pcommon.Map, attrs []models.Attr) {
	for _, a := range attrs {
		switch v := a.Value.(type) {
		case string:
			target.PutStr(a.Key, v)
		case bool:
			target.PutBool(a.Key, v)
		case int64:
			target.PutInt(a.Key, v)
		case float64:
			target.PutDouble(a.Key, v)
		case []string:
			arr := target.PutEmptySlice(a.Key)
			arr.EnsureCapacity(len(v))
			for _, s := range v {
				arr.AppendEmpty().SetStr(s)
			}
		}
	}
}

// AttrsFromMap reads a pcommon.Map and returns a primitive []models.Attr.
// Reverse of ApplyAttrs; lossy for value types we don't carry on
// RunContext.
func AttrsFromMap(m pcommon.Map) []models.Attr {
	out := make([]models.Attr, 0, m.Len())
	m.Range(func(k string, v pcommon.Value) bool {
		switch v.Type() {
		case pcommon.ValueTypeStr:
			out = append(out, models.StringAttr(k, v.Str()))
		case pcommon.ValueTypeBool:
			out = append(out, models.BoolAttr(k, v.Bool()))
		case pcommon.ValueTypeInt:
			out = append(out, models.IntAttr(k, v.Int()))
		case pcommon.ValueTypeDouble:
			out = append(out, models.DoubleAttr(k, v.Double()))
		case pcommon.ValueTypeSlice:
			s := v.Slice()
			vals := make([]string, 0, s.Len())
			for i := 0; i < s.Len(); i++ {
				if s.At(i).Type() == pcommon.ValueTypeStr {
					vals = append(vals, s.At(i).Str())
				}
			}
			out = append(out, models.StringArrayAttr(k, vals))
		}
		return true
	})
	return out
}

// RunDurationMetric returns a pmetric.Metrics carrying the auto-emitted
// gauge that records the wall-clock duration of one defrost exec
// invocation, in milliseconds. The metric name embeds the run's
// path-from-repo-root and the wrapped command so each invocation site
// has its own time series.
func RunDurationMetric(run models.RunContext, cmd []string, repoPrefix string, end time.Time) pmetric.Metrics {
	durationMs := float64(end.UnixNano()-run.StartTimeUnixNano) / 1e6
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	ApplyAttrs(rm.Resource().Attributes(), MetricResourceAttrs(run))
	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName(TestCaseScopeName)
	m := sm.Metrics().AppendEmpty()
	m.SetName("defrost.run." + RunFQN(repoPrefix, cmd))
	m.SetDescription("Wall-clock duration of one defrost exec invocation.")
	m.SetUnit("ms")
	g := m.SetEmptyGauge()
	dp := g.DataPoints().AppendEmpty()
	dp.SetStartTimestamp(pcommon.Timestamp(run.StartTimeUnixNano))
	dp.SetTimestamp(pcommon.Timestamp(end.UnixNano()))
	dp.SetDoubleValue(durationMs)
	return md
}

// RunFQN builds the fully qualified run identifier embedded in the
// duration metric name: "<path-from-repo-root>¬<space-joined cmd>"
// when invoked from a subdirectory, or just the joined command when
// invoked at the repo root or repoPrefix is empty.
func RunFQN(repoPrefix string, cmd []string) string {
	joined := strings.Join(cmd, " ")
	if repoPrefix == "" {
		return joined
	}
	return strings.TrimSuffix(repoPrefix, "/") + "¬" + joined
}

// NewEvalMetrics returns an empty pmetric.Metrics seeded with the
// run's metric resource and the "defrost" scope. AddGauge/AppendGauge
// helpers extend it with single-data-point gauges, the shape every
// eval adapter (inspect, promptfoo) emits.
func NewEvalMetrics(run models.RunContext) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	ApplyAttrs(rm.Resource().Attributes(), MetricResourceAttrs(run))
	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName(TestCaseScopeName)
	return md
}

// AppendGauge appends a single-data-point gauge to md's first
// ScopeMetrics. md must have been produced by NewEvalMetrics. attrs
// are the data-point attributes (typically test.case.name and the
// gen_ai.evaluation.* keys). ts is the data point timestamp.
func AppendGauge(md pmetric.Metrics, name string, value float64, ts time.Time, attrs []models.Attr) {
	if md.ResourceMetrics().Len() == 0 {
		return
	}
	rm := md.ResourceMetrics().At(0)
	if rm.ScopeMetrics().Len() == 0 {
		rm.ScopeMetrics().AppendEmpty().Scope().SetName(TestCaseScopeName)
	}
	sm := rm.ScopeMetrics().At(0)
	m := sm.Metrics().AppendEmpty()
	m.SetName(name)
	g := m.SetEmptyGauge()
	dp := g.DataPoints().AppendEmpty()
	dp.SetTimestamp(pcommon.Timestamp(ts.UnixNano()))
	dp.SetDoubleValue(value)
	ApplyAttrs(dp.Attributes(), attrs)
}

// MetricResourceAttrs returns the subset of the run's Attrs safe to
// stamp on metrics — i.e. excluding fields that would explode time-
// series cardinality (run_id, full cmd, dirty hash, author, commit).
// Spans keep the full identity; metrics do not.
func MetricResourceAttrs(run models.RunContext) []models.Attr {
	skip := map[string]struct{}{
		"cicd.pipeline.run.id":        {},
		"defrost.cmd":                 {},
		"defrost.dirty_hash":          {},
		"defrost.author_email":        {},
		"defrost.author_name":         {},
		"defrost.parent_commit":       {},
		"vcs.repository.ref.revision": {},
	}
	out := make([]models.Attr, 0, len(run.Attrs))
	for _, a := range run.Attrs {
		if _, drop := skip[a.Key]; drop {
			continue
		}
		out = append(out, a)
	}
	return out
}

// mapResultStatus translates a TestResult into the OTel test semconv
// `test.case.result.status` enum value: passed/failed/skipped/aborted.
func mapResultStatus(r models.TestResult) string {
	if !r.Ran {
		return "skipped"
	}
	if r.Passed {
		return "passed"
	}
	if strings.Contains(r.Output, "panic:") {
		return "aborted"
	}
	return "failed"
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
