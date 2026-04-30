package promptfoo

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

// promptfooDoc is the top-level JSON shape `promptfoo eval --output X.json`
// writes. We decode only the fields we use.
type promptfooDoc struct {
	Results struct {
		Results []promptfooResult `json:"results"`
	} `json:"results"`
}

type promptfooResult struct {
	Success       bool                  `json:"success"`
	Response      promptfooResponse     `json:"response"`
	Provider      promptfooProvider     `json:"provider"`
	Vars          map[string]any        `json:"vars"`
	GradingResult promptfooGradingShape `json:"gradingResult"`
}

type promptfooResponse struct {
	Output string `json:"output"`
}

type promptfooProvider struct {
	Label string `json:"label"`
	ID    string `json:"id"`
}

type promptfooGradingShape struct {
	Pass             bool                       `json:"pass"`
	Score            float64                    `json:"score"`
	Reason           string                     `json:"reason"`
	ComponentResults []promptfooComponentResult `json:"componentResults"`
}

type promptfooComponentResult struct {
	Pass      bool               `json:"pass"`
	Score     float64            `json:"score"`
	Reason    string             `json:"reason"`
	Assertion promptfooAssertion `json:"assertion"`
}

type promptfooAssertion struct {
	Type      string   `json:"type"`
	Value     any      `json:"value"`
	Threshold *float64 `json:"threshold,omitempty"`
	Metric    string   `json:"metric,omitempty"`
}

// Parse reads a `promptfoo eval --output X.json` document and emits the
// per-result TestResult plus one *metricspb.Metric (gauge, single data
// point) per assertion's componentResult.
//
// Returns nil/nil/error only on JSON decode failure.
func Parse(r io.Reader) ([]models.TestResult, []*metricspb.Metric, error) {
	var doc promptfooDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("parse promptfoo json: %w", err)
	}
	now := uint64(time.Now().UnixNano())
	var (
		tests   []models.TestResult
		metrics []*metricspb.Metric
	)
	for i, r := range doc.Results.Results {
		caseName := caseName(r.Vars, i)
		tests = append(tests, mapResult(r, caseName))
		for _, c := range r.GradingResult.ComponentResults {
			metrics = append(metrics, mapComponentResult(c, caseName, providerLabel(r.Provider), now))
		}
	}
	return tests, metrics, nil
}

func caseName(vars map[string]any, idx int) string {
	if len(vars) == 0 {
		return fmt.Sprintf("<unnamed>#%d", idx)
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		b, err := json.Marshal(vars[k])
		if err != nil {
			// Defensive: a vars value that can't marshal is exotic
			// (channels, funcs); fall back to %v rather than fail the run.
			parts = append(parts, fmt.Sprintf("%s=%v", k, vars[k]))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, b))
	}
	return strings.Join(parts, ",")
}

func providerLabel(p promptfooProvider) string {
	if p.Label != "" {
		return p.Label
	}
	return p.ID
}

func mapResult(r promptfooResult, caseName string) models.TestResult {
	output := r.Response.Output
	if !r.Success {
		var fails []string
		for _, c := range r.GradingResult.ComponentResults {
			if !c.Pass {
				fails = append(fails, fmt.Sprintf("[%s] %s", assertionMetricName(c.Assertion), c.Reason))
			}
		}
		if len(fails) > 0 {
			output = output + "\n--- defrost: failed assertions ---\n" + strings.Join(fails, "\n")
		}
	}
	return models.TestResult{
		Id:     caseName,
		Ran:    true,
		Passed: r.Success,
		Output: output,
	}
}

func mapComponentResult(c promptfooComponentResult, caseName, model string, timeUnixNano uint64) *metricspb.Metric {
	criterion := assertionMetricName(c.Assertion)
	score := c.Score

	attrs := []*commonpb.KeyValue{
		models.StringAttr("gen_ai.evaluation.name", criterion),
		models.DoubleAttr("gen_ai.evaluation.score.value", score),
		models.StringAttr("gen_ai.evaluation.score.label", passFailLabel(c.Pass)),
		models.StringAttr("gen_ai.evaluation.explanation", c.Reason),
		models.StringAttr("test.case.name", caseName),
	}
	if model != "" {
		attrs = append(attrs, models.StringAttr("gen_ai.request.model", model))
	}
	if c.Assertion.Threshold != nil {
		attrs = append(attrs, models.DoubleAttr("defrost.eval.threshold", *c.Assertion.Threshold))
	}

	return &metricspb.Metric{
		Name: "eval." + criterion,
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: timeUnixNano,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: score},
				Attributes:   attrs,
			}},
		}},
	}
}

func assertionMetricName(a promptfooAssertion) string {
	if a.Metric != "" {
		return a.Metric
	}
	return a.Type
}

func passFailLabel(pass bool) string {
	if pass {
		return "pass"
	}
	return "fail"
}
