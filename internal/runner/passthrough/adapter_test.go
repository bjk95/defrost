package passthrough

import (
	"os/exec"
	"testing"
)

func TestMatches_AnyNonEmpty(t *testing.T) {
	cases := []struct {
		cmd  []string
		want bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"true"}, true},
		{[]string{"go", "test"}, true},
		{[]string{"some-arbitrary-tool", "--flag"}, true},
	}
	for _, tc := range cases {
		if got := (Adapter{}).Matches(tc.cmd); got != tc.want {
			t.Errorf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestRun_PropagatesExitCode(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	results, _, code := (Adapter{}).Run([]string{"sh", "-c", "exit 3"})
	if results != nil {
		t.Errorf("expected no results, got %v", results)
	}
	if code != 3 {
		t.Errorf("got code %d, want 3", code)
	}
}

func TestRun_BinaryNotFoundReturnsOne(t *testing.T) {
	results, _, code := (Adapter{}).Run([]string{"/nonexistent/defrost-test-binary"})
	if results != nil {
		t.Errorf("expected no results, got %v", results)
	}
	if code != 1 {
		t.Errorf("got code %d, want 1 (start failure surfaces as 1)", code)
	}
}
