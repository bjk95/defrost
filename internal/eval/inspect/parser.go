package inspect

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)

// passThreshold is the cutoff a sample's numeric scorer must clear for the
// sample to count as passed. Inspect AI's per-sample JSON does not carry the
// scorer's threshold, and `match()`/binary scorers emit 0.0 or 1.0, so 0.5 is
// the only value that classifies both binary and continuous scores correctly
// without external configuration.
const passThreshold = 0.5

// inspectDoc is the subset of an Inspect AI JSON log we read.
type inspectDoc struct {
	Eval    inspectEval     `json:"eval"`
	Samples []inspectSample `json:"samples"`
}

type inspectEval struct {
	Task     string `json:"task"`
	TaskFile string `json:"task_file"`
	Model    string `json:"model"`
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

// ParseFile reads a single Inspect AI JSON log and emits one TestResult per
// sample plus one gauge metric per (sample, numeric scorer) pair. Scorers
// whose value is non-numeric (Letter "C"/"I", compound objects) are skipped
// with a stderr warning. Returns nil/zero/error only on JSON decode failure.
//
// repoRelCwd qualifies emitted metric names so the same task running from
// two different directories in the same repo can't collapse into one
// series. The full metric name is
// `eval.<repoRelCwd>.<eval.task_file>.<eval.task>.<scorer>` with empty
// segments dropped.
func ParseFile(r io.Reader, repoRelCwd string, run models.RunContext) ([]models.TestResult, pmetric.Metrics, error) {
	var doc inspectDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, pmetric.NewMetrics(), fmt.Errorf("parse inspect json: %w", err)
	}
	now := time.Now()
	tests := make([]models.TestResult, 0, len(doc.Samples))
	md := runner.NewEvalMetrics(run)
	for _, s := range doc.Samples {
		tr := mapSample(s, doc.Eval.Task, doc.Eval.TaskFile, doc.Eval.Model, repoRelCwd, now, md)
		tests = append(tests, tr)
	}
	return tests, md, nil
}

// mapSample produces the TestResult for one sample and appends the
// per-scorer gauge metrics into md. Pass/fail is the conjunction of all
// numeric scorers clearing passThreshold; a sample with no numeric
// scorers is treated as passed (it ran without scoring failures).
func mapSample(s inspectSample, task, taskFile, model, repoRelCwd string, now time.Time, md pmetric.Metrics) models.TestResult {
	caseName := sampleCaseName(s.ID, task)

	scorerNames := make([]string, 0, len(s.Scores))
	for k := range s.Scores {
		scorerNames = append(scorerNames, k)
	}
	sort.Strings(scorerNames)

	pass := true
	hasNumeric := false
	for _, name := range scorerNames {
		score := s.Scores[name]
		v, ok := numericScore(score.Value)
		if !ok {
			fmt.Fprintf(os.Stderr,
				"defrost: inspect: skipping non-numeric scorer %q on %s (value=%v)\n",
				name, caseName, score.Value)
			continue
		}
		hasNumeric = true
		if v < passThreshold {
			pass = false
		}
		appendScoreMetric(md, name, score, caseName, task, taskFile, model, repoRelCwd, v, now)
	}
	if !hasNumeric && len(scorerNames) == 0 {
		fmt.Fprintf(os.Stderr,
			"defrost: inspect: sample %s has no scorers; recording as passed\n",
			caseName)
	}

	return models.TestResult{
		Id:     caseName,
		Ran:    true,
		Passed: pass,
		Output: s.Output.Completion,
	}
}

func appendScoreMetric(md pmetric.Metrics, name string, s inspectScore, caseName, task, taskFile, model, repoRelCwd string, score float64, now time.Time) {
	attrs := []models.Attr{
		models.StringAttr("gen_ai.evaluation.name", name),
		models.DoubleAttr("gen_ai.evaluation.score.value", score),
		models.StringAttr("gen_ai.evaluation.score.label", passFailLabel(score >= passThreshold)),
		models.StringAttr("gen_ai.evaluation.explanation", s.Explanation),
		models.StringAttr("test.case.name", caseName),
	}
	if task != "" {
		attrs = append(attrs, models.StringAttr("test.suite.name", task))
	}
	if model != "" {
		attrs = append(attrs, models.StringAttr("gen_ai.request.model", model))
	}

	// Fully-qualified metric name:
	// eval.<repoRelCwd>.<taskFile>.<task>.<scorer>, with empty segments
	// dropped.
	segs := make([]string, 0, 4)
	for _, p := range []string{repoRelCwd, taskFile, task} {
		if p != "" {
			segs = append(segs, p)
		}
	}
	segs = append(segs, name)
	metricName := "eval." + strings.Join(segs, ".")
	runner.AppendGauge(md, metricName, score, now, attrs)
}

// numericScore extracts a float64 from Inspect AI's loosely-typed
// `scores[k].value`. Standard JSON decoding into `any` gives float64 for
// numbers; integers and floats both arrive as float64. Strings ("C", "I"),
// objects, arrays, and nulls return false so the caller can skip them.
func numericScore(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// sampleCaseName builds a deterministic case identifier of the form
// `task="<task>",sample="<id>"`. Both fields are JSON-encoded so values
// containing commas or quotes round-trip stably and the format matches the
// Promptfoo adapter's key="value" convention. Task is included so multi-task
// invocations don't collide on bare sample IDs.
func sampleCaseName(id any, task string) string {
	parts := make([]string, 0, 2)
	if task != "" {
		b, err := json.Marshal(task)
		if err != nil {
			parts = append(parts, fmt.Sprintf("task=%s", task))
		} else {
			parts = append(parts, fmt.Sprintf("task=%s", b))
		}
	}
	parts = append(parts, fmt.Sprintf("sample=%s", encodeSampleID(id)))
	return strings.Join(parts, ",")
}

// encodeSampleID renders Inspect's loosely-typed sample id as a stable
// JSON-encoded string suitable for use in a case-name field. Integer ids
// arrive from json.Unmarshal-into-any as float64; we render them without
// decimal noise. Strings round-trip as JSON literals so embedded commas and
// quotes don't break the case-name format.
func encodeSampleID(id any) string {
	switch v := id.(type) {
	case nil:
		return `"<no-id>"`
	case string:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%q", v)
		}
		return string(b)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("\"%d\"", int64(v))
		}
		return fmt.Sprintf("%q", strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), "."))
	}
	b, err := json.Marshal(id)
	if err != nil {
		return fmt.Sprintf("%q", fmt.Sprintf("%v", id))
	}
	return string(b)
}

func passFailLabel(pass bool) string {
	if pass {
		return "pass"
	}
	return "fail"
}
