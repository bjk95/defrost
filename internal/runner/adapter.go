package runner

import (
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/bjk95/defrost/internal/models"
)

// Adapter wraps a single language/test-framework integration. Each
// implementation inspects a defrost-exec argv and decides whether it
// handles it (Matches), then runs the underlying child command and
// returns the per-test spans, any framework-emitted metrics, and the
// child's exit code (Run). Adapters that don't emit metrics return an
// empty pmetric.Metrics.
//
// Adapters are in-process scrape receivers in the OTel sense: they
// parse non-OTLP runner output (go test -json, JUnit XML, jest JSON)
// and produce pdata directly. The receiver path captures over-the-wire
// pdata from any OTel SDK the user's tests speak to; both paths end up
// in the same in-memory Sink before the gitExporter flushes.
//
// Run takes the run context so adapters can stamp the trace_id and
// parent_span_id on each emitted span, and the run's resource on the
// emitted ResourceSpans/ResourceMetrics.
type Adapter interface {
	Matches(cmd []string) bool
	Run(cmd []string, run models.RunContext) (ptrace.Traces, pmetric.Metrics, int)
}

// Registry holds an ordered list of adapters. The first adapter whose
// Matches returns true wins. Order is registration order.
type Registry struct {
	adapters []Adapter
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(a Adapter) {
	r.adapters = append(r.adapters, a)
}

func (r *Registry) Find(cmd []string) Adapter {
	for _, a := range r.adapters {
		if a.Matches(cmd) {
			return a
		}
	}
	return nil
}
