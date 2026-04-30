package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestReceiver_AcceptsMetrics(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	req := &cmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{
					Name: "test.gauge",
					Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
						DataPoints: []*metricspb.NumberDataPoint{{
							TimeUnixNano: 1,
							Attributes:   []*commonpb.KeyValue{strKV("k", "v")},
							Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 1.5},
						}},
					}},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port), "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := r.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 buffered request, got %d", len(got))
	}
	if got[0].ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Name != "test.gauge" {
		t.Errorf("buffered metric name wrong: %q", got[0].ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Name)
	}
}

func TestReceiver_RejectsWrongMethod(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: want 405, got %d", resp.StatusCode)
	}
}

func TestReceiver_RejectsWrongPath(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/traces", port), "application/x-protobuf", bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", resp.StatusCode)
	}
}

func TestReceiver_RejectsWrongContentType(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port), "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status: want 415, got %d", resp.StatusCode)
	}
}

func TestReceiver_RejectsBadProtobuf(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port), "application/x-protobuf", bytes.NewReader([]byte{0xff, 0xff, 0xff}))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", resp.StatusCode)
	}
}

func TestReceiver_ShutdownDrainsAndIsIdempotent(t *testing.T) {
	r := New()
	if _, err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx := context.Background()
	if _, err := r.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if _, err := r.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown should be a no-op, got: %v", err)
	}
}

func TestReceiver_AcceptsGzippedMetrics(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	req := &cmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{
					Name: "test.gzip",
					Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
						DataPoints: []*metricspb.NumberDataPoint{{
							TimeUnixNano: 1,
							Attributes:   []*commonpb.KeyValue{strKV("k", "v")},
							Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 2.5},
						}},
					}},
				}},
			}},
		}},
	}
	raw, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(raw); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port), &compressed)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := r.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 buffered request, got %d", len(got))
	}
	if got[0].ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Name != "test.gzip" {
		t.Errorf("buffered metric name wrong: %q", got[0].ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Name)
	}
}
