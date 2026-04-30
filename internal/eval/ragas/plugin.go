package ragas

import (
	"errors"
	"fmt"
	"os"
	"strings"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// Plugin is the RAGAS plugin attached to pytest invocations. It satisfies
// the plugin shape sketched in docs/specs/2026-04-30-ragas-adapter.md §8 and
// the DeepEval spec's Plugin interface (Prepare → exec → Teardown).
//
// Defrost ships no Python helper module. Users emit results by writing one
// line of stdlib code in their test:
//
//	if path := os.environ.get("DEFROST_RAGAS_OUT"):
//	    result.to_pandas().to_json(path, orient="records")
//
// This stays a no-op when defrost isn't wrapping the run, so test files
// remain portable. Prepare creates the empty tempfile and exposes its path
// via DEFROST_RAGAS_OUT; Teardown reads + parses the file (if non-empty)
// and cleans up. Tolerant: an empty or absent tempfile means the user
// didn't write the snippet, didn't reach evaluate(), or RAGAS isn't
// installed — return nil metrics rather than erroring.
type Plugin struct {
	tempFile string
}

// Prepare creates the tempfile and returns env with DEFROST_RAGAS_OUT set
// to its path. Existing DEFROST_RAGAS_OUT entries in env are dropped —
// defrost owns this for the duration of the run, and a duplicate entry
// would let the child's Python pick the wrong value depending on platform
// iteration order.
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
	p.tempFile = tempPath

	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "DEFROST_RAGAS_OUT=") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "DEFROST_RAGAS_OUT="+tempPath)
	return out, nil
}

// Teardown reads the tempfile (if present and non-empty), parses it, and
// returns the resulting metrics. Always cleans up the tempfile, even on
// parse error, so a long-running process doesn't leak temps across many
// invocations.
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

// cleanup removes the tempfile created in Prepare. Called from Teardown,
// and from tests via t.Cleanup when Teardown is skipped.
func (p *Plugin) cleanup() error {
	if p.tempFile == "" {
		return nil
	}
	err := os.Remove(p.tempFile)
	p.tempFile = ""
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
