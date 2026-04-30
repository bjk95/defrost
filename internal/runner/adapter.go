package runner

import (
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

// Adapter wraps a single language/test-framework integration. Implementations
// inspect a defrost-exec argv and decide whether they handle it (Matches),
// then run the underlying child command and return the parsed test results,
// any framework-emitted eval/score metric data points, and the child's exit
// code (Run). Adapters that don't emit metrics return a nil slice.
//
// Each *metricspb.Metric in the returned slice MUST contain exactly one
// data point (gauge / sum / histogram). This matches the convention
// established by `otlp.MetricsToEntries` for receiver-emitted metrics —
// the persistence layer writes one line per *metricspb.Metric.
type Adapter interface {
	Matches(cmd []string) bool
	Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int)
}

// Registry holds an ordered list of adapters. The first adapter whose Matches
// returns true wins. Order is registration order.
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
