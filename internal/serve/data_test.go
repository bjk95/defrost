package serve

import (
	"testing"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

func TestLoad_SortsRunsNewestFirstAndCapsAtFifty(t *testing.T) {
	roots := []models.Span{}
	for i := 0; i < 60; i++ {
		roots = append(roots, models.Span{
			Schema:            models.SchemaV3,
			Name:              "defrost.run",
			StartTimeUnixNano: int64(i + 1),
			Resource:          map[string]any{"defrost.run_id": idFor(i)},
		})
	}
	byName := map[string][]models.Span{
		"tid-A": {
			{
				Schema:            models.SchemaV3,
				Name:              "pkg.TestA",
				StartTimeUnixNano: 1,
				Attributes: map[string]any{
					"defrost.run_id":          idFor(0),
					"test.case.result.status": "passed",
				},
			},
			{
				Schema:            models.SchemaV3,
				Name:              "pkg.TestA",
				StartTimeUnixNano: 60,
				Attributes: map[string]any{
					"defrost.run_id":          idFor(59),
					"test.case.result.status": "failed",
				},
			},
		},
	}

	prevLoadAll := persistLoadAll
	persistLoadAll = func(_ persist.Options) ([]models.Span, map[string][]models.Span, error) {
		return roots, byName, nil
	}
	defer func() { persistLoadAll = prevLoadAll }()

	ds, err := Load(persist.Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ds.Roots) != 50 {
		t.Errorf("want 50 roots after cap, got %d", len(ds.Roots))
	}
	if rid, _ := ds.Roots[0].Resource["defrost.run_id"].(string); rid != idFor(59) {
		t.Errorf("want newest root first, got %q", rid)
	}
	// Span whose run_id is no longer in the capped set must be dropped.
	if len(ds.TestSpans["tid-A"]) != 1 {
		t.Errorf("want 1 span for tid-A after run-cap filter, got %d", len(ds.TestSpans["tid-A"]))
	}
	if rid, _ := ds.TestSpans["tid-A"][0].Attributes["defrost.run_id"].(string); rid != idFor(59) {
		t.Errorf("want surviving span to reference run %q, got %q", idFor(59), rid)
	}
}

func idFor(i int) string {
	return "run-" + leftPad(i)
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
