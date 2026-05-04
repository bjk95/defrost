// Package otlp wires the upstream OTel `otlpreceiver` factory into a
// single in-process `bufferSink` that accumulates pdata across all three
// signals (traces, metrics, logs) until the run flushes. The sink is
// also the convergence point for in-process pdata produced by runner
// adapters: both the over-the-wire receiver path and the adapter path
// call ConsumeTraces / ConsumeMetrics / ConsumeLogs on the same sink.
package otlp

import (
	"context"
	"sync"

	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// Sink is the consumer chain endpoint shared by the OTLP receiver and
// any in-process adapter that produces pdata directly. It satisfies
// consumer.Traces, consumer.Metrics, and consumer.Logs simultaneously
// so a single instance can be passed to all three receiver factories.
type Sink struct {
	mu      sync.Mutex
	traces  ptrace.Traces
	metrics pmetric.Metrics
	logs    plog.Logs
}

// NewSink returns an empty Sink ready to accept pdata from the receiver
// or an adapter.
func NewSink() *Sink {
	return &Sink{
		traces:  ptrace.NewTraces(),
		metrics: pmetric.NewMetrics(),
		logs:    plog.NewLogs(),
	}
}

// Capabilities reports MutatesData=false so the receiver doesn't have
// to clone before handing data off.
func (s *Sink) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

// ConsumeTraces appends td's resource spans into the sink's accumulator.
// Safe for concurrent calls from multiple OTLP HTTP requests.
func (s *Sink) ConsumeTraces(_ context.Context, td ptrace.Traces) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	td.ResourceSpans().MoveAndAppendTo(s.traces.ResourceSpans())
	return nil
}

// ConsumeMetrics appends md's resource metrics into the accumulator.
func (s *Sink) ConsumeMetrics(_ context.Context, md pmetric.Metrics) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	md.ResourceMetrics().MoveAndAppendTo(s.metrics.ResourceMetrics())
	return nil
}

// ConsumeLogs appends ld's resource logs into the accumulator.
func (s *Sink) ConsumeLogs(_ context.Context, ld plog.Logs) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ld.ResourceLogs().MoveAndAppendTo(s.logs.ResourceLogs())
	return nil
}

// Drain returns the accumulated pdata for each signal and resets the
// sink. The returned values are detached from the sink — callers can
// mutate or serialize them freely.
func (s *Sink) Drain() (ptrace.Traces, pmetric.Metrics, plog.Logs) {
	s.mu.Lock()
	defer s.mu.Unlock()
	td, md, ld := s.traces, s.metrics, s.logs
	s.traces = ptrace.NewTraces()
	s.metrics = pmetric.NewMetrics()
	s.logs = plog.NewLogs()
	return td, md, ld
}

// compile-time interface checks
var (
	_ consumer.Traces  = (*Sink)(nil)
	_ consumer.Metrics = (*Sink)(nil)
	_ consumer.Logs    = (*Sink)(nil)
)
