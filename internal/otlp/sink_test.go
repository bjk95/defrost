package otlp

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestSink_ConsumeAndDrain(t *testing.T) {
	s := NewSink()

	td := ptrace.NewTraces()
	td.ResourceSpans().AppendEmpty().Resource().Attributes().PutStr("service.name", "T")
	if err := s.ConsumeTraces(context.Background(), td); err != nil {
		t.Fatalf("ConsumeTraces: %v", err)
	}

	md := pmetric.NewMetrics()
	md.ResourceMetrics().AppendEmpty().Resource().Attributes().PutStr("service.name", "M")
	if err := s.ConsumeMetrics(context.Background(), md); err != nil {
		t.Fatalf("ConsumeMetrics: %v", err)
	}

	ld := plog.NewLogs()
	ld.ResourceLogs().AppendEmpty().Resource().Attributes().PutStr("service.name", "L")
	if err := s.ConsumeLogs(context.Background(), ld); err != nil {
		t.Fatalf("ConsumeLogs: %v", err)
	}

	gotT, gotM, gotL := s.Drain()
	if gotT.ResourceSpans().Len() != 1 {
		t.Errorf("traces ResourceSpans: got %d, want 1", gotT.ResourceSpans().Len())
	}
	if gotM.ResourceMetrics().Len() != 1 {
		t.Errorf("metrics ResourceMetrics: got %d, want 1", gotM.ResourceMetrics().Len())
	}
	if gotL.ResourceLogs().Len() != 1 {
		t.Errorf("logs ResourceLogs: got %d, want 1", gotL.ResourceLogs().Len())
	}

	// Sink should be empty after drain.
	gotT2, gotM2, gotL2 := s.Drain()
	if gotT2.ResourceSpans().Len() != 0 || gotM2.ResourceMetrics().Len() != 0 || gotL2.ResourceLogs().Len() != 0 {
		t.Errorf("sink non-empty after drain: %d / %d / %d",
			gotT2.ResourceSpans().Len(), gotM2.ResourceMetrics().Len(), gotL2.ResourceLogs().Len())
	}
}

func TestSink_Capabilities(t *testing.T) {
	s := NewSink()
	caps := s.Capabilities()
	if caps.MutatesData {
		t.Errorf("expected MutatesData=false")
	}
}
