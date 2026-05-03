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

func TestVerbosityGates(t *testing.T) {
	cases := []struct {
		name      string
		verbosity int
		emit      func(*Printer)
		want      string
		notWant   string
	}{
		{"default hides Info", 0, func(p *Printer) { p.Info("hidden") }, "", "hidden"},
		{"-v shows Info", 1, func(p *Printer) { p.Info("shown") }, "shown", ""},
		{"-v hides Debug", 1, func(p *Printer) { p.Debug("hidden") }, "", "hidden"},
		{"-vv shows Debug", 2, func(p *Printer) { p.Debug("shown") }, "shown", ""},
		{"-q hides Pass", -1, func(p *Printer) { p.Pass("hidden") }, "", "hidden"},
		{"-q hides Warn", -1, func(p *Printer) { p.Warn("hidden") }, "", "hidden"},
		{"-q hides Step", -1, func(p *Printer) { p.Step("hidden") }, "", "hidden"},
		{"-q still shows Fail", -1, func(p *Printer) { p.Fail("shown") }, "shown", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr, stdout bytes.Buffer
			p := New(&stderr, &stdout, tc.verbosity, false)
			tc.emit(p)
			got := stderr.String()
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("expected %q in stderr, got %q", tc.want, got)
			}
			if tc.notWant != "" && strings.Contains(got, tc.notWant) {
				t.Errorf("did not expect %q in stderr, got %q", tc.notWant, got)
			}
		})
	}
}

func TestPlainStdoutWriters(t *testing.T) {
	var stderr, stdout bytes.Buffer
	p := New(&stderr, &stdout, -1, true) // -q + color: should not affect stdout
	p.Println("github.com/x/p.TestA")
	p.Printlnf("github.com/x/p.%s", "TestB")
	got := stdout.String()
	want := "github.com/x/p.TestA\ngithub.com/x/p.TestB\n"
	if got != want {
		t.Errorf("stdout = %q; want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", stderr.String())
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("stdout must never contain ANSI escapes: %q", got)
	}
}

func TestQuietAndVerboseAreMutuallyExclusive(t *testing.T) {
	// Defensive sanity: cliout doesn't enforce, but the constructor must
	// accept any combination — enforcement lives in flag parsing.
	var stderr, stdout bytes.Buffer
	p := New(&stderr, &stdout, -1, false)
	p.Fail("must still print")
	if !strings.Contains(stderr.String(), "must still print") {
		t.Error("Fail must always print, even at -q")
	}
}

// Printf-style variants for when the message is built from args.
func TestPassfWarnfStepfFailfInfofDebugf(t *testing.T) {
	var stderr, stdout bytes.Buffer
	p := New(&stderr, &stdout, 2, false)
	p.Stepf("running %s", "build")
	p.Passf("%d passed", 42)
	p.Failf("%d failed", 7)
	p.Warnf("warn %s", "x")
	p.Infof("info %s", "y")
	p.Debugf("debug %s", "z")
	got := stderr.String()
	for _, want := range []string{"→ running build", "✓ 42 passed", "✗ 7 failed", "! warn x", "· info y", "· debug z"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in stderr:\n%s", want, got)
		}
	}
}

func TestShouldUseColor(t *testing.T) {
	cases := []struct {
		name      string
		isTTY     bool
		noColor   bool
		envNoCol  string
		envFCol   string
		want      bool
	}{
		{"tty no flags", true, false, "", "", true},
		{"not tty", false, false, "", "", false},
		{"--no-color overrides tty", true, true, "", "", false},
		{"NO_COLOR env overrides tty", true, false, "1", "", false},
		{"NO_COLOR empty does not disable", true, false, "", "", true},
		{"FORCE_COLOR overrides not-tty", false, false, "", "1", true},
		{"--no-color beats FORCE_COLOR", false, true, "", "1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldUseColor(tc.isTTY, tc.noColor, tc.envNoCol, tc.envFCol)
			if got != tc.want {
				t.Errorf("ShouldUseColor(tty=%v, no-color=%v, NO_COLOR=%q, FORCE_COLOR=%q) = %v; want %v",
					tc.isTTY, tc.noColor, tc.envNoCol, tc.envFCol, got, tc.want)
			}
		})
	}
}
