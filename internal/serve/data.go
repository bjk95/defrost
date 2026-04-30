package serve

import (
	"sort"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/persist"
)

// MaxRuns is the cap on grid columns. Runs older than the latest 50 are
// dropped from the in-memory Dataset.
const MaxRuns = 50

// Dataset is a request-scoped view of the data branch sized for the grid
// UI. Roots are root run ResourceSpans (column axis, newest first).
// TestSpans are the per-test span time series, keyed by encoded span name
// (file basename in traces/), filtered to only those whose run_id still
// appears in Roots after capping.
type Dataset struct {
	Roots     []*tracepb.ResourceSpans
	TestSpans map[string][]*tracepb.ResourceSpans
}

// persistLoadAll is a package-level seam so tests can stub the data
// source without going through git. Default impl calls through the
// Backend selected by the persist.Options.
var persistLoadAll = func(opts persist.Options) ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	return persist.New(opts).LoadAll()
}

// Load reads the data branch and returns a sorted, capped Dataset suitable
// for serving over HTTP.
func Load(opts persist.Options) (Dataset, error) {
	roots, byName, err := persistLoadAll(opts)
	if err != nil {
		return Dataset{}, err
	}

	// Sort newest-first by start time so the grid columns flow most-
	// recent to oldest after the filter.
	sort.Slice(roots, func(i, j int) bool {
		ai := persist.SpanFromResourceSpans(roots[i])
		aj := persist.SpanFromResourceSpans(roots[j])
		if ai == nil || aj == nil {
			return false
		}
		return ai.StartTimeUnixNano > aj.StartTimeUnixNano
	})
	if len(roots) > MaxRuns {
		roots = roots[:MaxRuns]
	}

	keep := make(map[string]struct{}, len(roots))
	for _, rs := range roots {
		if id := runIDOf(rs); id != "" {
			keep[id] = struct{}{}
		}
	}

	filtered := make(map[string][]*tracepb.ResourceSpans, len(byName))
	for tid, spans := range byName {
		var kept []*tracepb.ResourceSpans
		for _, rs := range spans {
			if _, ok := keep[runIDOf(rs)]; ok {
				kept = append(kept, rs)
			}
		}
		if len(kept) > 0 {
			filtered[tid] = kept
		}
	}

	return Dataset{Roots: roots, TestSpans: filtered}, nil
}
