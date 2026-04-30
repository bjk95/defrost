package otlp

import (
	"bytes"
	"testing"
	"time"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
)

var (
	stubTraceID    = []byte{0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11}
	stubRootSpanID = []byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22}
)

func newRunContext() models.RunContext {
	return models.RunContext{
		RunID:      "run-001",
		TraceID:    stubTraceID,
		RootSpanID: stubRootSpanID,
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			models.StringAttr("service.name", "defrost"),
			models.StringAttr("vcs.repository.ref.revision", "abc123"),
		}},
		StartTimeUnixNano: 1714_500_000_000_000_000,
	}
}

func TestTestResultsToSpans_Pass(t *testing.T) {
	r := models.TestResult{
		Id:        "github.com/x/p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  5 * time.Millisecond,
		StartTime: time.Unix(0, 1714_500_001_000_000_000).UTC(),
	}
	got := TestResultsToSpans([]models.TestResult{r}, newRunContext())
	if len(got) != 1 {
		t.Fatalf("want 1 span, got %d", len(got))
	}
	s := got[0]
	if s.Name != r.Id {
		t.Errorf("name: want %q got %q", r.Id, s.Name)
	}
	if !bytes.Equal(s.TraceId, stubTraceID) {
		t.Errorf("trace_id: %x", s.TraceId)
	}
	if !bytes.Equal(s.ParentSpanId, stubRootSpanID) {
		t.Errorf("parent_span_id: %x", s.ParentSpanId)
	}
	if s.Status.Code != tracepb.Status_STATUS_CODE_OK {
		t.Errorf("status.code: want OK got %v", s.Status.Code)
	}
	if got := models.AttrString(s.Attributes, "test.case.name"); got != r.Id {
		t.Errorf("test.case.name attribute: %q", got)
	}
	if got := models.AttrString(s.Attributes, "test.case.result.status"); got != "passed" {
		t.Errorf("test.case.result.status: %q", got)
	}
	if got := models.AttrString(s.Attributes, "defrost.run_id"); got != "run-001" {
		t.Errorf("defrost.run_id attribute missing: %q", got)
	}
	if got := models.AttrString(s.Attributes, "test.suite.name"); got != "github.com/x/p" {
		t.Errorf("test.suite.name: %q", got)
	}
	if got := models.AttrString(s.Attributes, "code.function"); got != "TestA" {
		t.Errorf("code.function: %q", got)
	}
	if d := s.EndTimeUnixNano - s.StartTimeUnixNano; d != uint64(5*time.Millisecond) {
		t.Errorf("duration nanos: want %d got %d", uint64(5*time.Millisecond), d)
	}
	if len(s.Events) != 0 {
		t.Errorf("expected no events for passing test with empty output, got %v", s.Events)
	}
}

func TestTestResultsToSpans_FailWithOutput(t *testing.T) {
	r := models.TestResult{
		Id:        "github.com/x/p/TestB",
		Ran:       true,
		Passed:    false,
		Duration:  1 * time.Millisecond,
		StartTime: time.Unix(0, 1).UTC(),
		Output:    "FAIL\nexpected 1 got 2\n",
	}
	got := TestResultsToSpans([]models.TestResult{r}, newRunContext())
	s := got[0]
	if s.Status.Code != tracepb.Status_STATUS_CODE_ERROR {
		t.Errorf("status.code: want ERROR got %v", s.Status.Code)
	}
	if s.Status.Message != "FAIL" {
		t.Errorf("status.message: want first line 'FAIL', got %q", s.Status.Message)
	}
	if got := models.AttrString(s.Attributes, "test.case.result.status"); got != "failed" {
		t.Errorf("result status: %q", got)
	}
	if len(s.Events) != 1 {
		t.Fatalf("want 1 event for non-empty output, got %d", len(s.Events))
	}
	if s.Events[0].Name != "test.output" {
		t.Errorf("event name: %q", s.Events[0].Name)
	}
	if got := models.AttrString(s.Events[0].Attributes, "body"); got != r.Output {
		t.Errorf("event body: %q", got)
	}
}

