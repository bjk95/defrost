package otlp

import (
	"math"
	"strings"
	"time"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

// TestResultsToSpans converts runner-adapter output to OTel test-case
// spans under the supplied RunContext's trace and root span. Each result
// is a sibling — schema 3 does not model subtest hierarchy beyond the run
// root.
func TestResultsToSpans(results []models.TestResult, run models.RunContext) []models.Span {
	spans := make([]models.Span, 0, len(results))
	for _, r := range results {
		spans = append(spans, testResultToSpan(r, run))
	}
	return spans
}

func testResultToSpan(r models.TestResult, run models.RunContext) models.Span {
	start := r.StartTime
	if start.IsZero() {
		start = time.Now()
	}
	startNs := start.UnixNano()
	endNs := startNs + r.Duration.Nanoseconds()

	resultStatus := mapResultStatus(r)
	attrs := map[string]any{
		"test.case.name":          r.Id,
		"test.case.result.status": resultStatus,
		"defrost.run_id":          run.RunID,
	}
	if suite := suiteFromTestID(r.Id); suite != "" {
		attrs["test.suite.name"] = suite
	}
	ns, fn := codeFromTestID(r.Id)
	if ns != "" {
		attrs["code.namespace"] = ns
	}
	if fn != "" {
		attrs["code.function"] = fn
	}

	var events []models.SpanEvent
	if r.Output != "" {
		events = []models.SpanEvent{{
			TimeUnixNano: endNs,
			Name:         "test.output",
			Attributes:   map[string]any{"body": r.Output},
		}}
	}

	return models.Span{
		Schema:            models.SchemaV3,
		TraceID:           run.TraceID,
		SpanID:            models.NewSpanID(),
		ParentSpanID:      run.RootSpanID,
		Name:              r.Id,
		Kind:              "INTERNAL",
		StartTimeUnixNano: startNs,
		EndTimeUnixNano:   endNs,
		Status:            resultToSpanStatus(resultStatus, r.Output),
		Attributes:        attrs,
		Events:            events,
		Resource:          run.Resource,
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

func resultToSpanStatus(resultStatus, output string) models.SpanStatus {
	switch resultStatus {
	case "passed":
		return models.SpanStatus{Code: "OK"}
	case "skipped":
		return models.SpanStatus{Code: "UNSET"}
	case "failed", "aborted":
		return models.SpanStatus{Code: "ERROR", Message: firstLine(output)}
	}
	return models.SpanStatus{Code: "UNSET"}
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

// MetricsToEntries flattens an OTLP ExportMetricsServiceRequest into per-
// data-point MetricEntry rows, ready to append to metrics/<name>.ndjson.
//
// All entries inherit the supplied RunContext's Resource — caller-supplied
// resource attributes on the inbound request are dropped. We're a sink for
// metrics produced *during a run*, so the run's resource (commit, branch,
// etc.) is the one that matters for downstream alerting.
func MetricsToEntries(req *cmetricspb.ExportMetricsServiceRequest, run models.RunContext) []models.MetricEntry {
	if req == nil {
		return nil
	}
	var out []models.MetricEntry
	for _, rm := range req.ResourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				out = append(out, metricToEntries(m, run)...)
			}
		}
	}
	return out
}

func metricToEntries(m *metricspb.Metric, run models.RunContext) []models.MetricEntry {
	switch d := m.Data.(type) {
	case *metricspb.Metric_Gauge:
		out := make([]models.MetricEntry, 0, len(d.Gauge.DataPoints))
		for _, dp := range d.Gauge.DataPoints {
			e := baseEntry(m, run, "gauge", dp.TimeUnixNano, dp.StartTimeUnixNano, dp.Attributes)
			v := numberValue(dp)
			e.Value = &v
			out = append(out, e)
		}
		return out
	case *metricspb.Metric_Sum:
		out := make([]models.MetricEntry, 0, len(d.Sum.DataPoints))
		for _, dp := range d.Sum.DataPoints {
			e := baseEntry(m, run, "sum", dp.TimeUnixNano, dp.StartTimeUnixNano, dp.Attributes)
			e.Monotonic = d.Sum.IsMonotonic
			e.Temporality = temporalityString(d.Sum.AggregationTemporality)
			v := numberValue(dp)
			e.Value = &v
			out = append(out, e)
		}
		return out
	case *metricspb.Metric_Histogram:
		out := make([]models.MetricEntry, 0, len(d.Histogram.DataPoints))
		for _, dp := range d.Histogram.DataPoints {
			e := baseEntry(m, run, "histogram", dp.TimeUnixNano, dp.StartTimeUnixNano, dp.Attributes)
			e.Temporality = temporalityString(d.Histogram.AggregationTemporality)
			c := dp.Count
			e.Count = &c
			if dp.Sum != nil {
				s := *dp.Sum
				e.Sum = &s
			}
			if dp.Min != nil {
				v := *dp.Min
				e.Min = &v
			}
			if dp.Max != nil {
				v := *dp.Max
				e.Max = &v
			}
			e.Buckets = histogramBuckets(dp.BucketCounts, dp.ExplicitBounds)
			out = append(out, e)
		}
		return out
	case *metricspb.Metric_ExponentialHistogram:
		out := make([]models.MetricEntry, 0, len(d.ExponentialHistogram.DataPoints))
		for _, dp := range d.ExponentialHistogram.DataPoints {
			e := baseEntry(m, run, "histogram", dp.TimeUnixNano, dp.StartTimeUnixNano, dp.Attributes)
			e.Temporality = temporalityString(d.ExponentialHistogram.AggregationTemporality)
			c := dp.Count
			e.Count = &c
			if dp.Sum != nil {
				s := *dp.Sum
				e.Sum = &s
			}
			if dp.Min != nil {
				v := *dp.Min
				e.Min = &v
			}
			if dp.Max != nil {
				v := *dp.Max
				e.Max = &v
			}
			e.Buckets = exponentialHistogramBuckets(dp)
			out = append(out, e)
		}
		return out
	}
	return nil
}

func baseEntry(m *metricspb.Metric, run models.RunContext, instrument string, ts, startTs uint64, attrs []*commonpb.KeyValue) models.MetricEntry {
	return models.MetricEntry{
		Schema:            models.SchemaV3,
		Name:              m.Name,
		Description:       m.Description,
		Unit:              m.Unit,
		InstrumentType:    instrument,
		TimeUnixNano:      int64(ts),
		StartTimeUnixNano: int64(startTs),
		Attributes:        kvToMap(attrs),
		Resource:          run.Resource,
		TraceID:           run.TraceID,
	}
}

func numberValue(dp *metricspb.NumberDataPoint) float64 {
	switch v := dp.Value.(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		return v.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		return float64(v.AsInt)
	}
	return 0
}

func temporalityString(t metricspb.AggregationTemporality) string {
	switch t {
	case metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA:
		return "delta"
	case metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE:
		return "cumulative"
	}
	return ""
}

func histogramBuckets(counts []uint64, bounds []float64) []models.HistogramBucket {
	out := make([]models.HistogramBucket, 0, len(counts))
	for i, c := range counts {
		var ub *float64
		if i < len(bounds) {
			b := bounds[i]
			ub = &b
		}
		out = append(out, models.HistogramBucket{UpperBound: ub, Count: c})
	}
	return out
}

func exponentialHistogramBuckets(dp *metricspb.ExponentialHistogramDataPoint) []models.HistogramBucket {
	// Convert to explicit-bucket form. base = 2^(2^-scale); each positive
	// bucket i has upper bound base^(offset+i+1). Negative buckets are
	// not represented — only ZeroCount and Positive buckets are emitted.
	// Defrost's expected metric shapes (durations, sizes, counts) are
	// non-negative; revisit if a real use case demands signed values.
	// Always close with a +Inf bucket so the shape mirrors
	// histogramBuckets above.
	scale := dp.Scale
	base := math.Pow(2, math.Pow(2, float64(-scale)))
	var buckets []models.HistogramBucket
	if dp.ZeroCount > 0 {
		zero := 0.0
		buckets = append(buckets, models.HistogramBucket{UpperBound: &zero, Count: dp.ZeroCount})
	}
	if dp.Positive != nil {
		offset := int(dp.Positive.Offset)
		for i, c := range dp.Positive.BucketCounts {
			ub := math.Pow(base, float64(offset+i+1))
			b := ub
			buckets = append(buckets, models.HistogramBucket{UpperBound: &b, Count: c})
		}
	}
	buckets = append(buckets, models.HistogramBucket{UpperBound: nil, Count: 0})
	return buckets
}

func kvToMap(kvs []*commonpb.KeyValue) map[string]any {
	if len(kvs) == 0 {
		return nil
	}
	out := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		out[kv.Key] = anyValueToInterface(kv.Value)
	}
	return out
}

func anyValueToInterface(v *commonpb.AnyValue) any {
	if v == nil {
		return nil
	}
	switch x := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_BoolValue:
		return x.BoolValue
	case *commonpb.AnyValue_IntValue:
		return x.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return x.DoubleValue
	case *commonpb.AnyValue_ArrayValue:
		arr := make([]any, 0, len(x.ArrayValue.Values))
		for _, v := range x.ArrayValue.Values {
			arr = append(arr, anyValueToInterface(v))
		}
		return arr
	case *commonpb.AnyValue_KvlistValue:
		return kvToMap(x.KvlistValue.Values)
	}
	return nil
}
