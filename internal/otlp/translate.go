package otlp

import (
	"strings"
	"time"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/bjk95/defrost/internal/models"
)

// TestResultsToSpans converts runner-adapter output to OTel test-case
// spans under the supplied RunContext's trace and root span. Each result
// is a sibling — schema 4 does not model subtest hierarchy beyond the run
// root.
func TestResultsToSpans(results []models.TestResult, run models.RunContext) []*tracepb.Span {
	spans := make([]*tracepb.Span, 0, len(results))
	for _, r := range results {
		spans = append(spans, testResultToSpan(r, run))
	}
	return spans
}

func testResultToSpan(r models.TestResult, run models.RunContext) *tracepb.Span {
	start := r.StartTime
	if start.IsZero() {
		start = time.Now()
	}
	startNs := uint64(start.UnixNano())
	endNs := startNs + uint64(r.Duration.Nanoseconds())

	resultStatus := mapResultStatus(r)
	attrs := []*commonpb.KeyValue{
		models.StringAttr("test.case.name", r.Id),
		models.StringAttr("test.case.result.status", resultStatus),
		models.StringAttr("defrost.run_id", run.RunID),
	}
	if suite := suiteFromTestID(r.Id); suite != "" {
		attrs = append(attrs, models.StringAttr("test.suite.name", suite))
	}
	ns, fn := codeFromTestID(r.Id)
	if ns != "" {
		attrs = append(attrs, models.StringAttr("code.namespace", ns))
	}
	if fn != "" {
		attrs = append(attrs, models.StringAttr("code.function", fn))
	}

	var events []*tracepb.Span_Event
	if r.Output != "" {
		events = []*tracepb.Span_Event{{
			TimeUnixNano: endNs,
			Name:         "test.output",
			Attributes:   []*commonpb.KeyValue{models.StringAttr("body", r.Output)},
		}}
	}

	return &tracepb.Span{
		TraceId:           run.TraceID,
		SpanId:            models.NewSpanID(),
		ParentSpanId:      run.RootSpanID,
		Name:              r.Id,
		Kind:              tracepb.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: startNs,
		EndTimeUnixNano:   endNs,
		Status:            resultToSpanStatus(resultStatus, r.Output),
		Attributes:        attrs,
		Events:            events,
	}
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

func resultToSpanStatus(resultStatus, output string) *tracepb.Status {
	switch resultStatus {
	case "passed":
		return &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}
	case "skipped":
		return &tracepb.Status{Code: tracepb.Status_STATUS_CODE_UNSET}
	case "failed", "aborted":
		return &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: firstLine(output)}
	}
	return &tracepb.Status{Code: tracepb.Status_STATUS_CODE_UNSET}
}

// suiteFromTestID extracts the suite identifier:
// "github.com/x/p/TestFoo" → "github.com/x/p"
// "tests/test_foo.py::TestClass::test_method" → "tests/test_foo.py"
func suiteFromTestID(id string) string {
	if suite, _, ok := strings.Cut(id, "::"); ok {
		return suite
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[:i]
	}
	return ""
}

// codeFromTestID returns (namespace, function) for a test id. For Go
// "<pkg>/TestFoo" the namespace is the package and function is the test
// name. For pytest "<file>::<class>::<method>" the namespace is the file
// path and function is the trailing token.
func codeFromTestID(id string) (string, string) {
	if i := strings.LastIndex(id, "::"); i >= 0 {
		return suiteFromTestID(id), id[i+2:]
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// MetricsToEntries flattens an OTLP ExportMetricsServiceRequest into a
// slice of *metricspb.Metric, each containing exactly one data point.
// This per-data-point shape is what the persist layer writes one-per-line
// to metrics/<name>.ndjson.
//
// The returned metrics carry the inbound proto data verbatim — the
// caller (persist layer) is responsible for stamping them with the run's
// metric resource at write time.
func MetricsToEntries(req *cmetricspb.ExportMetricsServiceRequest, run models.RunContext) []*metricspb.Metric {
	if req == nil {
		return nil
	}
	var out []*metricspb.Metric
	for _, rm := range req.ResourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				out = append(out, splitMetricByDataPoint(m, run)...)
			}
		}
	}
	return out
}

// splitMetricByDataPoint clones m once per data point so each emitted
// *metricspb.Metric contains exactly one data point. This matches our
// per-line storage and lets each line stand alone for time-series
// aggregation.
func splitMetricByDataPoint(m *metricspb.Metric, run models.RunContext) []*metricspb.Metric {
	switch d := m.Data.(type) {
	case *metricspb.Metric_Gauge:
		out := make([]*metricspb.Metric, 0, len(d.Gauge.DataPoints))
		for _, dp := range d.Gauge.DataPoints {
			dp = stampDataPointTraceID(dp, run)
			out = append(out, &metricspb.Metric{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Data:        &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{dp}}},
			})
		}
		return out
	case *metricspb.Metric_Sum:
		out := make([]*metricspb.Metric, 0, len(d.Sum.DataPoints))
		for _, dp := range d.Sum.DataPoints {
			dp = stampDataPointTraceID(dp, run)
			out = append(out, &metricspb.Metric{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
					IsMonotonic:            d.Sum.IsMonotonic,
					AggregationTemporality: d.Sum.AggregationTemporality,
					DataPoints:             []*metricspb.NumberDataPoint{dp},
				}},
			})
		}
		return out
	case *metricspb.Metric_Histogram:
		out := make([]*metricspb.Metric, 0, len(d.Histogram.DataPoints))
		for _, dp := range d.Histogram.DataPoints {
			out = append(out, &metricspb.Metric{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
					AggregationTemporality: d.Histogram.AggregationTemporality,
					DataPoints:             []*metricspb.HistogramDataPoint{dp},
				}},
			})
		}
		return out
	case *metricspb.Metric_ExponentialHistogram:
		out := make([]*metricspb.Metric, 0, len(d.ExponentialHistogram.DataPoints))
		for _, dp := range d.ExponentialHistogram.DataPoints {
			out = append(out, &metricspb.Metric{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
					AggregationTemporality: d.ExponentialHistogram.AggregationTemporality,
					DataPoints:             []*metricspb.ExponentialHistogramDataPoint{dp},
				}},
			})
		}
		return out
	}
	return nil
}

// stampDataPointTraceID attaches the run's trace_id to the data point as
// an exemplar-style link, without inflating the attribute set's
// cardinality. The exemplar holds the trace bytes directly; metric
// attributes stay low-cardinality (cmd_hash, branch, etc. set by the
// caller's SDK).
func stampDataPointTraceID(dp *metricspb.NumberDataPoint, run models.RunContext) *metricspb.NumberDataPoint {
	if len(run.TraceID) == 0 {
		return dp
	}
	cloned := proto.Clone(dp).(*metricspb.NumberDataPoint)
	exemplar := &metricspb.Exemplar{
		TimeUnixNano: dp.TimeUnixNano,
		TraceId:      run.TraceID,
		SpanId:       run.RootSpanID,
	}
	switch v := dp.Value.(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		exemplar.Value = &metricspb.Exemplar_AsDouble{AsDouble: v.AsDouble}
	case *metricspb.NumberDataPoint_AsInt:
		exemplar.Value = &metricspb.Exemplar_AsInt{AsInt: v.AsInt}
	}
	cloned.Exemplars = append(cloned.Exemplars, exemplar)
	return cloned
}
