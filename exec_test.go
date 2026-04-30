package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// stubAdapter lets us drive HandleExecution with a known set of results
// and a known exit code without invoking go test / pytest / jest.
type stubAdapter struct {
	results []models.TestResult
	code    int
}

func (s stubAdapter) Matches(cmd []string) bool { return cmd[0] == "stub" }
func (s stubAdapter) Run(cmd []string) ([]models.TestResult, int) {
	return s.results, s.code
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
