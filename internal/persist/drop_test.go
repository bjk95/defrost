package persist

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func seedRun(t *testing.T, repoDir, runID, commit string) {
	t.Helper()
	run := newRunContext(runID, commit, "main")
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{
		NewRootSpan(run),
		makeTestSpan(run, "p/TestA", tracepb.Status_STATUS_CODE_OK),
	})
	v := 1.0
	metrics := []*metricspb.Metric{{
		Name: "eval.score",
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: uint64(run.StartTimeUnixNano) + 1,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: v},
			}},
		}},
	}}
	wrapped := WrapMetricsInResource(MetricResource(run), metrics)
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, wrapped); err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
}

func confirmYes(_ DropPlan) bool { return true }
func confirmNo(_ DropPlan) bool  { return false }

func TestDropHistory_BothSignals_RewritesBranchAndPreservesSuppressions(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)
	seedRun(t, repoDir, "run-1", "1111111111111111")
	seedRun(t, repoDir, "run-2", "2222222222222222")

	be := New(Options{RepoDir: repoDir})
	addX := func(cur []string) []string { return append(cur, "p/TestX") }
	if err := be.UpdateSuppressions(addX, "add X"); err != nil {
		t.Fatalf("seed suppressions: %v", err)
	}

	var planSeen DropPlan
	confirm := func(p DropPlan) bool { planSeen = p; return true }

	if err := be.DropHistory(DropSelector{DropTraces: true, DropMetrics: true}, confirm); err != nil {
		t.Fatalf("DropHistory: %v", err)
	}

	if planSeen.TraceFiles != 2 || planSeen.MetricFiles != 2 {
		t.Errorf("plan inventory: traces=%d metrics=%d, want 2/2", planSeen.TraceFiles, planSeen.MetricFiles)
	}
	if planSeen.SuppressionsN != 1 {
		t.Errorf("plan suppressions: %d want 1", planSeen.SuppressionsN)
	}

	verify := cloneDataBranch(t, originURL)
	if entries := listFilesWithSuffix(t, filepath.Join(verify, "traces"), fileSuffix); len(entries) != 0 {
		t.Errorf("traces should be gone, got %d files: %v", len(entries), entries)
	}
	if entries := listFilesWithSuffix(t, filepath.Join(verify, "metrics"), fileSuffix); len(entries) != 0 {
		t.Errorf("metrics should be gone, got %d files: %v", len(entries), entries)
	}
	suppBytes, err := os.ReadFile(filepath.Join(verify, "suppressions.json"))
	if err != nil {
		t.Fatalf("read suppressions after drop: %v", err)
	}
	if !strings.Contains(string(suppBytes), "p/TestX") {
		t.Errorf("suppressions not preserved across drop:\n%s", suppBytes)
	}
	if _, err := os.Stat(filepath.Join(verify, "README.md")); err != nil {
		t.Errorf("README missing after drop: %v", err)
	}

	// History was rewritten — exactly one commit on the data branch.
	out, err := exec.Command("git", "-C", verify, "rev-list", "--count", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-list: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "1" {
		t.Errorf("expected 1 commit on rewritten branch, got %s", got)
	}
}

func TestDropHistory_TracesOnly_KeepsMetrics(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)
	seedRun(t, repoDir, "run-1", "1111111111111111")

	be := New(Options{RepoDir: repoDir})
	if err := be.DropHistory(DropSelector{DropTraces: true}, confirmYes); err != nil {
		t.Fatalf("DropHistory traces-only: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	if entries := listFilesWithSuffix(t, filepath.Join(verify, "traces"), fileSuffix); len(entries) != 0 {
		t.Errorf("traces should be gone, got %v", entries)
	}
	if entries := listFilesWithSuffix(t, filepath.Join(verify, "metrics"), fileSuffix); len(entries) != 1 {
		t.Errorf("metrics should be preserved (1 file), got %d", len(entries))
	}
}

func TestDropHistory_MetricsOnly_KeepsTraces(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)
	seedRun(t, repoDir, "run-1", "1111111111111111")

	be := New(Options{RepoDir: repoDir})
	if err := be.DropHistory(DropSelector{DropMetrics: true}, confirmYes); err != nil {
		t.Fatalf("DropHistory metrics-only: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	if entries := listFilesWithSuffix(t, filepath.Join(verify, "metrics"), fileSuffix); len(entries) != 0 {
		t.Errorf("metrics should be gone, got %v", entries)
	}
	if entries := listFilesWithSuffix(t, filepath.Join(verify, "traces"), fileSuffix); len(entries) != 1 {
		t.Errorf("traces should be preserved (1 file), got %d", len(entries))
	}
}

