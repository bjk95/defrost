package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// stubAdapter lets us drive HandleExecution with a known set of results
// and a known exit code without invoking go test / pytest / jest.
type stubAdapter struct {
	results []models.TestResult
	metrics []*metricspb.Metric
	code    int
}

func (s stubAdapter) Matches(cmd []string) bool { return cmd[0] == "stub" }
func (s stubAdapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	return s.results, s.metrics, s.code
}

func makeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-q", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func writeDevSuppressions(t *testing.T, repoDir string, ids []string) {
	t.Helper()
	be := persist.New(persist.Options{RepoDir: repoDir, Dev: true})
	if err := be.UpdateSuppressions(func([]string) []string { return ids }, ""); err != nil {
		t.Fatal(err)
	}
}

func runExecWithAdapter(t *testing.T, a stubAdapter, repoDir string) int {
	t.Helper()
	return execWith(a, []string{"stub", "..."}, ExecOpts{
		RepoDir: repoDir,
		Persist: false,
		Dev:     true,
	})
}

func TestExec_SuppressedSingleFailure_ExitsZero(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA"})

	stub := stubAdapter{
		results: []models.TestResult{{Id: "pkg/TestA", Ran: true, Passed: false}},
		code:    1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 0 {
		t.Errorf("want exit 0, got %d", got)
	}
}

func TestExec_AllFailuresSuppressed_ExitsZero(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA", "pkg/TestB"})

	stub := stubAdapter{
		results: []models.TestResult{
			{Id: "pkg/TestA", Ran: true, Passed: false},
			{Id: "pkg/TestB", Ran: true, Passed: false},
		},
		code: 1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 0 {
		t.Errorf("want exit 0, got %d", got)
	}
}

func TestExec_PartialSuppression_ExitUnchanged(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA"})

	stub := stubAdapter{
		results: []models.TestResult{
			{Id: "pkg/TestA", Ran: true, Passed: false},
			{Id: "pkg/TestB", Ran: true, Passed: false},
		},
		code: 1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("want exit 1, got %d", got)
	}
}

func TestExec_NoFailures_ExitUnchanged(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA"})

	stub := stubAdapter{
		results: []models.TestResult{{Id: "pkg/TestA", Ran: true, Passed: true}},
		code:    0,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 0 {
		t.Errorf("want exit 0, got %d", got)
	}
}

func TestExec_SuppressionsReadError_ExitUnchanged(t *testing.T) {
	repoDir := makeRepo(t)
	// Plant a malformed suppressions.json in the dev scratch dir. The
	// fileBackend should error on read; exec should log and preserve the
	// original exit code rather than guessing the build is green.
	dir := filepath.Join(repoDir, persist.DevDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "suppressions.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	stub := stubAdapter{
		results: []models.TestResult{{Id: "pkg/TestA", Ran: true, Passed: false}},
		code:    1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("read error must preserve exit code: want 1, got %d", got)
	}
}

func TestExec_PersistFailure_AllSuppressed_NotRewritten(t *testing.T) {
	// RepoDir is a non-git tempdir → DetectRun fails → persistResults
	// errors. Even though every failing test is suppressed, exec must
	// preserve the non-zero exit so the persist failure doesn't get
	// silently swallowed alongside an apparent green build.
	repoDir := t.TempDir()
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA"})

	stub := stubAdapter{
		results: []models.TestResult{{Id: "pkg/TestA", Ran: true, Passed: false}},
		code:    1,
	}
	got := execWith(stub, []string{"stub", "..."}, ExecOpts{
		RepoDir: repoDir,
		Persist: true,
		Dev:     true,
	})
	if got != 1 {
		t.Errorf("persist failure must not let suppression rewrite exit: want 1, got %d", got)
	}
}

func TestExec_NoResultsNonZeroCode_NotSuppressed(t *testing.T) {
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"anything"})

	stub := stubAdapter{
		results: nil,
		code:    1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("no results + non-zero code must preserve exit: want 1, got %d", got)
	}
}

func TestExec_FileError_NotSuppressedEvenIfRanTrue(t *testing.T) {
	// Adapters synthesise file-level failures with `Ran: true` and the
	// FileErrorSuffix in the ID (jest's "could not load file" et al.).
	// These represent infrastructure failures, not test failures, and
	// must never be suppressible — even when the synthetic ID happens
	// to be on the suppression list.
	repoDir := makeRepo(t)
	syntheticID := "src/foo.test.ts" + models.FileErrorSuffix
	writeDevSuppressions(t, repoDir, []string{syntheticID})

	stub := stubAdapter{
		results: []models.TestResult{{Id: syntheticID, Ran: true, Passed: false}},
		code:    1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("file-level error must keep non-zero exit: want 1, got %d", got)
	}
}

func TestExec_FileErrorAlongsideSuppressedTest_NotRewritten(t *testing.T) {
	// Even when every "real" failing test is suppressed, the presence of
	// any file-level error must keep the exit non-zero. Suppressing the
	// real test failures should not mask infrastructure problems.
	repoDir := makeRepo(t)
	writeDevSuppressions(t, repoDir, []string{"pkg/TestA"})

	stub := stubAdapter{
		results: []models.TestResult{
			{Id: "pkg/TestA", Ran: true, Passed: false},
			{Id: "src/foo.test.ts" + models.FileErrorSuffix, Ran: true, Passed: false},
		},
		code: 1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("file-error alongside suppressed test must preserve exit: want 1, got %d", got)
	}
}

// TestHandleExecution_UnknownCommand_FallsThroughToPassthrough confirms that
// `defrost exec <anything>` runs the user's command via the passthrough
// adapter rather than refusing with exit 2 when no framework adapter
// matches. Regression-protects the "wrap any run command" guarantee.
func TestHandleExecution_UnknownCommand_FallsThroughToPassthrough(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	// Persist off so we don't need a git repo. The command exits 9 — the
	// passthrough adapter must propagate that, not synthesise its own
	// "unknown command" exit 2.
	got := HandleExecution([]string{"sh", "-c", "exit 9"}, ExecOpts{Persist: false})
	if got != 9 {
		t.Errorf("want exit 9 from passthrough, got %d", got)
	}
}

func TestExec_BuildOnlyFailure_NotSuppressed(t *testing.T) {
	repoDir := makeRepo(t)
	// "buildErr" is the only ID present; suppress it. We must NOT rewrite
	// the exit code because the failure isn't a test-level failure
	// (Ran == false → the test never executed).
	writeDevSuppressions(t, repoDir, []string{"buildErr"})

	stub := stubAdapter{
		results: []models.TestResult{{Id: "buildErr", Ran: false, Passed: false}},
		code:    1,
	}
	got := runExecWithAdapter(t, stub, repoDir)
	if got != 1 {
		t.Errorf("build failure should not be suppressible: want exit 1, got %d", got)
	}
}

func TestExecPlumbsAdapterMetricsToPersistence(t *testing.T) {
	repo := makeRepo(t)

	results := []models.TestResult{{Id: "x.y.Z", Ran: true, Passed: true, Duration: time.Millisecond}}
	score := 0.87
	now := uint64(time.Now().UnixNano())
	metrics := []*metricspb.Metric{{
		Name: "eval.faithfulness",
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: now,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: score},
				Attributes: []*commonpb.KeyValue{
					models.StringAttr("test.case.name", "x.y.Z"),
					models.StringAttr("gen_ai.evaluation.name", "faithfulness"),
				},
			}},
		}},
	}}
	a := stubAdapter{results: results, metrics: metrics, code: 0}

	code := execWith(a, []string{"stub"}, ExecOpts{
		RepoDir:  repo,
		Persist:  true,
		NoRemote: true,
		Dev:      true,
	})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	// The persist package has no GetMetricHistory API; assert the round-trip
	// by verifying the metric was written to the dev scratch directory.
	// fileBackend writes one NDJSON file per metric name under metrics/.
	metricFile := filepath.Join(repo, persist.DevDir, "metrics",
		persist.EncodeName("eval.faithfulness")+".ndjson")
	if _, err := os.Stat(metricFile); err != nil {
		t.Fatalf("expected persisted metric file %s, got: %v", metricFile, err)
	}
}
