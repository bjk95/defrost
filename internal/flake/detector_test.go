package flake

import (
	"math"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// makeRun builds a single-span ResourceSpans matching the per-span
// shape persist.LoadAll yields after splitting. branch may be empty;
// when set, it's stamped onto the Resource so branch-filter tests can
// exercise the path.
func makeRun(name, branch string, ts time.Time, status tracepb.Status_StatusCode) *tracepb.ResourceSpans {
	var resAttrs []*commonpb.KeyValue
	if branch != "" {
		resAttrs = append(resAttrs, models.StringAttr("vcs.repository.ref.name", branch))
	}
	return &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{Attributes: resAttrs},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{
				Name:              name,
				StartTimeUnixNano: uint64(ts.UnixNano()),
				Status:            &tracepb.Status{Code: status},
			}},
		}},
	}
}

func TestCompute_SortsByRateDesc(t *testing.T) {
	t0 := time.Now()
	byName := map[string][]*tracepb.ResourceSpans{
		persist.EncodeName("stable"): {
			makeRun("stable", "", t0.Add(0), tracepb.Status_STATUS_CODE_OK),
			makeRun("stable", "", t0.Add(time.Second), tracepb.Status_STATUS_CODE_OK),
			makeRun("stable", "", t0.Add(2*time.Second), tracepb.Status_STATUS_CODE_OK),
		},
		persist.EncodeName("flaky"): {
			makeRun("flaky", "", t0.Add(0), tracepb.Status_STATUS_CODE_OK),
			makeRun("flaky", "", t0.Add(time.Second), tracepb.Status_STATUS_CODE_ERROR),
			makeRun("flaky", "", t0.Add(2*time.Second), tracepb.Status_STATUS_CODE_OK),
			makeRun("flaky", "", t0.Add(3*time.Second), tracepb.Status_STATUS_CODE_ERROR),
		},
	}
	got := Compute(byName, Options{})
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
	if got[0].TestName != "flaky" {
		t.Errorf("want flaky first, got %s", got[0].TestName)
	}
	if math.Abs(got[0].Rate-1.0) > 1e-9 {
		t.Errorf("flaky rate = %f, want 1.0", got[0].Rate)
	}
	if got[1].TestName != "stable" || got[1].Rate != 0 {
		t.Errorf("want stable second with rate 0, got %s rate %f", got[1].TestName, got[1].Rate)
	}
}

func TestCompute_WindowKeepsOnlyMostRecent(t *testing.T) {
	t0 := time.Now()
	// 6 outcomes; the last 4 are P F P F → rate 1.0 over the window.
	byName := map[string][]*tracepb.ResourceSpans{
		persist.EncodeName("t"): {
			makeRun("t", "", t0.Add(0), tracepb.Status_STATUS_CODE_OK),
			makeRun("t", "", t0.Add(time.Second), tracepb.Status_STATUS_CODE_OK),
			makeRun("t", "", t0.Add(2*time.Second), tracepb.Status_STATUS_CODE_OK),
			makeRun("t", "", t0.Add(3*time.Second), tracepb.Status_STATUS_CODE_ERROR),
			makeRun("t", "", t0.Add(4*time.Second), tracepb.Status_STATUS_CODE_OK),
			makeRun("t", "", t0.Add(5*time.Second), tracepb.Status_STATUS_CODE_ERROR),
		},
	}
	got := Compute(byName, Options{Window: 4})
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %d", len(got))
	}
	if math.Abs(got[0].Rate-1.0) > 1e-9 {
		t.Errorf("rate = %f, want 1.0 (last 4 are PFPF)", got[0].Rate)
	}
	if len(got[0].Outcomes) != 4 {
		t.Errorf("want 4 outcomes after windowing, got %d", len(got[0].Outcomes))
	}
}

func TestCompute_BranchFilter(t *testing.T) {
	t0 := time.Now()
	byName := map[string][]*tracepb.ResourceSpans{
		persist.EncodeName("t"): {
			makeRun("t", "main", t0.Add(0), tracepb.Status_STATUS_CODE_OK),
			makeRun("t", "feature", t0.Add(time.Second), tracepb.Status_STATUS_CODE_ERROR),
			makeRun("t", "main", t0.Add(2*time.Second), tracepb.Status_STATUS_CODE_OK),
			makeRun("t", "feature", t0.Add(3*time.Second), tracepb.Status_STATUS_CODE_ERROR),
		},
	}
	// No filter: outcomes are PFPF → rate 1.0.
	all := Compute(byName, Options{})
	if math.Abs(all[0].Rate-1.0) > 1e-9 {
		t.Errorf("no-filter rate = %f, want 1.0", all[0].Rate)
	}
	// Filter to main: outcomes are PP → rate 0.
	main := Compute(byName, Options{Branch: "main"})
	if main[0].Rate != 0 {
		t.Errorf("main-only rate = %f, want 0", main[0].Rate)
	}
}

func TestCompute_OrdersOutcomesOldestFirst(t *testing.T) {
	t0 := time.Now()
	// Insert in reversed order — Compute should sort oldest first
	// before reading outcomes.
	byName := map[string][]*tracepb.ResourceSpans{
		persist.EncodeName("t"): {
			makeRun("t", "", t0.Add(2*time.Second), tracepb.Status_STATUS_CODE_OK),
			makeRun("t", "", t0.Add(time.Second), tracepb.Status_STATUS_CODE_ERROR),
			makeRun("t", "", t0.Add(0), tracepb.Status_STATUS_CODE_OK),
		},
	}
	got := Compute(byName, Options{})
	want := []Outcome{Pass, Fail, Pass}
	if len(got[0].Outcomes) != 3 {
		t.Fatalf("want 3 outcomes, got %d", len(got[0].Outcomes))
	}
	for i, o := range want {
		if got[0].Outcomes[i] != o {
			t.Errorf("outcome[%d] = %v, want %v", i, got[0].Outcomes[i], o)
		}
	}
}

func TestCompute_StatusUnsetIsSkip(t *testing.T) {
	t0 := time.Now()
	byName := map[string][]*tracepb.ResourceSpans{
		persist.EncodeName("t"): {
			makeRun("t", "", t0.Add(0), tracepb.Status_STATUS_CODE_OK),
			makeRun("t", "", t0.Add(time.Second), tracepb.Status_STATUS_CODE_UNSET),
			makeRun("t", "", t0.Add(2*time.Second), tracepb.Status_STATUS_CODE_ERROR),
		},
	}
	got := Compute(byName, Options{})
	// Outcome sequence is [Pass, Skip, Fail]; after the rate filter
	// removes the skip we have [Pass, Fail] → 1 transition / 1 pair = 1.0.
	if math.Abs(got[0].Rate-1.0) > 1e-9 {
		t.Errorf("rate = %f, want 1.0", got[0].Rate)
	}
}
