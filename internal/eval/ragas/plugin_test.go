package ragas

import (
	"os"
	"path/filepath"
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

func TestPluginPrepareInjectsEnvVars(t *testing.T) {
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

	pp := envValue(env, "PYTHONPATH")
	if pp == "" {
		t.Fatalf("PYTHONPATH not set in env")
	}
	// The PYTHONPATH entry must point at a directory containing the helper.
	first := strings.Split(pp, string(os.PathListSeparator))[0]
	helper := filepath.Join(first, "defrost_ragas.py")
	if _, err := os.Stat(helper); err != nil {
		t.Fatalf("defrost_ragas.py not found in PYTHONPATH dir %q: %v", first, err)
	}
}

func TestPluginPrepareAppendsToExistingPythonPath(t *testing.T) {
	// On Windows the path list separator is `;` but defrost runs on
	// Linux/macOS in CI. Skip the assertion only if the platform separator
	// would be ambiguous against the existing value.
	existing := "/some/existing/path" + string(os.PathListSeparator) + "/another"
	in := []string{"PYTHONPATH=" + existing, "OTHER=value"}

	p := &Plugin{}
	env, err := p.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = p.cleanup() })

	pp := envValue(env, "PYTHONPATH")
	if !strings.Contains(pp, existing) {
		t.Fatalf("Prepare clobbered existing PYTHONPATH; got %q, want it to contain %q", pp, existing)
	}
	// The defrost helper dir must be prepended so it wins import resolution
	// over a same-named module the user might have on their path.
	if !strings.HasPrefix(pp, filepath.Dir(filepath.Join(p.helperDir, "defrost_ragas.py"))) {
		t.Fatalf("expected helper dir at front of PYTHONPATH; got %q", pp)
	}

	// Unrelated env vars must pass through untouched.
	if got := envValue(env, "OTHER"); got != "value" {
		t.Fatalf("OTHER lost from env, got %q", got)
	}
}

func TestPluginPrepareEmitsExactlyOneEntryPerKey(t *testing.T) {
	// Calling Prepare must not duplicate PYTHONPATH or DEFROST_RAGAS_OUT
	// entries when the input env already carries them — pytest's child
	// inherits the merged env literally and duplicates would let the older
	// (stale) entry win on some Python versions.
	in := []string{
		"PYTHONPATH=/a",
		"DEFROST_RAGAS_OUT=/stale/path",
	}
	p := &Plugin{}
	env, err := p.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = p.cleanup() })

	count := func(prefix string) int {
		n := 0
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				n++
			}
		}
		return n
	}
	if got := count("PYTHONPATH="); got != 1 {
		t.Fatalf("expected exactly 1 PYTHONPATH entry, got %d (env=%v)", got, env)
	}
	if got := count("DEFROST_RAGAS_OUT="); got != 1 {
		t.Fatalf("expected exactly 1 DEFROST_RAGAS_OUT entry, got %d (env=%v)", got, env)
	}
	if got := envValue(env, "DEFROST_RAGAS_OUT"); got == "/stale/path" {
		t.Fatalf("DEFROST_RAGAS_OUT not overridden; got %q", got)
	}
}

func TestPluginTeardownEmptyFileReturnsNil(t *testing.T) {
	// The tempfile is created empty by Prepare. If the user's tests never
	// call defrost_ragas.write_results (or RAGAS isn't installed), the
	// tempfile stays empty and Teardown must return nil/nil — not an error
	// — so the pytest runner can ignore the plugin silently.
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
	helperDir := p.helperDir
	if _, err := p.Teardown(0); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("expected tempfile to be removed; stat err = %v", err)
	}
	if _, err := os.Stat(helperDir); !os.IsNotExist(err) {
		t.Fatalf("expected helper dir to be removed; stat err = %v", err)
	}
}

func TestPluginHelperFileWritten(t *testing.T) {
	// The embedded defrost_ragas.py must end up readable at the path we
	// inject into PYTHONPATH. A regression here would break user tests
	// silently — they'd see ImportError at runtime.
	p := &Plugin{}
	if _, err := p.Prepare(nil); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = p.cleanup() })

	helper := filepath.Join(p.helperDir, "defrost_ragas.py")
	data, err := os.ReadFile(helper)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}
	if !strings.Contains(string(data), "def write_results") {
		t.Fatalf("helper missing write_results function; got %d bytes", len(data))
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
