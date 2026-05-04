package otlp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
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

// TestReceiver_RoundTrip_Logs proves the otlpreceiver factory's
// CreateLogs path is wired through to our Sink. Same shape as the
// trace and metric round-trips above: build a tiny plog.Logs, marshal
// to canonical OTLP, POST to /v1/logs, drain the sink, assert.
//
// This is the smallest end-to-end test that exercises the third
// signal — sink-level coverage (sink_test.go) verifies ConsumeLogs
// in isolation, but doesn't prove the receiver's HTTP route or
// factory wiring; this one does.
func TestReceiver_RoundTrip_Logs(t *testing.T) {
	sink := NewSink()
	r, port, err := Start(context.Background(), sink)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "log-roundtrip")
	rec := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	rec.SetSeverityText("INFO")
	rec.Body().SetStr("hello from a test")
	body, err := plogotlp.NewExportRequestFromLogs(ld).MarshalProto()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/logs", port)
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
	_, _, gotL := sink.Drain()
	if gotL.ResourceLogs().Len() != 1 {
		t.Fatalf("logs drained: got %d ResourceLogs, want 1", gotL.ResourceLogs().Len())
	}
	first := gotL.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	if got := first.Body().Str(); got != "hello from a test" {
		t.Errorf("log body: got %q, want %q", got, "hello from a test")
	}
}
