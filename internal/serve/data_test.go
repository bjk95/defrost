package serve

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

func TestLoad_SortsRunsNewestFirstAndCapsAtFifty(t *testing.T) {
	roots := []*tracepb.ResourceSpans{}
	for i := 0; i < 60; i++ {
		roots = append(roots, &tracepb.ResourceSpans{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				models.StringAttr("defrost.run_id", idFor(i)),
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					Name:              "defrost.run",
					StartTimeUnixNano: uint64(i + 1),
				}},
			}},
		})
	}
	byName := map[string][]*tracepb.ResourceSpans{
		"tid-A": {
			testRSWithRun(idFor(0), "passed", 1),
			testRSWithRun(idFor(59), "failed", 60),
		},
	}

	prevLoadAll := persistLoadAll
	persistLoadAll = func(_ persist.Options) ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
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
	if rid := models.ResourceString(ds.Roots[0].Resource, "defrost.run_id"); rid != idFor(59) {
		t.Errorf("want newest root first, got %q", rid)
	}
	// Span whose run_id is no longer in the capped set must be dropped.
	if len(ds.TestSpans["tid-A"]) != 1 {
		t.Errorf("want 1 span for tid-A after run-cap filter, got %d", len(ds.TestSpans["tid-A"]))
	}
	survivor := persist.SpanFromResourceSpans(ds.TestSpans["tid-A"][0])
	if survivor == nil {
		t.Fatal("survivor span unexpectedly nil")
	}
	if rid := models.AttrString(survivor.Attributes, "defrost.run_id"); rid != idFor(59) {
		t.Errorf("want surviving span to reference run %q, got %q", idFor(59), rid)
	}
}

func TestLoad_PassesMetricsThroughUnfiltered(t *testing.T) {
	roots := []*tracepb.ResourceSpans{
		{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				models.StringAttr("defrost.run_id", "run-1"),
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{Name: "defrost.run", StartTimeUnixNano: 10}},
			}},
		},
	}
	// Two metrics: one with a matching exemplar, one without. Both must
	// survive Load — run-association now happens at the handler so the
	// exemplar-less metric still reaches the resolver.
	withExemplar := &metricspb.ResourceMetrics{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "g.with_exemplar",
				Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{{
						Value:     &metricspb.NumberDataPoint_AsDouble{AsDouble: 1},
						Exemplars: []*metricspb.Exemplar{{TraceId: models.DeriveTraceID("run-1")}},
					}},
				}},
			}},
		}},
	}
	noExemplar := &metricspb.ResourceMetrics{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "g.no_exemplar",
				Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{{
						Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 2},
					}},
				}},
			}},
		}},
	}

	prevSpans := persistLoadAll
	persistLoadAll = func(_ persist.Options) ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
		return roots, nil, nil
	}
	defer func() { persistLoadAll = prevSpans }()

	prevMetrics := persistLoadAllMetrics
	persistLoadAllMetrics = func(_ persist.Options) ([]*metricspb.ResourceMetrics, error) {
		return []*metricspb.ResourceMetrics{withExemplar, noExemplar}, nil
	}
	defer func() { persistLoadAllMetrics = prevMetrics }()

	ds, err := Load(persist.Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ds.Metrics) != 2 {
		t.Fatalf("want both metrics retained, got %d", len(ds.Metrics))
	}
}

func testRSWithRun(runID, status string, startNs uint64) *tracepb.ResourceSpans {
	return &tracepb.ResourceSpans{
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{
				Name:              "pkg.TestA",
				StartTimeUnixNano: startNs,
				Attributes: []*commonpb.KeyValue{
					models.StringAttr("defrost.run_id", runID),
					models.StringAttr("test.case.result.status", status),
				},
			}},
		}},
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
