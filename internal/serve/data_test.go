package serve

import (
	"testing"

	"github.com/bjk95/defrost/internal/persist"
)

func TestLoad_SortsRunsNewestFirstAndCapsAtFifty(t *testing.T) {
	runs := []persist.RunRecord{}
	for i := 0; i < 60; i++ {
		runs = append(runs, persist.RunRecord{
			RunID:     idFor(i),
			Timestamp: timestampFor(i),
		})
	}
	byTest := map[string][]persist.Entry{
		"tid-A": {
			{TestID: "tid-A", TestName: "pkg.TestA", RunID: idFor(0), Status: "pass"},
			{TestID: "tid-A", TestName: "pkg.TestA", RunID: idFor(59), Status: "fail"},
		},
	}

	prevLoadAll := persistLoadAll
	persistLoadAll = func(_ persist.Options) ([]persist.RunRecord, map[string][]persist.Entry, error) {
		return runs, byTest, nil
	}
	defer func() { persistLoadAll = prevLoadAll }()

	ds, err := Load(persist.Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ds.Runs) != 50 {
		t.Errorf("want 50 runs after cap, got %d", len(ds.Runs))
	}
	if ds.Runs[0].RunID != idFor(59) {
		t.Errorf("want newest run first, got %q", ds.Runs[0].RunID)
	}
	// Entry whose RunID is no longer in the capped set must be dropped.
	if len(ds.TestEntries["tid-A"]) != 1 {
		t.Errorf("want 1 entry for tid-A after run-cap filter, got %d", len(ds.TestEntries["tid-A"]))
	}
	if ds.TestEntries["tid-A"][0].RunID != idFor(59) {
		t.Errorf("want surviving entry to reference run %q, got %q", idFor(59), ds.TestEntries["tid-A"][0].RunID)
	}
}

func idFor(i int) string {
	return "run-" + leftPad(i)
}

func timestampFor(i int) string {
	// Lex-sortable timestamps. RFC3339 ascending with i.
	return "2026-01-01T00:00:" + leftPad(i) + "Z"
}

func leftPad(i int) string {
	if i < 10 {
		return "0" + itoa(i)
	}
	return itoa(i)
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(digits[i%10]) + out
		i /= 10
	}
	return out
}
