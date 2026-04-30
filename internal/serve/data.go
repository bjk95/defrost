package serve

import (
	"sort"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// MaxRuns is the cap on grid columns. Runs older than the latest 50 are
// dropped from the in-memory Dataset.
const MaxRuns = 50

// Dataset is a request-scoped view of the data branch sized for the grid
// UI. Roots are root run spans (column axis, newest first). TestSpans
// are the per-test span time series, keyed by encoded span name (file
// basename in traces/), filtered to only those whose run_id still appears
// in Roots after capping.
type Dataset struct {
	Roots     []models.Span
	TestSpans map[string][]models.Span
}

// persistLoadAll is a package-level seam so tests can stub the data
// source without going through git. Default impl calls through the
// Backend selected by the persist.Options.
var persistLoadAll = func(opts persist.Options) ([]models.Span, map[string][]models.Span, error) {
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
		return roots[i].StartTimeUnixNano > roots[j].StartTimeUnixNano
	})
	if len(roots) > MaxRuns {
		roots = roots[:MaxRuns]
	}

	keep := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		if id, _ := r.Resource["defrost.run_id"].(string); id != "" {
			keep[id] = struct{}{}
		}
	}

	filtered := make(map[string][]models.Span, len(byName))
	for tid, spans := range byName {
		var kept []models.Span
		for _, s := range spans {
			rid, _ := s.Attributes["defrost.run_id"].(string)
			if _, ok := keep[rid]; ok {
				kept = append(kept, s)
			}
		}
		if len(kept) > 0 {
			filtered[tid] = kept
		}
	}

	return Dataset{Roots: roots, TestSpans: filtered}, nil
}