func TestTestResultsToSpans_Skip(t *testing.T) {
	r := models.TestResult{Id: "p/TestSkipped", Ran: false}
	s := TestResultsToSpans([]models.TestResult{r}, newRunContext())[0]
	if s.Status.Code != tracepb.Status_STATUS_CODE_UNSET {
		t.Errorf("status.code: want UNSET got %v", s.Status.Code)
	}
	if got := models.AttrString(s.Attributes, "test.case.result.status"); got != "skipped" {
		t.Errorf("result status: %q", got)
	}
}

func TestTestResultsToSpans_Panic(t *testing.T) {
	r := models.TestResult{
		Id:     "p/TestPanic",
		Ran:    true,
		Passed: false,
		Output: "panic: nil pointer\nfoo()\n",
	}
	s := TestResultsToSpans([]models.TestResult{r}, newRunContext())[0]
	if s.Status.Code != tracepb.Status_STATUS_CODE_ERROR {
		t.Errorf("status.code: want ERROR got %v", s.Status.Code)
	}
	if got := models.AttrString(s.Attributes, "test.case.result.status"); got != "aborted" {
		t.Errorf("result status: want aborted got %q", got)
	}
}

func TestTestResultsToSpans_PytestId(t *testing.T) {
	r := models.TestResult{
		Id:     "tests/test_module.py::TestClass::test_method",
		Ran:    true,
		Passed: true,
	}
	s := TestResultsToSpans([]models.TestResult{r}, newRunContext())[0]
	if got := models.AttrString(s.Attributes, "test.suite.name"); got != "tests/test_module.py" {
		t.Errorf("test.suite.name: %q", got)
	}
	if got := models.AttrString(s.Attributes, "code.function"); got != "test_method" {
		t.Errorf("code.function: %q", got)
	}
}

func TestTestResultsToSpans_Siblings(t *testing.T) {
	rs := []models.TestResult{
		{Id: "p/TestA", Ran: true, Passed: true},
		{Id: "p/TestA/sub1", Ran: true, Passed: true},
		{Id: "p/TestA/sub2", Ran: true, Passed: false},
	}
	got := TestResultsToSpans(rs, newRunContext())
	if len(got) != 3 {
		t.Fatalf("want 3 spans, got %d", len(got))
	}
	for i, s := range got {
		if !bytes.Equal(s.ParentSpanId, stubRootSpanID) {
			t.Errorf("span %d: parent_span_id should be the run root, got %x", i, s.ParentSpanId)
		}
	}
}

func strKV(k, v string) *commonpb.KeyValue { return models.StringAttr(k, v) }

func makeRequest(metrics ...*metricspb.Metric) *cmetricspb.ExportMetricsServiceRequest {
	return &cmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{strKV("service.name", "client")}},
			ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: metrics}},
		}},
	}
}

func TestMetricsToEntries_Gauge(t *testing.T) {
	m := &metricspb.Metric{
		Name: "db.connection_pool.size",
		Unit: "{connections}",
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: 1714_500_000_000_000_000,
				Attributes:   []*commonpb.KeyValue{strKV("db.system", "postgresql")},
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 12.0},
			}},
		}},
	}
	got := MetricsToEntries(makeRequest(m), newRunContext())
	if len(got) != 1 {
		t.Fatalf("want 1 metric, got %d", len(got))
	}
	out := got[0]
	if out.Name != "db.connection_pool.size" {
		t.Errorf("name: %q", out.Name)
	}
	if out.Unit != "{connections}" {
		t.Errorf("unit: %q", out.Unit)
	}
	g, ok := out.Data.(*metricspb.Metric_Gauge)
	if !ok {
		t.Fatalf("not a gauge: %T", out.Data)
	}
	if len(g.Gauge.DataPoints) != 1 {
		t.Fatalf("want 1 data point, got %d", len(g.Gauge.DataPoints))
	}
	dp := g.Gauge.DataPoints[0]
	v, ok := dp.Value.(*metricspb.NumberDataPoint_AsDouble)
	if !ok || v.AsDouble != 12.0 {
		t.Errorf("value: %v", dp.Value)
	}
	if got := models.AttrString(dp.Attributes, "db.system"); got != "postgresql" {
		t.Errorf("attribute missing: %q", got)
	}
	// Trace exemplar present.
	if len(dp.Exemplars) != 1 {
		t.Fatalf("want 1 exemplar, got %d", len(dp.Exemplars))
	}
	if !bytes.Equal(dp.Exemplars[0].TraceId, stubTraceID) {
		t.Errorf("exemplar trace_id: %x", dp.Exemplars[0].TraceId)
	}
}

