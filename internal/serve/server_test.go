package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

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
