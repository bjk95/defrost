package promptfoo

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

// gaugeValue returns the AsDouble value of a single-data-point gauge metric.
// Fails the test if m isn't a gauge with exactly one numeric data point.
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

// attrString returns the string value of the named attribute on the gauge's
// data point, or "" if missing or non-string.
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

// attrDouble returns the double value of the named attribute on the gauge's
// data point, plus a bool indicating presence.
func attrDouble(m *metricspb.Metric, key string) (float64, bool) {
	g, _ := m.Data.(*metricspb.Metric_Gauge)
	if g == nil || len(g.Gauge.DataPoints) == 0 {
		return 0, false
	}
	for _, kv := range g.Gauge.DataPoints[0].Attributes {
		if kv.Key == key {
			if dv, ok := kv.Value.Value.(*commonpb.AnyValue_DoubleValue); ok {
				return dv.DoubleValue, true
			}
		}
	}
	return 0, false
}

func TestParseSingleAssertion(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "single_assertion.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test result, got %d", len(tests))
	}
	if !tests[0].Passed {
		t.Fatalf("expected Passed=true, got false")
	}
	if tests[0].Id != `country="France",provider="openai:gpt-4o"` {
		t.Fatalf("expected Id=country=\"France\",provider=\"openai:gpt-4o\", got %q", tests[0].Id)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.Name != "eval.contains" {
		t.Fatalf("expected metric name eval.contains, got %q", m.Name)
	}
	if got := gaugeValue(t, m); got != 1.0 {
		t.Fatalf("expected score 1.0, got %v", got)
	}
	if got := attrString(m, "gen_ai.evaluation.name"); got != "contains" {
		t.Fatalf("expected gen_ai.evaluation.name=contains, got %q", got)
	}
	if got := attrString(m, "gen_ai.evaluation.score.label"); got != "pass" {
		t.Fatalf("expected gen_ai.evaluation.score.label=pass, got %q", got)
	}
	if got := attrString(m, "test.case.name"); got != `country="France",provider="openai:gpt-4o"` {
		t.Fatalf("expected test.case.name=country=\"France\",provider=\"openai:gpt-4o\", got %q", got)
	}
	if got := attrString(m, "gen_ai.request.model"); got != "openai:gpt-4o" {
		t.Fatalf("expected gen_ai.request.model=openai:gpt-4o, got %q", got)
	}
}

func TestParseMultiAssertion(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "multi_assertion.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	if tests[0].Passed {
		t.Fatalf("expected Passed=false")
	}
	if !strings.Contains(tests[0].Output, "Missing source citation") {
		t.Fatalf("expected failure reason in Output, got %q", tests[0].Output)
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(metrics))
	}
	wantNames := map[string]bool{"eval.contains": false, "eval.llm-rubric": false, "eval.factuality": false}
	for _, m := range metrics {
		wantNames[m.Name] = true
	}
	for k, v := range wantNames {
		if !v {
			t.Fatalf("missing expected metric name %s", k)
		}
	}
}

func TestParseMultiTest(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "multi_test.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 3 {
		t.Fatalf("expected 3 tests, got %d", len(tests))
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics (one per test, one assertion each), got %d", len(metrics))
	}
	pass, fail := 0, 0
	for _, tr := range tests {
		if tr.Passed {
			pass++
		} else {
			fail++
		}
	}
	if pass != 2 || fail != 1 {
		t.Fatalf("expected 2 pass / 1 fail, got %d/%d", pass, fail)
	}
}

