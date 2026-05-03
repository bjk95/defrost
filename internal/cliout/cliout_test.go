package cliout

import (
	"bytes"
	"strings"
	"testing"
)

func TestDefaultSymbolsNoColor(t *testing.T) {
	var stderr, stdout bytes.Buffer
	p := New(&stderr, &stdout, 0, false) // verbosity=0, color=false

	p.Step("running tests")
	p.Pass("142 passed")
	p.Fail("3 failed")
	p.Warn("3 suppressed")

	got := stderr.String()
	wants := []string{
		"→ running tests\n",
		"✓ 142 passed\n",
		"✗ 3 failed\n",
		"! 3 suppressed\n",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("stderr missing %q\nfull stderr:\n%s", w, got)
		}
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty for decorated calls, got %q", stdout.String())
	}
	// No ANSI escapes when color is off.
	if strings.Contains(got, "\x1b[") {
		t.Errorf("color disabled but ANSI escapes present:\n%s", got)
	}
}

func TestColorEmitsANSI(t *testing.T) {
	var stderr, stdout bytes.Buffer
	p := New(&stderr, &stdout, 0, true)
	p.Pass("ok")
	got := stderr.String()
	if !strings.Contains(got, "\x1b[32m") {
		t.Errorf("expected green ANSI escape, got %q", got)
	}
	if !strings.Contains(got, "\x1b[0m") {
		t.Errorf("expected reset ANSI escape, got %q", got)
	}
	if !strings.Contains(got, "✓") {
		t.Errorf("expected ✓ symbol, got %q", got)
	}
}
