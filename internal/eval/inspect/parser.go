package inspect

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

// inspectDoc is the top-level JSON shape `inspect eval --log-format=json`
// writes per task. We decode only the fields we use.
type inspectDoc struct {
	Eval    inspectEval     `json:"eval"`
	Samples []inspectSample `json:"samples"`
}

type inspectEval struct {
	Task  string `json:"task"`
	Model string `json:"model"`
}

type inspectSample struct {
	ID     any                     `json:"id"`
	Output inspectOutput           `json:"output"`
	Scores map[string]inspectScore `json:"scores"`
}

type inspectOutput struct {
	Completion string `json:"completion"`
}

type inspectScore struct {
	Value       any    `json:"value"`
	Answer      string `json:"answer"`
	Explanation string `json:"explanation"`
}

// passThreshold is the heuristic boundary above which a numeric Inspect
// score is treated as a pass. Inspect's binary scorers (`match`,
// `includes`) emit 0.0 / 1.0; multi-grade scorers emit fractional scores.
// A 0.5 cutoff matches Inspect's own default for the `at_least` reducer
// and is the simplest rule that doesn't require per-scorer threshold
// configuration. See spec §11.4.
const passThreshold = 0.5

// ParseFile reads a single Inspect AI JSON log file and emits per-sample
// TestResults plus one *metricspb.Metric per (sample × numeric scorer).
// Non-numeric scorers (Letter grades, compound objects) are skipped with
// a stderr warning. Returns nil/nil/error only on JSON decode failure.
func ParseFile(r io.Reader) ([]models.TestResult, []*metricspb.Metric, error) {
	var doc inspectDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("parse inspect json: %w", err)
	}
	now := uint64(time.Now().UnixNano())
	var (
		tests   []models.TestResult
		metrics []*metricspb.Metric
	)
	for _, s := range doc.Samples {
		tr, mm := mapSample(s, doc.Eval.Task, doc.Eval.Model, now)
		tests = append(tests, tr)
		metrics = append(metrics, mm...)
	}
	return tests, metrics, nil
}

func mapSample(s inspectSample, task, model string, now uint64) (models.TestResult, []*metricspb.Metric) {
	caseName := sampleCaseName(s.ID)

	// Iterate scorers in deterministic name order so the metric slice
	// (and any output the test asserts on) is stable across runs.
	keys := make([]string, 0, len(s.Scores))
	for k := range s.Scores {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var (
		metrics    []*metricspb.Metric
		numericN   int
		failingN   int
	)
	for _, k := range keys {
		v := s.Scores[k]
		score, ok := numericScore(v.Value)
		if !ok {
			fmt.Fprintf(os.Stderr,
				"defrost: inspect: skipping non-numeric scorer %q for %s\n",
				k, caseName)
			continue
		}
		numericN++
		if score < passThreshold {
			failingN++
		}
		metrics = append(metrics, mapScore(k, score, v.Explanation, caseName, model, task, now))
	}

	// A sample passes when at least one numeric scorer ran AND every numeric
	// scorer is at/above the heuristic threshold. Zero numeric scorers means
	// we have no evidence either way; mark as not-passed so the failure isn't
	// silently masked.
	passed := numericN > 0 && failingN == 0

	return models.TestResult{
		Id:     caseName,
		Ran:    true,
		Passed: passed,
		Output: s.Output.Completion,
	}, metrics
}

func mapScore(name string, score float64, explanation, caseName, model, task string, now uint64) *metricspb.Metric {
	attrs := []*commonpb.KeyValue{
		models.StringAttr("gen_ai.evaluation.name", name),
		models.DoubleAttr("gen_ai.evaluation.score.value", score),
		models.StringAttr("gen_ai.evaluation.score.label", passFailLabel(score >= passThreshold)),
		models.StringAttr("gen_ai.evaluation.explanation", explanation),
		models.StringAttr("test.case.name", caseName),
	}
	if model != "" {
		attrs = append(attrs, models.StringAttr("gen_ai.request.model", model))
	}
	if task != "" {
		attrs = append(attrs, models.StringAttr("test.suite.name", task))
	}

	return &metricspb.Metric{
		Name: "eval." + name,
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: now,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: score},
				Attributes:   attrs,
			}},
		}},
	}
}

// numericScore extracts a float64 score from Inspect's loosely-typed
// `value` field. Recognises floats, integers (decoded as float64 by
// encoding/json into `any`), and booleans (`correct` scorers).
// Letter grades (`"C"`/`"I"`) and compound objects return false so the
// caller can skip them with a warning.
func numericScore(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case bool:
		if x {
			return 1.0, true
		}
		return 0.0, true
	}
	return 0, false
}

// sampleCaseName builds a stable case name from Inspect's sample id, which
// the JSON schema allows to be either an int or a string. Integer ids are
// decoded as float64 by encoding/json into `any`; render them without a
// fractional part so two runs with the same dataset produce byte-equal
// case ids.
func sampleCaseName(id any) string {
	switch v := id.(type) {
	case nil:
		return "sample_<unnamed>"
	case string:
		return "sample_" + v
	case float64:
		if v == float64(int64(v)) {
			return "sample_" + strconv.FormatInt(int64(v), 10)
		}
		return "sample_" + strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return fmt.Sprintf("sample_%v", v)
	}
}

func passFailLabel(pass bool) string {
	if pass {
		return "pass"
	}
	return "fail"
}
