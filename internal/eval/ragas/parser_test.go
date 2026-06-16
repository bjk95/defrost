package ragas

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func gaugeValue(t *testing.T, m *metricspb.Metric) float64 {
	t.Helper()
	g, ok := m.Data.(*metricspb.Metric_Gauge)
	if !ok {
		t.Fatalf("expected gauge, got %T", m.Data)
	}
	if len(g.Gauge.DataPoints) != 1 {
		t.Fatalf("expected 1 data point, got %d", len(g.Gauge.DataPoints))
	}
	dp := g.Gauge.DataPoints[0]
	dv, ok := dp.Value.(*metricspb.NumberDataPoint_AsDouble)
	if !ok {
		t.Fatalf("expected AsDouble, got %T", dp.Value)
	}
	return dv.AsDouble
}

func attrString(m *metricspb.Metric, key string) string {
	g, _ := m.Data.(*metricspb.Metric_Gauge)
	if g == nil || len(g.Gauge.DataPoints) == 0 {
		return ""
	}
	for _, kv := range g.Gauge.DataPoints[0].Attributes {
		if kv.Key == key {
			if sv, ok := kv.Value.Value.(*commonpb.AnyValue_StringValue); ok {
				return sv.StringValue
			}
		}
	}
	return ""
}

func attrPresent(m *metricspb.Metric, key string) bool {
	g, _ := m.Data.(*metricspb.Metric_Gauge)
	if g == nil || len(g.Gauge.DataPoints) == 0 {
		return false
	}
	for _, kv := range g.Gauge.DataPoints[0].Attributes {
		if kv.Key == key {
			return true
		}
	}
	return false
}

func TestParseSmoke(t *testing.T) {
	metrics, err := Parse(bytes.NewReader(loadFixture(t, "ragas_smoke.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Two rows × two metrics = 4 metrics. Row 2 has null scores, contributes none.
	if len(metrics) != 4 {
		t.Fatalf("expected 4 metrics, got %d", len(metrics))
	}

	type key struct {
		caseName string
		name     string
	}
	byKey := map[key]*metricspb.Metric{}
	for _, m := range metrics {
		byKey[key{attrString(m, "test.case.name"), m.Name}] = m
	}

	row0Faith, ok := byKey[key{"ragas_row_0", "eval.faithfulness"}]
	if !ok {
		t.Fatalf("missing row 0 faithfulness metric; got keys %v", byKey)
	}
	if got := gaugeValue(t, row0Faith); got != 0.85 {
		t.Fatalf("row 0 faithfulness: expected 0.85, got %v", got)
	}
	if got := attrString(row0Faith, "gen_ai.evaluation.name"); got != "faithfulness" {
		t.Fatalf("row 0 faithfulness: gen_ai.evaluation.name = %q, want faithfulness", got)
	}

	row1Faith, ok := byKey[key{"ragas_row_1", "eval.faithfulness"}]
	if !ok {
		t.Fatalf("missing row 1 faithfulness metric")
	}
	// 0.0 must not be filtered or coalesced — it's a meaningful score.
	if got := gaugeValue(t, row1Faith); got != 0.0 {
		t.Fatalf("row 1 faithfulness: expected 0.0, got %v", got)
	}

	if _, ok := byKey[key{"ragas_row_0", "eval.answer_relevancy"}]; !ok {
		t.Fatalf("missing row 0 answer_relevancy metric")
	}
	if _, ok := byKey[key{"ragas_row_1", "eval.answer_relevancy"}]; !ok {
		t.Fatalf("missing row 1 answer_relevancy metric")
	}

	// Row 2's null scores must not have produced metrics.
	if _, ok := byKey[key{"ragas_row_2", "eval.faithfulness"}]; ok {
		t.Fatalf("null score in row 2 should not have produced a metric")
	}
}

func TestParseSingleRow(t *testing.T) {
	metrics, err := Parse(bytes.NewReader(loadFixture(t, "single_row.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.Name != "eval.faithfulness" {
		t.Fatalf("metric name = %q, want eval.faithfulness", m.Name)
	}
	if got := gaugeValue(t, m); got != 0.95 {
		t.Fatalf("score = %v, want 0.95", got)
	}
	if got := attrString(m, "test.case.name"); got != "ragas_row_0" {
		t.Fatalf("test.case.name = %q, want ragas_row_0", got)
	}
}

func TestParseSkipsKnownInputColumns(t *testing.T) {
	// `question`, `answer`, `contexts`, `ground_truth` carry user input
	// and references — never scores. The parser must not emit metrics
	// named eval.question, eval.answer, etc.
	metrics, err := Parse(bytes.NewReader(loadFixture(t, "single_row.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, m := range metrics {
		if strings.HasPrefix(m.Name, "eval.question") ||
			strings.HasPrefix(m.Name, "eval.answer") && m.Name != "eval.answer_relevancy" ||
			strings.HasPrefix(m.Name, "eval.contexts") ||
			strings.HasPrefix(m.Name, "eval.ground_truth") {
			t.Fatalf("input column leaked as metric: %q", m.Name)
		}
	}
}

func TestParseHandlesLegacyColumnNames(t *testing.T) {
	// RAGAS 0.1.x used `question`/`answer`/`ground_truths`. 0.2.x renamed
	// these to `user_input`/`response`/`reference`. The parser must
	// recognise both as inputs (not score columns).
	metrics, err := Parse(bytes.NewReader(loadFixture(t, "legacy_columns.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d (%v)", len(metrics), metricNames(metrics))
	}
	wantNames := map[string]bool{"eval.faithfulness": false, "eval.answer_relevancy": false}
	for _, m := range metrics {
		wantNames[m.Name] = true
	}
	for k, v := range wantNames {
		if !v {
			t.Fatalf("missing expected metric %q", k)
		}
	}
}

func TestParseOmitsThresholdAndJudgeModel(t *testing.T) {
	// Spec §6: RAGAS surfaces neither a threshold nor a judge model in its
	// result object. The parser must omit both attributes rather than
	// emitting an empty-string placeholder.
	metrics, err := Parse(bytes.NewReader(loadFixture(t, "single_row.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatalf("expected at least one metric")
	}
	if attrPresent(metrics[0], "defrost.eval.threshold") {
		t.Fatalf("did not expect defrost.eval.threshold attribute")
	}
	if attrPresent(metrics[0], "defrost.eval.judge_model") {
		t.Fatalf("did not expect defrost.eval.judge_model attribute")
	}
}

func TestParseEmpty(t *testing.T) {
	metrics, err := Parse(bytes.NewReader(loadFixture(t, "empty.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) != 0 {
		t.Fatalf("expected 0 metrics, got %d", len(metrics))
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := Parse(strings.NewReader("not json"))
	if err == nil {
		t.Fatalf("expected error on invalid JSON")
	}
}

func TestParseDeterministicOrdering(t *testing.T) {
	// Within a single fixture, two parse calls must emit metrics in the
	// same order. JSON object iteration in Go is randomised, so the
	// parser must sort keys before mapping them to metrics.
	raw := loadFixture(t, "ragas_smoke.json")
	a, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			t.Fatalf("ordering not stable at %d: %q vs %q", i, a[i].Name, b[i].Name)
		}
		if attrString(a[i], "test.case.name") != attrString(b[i], "test.case.name") {
			t.Fatalf("case name ordering not stable at %d", i)
		}
	}
}

func metricNames(ms []*metricspb.Metric) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}
