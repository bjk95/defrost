package persist

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjk95/defrost/internal/models"
)

func TestEncodeName_RoundTrip(t *testing.T) {
	cases := []string{
		"github.com/bjk95/defrost/internal/x/TestFoo",
		"p/TestSubtest/with spaces",
		"p/TestPercent_already%encoded",
		"db.query.duration",
		"http.server.request.duration",
		"defrost.run",
	}
	for _, name := range cases {
		id := EncodeName(name)
		if strings.ContainsAny(id, `/\`) {
			t.Errorf("encoded id contains path separator: %q (from %q)", id, name)
		}
		got, err := DecodeName(id)
		if err != nil {
			t.Errorf("decode %q: %v", id, err)
			continue
		}
		if got != name {
			t.Errorf("round-trip mismatch:\n want: %q\n got:  %q", name, got)
		}
	}
}

func TestPersist_WritesTracesAndMetricsOnFirstWrite(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	run := newRunContext("run-001", "abc123def4567890", "main")
	root := NewRootSpan(run)
	root.EndTimeUnixNano = root.StartTimeUnixNano + 100
	root.Status = models.SpanStatus{Code: "OK"}

	testSpans := []models.Span{
		{
			Schema:            models.SchemaV3,
			TraceID:           run.TraceID,
			SpanID:            "aaaaaaaaaaaaaaaa",
			ParentSpanID:      run.RootSpanID,
			Name:              "github.com/x/p/TestA",
			StartTimeUnixNano: run.StartTimeUnixNano + 10,
			EndTimeUnixNano:   run.StartTimeUnixNano + 50,
			Status:            models.SpanStatus{Code: "OK"},
			Resource:          run.Resource,
			Attributes:        map[string]any{"test.case.name": "github.com/x/p/TestA"},
		},
	}
	v := 12.0
	metrics := []models.MetricEntry{
		{
			Schema:         models.SchemaV3,
			Name:           "db.connection_pool.size",
			InstrumentType: "gauge",
			TimeUnixNano:   run.StartTimeUnixNano + 30,
			Resource:       run.Resource,
			TraceID:        run.TraceID,
			Value:          &v,
		},
	}

	if err := New(Options{RepoDir: repoDir}).InsertNewRun(root, testSpans, metrics); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	verify := cloneDataBranch(t, originURL)

	tracesPath := filepath.Join(verify, "traces", EncodeName("github.com/x/p/TestA")+".ndjson")
	if b, err := os.ReadFile(tracesPath); err != nil {
		t.Errorf("missing %s: %v", tracesPath, err)
	} else if !strings.Contains(string(b), run.TraceID) {
		t.Errorf("test span missing trace_id %q:\n%s", run.TraceID, b)
	}

	rootPath := filepath.Join(verify, "traces", EncodeName("defrost.run")+".ndjson")
	if b, err := os.ReadFile(rootPath); err != nil {
		t.Errorf("missing root span file %s: %v", rootPath, err)
	} else if !strings.Contains(string(b), run.RunID) {
		t.Errorf("root span missing run_id %q:\n%s", run.RunID, b)
	}

	metricsPath := filepath.Join(verify, "metrics", EncodeName("db.connection_pool.size")+".ndjson")
	if b, err := os.ReadFile(metricsPath); err != nil {
		t.Errorf("missing metrics file %s: %v", metricsPath, err)
	} else if !strings.Contains(string(b), `"value":12`) {
		t.Errorf("metric file missing value:\n%s", b)
	}

	if _, err := os.Stat(filepath.Join(verify, ".gitattributes")); err != nil {
		t.Errorf("missing .gitattributes seed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(verify, "runs")); err == nil {
		t.Errorf("runs/ directory should not exist in schema 3")
	}
	if _, err := os.Stat(filepath.Join(verify, "tests")); err == nil {
		t.Errorf("tests/ directory should not exist in schema 3")
	}
}

func TestPersist_AppendsToExistingBranch(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	first := newRunContext("run-A", "1111111111111111", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(NewRootSpan(first), []models.Span{makeTestSpan(first, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("first InsertNewRun: %v", err)
	}

	second := newRunContext("run-B", "2222222222222222", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(NewRootSpan(second), []models.Span{makeTestSpan(second, "p/TestA", "ERROR")}, nil); err != nil {
		t.Fatalf("second InsertNewRun: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	path := filepath.Join(verify, "traces", EncodeName("p/TestA")+".ndjson")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 spans after two runs, got %d:\n%s", len(lines), b)
	}
	if !strings.Contains(lines[0], first.RunID) || !strings.Contains(lines[1], second.RunID) {
		t.Errorf("expected one line per run id; got:\n%s", b)
	}
}

func TestHistory_ReturnsSpansSortedByStartTime(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	run := newRunContext("run-history", "deadbeefcafebabe", "main")
	root := NewRootSpan(run)
	span := models.Span{
		Schema:            models.SchemaV3,
		TraceID:           run.TraceID,
		SpanID:            "bbbbbbbbbbbbbbbb",
		ParentSpanID:      run.RootSpanID,
		Name:              "github.com/x/p/TestA",
		StartTimeUnixNano: 100,
		EndTimeUnixNano:   200,
		Status:            models.SpanStatus{Code: "OK"},
		Resource:          run.Resource,
		Attributes:        map[string]any{"test.case.name": "github.com/x/p/TestA"},
	}
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(root, []models.Span{span}, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("github.com/x/p/TestA")
	if err != nil {
		t.Fatalf("GetTestHistory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 span, got %d", len(got))
	}
	if got[0].Name != "github.com/x/p/TestA" {
		t.Errorf("name: %q", got[0].Name)
	}
	if got[0].Resource["vcs.repository.ref.revision"] != "deadbeefcafebabe" {
		t.Errorf("resource not inlined: %+v", got[0].Resource)
	}
	if got[0].TraceID != run.TraceID {
		t.Errorf("trace_id: %q", got[0].TraceID)
	}
}

func TestHistory_UnknownTestReturnsEmpty(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)
	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("github.com/x/p/NeverWritten")
	if err != nil {
		t.Fatalf("GetTestHistory on empty origin: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 spans, got %d", len(got))
	}
}

// TestPushWithRetry_RebasesOnConflict drives the rebase path manually:
// writer 1 stages a commit, writer 2 races ahead and pushes, then writer 1
// pushes — the retry loop must fetch, rebase under merge=union, and land
// without losing either side's appended line.
func TestPushWithRetry_RebasesOnConflict(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	seedRun := newRunContext("run-seed", "1111111111111111", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(NewRootSpan(seedRun), []models.Span{makeTestSpan(seedRun, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("seed InsertNewRun: %v", err)
	}

	w1Dir := filepath.Join(t.TempDir(), "w1")
	branchExisted, err := openOrInitDataRepo(w1Dir, originURL, DefaultDataBranch)
	if err != nil {
		t.Fatalf("w1 openOrInit: %v", err)
	}
	if !branchExisted {
		t.Fatal("w1: expected branch to exist after seed")
	}
	w1Run := newRunContext("run-w1", "2222222222222222", "main")
	if err := appendSpans(w1Dir, []models.Span{NewRootSpan(w1Run), makeTestSpan(w1Run, "p/TestA", "ERROR")}); err != nil {
		t.Fatalf("w1 appendSpans: %v", err)
	}
	if err := commitAll(w1Dir, "writer 1"); err != nil {
		t.Fatalf("w1 commit: %v", err)
	}

	racerRun := newRunContext("run-racer", "3333333333333333", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(NewRootSpan(racerRun), []models.Span{makeTestSpan(racerRun, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("racer InsertNewRun: %v", err)
	}

	if err := pushWithRetry(w1Dir, DefaultDataBranch, true); err != nil {
		t.Fatalf("pushWithRetry: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	path := filepath.Join(verify, "traces", EncodeName("p/TestA")+".ndjson")
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

func TestReadOriginURL_NoOriginReturnsErrNoOrigin(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", dir)
	_, err := readOriginURL(dir)
	if !errors.Is(err, ErrNoOrigin) {
		t.Errorf("expected ErrNoOrigin, got %v", err)
	}
}

func TestPersist_RequiresOriginByDefault(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", dir)
	run := newRunContext("orphan-run", "abc", "main")
	err := New(Options{RepoDir: dir}).InsertNewRun(NewRootSpan(run), []models.Span{makeTestSpan(run, "p/TestA", "OK")}, nil)
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

	run := newRunContext("local-run", "abc123", "main")
	if err := New(Options{RepoDir: dir, NoRemote: true}).InsertNewRun(NewRootSpan(run), []models.Span{makeTestSpan(run, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("InsertNewRun (no-remote): %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "show", DefaultDataBranch+":traces/"+EncodeName("p/TestA")+".ndjson").CombinedOutput()
	if err != nil {
		t.Fatalf("read traces file from data branch: %v: %s", err, out)
	}
	if !strings.Contains(string(out), run.RunID) {
		t.Errorf("trace ndjson missing run_id %q:\n%s", run.RunID, out)
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

	run := newRunContext("rel-run", "abc123", "main")
	if err := New(Options{RepoDir: "repo", NoRemote: true}).InsertNewRun(NewRootSpan(run), []models.Span{makeTestSpan(run, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("InsertNewRun (no-remote, relative): %v", err)
	}

	out, err := exec.Command("git", "-C", "repo", "show", DefaultDataBranch+":traces/"+EncodeName("p/TestA")+".ndjson").CombinedOutput()
	if err != nil {
		t.Fatalf("read traces file from data branch: %v: %s", err, out)
	}
	if !strings.Contains(string(out), run.RunID) {
		t.Errorf("trace ndjson missing run_id %q:\n%s", run.RunID, out)
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

func TestPersist_DevModeWritesScratchDirAndSkipsGit(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", "-b", "main", dir)

	run := newRunContext("dev-run", "abc123", "main")
	if err := New(Options{RepoDir: dir, Dev: true}).InsertNewRun(NewRootSpan(run), []models.Span{makeTestSpan(run, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("InsertNewRun (dev): %v", err)
	}

	scratch := filepath.Join(dir, DevDir)
	tracesPath := filepath.Join(scratch, "traces", EncodeName("p/TestA")+".ndjson")
	if _, err := os.Stat(tracesPath); err != nil {
		t.Errorf("trace file not written to scratch dir: %v", err)
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", DefaultDataBranch).CombinedOutput(); err == nil {
		t.Errorf("expected no %s branch, but rev-parse succeeded: %s", DefaultDataBranch, out)
	}
}

// --- helpers ---

func newRunContext(runID, commit, branch string) models.RunContext {
	return models.RunContext{
		RunID:             runID,
		TraceID:           models.DeriveTraceID(runID),
		RootSpanID:        models.NewSpanID(),
		StartTimeUnixNano: 1,
		Resource: map[string]any{
			"service.name":                "defrost",
			"vcs.repository.ref.revision": commit,
			"vcs.repository.ref.name":     branch,
			"defrost.run_id":              runID,
		},
	}
}

func makeTestSpan(run models.RunContext, name, statusCode string) models.Span {
	return models.Span{
		Schema:            models.SchemaV3,
		TraceID:           run.TraceID,
		SpanID:            models.NewSpanID(),
		ParentSpanID:      run.RootSpanID,
		Name:              name,
		StartTimeUnixNano: run.StartTimeUnixNano + 1,
		EndTimeUnixNano:   run.StartTimeUnixNano + 5,
		Status:            models.SpanStatus{Code: statusCode},
		Resource:          run.Resource,
		Attributes:        map[string]any{"test.case.name": name, "defrost.run_id": run.RunID},
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