func TestDropHistory_ConfirmFalse_NoChanges(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)
	seedRun(t, repoDir, "run-1", "1111111111111111")

	be := New(Options{RepoDir: repoDir})
	if err := be.DropHistory(DropSelector{DropTraces: true, DropMetrics: true}, confirmNo); err != nil {
		t.Fatalf("DropHistory (rejected): %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	if entries := listFilesWithSuffix(t, filepath.Join(verify, "traces"), fileSuffix); len(entries) != 1 {
		t.Errorf("rejected drop should not change traces, got %d files", len(entries))
	}
}

func TestDropHistory_NoBranch_CallsConfirmAndReturnsNil(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	var planSeen DropPlan
	confirm := func(p DropPlan) bool { planSeen = p; return true }

	if err := New(Options{RepoDir: repoDir}).DropHistory(
		DropSelector{DropTraces: true, DropMetrics: true}, confirm); err != nil {
		t.Fatalf("DropHistory on missing branch: %v", err)
	}
	if !planSeen.BranchMissing {
		t.Errorf("expected BranchMissing=true, got plan=%+v", planSeen)
	}
}

func TestDropHistory_NothingSelected_Errors(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)
	err := New(Options{RepoDir: repoDir}).DropHistory(DropSelector{}, confirmYes)
	if err == nil {
		t.Errorf("expected error when no selectors set, got nil")
	}
}

func TestDropHistory_LeaseAbortsOnConcurrentPush(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)
	seedRun(t, repoDir, "run-1", "1111111111111111")

	// Tee up: capture the cloned tip + working dir mid-flight by using
	// confirm to land a competing commit on origin between our clone and
	// our push. The lease must reject the push and the error must mention
	// it (so the user knows to retry, not that there's a generic git
	// failure).
	be := New(Options{RepoDir: repoDir})
	confirm := func(_ DropPlan) bool {
		// Race a fresh run onto origin while the dropper holds its workdir.
		seedRun(t, repoDir, "run-mid", "3333333333333333")
		return true
	}
	err := be.DropHistory(DropSelector{DropTraces: true, DropMetrics: true}, confirm)
	if err == nil {
		t.Fatal("expected lease to reject push when origin advanced mid-drop")
	}
	if !strings.Contains(err.Error(), "force-push") && !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected force-push/lease error, got %v", err)
	}

	// And the racing run must still be there — i.e. we did NOT silently
	// destroy it.
	verify := cloneDataBranch(t, originURL)
	traces := listFilesWithSuffix(t, filepath.Join(verify, "traces"), fileSuffix)
	if len(traces) != 2 {
		t.Errorf("racing run should be preserved on origin: want 2 trace files, got %d", len(traces))
	}
}

func TestDropHistory_DevMode_RemovesSignalDirs(t *testing.T) {
	requireGit(t)
	repoDir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	be := New(Options{RepoDir: repoDir, Dev: true})

	run := newRunContext("dev-run", "abc", "main")
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{NewRootSpan(run), makeTestSpan(run, "p/TestA", tracepb.Status_STATUS_CODE_OK)})
	if err := be.InsertNewRun(traces, nil); err != nil {
		t.Fatalf("seed dev run: %v", err)
	}
	if err := be.UpdateSuppressions(func(c []string) []string { return append(c, "S") }, "n/a"); err != nil {
		t.Fatalf("seed dev suppression: %v", err)
	}

	scratch := filepath.Join(repoDir, DevDir)
	if entries := listFilesWithSuffix(t, filepath.Join(scratch, "traces"), fileSuffix); len(entries) != 1 {
		t.Fatalf("precondition: want 1 dev trace, got %d", len(entries))
	}

	if err := be.DropHistory(DropSelector{DropTraces: true, DropMetrics: true}, confirmYes); err != nil {
		t.Fatalf("DropHistory dev: %v", err)
	}
	if _, err := os.Stat(filepath.Join(scratch, "traces")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected traces dir gone, stat err=%v", err)
	}
	// Suppressions preserved.
	got, err := be.GetSuppressions()
	if err != nil {
		t.Fatalf("read suppressions: %v", err)
	}
	if len(got) != 1 || got[0] != "S" {
		t.Errorf("suppressions not preserved: %v", got)
	}
}

func TestDropHistory_DevMode_NothingToDrop_CallsConfirm(t *testing.T) {
	requireGit(t)
	repoDir := t.TempDir()
	be := New(Options{RepoDir: repoDir, Dev: true})

	var planSeen DropPlan
	confirm := func(p DropPlan) bool { planSeen = p; return true }
	if err := be.DropHistory(DropSelector{DropTraces: true, DropMetrics: true}, confirm); err != nil {
		t.Fatalf("DropHistory: %v", err)
	}
	if !planSeen.Dev {
		t.Errorf("plan.Dev should be true")
	}
	if !planSeen.Nothing() {
		t.Errorf("plan should report Nothing()=true, got %+v", planSeen)
	}
}