func TestParseWithThreshold(t *testing.T) {
	_, metrics, err := Parse(bytes.NewReader(loadFixture(t, "with_threshold.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	got, ok := attrDouble(metrics[0], "defrost.eval.threshold")
	if !ok {
		t.Fatalf("expected defrost.eval.threshold attribute")
	}
	if got != 0.85 {
		t.Fatalf("expected threshold=0.85, got %v", got)
	}
}

func TestParseWithMetricOverride(t *testing.T) {
	_, metrics, err := Parse(bytes.NewReader(loadFixture(t, "with_metric_override.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Name != "eval.relevance" {
		t.Fatalf("expected name eval.relevance (from metric override), got %q", metrics[0].Name)
	}
}

func TestParseEmpty(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "empty.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected zero results, got %d tests / %d metrics", len(tests), len(metrics))
	}
}

func TestParseNestedVarsDeterministic(t *testing.T) {
	// Both result entries have identical vars (including a nested
	// map). The case IDs must be byte-equal across the two — and
	// across multiple Parse invocations — to keep history continuity
	// working for promptfoo configs that pass structured vars.
	raw := loadFixture(t, "nested_vars.json")
	tests1, _, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tests2, _, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests1) != 2 || len(tests2) != 2 {
		t.Fatalf("expected 2 tests in each run, got %d / %d", len(tests1), len(tests2))
	}
	if tests1[0].Id != tests1[1].Id {
		t.Fatalf("identical vars produced different ids in same run: %q vs %q", tests1[0].Id, tests1[1].Id)
	}
	if tests1[0].Id != tests2[0].Id {
		t.Fatalf("identical vars produced different ids across runs: %q vs %q", tests1[0].Id, tests2[0].Id)
	}
	// Sanity check: id contains both var keys (config and name) and the provider.
	if !strings.Contains(tests1[0].Id, "config=") || !strings.Contains(tests1[0].Id, "name=") || !strings.Contains(tests1[0].Id, "provider=") {
		t.Fatalf("id missing expected keys: %q", tests1[0].Id)
	}
}

func TestParseEmptyVarsKeepsCasesDistinct(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "empty_vars_collision.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	if tests[0].Id == tests[1].Id {
		t.Fatalf("expected distinct case ids for empty-vars cases, both got %q", tests[0].Id)
	}
	if tests[0].Id != `<unnamed>#0,provider="openai:gpt-4o"` || tests[1].Id != `<unnamed>#1,provider="openai:gpt-4o"` {
		t.Fatalf("unexpected ids: %q, %q", tests[0].Id, tests[1].Id)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	// The metric attributes should also reflect the unique case names.
	name0 := attrString(metrics[0], "test.case.name")
	name1 := attrString(metrics[1], "test.case.name")
	if name0 == name1 {
		t.Fatalf("expected distinct test.case.name attrs, both got %q", name0)
	}
}

func TestParseStructuredResponseOutput(t *testing.T) {
	tests, _, err := Parse(bytes.NewReader(loadFixture(t, "structured_output.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 3 {
		t.Fatalf("expected 3 tests, got %d", len(tests))
	}
	// Object output: should be JSON-encoded.
	if tests[0].Output != `{"answer":"Paris","confidence":0.92}` {
		t.Fatalf("object output = %q", tests[0].Output)
	}
	// Array output: should be JSON-encoded.
	if tests[1].Output != `[1,2,3]` {
		t.Fatalf("array output = %q", tests[1].Output)
	}
	// Null output: should be empty string.
	if tests[2].Output != "" {
		t.Fatalf("null output = %q (want empty)", tests[2].Output)
	}
}

func TestParseSameVarsDifferentPromptsGetDistinctIds(t *testing.T) {
	tests, _, err := Parse(bytes.NewReader(loadFixture(t, "multi_prompt.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 3 {
		t.Fatalf("expected 3 tests, got %d", len(tests))
	}
	ids := map[string]struct{}{}
	for _, tr := range tests {
		ids[tr.Id] = struct{}{}
	}
	if len(ids) != 3 {
		t.Fatalf("identical vars/provider across three prompts must produce distinct ids; got %v", tests)
	}
	// We prefer the id (short hash) over the label so case-name
	// filenames stay under common 255-byte FS limits even when prompt
	// labels are full templates.
	if !strings.Contains(tests[0].Id, `prompt="abc123"`) {
		t.Fatalf("expected prompt id in case id, got %q", tests[0].Id)
	}
	if !strings.Contains(tests[1].Id, `prompt="def456"`) {
		t.Fatalf("expected prompt id in case id, got %q", tests[1].Id)
	}
	if !strings.Contains(tests[2].Id, `prompt="ghi789"`) {
		t.Fatalf("expected prompt id, got %q", tests[2].Id)
	}
}

func TestParseSkipsComponentsWithoutAssertionType(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "composite_assertion.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	// One leaf assertion produces one metric. The wrapper with empty
	// assertion metadata must be skipped to avoid emitting `eval.` with
	// no criterion.
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric (leaf only), got %d", len(metrics))
	}
	if metrics[0].Name != "eval.contains" {
		t.Fatalf("expected leaf metric name eval.contains, got %q", metrics[0].Name)
	}
}

func TestParseSameVarsDifferentProvidersGetDistinctIds(t *testing.T) {
	tests, _, err := Parse(bytes.NewReader(loadFixture(t, "cross_product.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tests))
	}
	if tests[0].Id == tests[1].Id {
		t.Fatalf("identical vars across two providers must produce distinct ids, both got %q", tests[0].Id)
	}
	if !strings.Contains(tests[0].Id, "provider=") || !strings.Contains(tests[1].Id, "provider=") {
		t.Fatalf("provider missing from ids: %q / %q", tests[0].Id, tests[1].Id)
	}
}
