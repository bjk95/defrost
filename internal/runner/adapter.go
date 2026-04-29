package runner

import "github.com/bjk95/defrost/internal/models"

// Adapter wraps a single language/test-framework integration. Implementations
// inspect a defrost-exec argv and decide whether they handle it (Matches),
// then run the underlying child command and return the parsed test results
// plus the child's exit code (Run). The caller is responsible for how to
// present or persist the results.
type Adapter interface {
	Matches(cmd []string) bool
	Run(cmd []string) ([]models.TestResult, int)
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
