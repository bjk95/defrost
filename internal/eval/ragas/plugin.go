package ragas

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// helperSource is the Python helper that user tests import as
// `defrost_ragas`. Embedded so a single defrost binary ships with everything
// it needs; the alternative (a fixed install path) breaks under `go run`,
// `go install`, and Docker images.
//
//go:embed defrost_ragas.py
var helperSource []byte

// Plugin is the RAGAS plugin attached to pytest invocations. It satisfies
// the plugin shape sketched in docs/specs/2026-04-30-ragas-adapter.md §8 and
// the DeepEval spec's Plugin interface (Prepare → exec → Teardown).
//
// Prepare is responsible for:
//
//   - creating an empty tempfile that defrost_ragas.write_results writes
//     to (DEFROST_RAGAS_OUT);
//   - extracting the embedded helper to a tempdir on PYTHONPATH so the
//     child Python process can `import defrost_ragas`.
//
// Teardown reads the tempfile if non-empty, parses it via Parse, and
// removes both temp paths. Tolerant: an empty or absent tempfile means the
// user didn't call write_results (or RAGAS isn't installed) — return nil
// metrics rather than erroring.
type Plugin struct {
	tempFile  string // path the child writes JSON results to
	helperDir string // dir holding defrost_ragas.py, prepended to PYTHONPATH
}

// Prepare creates the tempfile + helper dir and returns a copy of env with
// DEFROST_RAGAS_OUT and PYTHONPATH set.
//
// Existing entries for either key in env are dropped; defrost owns these
// for the duration of the run, and a duplicate entry would let the child's
// Python pick the wrong value depending on platform iteration order.
//
// Returns the original env unmodified on error so callers can decide
// whether to abort or continue without the plugin.
func (p *Plugin) Prepare(env []string) ([]string, error) {
	f, err := os.CreateTemp("", "defrost-ragas-*.json")
	if err != nil {
		return env, fmt.Errorf("ragas: create tempfile: %w", err)
	}
	tempPath := f.Name()
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(tempPath)
		return env, fmt.Errorf("ragas: close tempfile: %w", cerr)
	}

	dir, err := os.MkdirTemp("", "defrost-ragas-helper-*")
	if err != nil {
		_ = os.Remove(tempPath)
		return env, fmt.Errorf("ragas: create helper dir: %w", err)
	}
	helperPath := filepath.Join(dir, "defrost_ragas.py")
	if err := os.WriteFile(helperPath, helperSource, 0o644); err != nil {
		_ = os.Remove(tempPath)
		_ = os.RemoveAll(dir)
		return env, fmt.Errorf("ragas: write helper: %w", err)
	}

	p.tempFile = tempPath
	p.helperDir = dir

	out := make([]string, 0, len(env)+2)
	var existingPyPath string
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "DEFROST_RAGAS_OUT="):
			// Drop — defrost owns this for the run.
		case strings.HasPrefix(e, "PYTHONPATH="):
			existingPyPath = strings.TrimPrefix(e, "PYTHONPATH=")
		default:
			out = append(out, e)
		}
	}

	pyPath := dir
	if existingPyPath != "" {
		pyPath = dir + string(os.PathListSeparator) + existingPyPath
	}
	out = append(out, "PYTHONPATH="+pyPath)
	out = append(out, "DEFROST_RAGAS_OUT="+tempPath)
	return out, nil
}

// Teardown reads the tempfile (if present and non-empty), parses it, and
// returns the resulting metrics. Always cleans up the tempfile and helper
// dir, even on parse error, so a long-running process doesn't leak temps
// across many invocations.
//
// exitCode is accepted for interface compatibility with the broader pytest
// plugin shape — the RAGAS plugin doesn't make pass/fail decisions, so it
// is unused.
func (p *Plugin) Teardown(exitCode int) ([]*metricspb.Metric, error) {
	if p.tempFile == "" {
		return nil, nil
	}
	defer func() { _ = p.cleanup() }()

	info, err := os.Stat(p.tempFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// User didn't call defrost_ragas.write_results, or RAGAS
			// isn't installed. Both are normal.
			return nil, nil
		}
		return nil, fmt.Errorf("ragas: stat tempfile: %w", err)
	}
	if info.Size() == 0 {
		return nil, nil
	}

	f, err := os.Open(p.tempFile)
	if err != nil {
		return nil, fmt.Errorf("ragas: open tempfile: %w", err)
	}
	defer f.Close()

	metrics, err := Parse(f)
	if err != nil {
		return nil, err
	}
	return metrics, nil
}

// cleanup removes the tempfile + helper dir created in Prepare. Called from
// Teardown, and from tests via t.Cleanup when Teardown is skipped.
func (p *Plugin) cleanup() error {
	var firstErr error
	if p.tempFile != "" {
		if err := os.Remove(p.tempFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			firstErr = err
		}
		p.tempFile = ""
	}
	if p.helperDir != "" {
		if err := os.RemoveAll(p.helperDir); err != nil && firstErr == nil {
			firstErr = err
		}
		p.helperDir = ""
	}
	return firstErr
}
