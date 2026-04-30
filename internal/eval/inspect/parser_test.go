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

func TestParseSmoke(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "smoke.json")), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	if !tests[0].Passed {
		t.Fatalf("expected sample 1 passed=true")
	}
	if tests[1].Passed {
		t.Fatalf("expected sample 2 passed=false")
	}
	wantID := `task="capital_cities",sample="1"`
	if tests[0].Id != wantID {
		t.Fatalf("expected id=%q, got %q", wantID, tests[0].Id)
	}
	if tests[0].Output != "Paris" {
		t.Fatalf("expected output=Paris, got %q", tests[0].Output)
	}
	if !tests[0].Ran || !tests[1].Ran {
		t.Fatalf("expected Ran=true for both samples")
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if metrics[0].Name != "eval.capital_cities.match" {
		t.Fatalf("expected metric eval.capital_cities.match, got %q", metrics[0].Name)
	}
	if got := gaugeValue(t, metrics[0]); got != 1.0 {
		t.Fatalf("expected score 1.0, got %v", got)
	}
	if got := gaugeValue(t, metrics[1]); got != 0.0 {
		t.Fatalf("expected score 0.0, got %v", got)
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
	if got := attrString(metrics[0], "test.case.name"); got != wantID {
		t.Fatalf("expected test.case.name=%q, got %q", wantID, got)
	}
	if got := attrString(metrics[0], "test.suite.name"); got != "capital_cities" {
		t.Fatalf("expected test.suite.name=capital_cities, got %q", got)
	}
	if got := attrString(metrics[0], "gen_ai.request.model"); got != "openai/gpt-4o" {
		t.Fatalf("expected gen_ai.request.model=openai/gpt-4o, got %q", got)
	}
	if got := attrString(metrics[0], "gen_ai.evaluation.explanation"); got != "Exact match" {
		t.Fatalf("expected explanation=Exact match, got %q", got)
	}
}

func TestParseMultiScorer(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "multi_scorer.json")), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	if !tests[0].Passed {
		t.Fatalf("expected passed=true (both scorers ≥ 0.5)")
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics (one per scorer), got %d", len(metrics))
	}
	wantNames := map[string]bool{"eval.qa_eval.accuracy": false, "eval.qa_eval.f1_score": false}
	for _, m := range metrics {
		wantNames[m.Name] = true
	}
	for k, v := range wantNames {
		if !v {
			t.Fatalf("missing expected metric %s", k)
		}
	}
}

func TestParseLetterScorerSkipped(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "letter_scorer.json")), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	// Both samples have only Letter scorers, which the parser skips. The
	// fallback for "no numeric scorers" is Passed=true (no failure signal).
	if !tests[0].Passed || !tests[1].Passed {
		t.Fatalf("expected both samples passed=true (no numeric scorers means no failure)")
	}
	if len(metrics) != 0 {
		t.Fatalf("expected 0 metrics (Letter scorers skipped), got %d", len(metrics))
	}
}

func TestParseNoScores(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "no_scores.json")), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	if !tests[0].Passed {
		t.Fatalf("expected passed=true (no scorers means no failure)")
	}
	if len(metrics) != 0 {
		t.Fatalf("expected 0 metrics, got %d", len(metrics))
	}
}

func TestParseStringID(t *testing.T) {
	tests, _, err := ParseFile(bytes.NewReader(loadFixture(t, "string_id.json")), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	want := `task="string_id_eval",sample="case-paris"`
	if tests[0].Id != want {
		t.Fatalf("expected id=%q, got %q", want, tests[0].Id)
	}
}

func TestParseCompoundValueSkipped(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "compound_value.json")), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	if !tests[0].Passed {
		t.Fatalf("expected passed=true (accuracy=0.95 ≥ 0.5)")
	}
	// Compound `multidim` is skipped; only `accuracy` produces a metric.
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric (compound skipped), got %d", len(metrics))
	}
	if metrics[0].Name != "eval.compound_eval.accuracy" {
		t.Fatalf("expected eval.compound_eval.accuracy, got %q", metrics[0].Name)
	}
}

func TestParseEmptySamples(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "empty_samples.json")), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected zero results, got %d tests / %d metrics", len(tests), len(metrics))
	}
}

func TestParseIntegerScore(t *testing.T) {
	tests, metrics, err := ParseFile(bytes.NewReader(loadFixture(t, "integer_score.json")), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 || !tests[0].Passed {
		t.Fatalf("expected 1 passing test, got %v", tests)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if got := gaugeValue(t, metrics[0]); got != 1.0 {
		t.Fatalf("expected score 1.0 from integer 1, got %v", got)
	}
}

func TestParseDeterministicCaseNames(t *testing.T) {
	raw := loadFixture(t, "smoke.json")
	tests1, _, err := ParseFile(bytes.NewReader(raw), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tests2, _, err := ParseFile(bytes.NewReader(raw), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests1) != len(tests2) {
		t.Fatalf("non-deterministic length: %d vs %d", len(tests1), len(tests2))
	}
	for i := range tests1 {
		if tests1[i].Id != tests2[i].Id {
			t.Fatalf("non-deterministic id: %q vs %q", tests1[i].Id, tests2[i].Id)
		}
	}
}

func TestParseQualifiesMetricNameWithScope(t *testing.T) {
	_, metrics, err := ParseFile(
		bytes.NewReader(loadFixture(t, "smoke.json")),
		"examples/inspect.task.py",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	want := "eval.examples/inspect.task.py.capital_cities.match"
	if metrics[0].Name != want {
		t.Fatalf("metric name = %q, want %q", metrics[0].Name, want)
	}
	// gen_ai.evaluation.name stays the un-scoped scorer key so OTel
	// consumers can still aggregate by scorer type across tasks.
	if got := attrString(metrics[0], "gen_ai.evaluation.name"); got != "match" {
		t.Fatalf("gen_ai.evaluation.name = %q, want %q", got, "match")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, _, err := ParseFile(bytes.NewReader([]byte("not json")), "")
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestNumericScore(t *testing.T) {
	cases := []struct {
		in     any
		want   float64
		wantOK bool
	}{
		{1.0, 1.0, true},
		{0.85, 0.85, true},
		{float64(0), 0.0, true},
		{"C", 0, false},
		{"I", 0, false},
		{"1.0", 0, false},
		{nil, 0, false},
		{map[string]any{"precision": 0.9}, 0, false},
		{[]any{1.0}, 0, false},
	}
	for _, tc := range cases {
		got, ok := numericScore(tc.in)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("numericScore(%v) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}
