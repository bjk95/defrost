package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/bjk95/defrost/internal/persist"
)

// fakeDropBackend stands in for persist.Backend in the drop endpoint
// tests. It always reports a fixed plan and records whether the confirm
// callback returned true (i.e. the destructive path executed).
type fakeDropBackend struct {
	mu        sync.Mutex
	plan      persist.DropPlan
	executed  bool
	calls     int
	seenSel   persist.DropSelector
	returnErr error
}

func (f *fakeDropBackend) DropHistory(sel persist.DropSelector, confirm func(persist.DropPlan) bool) error {
	f.mu.Lock()
	f.calls++
	f.seenSel = sel
	f.mu.Unlock()
	if f.returnErr != nil {
		return f.returnErr
	}
	plan := f.plan
	plan.Sel = sel
	if confirm != nil && confirm(plan) {
		f.mu.Lock()
		f.executed = true
		f.mu.Unlock()
	}
	return nil
}

func (f *fakeDropBackend) lastSel() persist.DropSelector {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seenSel
}

func newDropServer(t *testing.T, plan persist.DropPlan) (*httptest.Server, *fakeDropBackend) {
	t.Helper()
	fake := &fakeDropBackend{plan: plan}
	prev := dropBackendFn
	dropBackendFn = func(_ persist.Options) dropBackend { return fake }
	t.Cleanup(func() { dropBackendFn = prev })

	prevLoader := loaderFn
	loaderFn = func(_ persist.Options, _ ProgressEmitter) (Dataset, error) { return Dataset{}, nil }
	t.Cleanup(func() { loaderFn = prevLoader })

	h := New(persist.Options{}, fstest.MapFS{})
	return httptest.NewServer(h), fake
}

func TestDropPlan_GetReturnsInventoryWithoutExecuting(t *testing.T) {
	srv, fake := newDropServer(t, persist.DropPlan{
		Branch:        "_defrost",
		OriginURL:     "git@github.com:org/repo.git",
		TraceFiles:    50,
		MetricFiles:   12,
		TraceBytes:    8412 * 1024,
		MetricBytes:   312 * 1024,
		OldestRunUTC:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NewestRunUTC:  time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		SuppressionsN: 12,
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/drop/plan?drop_traces=true&drop_metrics=true")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var body dropPlanResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TraceFiles != 50 || body.MetricFiles != 12 {
		t.Errorf("file counts: want (50,12), got (%d,%d)", body.TraceFiles, body.MetricFiles)
	}
	if body.OldestRunUTC == "" || body.NewestRunUTC == "" {
		t.Errorf("expected RFC3339 oldest/newest, got %q / %q", body.OldestRunUTC, body.NewestRunUTC)
	}
	if !body.DropTraces || !body.DropMetrics {
		t.Errorf("selector: want both true, got traces=%v metrics=%v", body.DropTraces, body.DropMetrics)
	}
	if fake.executed {
		t.Error("plan-only request must not execute the drop")
	}
}

func TestDropPlan_RejectsEmptySelector(t *testing.T) {
	srv, _ := newDropServer(t, persist.DropPlan{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/drop/plan?drop_traces=false&drop_metrics=false")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", resp.StatusCode)
	}
}

func TestDropPlan_SanitizesOriginURL(t *testing.T) {
	srv, _ := newDropServer(t, persist.DropPlan{
		Branch:    "_defrost",
		OriginURL: "https://ghp_secrettoken@github.com/org/repo.git",
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/drop/plan?drop_traces=true&drop_metrics=true")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var body dropPlanResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.OriginURL != "https://github.com/org/repo.git" {
		t.Errorf("origin URL leaked secret: got %q", body.OriginURL)
	}
}

func TestDrop_PostExecutesAndReturnsPlan(t *testing.T) {
	srv, fake := newDropServer(t, persist.DropPlan{
		Branch:      "_defrost",
		TraceFiles:  50,
		MetricFiles: 12,
	})
	defer srv.Close()

	body := bytes.NewBufferString(`{"drop_traces":true,"drop_metrics":false}`)
	resp, err := http.Post(srv.URL+"/api/drop", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	if !fake.executed {
		t.Error("expected drop to execute on POST")
	}
}

func TestDrop_PostRejectsEmptySelector(t *testing.T) {
	srv, _ := newDropServer(t, persist.DropPlan{})
	defer srv.Close()

	body := bytes.NewBufferString(`{"drop_traces":false,"drop_metrics":false}`)
	resp, err := http.Post(srv.URL+"/api/drop", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", resp.StatusCode)
	}
}

func TestDropPlan_PassesBeforeUTCToBackend(t *testing.T) {
	srv, fake := newDropServer(t, persist.DropPlan{Branch: "_defrost", TraceFiles: 5})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/drop/plan?drop_traces=true&drop_metrics=true&before_utc=2026-04-01")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var got dropPlanResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if !fake.lastSel().BeforeUTC.Equal(want) {
		t.Errorf("backend selector BeforeUTC: want %v, got %v", want, fake.lastSel().BeforeUTC)
	}
	if got.BeforeUTC == "" {
		t.Errorf("response should echo before_utc, got empty")
	}
}

func TestDropPlan_RejectsInvalidBeforeUTC(t *testing.T) {
	srv, _ := newDropServer(t, persist.DropPlan{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/drop/plan?drop_traces=true&drop_metrics=true&before_utc=not-a-date")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", resp.StatusCode)
	}
}

func TestDrop_PostHonorsBeforeUTC(t *testing.T) {
	srv, fake := newDropServer(t, persist.DropPlan{Branch: "_defrost", TraceFiles: 5})
	defer srv.Close()

	body := bytes.NewBufferString(`{"drop_traces":true,"drop_metrics":true,"before_utc":"2026-04-01"}`)
	resp, err := http.Post(srv.URL+"/api/drop", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if !fake.lastSel().BeforeUTC.Equal(want) {
		t.Errorf("backend selector BeforeUTC: want %v, got %v", want, fake.lastSel().BeforeUTC)
	}
}

func TestDrop_NoOpPlanReturnsNothingTrue(t *testing.T) {
	srv, fake := newDropServer(t, persist.DropPlan{
		Branch: "_defrost",
		// zero file counts → Nothing() returns true
	})
	defer srv.Close()

	body := bytes.NewBufferString(`{"drop_traces":true,"drop_metrics":true}`)
	resp, err := http.Post(srv.URL+"/api/drop", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var got dropPlanResponse
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if !got.Nothing {
		t.Error("expected nothing=true for zero-file plan")
	}
	if fake.executed {
		t.Error("nothing-to-drop plan must not execute")
	}
}
