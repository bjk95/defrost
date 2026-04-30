package otlp

import (
	"testing"
	"time"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"github.com/bjk95/defrost/internal/models"
)

func newRunContext() models.RunContext {
	return models.RunContext{
		RunID:             "run-001",
		TraceID:           "11111111111111111111111111111111",
		RootSpanID:        "2222222222222222",
		Resource:          map[string]any{"service.name": "defrost", "vcs.repository.ref.revision": "abc123"},
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
	if s.TraceID != "11111111111111111111111111111111" {
		t.Errorf("trace_id: %q", s.TraceID)
	}
	if s.ParentSpanID != "2222222222222222" {
		t.Errorf("parent_span_id: %q", s.ParentSpanID)
	}
	if s.Status.Code != "OK" {
		t.Errorf("status.code: want OK got %q", s.Status.Code)
	}
	if s.Attributes["test.case.name"] != r.Id {
		t.Errorf("test.case.name attribute missing: %+v", s.Attributes)
	}
	if s.Attributes["test.case.result.status"] != "passed" {
		t.Errorf("test.case.result.status: %v", s.Attributes["test.case.result.status"])
	}
	if s.Attributes["defrost.run_id"] != "run-001" {
		t.Errorf("defrost.run_id attribute missing: %+v", s.Attributes)
	}
	if s.Attributes["test.suite.name"] != "github.com/x/p" {
		t.Errorf("test.suite.name: %v", s.Attributes["test.suite.name"])
	}
	if s.Attributes["code.function"] != "TestA" {
		t.Errorf("code.function: %v", s.Attributes["code.function"])
	}
	if got := s.EndTimeUnixNano - s.StartTimeUnixNano; got != int64(5*time.Millisecond) {
		t.Errorf("duration nanos: want %d got %d", int64(5*time.Millisecond), got)
	}
	if len(s.Events) != 0 {
		t.Errorf("expected no events for passing test with empty output, got %+v", s.Events)
	}
	if s.Resource["service.name"] != "defrost" {
		t.Errorf("resource not inlined: %+v", s.Resource)
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
	if s.Status.Code != "ERROR" {
		t.Errorf("status.code: want ERROR got %q", s.Status.Code)
	}
	if s.Status.Message != "FAIL" {
		t.Errorf("status.message: want first line 'FAIL', got %q", s.Status.Message)
	}
	if s.Attributes["test.case.result.status"] != "failed" {
		t.Errorf("result status: %v", s.Attributes["test.case.result.status"])
	}
	if len(s.Events) != 1 {
		t.Fatalf("want 1 event for non-empty output, got %d", len(s.Events))
	}
	if s.Events[0].Name != "test.output" {
		t.Errorf("event name: %q", s.Events[0].Name)
	}
	if s.Events[0].Attributes["body"] != r.Output {
		t.Errorf("event body: %v", s.Events[0].Attributes["body"])
	}
}

func TestTestResultsToSpans_Skip(t *testing.T) {
	r := models.TestResult{Id: "p/TestSkipped", Ran: false}
	s := TestResultsToSpans([]models.TestResult{r}, newRunContext())[0]
	if s.Status.Code != "UNSET" {
		t.Errorf("status.code: want UNSET got %q", s.Status.Code)
	}
	if s.Attributes["test.case.result.status"] != "skipped" {
		t.Errorf("result status: %v", s.Attributes["test.case.result.status"])
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
	if s.Status.Code != "ERROR" {
		t.Errorf("status.code: want ERROR got %q", s.Status.Code)
	}
	if s.Attributes["test.case.result.status"] != "aborted" {
		t.Errorf("result status: want aborted got %v", s.Attributes["test.case.result.status"])
	}
}

func TestTestResultsToSpans_PytestId(t *testing.T) {
	r := models.TestResult{
		Id:     "tests/test_module.py::TestClass::test_method",
		Ran:    true,
		Passed: true,
	}
	s := TestResultsToSpans([]models.TestResult{r}, newRunContext())[0]
	if s.Attributes["test.suite.name"] != "tests/test_module.py" {
		t.Errorf("test.suite.name: %v", s.Attributes["test.suite.name"])
	}
	if s.Attributes["code.function"] != "test_method" {
		t.Errorf("code.function: %v", s.Attributes["code.function"])
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
		if s.ParentSpanID != "2222222222222222" {
			t.Errorf("span %d: parent_span_id should be the run root, got %q", i, s.ParentSpanID)
		}
	}
}

func strKV(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func makeRequest(metrics ...*metricspb.Metric) *cmetricspb.ExportMetricsServiceRequest {
	return &cmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{strKV("service.name", "client")}},
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: metrics,
			}},
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
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	e := got[0]
	if e.Name != "db.connection_pool.size" {
		t.Errorf("name: %q", e.Name)
	}
	if e.InstrumentType != "gauge" {
		t.Errorf("instrument_type: %q", e.InstrumentType)
	}
	if e.Unit != "{connections}" {
		t.Errorf("unit: %q", e.Unit)
	}
	if e.Value == nil || *e.Value != 12.0 {
		t.Errorf("value: %v", e.Value)
	}
	if e.Attributes["db.system"] != "postgresql" {
		t.Errorf("attribute missing: %+v", e.Attributes)
	}
	if e.Resource["service.name"] != "defrost" {
		t.Errorf("resource: defrost RunContext should override caller resource: %+v", e.Resource)
	}
	if e.TraceID != "11111111111111111111111111111111" {
		t.Errorf("trace_id: %q", e.TraceID)
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
	if got.InstrumentType != "sum" {
		t.Errorf("instrument_type: %q", got.InstrumentType)
	}
	if !got.Monotonic {
		t.Error("monotonic should be true")
	}
	if got.Temporality != "delta" {
		t.Errorf("temporality: %q", got.Temporality)
	}
	if got.Value == nil || *got.Value != 7 {
		t.Errorf("value: %v", got.Value)
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
	if got.InstrumentType != "histogram" {
		t.Errorf("instrument_type: %q", got.InstrumentType)
	}
	if got.Count == nil || *got.Count != 3 {
		t.Errorf("count: %v", got.Count)
	}
	if got.Sum == nil || *got.Sum != 0.45 {
		t.Errorf("sum: %v", got.Sum)
	}
	if got.Min == nil || *got.Min != 0.05 {
		t.Errorf("min: %v", got.Min)
	}
	if got.Max == nil || *got.Max != 0.25 {
		t.Errorf("max: %v", got.Max)
	}
	if len(got.Buckets) != 3 {
		t.Fatalf("buckets: want 3, got %d (%v)", len(got.Buckets), got.Buckets)
	}
	if got.Buckets[0].UpperBound == nil || *got.Buckets[0].UpperBound != 0.1 || got.Buckets[0].Count != 1 {
		t.Errorf("bucket 0: %+v", got.Buckets[0])
	}
	if got.Buckets[2].UpperBound != nil {
		t.Errorf("bucket 2 (+Inf) should have nil UpperBound, got %v", got.Buckets[2].UpperBound)
	}
}

func ptr[T any](v T) *T { return &v }
