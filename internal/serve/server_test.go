package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"testing/fstest"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// stubBackend is a minimal in-memory persist.Backend used to test the
// suppressions endpoints without going through the real git/file
// backends. Only the suppressions methods are exercised; the rest are
// stubs that return zero values.
type stubBackend struct {
	ids []string
}

func (s *stubBackend) InitialisePersistence() error { return nil }
func (s *stubBackend) InsertNewRun(_ []*tracepb.ResourceSpans, _ []*metricspb.ResourceMetrics) error {
	return nil
}
func (s *stubBackend) GetTestHistory(_ string) ([]*tracepb.ResourceSpans, error) { return nil, nil }
func (s *stubBackend) LoadAll() ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	return nil, nil, nil
}
func (s *stubBackend) GetSuppressions() ([]string, error) {
	out := append([]string(nil), s.ids...)
	return out, nil
}
func (s *stubBackend) UpdateSuppressions(mutate func([]string) []string, _ string) error {
	cur := append([]string(nil), s.ids...)
	next := mutate(cur)
	// Mirror the on-disk dedupe+sort so reads are deterministic.
	seen := make(map[string]struct{}, len(next))
	out := make([]string, 0, len(next))
	for _, id := range next {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	s.ids = out
	return nil
}

func newSuppressionsServer(t *testing.T, ids []string) (*httptest.Server, *stubBackend) {
	t.Helper()
	be := &stubBackend{ids: append([]string(nil), ids...)}
	prev := backendFn
	backendFn = func(_ persist.Options) persist.Backend { return be }
	t.Cleanup(func() { backendFn = prev })
	h := New(persist.Options{}, fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, be
}

func stubDataset() Dataset {
	return Dataset{
		Roots: []*tracepb.ResourceSpans{
			{
				Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
					models.StringAttr("defrost.run_id", "run-2"),
					models.StringAttr("vcs.repository.ref.revision", "deadbee"),
					models.StringAttr("vcs.repository.ref.name", "main"),
				}},
				ScopeSpans: []*tracepb.ScopeSpans{{
					Spans: []*tracepb.Span{{
						Name:              "defrost.run",
						StartTimeUnixNano: 1735_776_000_000_000_000, // 2026-01-02T00:00:00Z
					}},
				}},
			},
			{
				Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
					models.StringAttr("defrost.run_id", "run-1"),
					models.StringAttr("vcs.repository.ref.revision", "cafebab"),
					models.StringAttr("vcs.repository.ref.name", "main"),
				}},
				ScopeSpans: []*tracepb.ScopeSpans{{
					Spans: []*tracepb.Span{{
						Name:              "defrost.run",
						StartTimeUnixNano: 1735_689_600_000_000_000, // 2026-01-01T00:00:00Z
					}},
				}},
			},
		},
		TestSpans: map[string][]*tracepb.ResourceSpans{
			"tid-A": {
				makeTestRS("pkg.TestA", "run-1", "passed", 1735_689_600_000_000_000, 1735_689_600_005_000_000, ""),
				makeTestRS("pkg.TestA", "run-2", "failed", 1735_776_000_000_000_000, 1735_776_000_009_000_000, "BOOM"),
			},
		},
	}
}

func makeTestRS(name, runID, status string, startNs, endNs uint64, output string) *tracepb.ResourceSpans {
	span := &tracepb.Span{
		Name:              name,
		StartTimeUnixNano: startNs,
		EndTimeUnixNano:   endNs,
		Attributes: []*commonpb.KeyValue{
			models.StringAttr("defrost.run_id", runID),
			models.StringAttr("test.case.result.status", status),
		},
	}
	if output != "" {
		span.Events = []*tracepb.Span_Event{{
			Name:       "test.output",
			Attributes: []*commonpb.KeyValue{models.StringAttr("body", output)},
		}}
	}
	return &tracepb.ResourceSpans{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{span}}},
	}
}

func newTestServer(t *testing.T, ds Dataset, assets fstest.MapFS) *httptest.Server {
	t.Helper()
	prev := loaderFn
	loaderFn = func(_ persist.Options) (Dataset, error) { return ds, nil }
	t.Cleanup(func() { loaderFn = prev })

	h := New(persist.Options{}, assets)
	return httptest.NewServer(h)
}

