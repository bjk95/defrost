package persist

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

func TestEncodeTestID_RoundTrip(t *testing.T) {
	cases := []string{
		"github.com/bjk95/defrost/internal/x/TestFoo",
		"p/TestSubtest/with spaces",
		"p/TestPercent_already%encoded",
		"p/Test_no_special_chars",
		"p/TestUnicode_éà",
		"p/TestQuestion?_and_star*",
	}
	for _, name := range cases {
		id := EncodeTestID(name)
		if strings.ContainsAny(id, `/\`) {
			t.Errorf("encoded id contains path separator: %q (from %q)", id, name)
		}
		got, err := DecodeTestID(id)
		if err != nil {
			t.Errorf("decode %q: %v", id, err)
			continue
		}
		if got != name {
			t.Errorf("round-trip mismatch:\n want: %q\n got:  %q", name, got)
		}
	}
}

func TestNewRunID_TimePrefixSortable(t *testing.T) {
	a := NewRunID()
	time.Sleep(2 * time.Millisecond)
	b := NewRunID()
	if a >= b {
		t.Errorf("expected b > a after time gap; a=%q b=%q", a, b)
	}
}

func TestPersist_CreatesDataBranchOnFirstWrite(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	results := []models.TestResult{
		{
			Id:        "github.com/x/p/TestA",
			Ran:       true,
			Passed:    true,
			Duration:  5 * time.Millisecond,
			StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			Id:        "github.com/x/p/TestB",
			Ran:       true,
			Passed:    false,
			Duration:  12 * time.Millisecond,
			StartTime: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
			Output:    "FAIL\n",
		},
	}
	run := newTestRun("run-001", "abc123def4567890", "main")

	if err := New(Options{RepoDir: repoDir}).InsertNewTestResults(run, results); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	verify := cloneDataBranch(t, originURL)

	for _, r := range results {
		path := filepath.Join(verify, "tests", EncodeTestID(r.Id)+".ndjson")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("missing %s: %v", path, err)
			continue
		}
		if !strings.Contains(string(b), r.Id) {
			t.Errorf("file %s does not contain test name %q\ncontent: %s", path, r.Id, b)
		}
		if !strings.Contains(string(b), run.RunID) {
			t.Errorf("file %s does not contain run_id %q", path, run.RunID)
		}
	}

	runPath := filepath.Join(verify, "runs", run.RunID+".json")
	b, err := os.ReadFile(runPath)
	if err != nil {
		t.Fatalf("missing run record %s: %v", runPath, err)
	}
	for _, want := range []string{run.RunID, run.Commit, run.Branch} {
		if !strings.Contains(string(b), want) {
			t.Errorf("run record missing %q:\n%s", want, b)
		}
	}
	if _, err := os.Stat(filepath.Join(verify, ".gitattributes")); err != nil {
		t.Errorf("missing .gitattributes seed file: %v", err)
	}
}

func TestPersist_AppendsToExistingBranch(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	first := []models.TestResult{{
		Id:        "github.com/x/p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  5 * time.Millisecond,
		StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	run1 := newTestRun("run-A", "1111111111111111", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewTestResults(run1, first); err != nil {
		t.Fatalf("first Persist: %v", err)
	}

	second := []models.TestResult{{
		Id:        "github.com/x/p/TestA",
		Ran:       true,
		Passed:    false,
		Duration:  6 * time.Millisecond,
		StartTime: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}}
	run2 := newTestRun("run-B", "2222222222222222", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewTestResults(run2, second); err != nil {
		t.Fatalf("second Persist: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	path := filepath.Join(verify, "tests", EncodeTestID("github.com/x/p/TestA")+".ndjson")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after two persists, got %d:\n%s", len(lines), b)
	}
	if !strings.Contains(lines[0], run1.RunID) {
		t.Errorf("line 0 missing first run_id %q: %q", run1.RunID, lines[0])
	}
	if !strings.Contains(lines[1], run2.RunID) {
		t.Errorf("line 1 missing second run_id %q: %q", run2.RunID, lines[1])
	}

	for _, run := range []RunRecord{run1, run2} {
		if _, err := os.Stat(filepath.Join(verify, "runs", run.RunID+".json")); err != nil {
			t.Errorf("missing run record for %s: %v", run.RunID, err)
		}
	}
}

func TestHistory_RoundTripJoinsRunRecord(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	in := []models.TestResult{{
		Id:        "github.com/x/p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  5 * time.Millisecond,
		StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Output:    "PASS",
	}}
	run := newTestRun("run-history", "deadbeefcafebabe", "main")
	run.AuthorEmail = "alice@example.com"
	run.AuthorName = "Alice"
	run.Cmd = []string{"go", "test", "./..."}
	run.CmdHash = cmdHash(run.Cmd)
	run.Dirty = true
	run.DirtyHash = "abcd1234"

	if err := New(Options{RepoDir: repoDir}).InsertNewTestResults(run, in); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("github.com/x/p/TestA")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	h := got[0]

	if h.Test.TestName != in[0].Id {
		t.Errorf("test_name: want %q got %q", in[0].Id, h.Test.TestName)
	}
	if h.Test.RunID != run.RunID {
		t.Errorf("run_id: want %q got %q", run.RunID, h.Test.RunID)
	}
	if !h.Test.Passed || !h.Test.Ran {
		t.Errorf("ran/passed: want true/true got %v/%v", h.Test.Ran, h.Test.Passed)
	}
	if h.Test.Status != "pass" {
		t.Errorf("status: want pass got %q", h.Test.Status)
	}
	if h.Test.DurationMs != 5 {
		t.Errorf("duration_ms: want 5 got %d", h.Test.DurationMs)
	}
	if h.Test.Output != "PASS" {
		t.Errorf("output: want %q got %q", "PASS", h.Test.Output)
	}

	if h.Run.Commit != run.Commit {
		t.Errorf("run.commit: want %q got %q", run.Commit, h.Run.Commit)
	}
	if h.Run.AuthorEmail != "alice@example.com" {
		t.Errorf("run.author_email: want alice@example.com got %q", h.Run.AuthorEmail)
	}
	if !h.Run.Dirty || h.Run.DirtyHash != "abcd1234" {
		t.Errorf("run.dirty/dirty_hash: want true/abcd1234 got %v/%q", h.Run.Dirty, h.Run.DirtyHash)
	}
	if h.Run.CmdHash == "" {
		t.Errorf("run.cmd_hash: want non-empty")
	}
}

// TestPushWithRetry_RebasesOnConflict drives the rebase path manually:
// writer 1 stages a commit, writer 2 races ahead and pushes, then writer 1
// pushes — the retry loop must fetch, rebase under merge=union, and land
// without losing either side's appended line.
func TestPushWithRetry_RebasesOnConflict(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	seed := []models.TestResult{{
		Id:        "p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  1 * time.Millisecond,
		StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	if err := New(Options{RepoDir: repoDir}).InsertNewTestResults(newTestRun("run-seed", "1111111111111111", "main"), seed); err != nil {
		t.Fatalf("seed Persist: %v", err)
	}

	// Writer 1: clone data branch and stage a commit, but do not push yet.
	w1Dir := filepath.Join(t.TempDir(), "w1")
	branchExisted, err := openOrInitDataRepo(w1Dir, originURL, DefaultDataBranch)
	if err != nil {
		t.Fatalf("w1 openOrInit: %v", err)
	}
	if !branchExisted {
		t.Fatal("w1: expected branch to exist after seed")
	}
	w1Run := newTestRun("run-w1", "2222222222222222", "main")
	w1Results := []models.TestResult{{
		Id:        "p/TestA",
		Ran:       true,
		Passed:    false,
		Duration:  2 * time.Millisecond,
		StartTime: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}}
	if err := writeRunRecord(w1Dir, w1Run); err != nil {
		t.Fatalf("w1 writeRunRecord: %v", err)
	}
	if err := appendEntries(w1Dir, w1Run, w1Results); err != nil {
		t.Fatalf("w1 append: %v", err)
	}
	if err := commitAll(w1Dir, "writer 1"); err != nil {
		t.Fatalf("w1 commit: %v", err)
	}

	// Writer 2 (the racer) advances origin/data via a normal Persist.
	racer := []models.TestResult{{
		Id:        "p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  3 * time.Millisecond,
		StartTime: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
	}}
	if err := New(Options{RepoDir: repoDir}).InsertNewTestResults(newTestRun("run-racer", "3333333333333333", "main"), racer); err != nil {
		t.Fatalf("racer Persist: %v", err)
	}

	// Writer 1 pushes — first attempt is non-fast-forward, retry must rebase.
	if err := pushWithRetry(w1Dir, DefaultDataBranch, true); err != nil {
		t.Fatalf("pushWithRetry: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	path := filepath.Join(verify, "tests", EncodeTestID("p/TestA")+".ndjson")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final ndjson: %v", err)
	}
	final := string(b)
	for _, runID := range []string{"run-seed", "run-w1", "run-racer"} {
		if !strings.Contains(final, runID) {
			t.Errorf("expected run_id %s entry in final file:\n%s", runID, final)
		}
	}
	if got := strings.Count(strings.TrimRight(final, "\n"), "\n") + 1; got != 3 {
		t.Errorf("expected 3 lines after rebase, got %d:\n%s", got, final)
	}
}

func TestHistory_UnknownTestReturnsEmpty(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)
	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("github.com/x/p/NeverWritten")
	if err != nil {
		t.Fatalf("History on empty origin: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 entries, got %d", len(got))
	}
}

func TestReadOriginURL_WorksInWorktree(t *testing.T) {
	requireGit(t)
	base := t.TempDir()
	bareDir := filepath.Join(base, "origin.git")
	repoDir := filepath.Join(base, "main")
	wtDir := filepath.Join(base, "wt")

	gitMust(t, "", "init", "--bare", bareDir)
	gitMust(t, "", "init", "-b", "main", repoDir)
	gitMust(t, repoDir, "remote", "add", "origin", bareDir)
	gitMust(t, repoDir, "config", "user.email", "t@example.com")
	gitMust(t, repoDir, "config", "user.name", "t")
	gitMust(t, repoDir, "commit", "--allow-empty", "-m", "init")
	gitMust(t, repoDir, "worktree", "add", "-b", "feature", wtDir)

	url, err := readOriginURL(wtDir)
	if err != nil {
		t.Fatalf("readOriginURL in worktree: %v", err)
	}
	if url != bareDir {
		t.Errorf("expected URL %q, got %q", bareDir, url)
	}
}

func TestReadOriginURL_NoOriginReturnsErrNoOrigin(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", dir)
	_, err := readOriginURL(dir)
	if !errors.Is(err, ErrNoOrigin) {
		t.Errorf("expected ErrNoOrigin, got %v", err)
	}
}

func TestPersist_LocalOnlyNoRemote(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", "-b", "main", dir)
	gitMust(t, dir, "config", "user.email", "t@example.com")
	gitMust(t, dir, "config", "user.name", "t")
	gitMust(t, dir, "commit", "--allow-empty", "-m", "init")

	results := []models.TestResult{{
		Id:        "p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  1 * time.Millisecond,
		StartTime: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	}}
	run := newTestRun("local-run", "abc123", "main")
	if err := New(Options{RepoDir: dir, NoRemote: true}).InsertNewTestResults(run, results); err != nil {
		t.Fatalf("Persist (no-remote): %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "show", DefaultDataBranch+":tests/"+EncodeTestID("p/TestA")+".ndjson").CombinedOutput()
	if err != nil {
		t.Fatalf("read tests file from data branch: %v: %s", err, out)
	}
	if !strings.Contains(string(out), run.RunID) {
		t.Errorf("test ndjson missing run_id %q:\n%s", run.RunID, out)
	}

	hist, err := New(Options{RepoDir: dir, NoRemote: true}).GetTestHistory("p/TestA")
	if err != nil {
		t.Fatalf("History (no-remote): %v", err)
	}
	if len(hist) != 1 || hist[0].Test.RunID != run.RunID {
		t.Fatalf("unexpected history: %+v", hist)
	}
}

// TestPersist_LocalOnlyNoRemote_RelativeRepoDir guards the localGitDir
// regression: when RepoDir was passed as a relative path (e.g. "."), the
// resolved .git path stayed relative and the push from the ephemeral
// workdir silently no-op'd against the wrong cwd.
func TestPersist_LocalOnlyNoRemote_RelativeRepoDir(t *testing.T) {
	requireGit(t)
	parent := t.TempDir()
	gitMust(t, "", "init", "-b", "main", filepath.Join(parent, "repo"))
	gitMust(t, filepath.Join(parent, "repo"), "config", "user.email", "t@example.com")
	gitMust(t, filepath.Join(parent, "repo"), "config", "user.name", "t")
	gitMust(t, filepath.Join(parent, "repo"), "commit", "--allow-empty", "-m", "init")

	t.Chdir(parent)

	results := []models.TestResult{{
		Id:        "p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  1 * time.Millisecond,
		StartTime: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	}}
	run := newTestRun("rel-run", "abc123", "main")
	if err := New(Options{RepoDir: "repo", NoRemote: true}).InsertNewTestResults(run, results); err != nil {
		t.Fatalf("Persist (no-remote, relative): %v", err)
	}

	out, err := exec.Command("git", "-C", "repo", "show", DefaultDataBranch+":tests/"+EncodeTestID("p/TestA")+".ndjson").CombinedOutput()
	if err != nil {
		t.Fatalf("read tests file from data branch: %v: %s", err, out)
	}
	if !strings.Contains(string(out), run.RunID) {
		t.Errorf("test ndjson missing run_id %q:\n%s", run.RunID, out)
	}
}

func TestPersist_DevModeWritesScratchDirAndSkipsGit(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", "-b", "main", dir)

	results := []models.TestResult{{
		Id:        "p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  1 * time.Millisecond,
		StartTime: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	}}
	run := newTestRun("dev-run", "abc123", "main")
	if err := New(Options{RepoDir: dir, Dev: true}).InsertNewTestResults(run, results); err != nil {
		t.Fatalf("Persist (dev): %v", err)
	}

	scratch := filepath.Join(dir, DevDir)
	runPath := filepath.Join(scratch, "runs", run.RunID+".json")
	if _, err := os.Stat(runPath); err != nil {
		t.Errorf("run record not written to scratch dir: %v", err)
	}
	entryPath := filepath.Join(scratch, "tests", EncodeTestID("p/TestA")+".ndjson")
	b, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatalf("entry file not written: %v", err)
	}
	if !strings.Contains(string(b), run.RunID) {
		t.Errorf("entry file missing run_id %q:\n%s", run.RunID, b)
	}

	// No data branch ref should exist — git path was skipped.
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", DefaultDataBranch).CombinedOutput(); err == nil {
		t.Errorf("expected no %s branch, but rev-parse succeeded: %s", DefaultDataBranch, out)
	}
}

func TestPersist_RequiresOriginByDefault(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", dir)
	results := []models.TestResult{{Id: "p/TestA", Ran: true, Passed: true}}
	run := newTestRun("orphan-run", "abc", "main")
	err := New(Options{RepoDir: dir}).InsertNewTestResults(run, results)
	if !errors.Is(err, ErrNoOrigin) {
		t.Errorf("expected ErrNoOrigin, got %v", err)
	}
}

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		r    models.TestResult
		want string
	}{
		{models.TestResult{Ran: false}, "skip"},
		{models.TestResult{Ran: true, Passed: true}, "pass"},
		{models.TestResult{Ran: true, Passed: false}, "fail"},
		{models.TestResult{Ran: true, Passed: false, Output: "panic: nil deref\n"}, "panic"},
	}
	for _, tc := range cases {
		if got := deriveStatus(tc.r); got != tc.want {
			t.Errorf("deriveStatus(%+v) = %q, want %q", tc.r, got, tc.want)
		}
	}
}

// --- test helpers ---

func newTestRun(runID, commit, branch string) RunRecord {
	return RunRecord{
		Schema:    SchemaVersion,
		RunID:     runID,
		Commit:    commit,
		Branch:    branch,
		Timestamp: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not available")
	}
}

func gitMust(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %q: %v: %s", args, dir, err, out)
	}
}

func makeFixture(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	bareDir := filepath.Join(base, "origin.git")
	gitMust(t, "", "init", "--bare", bareDir)

	repoDir := filepath.Join(base, "repo")
	gitMust(t, "", "init", repoDir)
	gitMust(t, repoDir, "remote", "add", "origin", bareDir)
	return repoDir, bareDir
}

func cloneDataBranch(t *testing.T, originURL string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "verify")
	gitMust(t, "", "clone", "--quiet", "--single-branch", "--branch", DefaultDataBranch, originURL, dir)
	return dir
}

func TestLoadAll_ReturnsAllRunsAndEntriesGroupedByTest(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	resultsRun1 := []models.TestResult{
		{Id: "github.com/x/p/TestA", Ran: true, Passed: true, Duration: 5 * time.Millisecond, StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Id: "github.com/x/p/TestB", Ran: true, Passed: false, Duration: 9 * time.Millisecond, StartTime: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), Output: "fail one"},
	}
	run1 := newTestRun("run-1", "1111111111111111", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewTestResults(run1, resultsRun1); err != nil {
		t.Fatalf("persist run1: %v", err)
	}

	resultsRun2 := []models.TestResult{
		{Id: "github.com/x/p/TestA", Ran: true, Passed: true, Duration: 4 * time.Millisecond, StartTime: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	run2 := newTestRun("run-2", "2222222222222222", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewTestResults(run2, resultsRun2); err != nil {
		t.Fatalf("persist run2: %v", err)
	}

	runs, byTest, err := New(Options{RepoDir: repoDir}).LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	gotRunIDs := map[string]bool{}
	for _, r := range runs {
		gotRunIDs[r.RunID] = true
	}
	if !gotRunIDs["run-1"] || !gotRunIDs["run-2"] {
		t.Errorf("missing run IDs in %v", gotRunIDs)
	}

	idA := EncodeTestID("github.com/x/p/TestA")
	idB := EncodeTestID("github.com/x/p/TestB")
	if len(byTest[idA]) != 2 {
		t.Errorf("TestA: want 2 entries, got %d", len(byTest[idA]))
	}
	if len(byTest[idB]) != 1 {
		t.Errorf("TestB: want 1 entry, got %d", len(byTest[idB]))
	}
}

func TestLoadAll_NoBranch_ReturnsEmpty(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	runs, byTest, err := New(Options{RepoDir: repoDir}).LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(runs))
	}
	if len(byTest) != 0 {
		t.Errorf("expected 0 test groups, got %d", len(byTest))
	}
}
