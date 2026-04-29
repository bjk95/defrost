package runner

import (
	"testing"

	"github.com/bjk95/defrost/internal/models"
)

type fakeAdapter struct {
	name    string
	matchOn string
	exit    int
}

func (a fakeAdapter) Matches(cmd []string) bool {
	return len(cmd) > 0 && cmd[0] == a.matchOn
}

func (a fakeAdapter) Run(cmd []string) ([]models.TestResult, int) { return nil, a.exit }

func TestFindReturnsNilWhenEmpty(t *testing.T) {
	r := NewRegistry()
	if got := r.Find([]string{"go", "test"}); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestFindReturnsNilWhenNoMatch(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeAdapter{name: "go", matchOn: "go"})
	if got := r.Find([]string{"pytest"}); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestFindReturnsFirstMatch(t *testing.T) {
	r := NewRegistry()
	first := fakeAdapter{name: "first", matchOn: "go", exit: 1}
	second := fakeAdapter{name: "second", matchOn: "go", exit: 2}
	r.Register(first)
	r.Register(second)

	got := r.Find([]string{"go", "test"})
	if got == nil {
		t.Fatal("expected match, got nil")
	}
	if _, code := got.Run(nil); code != 1 {
		t.Fatalf("expected first adapter (exit=1), got exit=%d", code)
	}
}

func TestFindMatchesByCmd(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeAdapter{name: "go", matchOn: "go"})
	r.Register(fakeAdapter{name: "pytest", matchOn: "pytest"})

	got := r.Find([]string{"pytest", "tests/"})
	if got == nil {
		t.Fatal("expected match, got nil")
	}
}
