// Package ragas parses the JSON document written by the
// `defrost_ragas.write_results(...)` Python helper and emits one OTLP gauge
// metric per (row × scorer) pair. RAGAS itself has no on-disk output format —
// this is a defrost-owned schema; see docs/specs/2026-04-30-ragas-adapter.md §5.
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

type ragasDoc struct {
	RagasVersion string     `json:"ragas_version"`
	Rows         []ragasRow `json:"rows"`
}

type ragasRow struct {
	RowIndex int                `json:"row_index"`
	Question string             `json:"question"`
	Scores   map[string]float64 `json:"scores"`
}

// Parse reads a defrost_ragas.write_results() JSON document and emits one
// *metricspb.Metric per (row, scorer) pair.
//
// Returns nil/error only on JSON decode failure. RAGAS contributes metrics
// only — the pytest runner owns pass/fail and TestResult emission.
func Parse(r io.Reader) ([]*metricspb.Metric, error) {
	var doc ragasDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse ragas json: %w", err)
	}
	now := uint64(time.Now().UnixNano())

	var metrics []*metricspb.Metric
	for _, row := range doc.Rows {
		caseName := rowCaseName(row.RowIndex)
		// Sort metric names so emission order is stable across runs —
		// JSON object iteration in Go is randomised, and downstream
		// history-diff tooling assumes per-(row × scorer) order is
		// reproducible.
		names := make([]string, 0, len(row.Scores))
		for k := range row.Scores {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			metrics = append(metrics, mapScore(caseName, name, row.Scores[name], now))
		}
	}
	return metrics, nil
}

func rowCaseName(rowIndex int) string {
	return fmt.Sprintf("ragas_row_%d", rowIndex)
}

// mapScore builds the gauge metric for one (case, scorer, value) triple.
// Mirrors the Promptfoo parser's mapComponentResult shape so dashboards can
// query both adapters' eval.* metrics with the same attribute set.
//
// RAGAS doesn't surface a pass/fail label, threshold, or judge-model name in
// its result object, so those attributes are intentionally absent here. See
// spec §6.
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
