// Package cliout prints the chrome around defrost's commands —
// progress symbols, success/failure markers, warnings — with optional
// ANSI color and verbosity gating. Plain data (NDJSON, suppression
// lists) goes through Println / Printlnf and is never decorated.
package cliout

import (
	"fmt"
	"io"
)

// Printer renders decorated lines to stderr and plain lines to stdout.
// Construct one per process via New and pass it down to handlers.
type Printer struct {
	stderr    io.Writer
	stdout    io.Writer
	verbosity int  // 0 = default, 1 = -v, 2 = -vv. -q is represented as -1.
	color     bool // true if ANSI escapes should be emitted on stderr.
}

// New constructs a Printer. Caller decides verbosity and color based on
// flags / env / isatty — cliout does no detection itself so it stays
// trivially testable.
func New(stderr, stdout io.Writer, verbosity int, color bool) *Printer {
	return &Printer{stderr: stderr, stdout: stdout, verbosity: verbosity, color: color}
}

// Step prints a → cyan line. Default verbosity.
func (p *Printer) Step(msg string) { p.line("→", colorCyan, msg, 0) }

// Pass prints a ✓ green line. Default verbosity.
func (p *Printer) Pass(msg string) { p.line("✓", colorGreen, msg, 0) }

// Fail prints a ✗ red line. Always shown, even with -q.
func (p *Printer) Fail(msg string) { p.line("✗", colorRed, msg, -2) }

// Warn prints a ! yellow line. Default verbosity.
func (p *Printer) Warn(msg string) { p.line("!", colorYellow, msg, 0) }

const (
	colorReset  = "\x1b[0m"
	colorCyan   = "\x1b[36m"
	colorGreen  = "\x1b[32m"
	colorRed    = "\x1b[31m"
	colorYellow = "\x1b[33m"
	colorDim    = "\x1b[2m"
)

// line emits "<symbol> <msg>\n" to stderr, gated by minVerbosity.
// minVerbosity is the lowest verbosity level at which the line is shown.
// A line with minVerbosity = -2 is shown even when verbosity == -1 (-q).
func (p *Printer) line(symbol, color, msg string, minVerbosity int) {
	if p.verbosity < minVerbosity {
		return
	}
	if p.color {
		fmt.Fprintf(p.stderr, "%s%s%s %s\n", color, symbol, colorReset, msg)
	} else {
		fmt.Fprintf(p.stderr, "%s %s\n", symbol, msg)
	}
}
