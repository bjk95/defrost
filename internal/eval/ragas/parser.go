// Package ragas parses the JSON document a user produces by calling
// `result.to_pandas().to_json($DEFROST_RAGAS_OUT, orient="records")` after
// ragas.evaluate(...). RAGAS itself has no on-disk dump format and we
// deliberately avoid shipping a Python helper — see
// docs/specs/2026-04-30-ragas-adapter.md and the discussion on PR #21.
//
// Records-orient produces a top-level JSON array, one element per dataset
// row, with every DataFrame column flattened to a key. We treat any
// numeric column whose name isn't a known input/reference column as a
// score, and emit one OTLP gauge metric per (row × score) pair.
package ragas

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

// inputColumns is the set of RAGAS dataset columns that carry inputs or
// references rather than scorer outputs. Anything outside this set with a
// numeric value is treated as a score.
//
// RAGAS column conventions have shifted between 0.1.x and 0.2.x (e.g.
// `question` → `user_input`, `ground_truths` → `reference`). The list
// covers both old and new names so a single defrost binary parses output
// from either generation without code changes.
var inputColumns = map[string]bool{
	"question":           true,
	"answer":             true,
	"contexts":           true,
	"ground_truth":       true,
	"ground_truths":      true,
	"reference":          true,
	"reference_contexts": true,
	"retrieved_contexts": true,
	"user_input":         true,
	"response":           true,
}

// Parse reads a `result.to_pandas().to_json(..., orient="records")`
// document and emits one *metricspb.Metric per (row, scorer) pair.
//
// Returns nil/error only on JSON decode failure. RAGAS contributes metrics
// only — the pytest runner owns pass/fail and TestResult emission.
func Parse(r io.Reader) ([]*metricspb.Metric, error) {
	var rows []map[string]any
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return nil, fmt.Errorf("parse ragas json: %w", err)
	}
	now := uint64(time.Now().UnixNano())

	var metrics []*metricspb.Metric
	for i, row := range rows {
		caseName := rowCaseName(i)
		// Sort keys so emission order is stable across runs — Go's
		// map iteration is randomised, and downstream history-diff
		// tooling assumes per-(row × scorer) order is reproducible.
		names := make([]string, 0, len(row))
		for k := range row {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			if inputColumns[name] {
				continue
			}
			value, ok := numericScore(row[name])
			if !ok {
				// Non-numeric value or null — RAGAS emits null for
				// rows where the scorer couldn't produce a result
				// (e.g. empty context). Skip silently.
				continue
			}
			metrics = append(metrics, mapScore(caseName, name, value, now))
		}
	}
	return metrics, nil
}

// numericScore returns v as a float64 when v is a JSON number, including
// integers (which Go's json package decodes into any as float64 already).
// nil, strings, arrays, and objects all return ok=false.
func numericScore(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func rowCaseName(rowIndex int) string {
	return fmt.Sprintf("ragas_row_%d", rowIndex)
}

// mapScore builds the gauge metric for one (case, scorer, value) triple.
// Mirrors the Promptfoo parser's mapComponentResult shape so dashboards can
// query both adapters' eval.* metrics with the same attribute set.
//
// RAGAS doesn't surface a pass/fail label, threshold, or judge-model name
// in its result object, so those attributes are intentionally absent here.
// See spec §6.
func mapScore(caseName, scorerName string, value float64, timeUnixNano uint64) *metricspb.Metric {
	attrs := []*commonpb.KeyValue{
		models.StringAttr("gen_ai.evaluation.name", scorerName),
		models.DoubleAttr("gen_ai.evaluation.score.value", value),
		models.StringAttr("test.case.name", caseName),
	}
	return &metricspb.Metric{
		Name: "eval." + scorerName,
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: timeUnixNano,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: value},
				Attributes:   attrs,
			}},
		}},
	}
}
