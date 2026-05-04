package otlp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

func TestReceiver_RoundTrip_Traces(t *testing.T) {
	sink := NewSink()
	r, port, err := Start(context.Background(), sink)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "round-trip")
	rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("TestFoo")
	body, err := ptraceotlp.NewExportRequestFromTraces(td).MarshalProto()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/traces", port)
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("new req: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-protobuf")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("post: %v", lastErr)
	}

	got, _, _ := sink.Drain()
	if got.ResourceSpans().Len() != 1 {
		t.Errorf("traces drained: got %d ResourceSpans, want 1", got.ResourceSpans().Len())
	}
}

func TestReceiver_RoundTrip_Metrics(t *testing.T) {
	sink := NewSink()
	r, port, err := Start(context.Background(), sink)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "metric-roundtrip")
	m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("test.metric")
	g := m.SetEmptyGauge()
	g.DataPoints().AppendEmpty().SetDoubleValue(42)
	body, err := pmetricotlp.NewExportRequestFromMetrics(md).MarshalProto()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port)
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/x-protobuf")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("post: %v", lastErr)
	}
	_, gotM, _ := sink.Drain()
	if gotM.ResourceMetrics().Len() != 1 {
		t.Errorf("metrics drained: got %d, want 1", gotM.ResourceMetrics().Len())
	}
}
