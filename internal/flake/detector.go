package flake

import (
	"sort"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// Options controls how a per-test history is reduced to outcomes before
// the transition rate is taken.
type Options struct {
	// Window is the number of most-recent runs to consider, per test.
	// 0 means "use the full history".
	Window int

	// Branch restricts considered runs to those whose
	// vcs.repository.ref.name resource attribute equals this value.
	// Empty means "all branches".
	Branch string
}

// Result holds one test's transition-rate analysis.
type Result struct {
	TestName string
	// Rate is the transition rate over Outcomes (after Window+Branch
	// filtering have been applied). In [0, 1].
	Rate float64
	// Outcomes is the post-filter outcome sequence the rate was computed
	// from, oldest first. Useful for rendering "PPFPP…" displays.
	Outcomes []Outcome
}

// Compute returns one Result per test in byEncodedName, sorted by Rate
// descending (ties broken by test name ascending). byEncodedName is the
// second return value of persist.Backend.LoadAll() — keys are URL-encoded
// test names; values are every persisted span (one ResourceSpans per
// span) for that test.
func Compute(byEncodedName map[string][]*tracepb.ResourceSpans, opts Options) []Result {
	results := make([]Result, 0, len(byEncodedName))
	for encName, spans := range byEncodedName {
		name, err := persist.DecodeName(encName)
		if err != nil {
			name = encName
		}
		filtered := filterByBranch(spans, opts.Branch)
		filtered = sortedByStartTime(filtered)
		if opts.Window > 0 && len(filtered) > opts.Window {
			filtered = filtered[len(filtered)-opts.Window:]
		}
		outcomes := toOutcomes(filtered)
		results = append(results, Result{
			TestName: name,
			Rate:     TransitionRate(outcomes),
			Outcomes: outcomes,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Rate != results[j].Rate {
			return results[i].Rate > results[j].Rate
		}
		return results[i].TestName < results[j].TestName
	})
	return results
}

// filterByBranch returns the subset of spans whose Resource carries the
// requested branch name. branch == "" disables filtering (returns spans
// unchanged).
func filterByBranch(spans []*tracepb.ResourceSpans, branch string) []*tracepb.ResourceSpans {
	if branch == "" {
		return spans
	}
	out := make([]*tracepb.ResourceSpans, 0, len(spans))
	for _, rs := range spans {
		if models.ResourceString(rs.Resource, "vcs.repository.ref.name") == branch {
			out = append(out, rs)
		}
	}
	return out
}

// sortedByStartTime returns a copy of spans ordered oldest-first by the
// inner span's StartTimeUnixNano. Defensive against callers that pass
// spans in arbitrary order — LoadAll's contract does not require the
// per-name slices to be sorted.
func sortedByStartTime(spans []*tracepb.ResourceSpans) []*tracepb.ResourceSpans {
	out := make([]*tracepb.ResourceSpans, len(spans))
	copy(out, spans)
	sort.Slice(out, func(i, j int) bool {
		a := persist.SpanFromResourceSpans(out[i])
		b := persist.SpanFromResourceSpans(out[j])
		if a == nil || b == nil {
			return a != nil
		}
		return a.StartTimeUnixNano < b.StartTimeUnixNano
	})
	return out
}

// toOutcomes maps each span to a Pass/Fail/Skip Outcome via OTel
// Status.Code: OK→Pass, ERROR→Fail, anything else→Skip. Skips are
// filtered out by TransitionRate; here we keep them in the returned
// slice so callers rendering outcome strings see the full sequence.
func toOutcomes(spans []*tracepb.ResourceSpans) []Outcome {
	out := make([]Outcome, 0, len(spans))
	for _, rs := range spans {
		s := persist.SpanFromResourceSpans(rs)
		if s == nil || s.Status == nil {
			out = append(out, Skip)
			continue
		}
		switch s.Status.Code {
		case tracepb.Status_STATUS_CODE_OK:
			out = append(out, Pass)
		case tracepb.Status_STATUS_CODE_ERROR:
			out = append(out, Fail)
		default:
			out = append(out, Skip)
		}
	}
	return out
}
