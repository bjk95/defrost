// Package flake provides simple, well-understood heuristics for spotting
// flaky tests in persisted run history.
//
// The first heuristic is the "transition-state metric": count adjacent
// pass↔fail flips in a test's outcome sequence and divide by the number
// of adjacent pairs. Stable tests (always pass, always fail) score 0;
// constantly flipping tests score 1. Cheap, easy to explain, and a
// strong default signal before more sophisticated detectors layer on.
package flake

// Outcome is one observation from a test's run history.
type Outcome int

const (
	Pass Outcome = iota
	Fail
	// Skip is a run where the test did not produce a pass/fail outcome
	// (skipped, status unset, no result recorded). Skips are filtered
	// out before the transition rate is computed — a skip is not a
	// "stable" run, but it's not a transition either.
	Skip
)

// TransitionRate returns the proportion of adjacent pass↔fail
// transitions in outcomes after Skip values are filtered out. Result
// is in [0, 1]. Returns 0 when fewer than two non-skip outcomes are
// present (no adjacent pair exists to score).
//
// Examples:
//   - [P P P P]   → 0.00  (stable pass)
//   - [F F F F]   → 0.00  (stable fail — broken, not flake; the metric
//     deliberately does not distinguish broken from healthy)
//   - [P F P F]   → 1.00  (constantly flipping)
//   - [P P F P P] → 0.50  (one flip, four adjacent pairs after filter)
//   - [P S F S P] → 1.00  (skips removed → [P F P], two flips, two pairs)
func TransitionRate(outcomes []Outcome) float64 {
	filtered := make([]Outcome, 0, len(outcomes))
	for _, o := range outcomes {
		if o == Skip {
			continue
		}
		filtered = append(filtered, o)
	}
	if len(filtered) < 2 {
		return 0
	}
	transitions := 0
	for i := 1; i < len(filtered); i++ {
		if filtered[i] != filtered[i-1] {
			transitions++
		}
	}
	return float64(transitions) / float64(len(filtered)-1)
}