func TestMetricsToEntries_SumDelta(t *testing.T) {
	m := &metricspb.Metric{
		Name: "http.server.request.count",
		Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
			IsMonotonic:            true,
			AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano:      1714_500_000_000_000_000,
				StartTimeUnixNano: 1714_499_990_000_000_000,
				Value:             &metricspb.NumberDataPoint_AsInt{AsInt: 7},
			}},
		}},
	}
	got := MetricsToEntries(makeRequest(m), newRunContext())[0]
	sum, ok := got.Data.(*metricspb.Metric_Sum)
	if !ok {
		t.Fatalf("not a sum: %T", got.Data)
	}
	if !sum.Sum.IsMonotonic {
		t.Error("monotonic should be true")
	}
	if sum.Sum.AggregationTemporality != metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
		t.Errorf("temporality: %v", sum.Sum.AggregationTemporality)
	}
	dp := sum.Sum.DataPoints[0]
	v, ok := dp.Value.(*metricspb.NumberDataPoint_AsInt)
	if !ok || v.AsInt != 7 {
		t.Errorf("value: %v", dp.Value)
	}
}

func TestMetricsToEntries_Histogram(t *testing.T) {
	m := &metricspb.Metric{
		Name: "http.server.request.duration",
		Unit: "s",
		Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
			AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
			DataPoints: []*metricspb.HistogramDataPoint{{
				TimeUnixNano:   1714_500_000_000_000_000,
				Count:          3,
				Sum:            ptr(0.45),
				Min:            ptr(0.05),
				Max:            ptr(0.25),
				BucketCounts:   []uint64{1, 1, 1},
				ExplicitBounds: []float64{0.1, 0.2},
			}},
		}},
	}
	got := MetricsToEntries(makeRequest(m), newRunContext())[0]
	hist, ok := got.Data.(*metricspb.Metric_Histogram)
	if !ok {
		t.Fatalf("not a histogram: %T", got.Data)
	}
	dp := hist.Histogram.DataPoints[0]
	if dp.Count != 3 {
		t.Errorf("count: %d", dp.Count)
	}
	if dp.Sum == nil || *dp.Sum != 0.45 {
		t.Errorf("sum: %v", dp.Sum)
	}
	if len(dp.BucketCounts) != 3 || len(dp.ExplicitBounds) != 2 {
		t.Errorf("buckets shape: counts=%v bounds=%v", dp.BucketCounts, dp.ExplicitBounds)
	}
}

func TestMetricsToEntries_ExponentialHistogram(t *testing.T) {
	m := &metricspb.Metric{
		Name: "rpc.client.duration",
		Unit: "ms",
		Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
			AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
			DataPoints: []*metricspb.ExponentialHistogramDataPoint{{
				TimeUnixNano: 1714_500_000_000_000_000,
				Count:        4,
				Sum:          ptr(10.0),
				Scale:        0,
				ZeroCount:    1,
				Positive: &metricspb.ExponentialHistogramDataPoint_Buckets{
					Offset:       0,
					BucketCounts: []uint64{2, 1},
				},
			}},
		}},
	}
	got := MetricsToEntries(makeRequest(m), newRunContext())[0]
	exp, ok := got.Data.(*metricspb.Metric_ExponentialHistogram)
	if !ok {
		t.Fatalf("not an exponential histogram: %T", got.Data)
	}
	if exp.ExponentialHistogram.DataPoints[0].Count != 4 {
		t.Errorf("count: %d", exp.ExponentialHistogram.DataPoints[0].Count)
	}
}

func ptr[T any](v T) *T { return &v }
