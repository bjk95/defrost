package serve

import (
	"sort"

	"github.com/bjk95/defrost/internal/persist"
)

// MaxRuns is the cap on grid columns. Runs older than the latest 50 are
// dropped from the in-memory Dataset.
const MaxRuns = 50

// Dataset is a request-scoped view of the data branch sized for the grid
// UI. Runs is the column axis (newest first); TestEntries are filtered to
// only those whose RunID still appears in Runs after capping.
type Dataset struct {
	Runs        []persist.RunRecord
	TestEntries map[string][]persist.Entry
}

// persistLoadAll is a package-level seam so tests can stub the data
// source without going through git.
var persistLoadAll = persist.LoadAll

// Load reads the data branch and returns a sorted, capped Dataset suitable
// for serving over HTTP.
func Load(opts persist.Options) (Dataset, error) {
	runs, byTest, err := persistLoadAll(opts)
	if err != nil {
		return Dataset{}, err
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].Timestamp > runs[j].Timestamp
	})
	if len(runs) > MaxRuns {
		runs = runs[:MaxRuns]
	}

	keep := make(map[string]struct{}, len(runs))
	for _, r := range runs {
		keep[r.RunID] = struct{}{}
	}

	filtered := make(map[string][]persist.Entry, len(byTest))
	for tid, entries := range byTest {
		var kept []persist.Entry
		for _, e := range entries {
			if _, ok := keep[e.RunID]; ok {
				kept = append(kept, e)
			}
		}
		if len(kept) > 0 {
			filtered[tid] = kept
		}
	}

	return Dataset{Runs: runs, TestEntries: filtered}, nil
}
