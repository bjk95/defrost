package promptfoo

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
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
	Prompt        promptfooPrompt       `json:"prompt"`
	Vars          map[string]any        `json:"vars"`
	GradingResult promptfooGradingShape `json:"gradingResult"`
}

type promptfooPrompt struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type promptfooResponse struct {
	Output any `json:"output"`
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
// scope qualifies emitted metric names so two configs in the same repo
// can't collapse into one series. The adapter passes
// `<repo-rel-cwd>.<config-path>` (with `.` separator); empty segments are
// already stripped by the caller. An empty scope produces unscoped names
// (`eval.<criterion>`) and is intended for direct unit tests of Parse.
//
// Returns nil/nil/error only on JSON decode failure.
func Parse(r io.Reader, scope string, run models.RunContext) ([]models.TestResult, pmetric.Metrics, error) {
	var doc promptfooDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, pmetric.NewMetrics(), fmt.Errorf("parse promptfoo json: %w", err)
	}
	now := time.Now()
	tests := make([]models.TestResult, 0, len(doc.Results.Results))
	md := runner.NewEvalMetrics(run)
	for i, r := range doc.Results.Results {
		provider := providerLabel(r.Provider)
		prompt := promptIdentity(r.Prompt)
		cName := caseName(r.Vars, i, provider, prompt)
		tests = append(tests, mapResult(r, cName))
		for _, c := range r.GradingResult.ComponentResults {
			appendComponentMetric(md, c, cName, provider, scope, now)
		}
	}
	return tests, md, nil
}

func caseName(vars map[string]any, idx int, providerLabel, promptID string) string {
	var parts []string
	if len(vars) == 0 {
		parts = []string{fmt.Sprintf("<unnamed>#%d", idx)}
	} else {
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts = make([]string, 0, len(keys)+2)
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
	}
	if promptID != "" {
		b, err := json.Marshal(promptID)
		if err != nil {
			parts = append(parts, fmt.Sprintf("prompt=%s", promptID))
		} else {
			parts = append(parts, fmt.Sprintf("prompt=%s", b))
		}
	}
	if providerLabel != "" {
		// json.Marshal so a label containing commas/quotes round-trips
		// stably. Most labels are scalar identifiers like
		// "openai:gpt-4o-mini" so this is mostly cosmetic.
		b, err := json.Marshal(providerLabel)
		if err != nil {
			parts = append(parts, fmt.Sprintf("provider=%s", providerLabel))
		} else {
			parts = append(parts, fmt.Sprintf("provider=%s", b))
		}
	}
	return strings.Join(parts, ",")
}

// promptIdentity returns a stable, compact identifier for a prompt.
// Prefer prompt.id (promptfoo emits a short hash) over prompt.label
// because labels are often the full prompt template — long enough to
// push URL-encoded case-name filenames past common 255-byte FS limits
// when used as metric/trace partition keys. When id is empty (some
// promptfoo versions don't populate it), hash the label ourselves to
// keep the cross-product disambiguation property without bloating
// filenames. Returns "" when neither is present.
func promptIdentity(p promptfooPrompt) string {
	if p.ID != "" {
		return p.ID
	}
	if p.Label == "" {
		return ""
	}
	h := sha256.Sum256([]byte(p.Label))
	return fmt.Sprintf("%x", h[:6])
}

func providerLabel(p promptfooProvider) string {
	if p.Label != "" {
		return p.Label
	}
	return p.ID
}

// outputAsString renders Promptfoo's loosely-typed response.output as a
// string for the TestResult.Output field. Strings pass through verbatim;
// nil becomes empty; everything else is rendered as canonical JSON so
// structured outputs survive into the human-readable trace.
func outputAsString(o any) string {
	switch v := o.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	b, err := json.Marshal(o)
	if err != nil {
		return fmt.Sprintf("%v", o)
	}
	return string(b)
}

func mapResult(r promptfooResult, caseName string) models.TestResult {
	output := outputAsString(r.Response.Output)
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

func appendComponentMetric(md pmetric.Metrics, c promptfooComponentResult, caseName, model, scope string, ts time.Time) {
	criterion := assertionMetricName(c.Assertion)
	if criterion == "" {
		// Composite/parent component result without assertion metadata.
		// Skip — leaf nodes underneath produce their own metrics, and
		// emitting `eval.` with an empty name would collapse unrelated
		// scores into one stream.
		return
	}
	score := c.Score

	attrs := []models.Attr{
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

	name := "eval." + criterion
	if scope != "" {
		name = "eval." + scope + "." + criterion
	}
	runner.AppendGauge(md, name, score, ts, attrs)
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