func TestServer_GetTests_ShapeAndCacheControl(t *testing.T) {
	srv := newTestServer(t, stubDataset(), fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tests")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control: want %q, got %q", "public, max-age=60", got)
	}

	var body struct {
		Runs  []map[string]any `json:"runs"`
		Tests []struct {
			TestID   string           `json:"test_id"`
			TestName string           `json:"test_name"`
			Cells    []map[string]any `json:"cells"`
		} `json:"tests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Runs) != 2 {
		t.Errorf("want 2 runs, got %d", len(body.Runs))
	}
	if len(body.Tests) != 1 || body.Tests[0].TestName != "pkg.TestA" {
		t.Errorf("unexpected tests: %+v", body.Tests)
	}
	if len(body.Tests[0].Cells) != 2 {
		t.Errorf("want 2 cells, got %d", len(body.Tests[0].Cells))
	}
	if _, hasOutput := body.Tests[0].Cells[0]["output"]; hasOutput {
		t.Errorf("Output must be omitted from /api/tests cells")
	}
}

func TestServer_GetTestRun_HappyPath(t *testing.T) {
	srv := newTestServer(t, stubDataset(), fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/test/tid-A/run/run-2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=86400" {
		t.Errorf("Cache-Control: want %q, got %q", "public, max-age=86400", got)
	}

	var body struct {
		Test struct {
			Output string `json:"output"`
		} `json:"test"`
		Run struct {
			RunID string `json:"run_id"`
		} `json:"run"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Test.Output != "BOOM" {
		t.Errorf("want full Output, got %q", body.Test.Output)
	}
	if body.Run.RunID != "run-2" {
		t.Errorf("want run-2, got %q", body.Run.RunID)
	}
}

func TestServer_GetTestRun_404OnUnknown(t *testing.T) {
	srv := newTestServer(t, stubDataset(), fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/test/unknown/run/run-2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestServer_GetTestRun_HandlesDoubleEncodedTestIDs(t *testing.T) {
	encodedTID := "github.com%2Fx%2Fp%2FTestA"
	ds := Dataset{
		Roots: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				models.StringAttr("defrost.run_id", "run-1"),
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{Name: "defrost.run", StartTimeUnixNano: 1}},
			}},
		}},
		TestSpans: map[string][]*tracepb.ResourceSpans{
			encodedTID: {makeTestRS("github.com/x/p/TestA", "run-1", "passed", 1, 4_000_000, "ok")},
		},
	}
	srv := newTestServer(t, ds, fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")},
	})
	defer srv.Close()

	doubleEncodedTID := url.PathEscape(encodedTID)
	resp, err := http.Get(srv.URL + "/api/test/" + doubleEncodedTID + "/run/run-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d (URL: %s)", resp.StatusCode, "/api/test/"+doubleEncodedTID+"/run/run-1")
	}
}

func TestServer_SPAFallback(t *testing.T) {
	srv := newTestServer(t, stubDataset(), fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html>spa</html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/some/spa/path")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != "<!doctype html><html>spa</html>" {
		t.Errorf("fallback body wrong: %q", buf[:n])
	}
}

func TestServer_GetSuppressions_EmptyIsEmptyArray(t *testing.T) {
	srv, _ := newSuppressionsServer(t, nil)

	resp, err := http.Get(srv.URL + "/api/suppressions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var body suppressionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TestIDs == nil {
		t.Errorf("test_ids must be [] not null when empty")
	}
	if len(body.TestIDs) != 0 {
		t.Errorf("want 0 ids, got %d", len(body.TestIDs))
	}
}

func TestServer_PostSuppressions_AddsAndReturnsCurrent(t *testing.T) {
	srv, be := newSuppressionsServer(t, []string{"existing"})

	body, _ := json.Marshal(suppressionMutation{TestID: "pkg.TestNew"})
	resp, err := http.Post(srv.URL+"/api/suppressions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var got suppressionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.TestIDs) != 2 || got.TestIDs[0] != "existing" || got.TestIDs[1] != "pkg.TestNew" {
		t.Errorf("unexpected response ids: %v", got.TestIDs)
	}
	if len(be.ids) != 2 || be.ids[0] != "existing" || be.ids[1] != "pkg.TestNew" {
		t.Errorf("backend state: %v", be.ids)
	}
}

func TestServer_PostSuppressions_RejectsMissingID(t *testing.T) {
	srv, _ := newSuppressionsServer(t, nil)
	resp, err := http.Post(srv.URL+"/api/suppressions", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestServer_DeleteSuppressions_RemovesAndReturnsCurrent(t *testing.T) {
	srv, be := newSuppressionsServer(t, []string{"a", "b", "c"})

	body, _ := json.Marshal(suppressionMutation{TestID: "b"})
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/suppressions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var got suppressionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.TestIDs) != 2 || got.TestIDs[0] != "a" || got.TestIDs[1] != "c" {
		t.Errorf("unexpected response ids: %v", got.TestIDs)
	}
	if len(be.ids) != 2 || be.ids[0] != "a" || be.ids[1] != "c" {
		t.Errorf("backend state: %v", be.ids)
	}
}

func TestServer_Suppressions_RejectsUnsupportedMethod(t *testing.T) {
	srv, _ := newSuppressionsServer(t, nil)
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/suppressions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", resp.StatusCode)
	}
}
