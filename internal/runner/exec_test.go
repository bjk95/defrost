package runner

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestRunChild_PropagatesExitCode(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	c := exec.Command("sh", "-c", "exit 7")
	// Discard child stdio so the test runner's output stays clean.
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	code, err := RunChild(c)
	if err != nil {
		t.Fatalf("RunChild err: %v", err)
	}
	if code != 7 {
		t.Errorf("got code %d, want 7", code)
	}
}

func TestRunChild_BinaryNotFound(t *testing.T) {
	c := exec.Command("/nonexistent/defrost-test-binary")
	code, err := RunChild(c)
	if err == nil {
		t.Fatalf("expected error for missing binary, got code=%d", code)
	}
	if code != -1 {
		t.Errorf("got code %d, want -1 for start failure", code)
	}
}

func TestRunChild_ZeroExit(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	c := exec.Command("sh", "-c", "exit 0")
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	code, err := RunChild(c)
	if err != nil || code != 0 {
		t.Fatalf("got (%d, %v), want (0, nil)", code, err)
	}
}
