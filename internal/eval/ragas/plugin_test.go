package ragas

import (
	"os"
	"strings"
	"testing"
)

// envValue returns the first value of key in env, or "" if absent.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
}

func TestPluginPrepareInjectsTempfile(t *testing.T) {
	p := &Plugin{}
	env, err := p.Prepare(nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = p.cleanup() })

	out := envValue(env, "DEFROST_RAGAS_OUT")
	if out == "" {
		t.Fatalf("DEFROST_RAGAS_OUT not set in env")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("DEFROST_RAGAS_OUT path %q does not exist: %v", out, err)
	}
}

func TestPluginPreparePreservesUnrelatedEnv(t *testing.T) {
	in := []string{"PYTHONPATH=/some/path", "OTHER=value"}

	p := &Plugin{}
	env, err := p.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = p.cleanup() })

	if got := envValue(env, "PYTHONPATH"); got != "/some/path" {
		t.Fatalf("PYTHONPATH lost from env, got %q", got)
	}
	if got := envValue(env, "OTHER"); got != "value" {
		t.Fatalf("OTHER lost from env, got %q", got)
	}
}

func TestPluginPrepareOverridesStaleOutVar(t *testing.T) {
	// A pre-existing DEFROST_RAGAS_OUT entry must be dropped — defrost
	// owns this for the run, and a duplicate would let the child's
	// Python pick the wrong value depending on platform iteration order.
	in := []string{"DEFROST_RAGAS_OUT=/stale/path"}
	p := &Plugin{}
	env, err := p.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = p.cleanup() })

	count := 0
	for _, e := range env {
		if strings.HasPrefix(e, "DEFROST_RAGAS_OUT=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 DEFROST_RAGAS_OUT entry, got %d (env=%v)", count, env)
	}
	if got := envValue(env, "DEFROST_RAGAS_OUT"); got == "/stale/path" {
		t.Fatalf("DEFROST_RAGAS_OUT not overridden; got %q", got)
	}
}

func TestPluginTeardownEmptyFileReturnsNil(t *testing.T) {
	// The tempfile is created empty by Prepare. If the user's tests never
	// dump results (or RAGAS isn't installed), the tempfile stays empty
	// and Teardown must return nil/nil — not an error — so the pytest
	// runner can ignore the plugin silently.
	p := &Plugin{}
	if _, err := p.Prepare(nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	metrics, err := p.Teardown(0)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if metrics != nil {
		t.Fatalf("expected nil metrics on empty tempfile, got %d", len(metrics))
	}
}

func TestPluginTeardownAbsentFileReturnsNil(t *testing.T) {
	// If something deletes the tempfile between Prepare and Teardown, we
	// must not surface a stat error — the contract is "tolerant teardown".
	p := &Plugin{}
	if _, err := p.Prepare(nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := os.Remove(p.tempFile); err != nil {
		t.Fatalf("rm tempfile: %v", err)
	}
	metrics, err := p.Teardown(0)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if metrics != nil {
		t.Fatalf("expected nil metrics on absent tempfile, got %d", len(metrics))
	}
}

func TestPluginTeardownParsesValidJSON(t *testing.T) {
	p := &Plugin{}
	if _, err := p.Prepare(nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := os.WriteFile(p.tempFile, loadFixture(t, "single_row.json"), 0o644); err != nil {
		t.Fatalf("write tempfile: %v", err)
	}
	metrics, err := p.Teardown(0)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Name != "eval.faithfulness" {
		t.Fatalf("metric name = %q, want eval.faithfulness", metrics[0].Name)
	}
}

func TestPluginTeardownCleansUpTempfile(t *testing.T) {
	p := &Plugin{}
	if _, err := p.Prepare(nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	temp := p.tempFile
	if _, err := p.Teardown(0); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("expected tempfile to be removed; stat err = %v", err)
	}
}

func TestPluginTeardownWithoutPrepareNoOps(t *testing.T) {
	// Defensive: a runner that calls Teardown without a successful Prepare
	// (e.g. plugin attached after exec started) must not panic — return
	// nil/nil.
	p := &Plugin{}
	metrics, err := p.Teardown(1)
	if err != nil {
		t.Fatalf("Teardown without Prepare: %v", err)
	}
	if metrics != nil {
		t.Fatalf("expected nil metrics, got %d", len(metrics))
	}
}
