package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
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

func TestServer_GetMetrics_TranslatesGaugeAndHistogram(t *testing.T) {
	runID := "run-1"
	traceID := models.DeriveTraceID(runID)

	rootResource := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
		models.StringAttr("defrost.run_id", runID),
	}}
	root := &tracepb.ResourceSpans{
		Resource: rootResource,
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{
				Name:              "defrost.run",
				StartTimeUnixNano: 1735_689_600_000_000_000, // 2026-01-01T00:00:00Z
			}},
		}},
	}

	gauge := &metricspb.ResourceMetrics{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "eval.factuality",
				Unit: "{score}",
				Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{{
						Attributes: []*commonpb.KeyValue{models.StringAttr("eval.model", "claude-sonnet-4.5")},
						Value:      &metricspb.NumberDataPoint_AsDouble{AsDouble: 0.91},
						Exemplars:  []*metricspb.Exemplar{{TraceId: traceID}},
					}},
				}},
			}},
		}},
	}
	hist := &metricspb.ResourceMetrics{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "http.server.request.duration",
				Unit: "ms",
				Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
					AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
					DataPoints: []*metricspb.HistogramDataPoint{{
						Count:          7,
						ExplicitBounds: []float64{1, 5, 10},
						BucketCounts:   []uint64{2, 3, 1, 1},
						Exemplars:      []*metricspb.Exemplar{{TraceId: traceID}},
					}},
				}},
			}},
		}},
	}

	ds := Dataset{Roots: []*tracepb.ResourceSpans{root}, Metrics: []*metricspb.ResourceMetrics{gauge, hist}}
	srv := newTestServer(t, ds, fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/metrics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	var body struct {
		Metrics []struct {
			Name       string `json:"name"`
			Instrument string `json:"instrument"`
			Points     []struct {
				RunID   string             `json:"run_id"`
				Attrs   map[string]string  `json:"attrs"`
				Value   *float64           `json:"value"`
				Count   *uint64            `json:"count"`
				Buckets []struct {
					LE    *float64 `json:"le"`
					Count uint64   `json:"count"`
				} `json:"buckets"`
			} `json:"points"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Metrics) != 2 {
		t.Fatalf("want 2 metrics, got %d", len(body.Metrics))
	}

	// Gauges sort first.
	g := body.Metrics[0]
	if g.Name != "eval.factuality" || g.Instrument != "gauge" {
		t.Errorf("first metric: %+v", g)
	}
	if len(g.Points) != 1 || g.Points[0].RunID != runID {
		t.Errorf("gauge point run_id missing: %+v", g.Points)
	}
	if g.Points[0].Value == nil || *g.Points[0].Value != 0.91 {
		t.Errorf("gauge value: %+v", g.Points[0].Value)
	}
	if g.Points[0].Attrs["eval.model"] != "claude-sonnet-4.5" {
		t.Errorf("gauge attr: %+v", g.Points[0].Attrs)
	}

	h := body.Metrics[1]
	if h.Name != "http.server.request.duration" || h.Instrument != "histogram" {
		t.Errorf("second metric: %+v", h)
	}
	if len(h.Points) != 1 {
		t.Fatalf("want 1 hist point, got %d", len(h.Points))
	}
	if h.Points[0].Count == nil || *h.Points[0].Count != 7 {
		t.Errorf("hist count: %+v", h.Points[0].Count)
	}
	if len(h.Points[0].Buckets) != 4 {
		t.Fatalf("want 4 buckets (incl. +Inf), got %d", len(h.Points[0].Buckets))
	}
	// Last bucket should encode +Inf as null LE.
	if last := h.Points[0].Buckets[3]; last.LE != nil {
		t.Errorf("+Inf bucket LE should be null, got %v", *last.LE)
	}
}

func TestServer_GetMetrics_DropsDataPointsWithoutKeptRun(t *testing.T) {
	runID := "run-kept"

	root := &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			models.StringAttr("defrost.run_id", runID),
		}},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{Name: "defrost.run", StartTimeUnixNano: 1}},
		}},
	}
	// Exemplar trace_id for an unknown run — should be filtered out.
	stranger := &metricspb.ResourceMetrics{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "stale.metric",
				Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{{
						Value:     &metricspb.NumberDataPoint_AsDouble{AsDouble: 1.0},
						Exemplars: []*metricspb.Exemplar{{TraceId: models.DeriveTraceID("run-stranger")}},
					}},
				}},
			}},
		}},
	}
	ds := Dataset{Roots: []*tracepb.ResourceSpans{root}, Metrics: []*metricspb.ResourceMetrics{stranger}}
	srv := newTestServer(t, ds, fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/metrics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Metrics []json.RawMessage `json:"metrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Metrics) != 0 {
		t.Errorf("want 0 metrics (data point belongs to dropped run), got %d", len(body.Metrics))
	}
}

func TestServer_GetMetrics_TimeWindowFallbackForExemplarlessPoints(t *testing.T) {
	// Run window: [1000, 2000].
	root := &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			models.StringAttr("defrost.run_id", "run-1"),
		}},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{
				Name:              "defrost.run",
				StartTimeUnixNano: 1000,
				EndTimeUnixNano:   2000,
			}},
		}},
	}
	// No exemplars; time falls inside the run window. Mirrors the
	// auto-emitted defrost.run.<cmd> gauge shape on disk.
	pointInWindow := &metricspb.ResourceMetrics{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "defrost.run.go test",
				Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{{
						TimeUnixNano: 1500,
						Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 12345},
					}},
				}},
			}},
		}},
	}

	ds := Dataset{Roots: []*tracepb.ResourceSpans{root}, Metrics: []*metricspb.ResourceMetrics{pointInWindow}}
	srv := newTestServer(t, ds, fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/metrics")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Metrics []struct {
			Name   string `json:"name"`
			Points []struct {
				RunID string `json:"run_id"`
			} `json:"points"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Metrics) != 1 || len(body.Metrics[0].Points) != 1 {
		t.Fatalf("want 1 metric/1 point via time fallback, got %+v", body.Metrics)
	}
	if body.Metrics[0].Points[0].RunID != "run-1" {
		t.Errorf("expected run-1 attribution, got %q", body.Metrics[0].Points[0].RunID)
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
