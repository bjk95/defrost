package inspect

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestParseSmokePassFail(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "smoke.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	if tests[0].Id != "sample_1" {
		t.Fatalf("expected Id=sample_1, got %q", tests[0].Id)
	}
	if !tests[0].Passed {
		t.Fatalf("expected sample_1 to pass (score 1.0)")
	}
	if tests[1].Id != "sample_2" {
		t.Fatalf("expected Id=sample_2, got %q", tests[1].Id)
	}
	if tests[1].Passed {
		t.Fatalf("expected sample_2 to fail (score 0.0)")
	}
	if tests[0].Output != "Paris" {
		t.Fatalf("expected sample_1 output=Paris, got %q", tests[0].Output)
	}

	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics (one per sample × scorer), got %d", len(metrics))
	}
	if metrics[0].Name != "eval.match" {
		t.Fatalf("expected metric name eval.match, got %q", metrics[0].Name)
	}
	if got := gaugeValue(t, metrics[0]); got != 1.0 {
		t.Fatalf("expected metric[0] score 1.0, got %v", got)
	}
	if got := gaugeValue(t, metrics[1]); got != 0.0 {
		t.Fatalf("expected metric[1] score 0.0, got %v", got)
	}
	if got := attrString(metrics[0], "gen_ai.evaluation.name"); got != "match" {
		t.Fatalf("expected gen_ai.evaluation.name=match, got %q", got)
	}
	if got := attrString(metrics[0], "gen_ai.evaluation.score.label"); got != "pass" {
		t.Fatalf("expected score.label=pass, got %q", got)
	}
	if got := attrString(metrics[1], "gen_ai.evaluation.score.label"); got != "fail" {
		t.Fatalf("expected score.label=fail, got %q", got)
	}
	if got := attrString(metrics[0], "test.case.name"); got != "sample_1" {
		t.Fatalf("expected test.case.name=sample_1, got %q", got)
	}
	if got := attrString(metrics[0], "gen_ai.request.model"); got != "openai/gpt-4o" {
		t.Fatalf("expected gen_ai.request.model=openai/gpt-4o, got %q", got)
	}
	if got := attrString(metrics[0], "test.suite.name"); got != "capital_cities" {
		t.Fatalf("expected test.suite.name=capital_cities, got %q", got)
	}
	if got := attrString(metrics[0], "gen_ai.evaluation.explanation"); got != "Correct" {
		t.Fatalf("expected explanation=Correct, got %q", got)
	}
}

func TestParseLetterScorerSkipped(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "letter_skipped.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	// Both samples have only Letter scorers; no numeric scorers means we
	// can't claim they passed. Both should be Ran=true, Passed=false.
	for i, tr := range tests {
		if !tr.Ran {
			t.Fatalf("sample[%d] expected Ran=true, got false", i)
		}
		if tr.Passed {
			t.Fatalf("sample[%d] expected Passed=false (no numeric scorers), got true", i)
		}
	}
	if len(metrics) != 0 {
		t.Fatalf("expected 0 metrics (Letter scorers skipped), got %d", len(metrics))
	}
}

func TestParseMultiScorer(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "multi_scorer.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	if !tests[0].Passed {
		t.Fatalf("expected pass: both numeric scorers >= 0.5 (1.0 and 0.85)")
	}
	// Two numeric scorers (accuracy, f1_score); the Letter scorer is skipped.
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics (Letter scorer skipped), got %d", len(metrics))
	}
	names := map[string]bool{}
	for _, m := range metrics {
		names[m.Name] = true
	}
	if !names["eval.accuracy"] || !names["eval.f1_score"] {
		t.Fatalf("expected eval.accuracy and eval.f1_score, got %v", names)
	}
	if names["eval.letter_grade"] {
		t.Fatal("Letter scorer must not produce a metric")
	}
}

func TestParseNoScoresMap(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "no_scores.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	if !tests[0].Ran {
		t.Fatalf("expected Ran=true even with no scores")
	}
	if tests[0].Passed {
		t.Fatalf("expected Passed=false when no scorers ran")
	}
	if tests[0].Id != "sample_7" {
		t.Fatalf("expected Id=sample_7, got %q", tests[0].Id)
	}
	if tests[0].Output != "raw output, no scoring ran" {
		t.Fatalf("expected output preserved, got %q", tests[0].Output)
	}
	if len(metrics) != 0 {
		t.Fatalf("expected 0 metrics, got %d", len(metrics))
	}
}

func TestParseStringSampleID(t *testing.T) {
	tests, _, err := ParseFile(bytes.NewReader(loadFixture(t, "string_id.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	if tests[0].Id != "sample_qa_001" {
		t.Fatalf("expected Id=sample_qa_001, got %q", tests[0].Id)
	}
	if tests[1].Id != "sample_qa_002" {
		t.Fatalf("expected Id=sample_qa_002, got %q", tests[1].Id)
	}
}

func TestParseEmpty(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "empty.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected zero results, got %d tests / %d metrics", len(tests), len(metrics))
	}
}

func TestParseDecodeError(t *testing.T) {
	_, _, err := ParseFile(bytes.NewReader([]byte("not json")))
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestNumericScoreVariants(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want float64
		ok   bool
	}{
		{"float", 0.75, 0.75, true},
		{"int decoded as float64", float64(1), 1.0, true},
		{"zero", 0.0, 0.0, true},
		{"bool true", true, 1.0, true},
		{"bool false", false, 0.0, true},
		{"letter C", "C", 0, false},
		{"letter I", "I", 0, false},
		{"nil", nil, 0, false},
		{"compound", map[string]any{"primary": 0.9}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := numericScore(tc.v)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSampleCaseName(t *testing.T) {
	cases := []struct {
		name string
		id   any
		want string
	}{
		{"int via float64", float64(1), "sample_1"},
		{"string", "qa_42", "sample_qa_42"},
		{"large int", float64(1234567), "sample_1234567"},
		{"nil", nil, "sample_<unnamed>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sampleCaseName(tc.id)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
