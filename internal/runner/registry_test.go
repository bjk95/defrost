package runner

import (
	"testing"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/bjk95/defrost/internal/models"
)

type fakeAdapter struct {
	matchPrefix string
	tag         string
}

func (f *fakeAdapter) Matches(cmd []string) bool {
	return len(cmd) > 0 && cmd[0] == f.matchPrefix
}

func (f *fakeAdapter) Run(cmd []string, _ models.RunContext) (ptrace.Traces, pmetric.Metrics, int) {
	return ptrace.NewTraces(), pmetric.NewMetrics(), 0
}

func TestRegistry_FindReturnsMatch(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeAdapter{matchPrefix: "go", tag: "go"})
	r.Register(&fakeAdapter{matchPrefix: "pytest", tag: "py"})

	got := r.Find([]string{"pytest", "tests/"})
	if got == nil {
		t.Fatalf("expected to find pytest adapter, got nil")
	}
	if got.(*fakeAdapter).tag != "py" {
		t.Errorf("expected py, got %s", got.(*fakeAdapter).tag)
	}
}

func TestRegistry_FindReturnsFirstMatch(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeAdapter{matchPrefix: "go", tag: "first"})
	r.Register(&fakeAdapter{matchPrefix: "go", tag: "second"})

	got := r.Find([]string{"go", "test"})
	if got == nil {
		t.Fatalf("expected match, got nil")
	}
	if got.(*fakeAdapter).tag != "first" {
		t.Errorf("expected first adapter, got %s", got.(*fakeAdapter).tag)
	}
}

func TestRegistry_FindReturnsNilWhenNoMatch(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeAdapter{matchPrefix: "go"})
	if got := r.Find([]string{"pytest"}); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
