package persist

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
)

func TestEncodeName_RoundTrip(t *testing.T) {
	cases := []string{
		"github.com/bjk95/defrost/internal/x/TestFoo",
		"p/TestSubtest/with spaces",
		"db.query.duration",
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
	root.EndTimeUnixNano = uint64(int64(run.StartTimeUnixNano) + 100)
	root.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}

	testSpans := []*tracepb.Span{makeTestSpan(run, "github.com/x/p/TestA", tracepb.Status_STATUS_CODE_OK)}
	allSpans := append([]*tracepb.Span{root}, testSpans...)
	traces := WrapSpansInResource(run.Resource, allSpans)

	v := 12.0
	metrics := []*metricspb.Metric{{
		Name: "db.connection_pool.size",
		Unit: "{connections}",
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: uint64(run.StartTimeUnixNano) + 30,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: v},
			}},
		}},
	}}
	wrappedMetrics := WrapMetricsInResource(MetricResource(run), metrics)

	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, wrappedMetrics); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	verify := cloneDataBranch(t, originURL)

	tracesPath := filepath.Join(verify, "traces", EncodeName("github.com/x/p/TestA")+".ndjson")
	if b, err := os.ReadFile(tracesPath); err != nil {
		t.Errorf("missing %s: %v", tracesPath, err)
	} else if !strings.Contains(string(b), run.RunID) {
		t.Errorf("test span file should mention run id %q:\n%s", run.RunID, b)
	}

	rootPath := filepath.Join(verify, "traces", EncodeName("defrost.run")+".ndjson")
	if b, err := os.ReadFile(rootPath); err != nil {
		t.Errorf("missing root span file %s: %v", rootPath, err)
	} else if !strings.Contains(string(b), run.RunID) {
		t.Errorf("root span file missing run_id %q:\n%s", run.RunID, b)
	}

	metricsPath := filepath.Join(verify, "metrics", EncodeName("db.connection_pool.size")+".ndjson")
	if b, err := os.ReadFile(metricsPath); err != nil {
		t.Errorf("missing metrics file %s: %v", metricsPath, err)
	} else if !strings.Contains(string(b), "db.connection_pool.size") {
		t.Errorf("metric file missing name:\n%s", b)
	}

	if _, err := os.Stat(filepath.Join(verify, ".gitattributes")); err != nil {
		t.Errorf("missing .gitattributes seed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(verify, "runs")); err == nil {
		t.Errorf("runs/ directory should not exist")
	}
	if _, err := os.Stat(filepath.Join(verify, "tests")); err == nil {
		t.Errorf("tests/ directory should not exist")
	}
}

func TestPersist_AppendsToExistingBranch(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	first := newRunContext("run-A", "1111111111111111", "main")
	traces1 := WrapSpansInResource(first.Resource, []*tracepb.Span{
		NewRootSpan(first),
		makeTestSpan(first, "p/TestA", tracepb.Status_STATUS_CODE_OK),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces1, nil); err != nil {
		t.Fatalf("first InsertNewRun: %v", err)
	}

	second := newRunContext("run-B", "2222222222222222", "main")
	traces2 := WrapSpansInResource(second.Resource, []*tracepb.Span{
		NewRootSpan(second),
		makeTestSpan(second, "p/TestA", tracepb.Status_STATUS_CODE_ERROR),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces2, nil); err != nil {
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
		t.Fatalf("expected 2 lines after two runs, got %d:\n%s", len(lines), b)
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
	span := &tracepb.Span{
		TraceId:           run.TraceID,
		SpanId:            mustHex8(),
		ParentSpanId:      run.RootSpanID,
		Name:              "github.com/x/p/TestA",
		StartTimeUnixNano: 100,
		EndTimeUnixNano:   200,
		Status:            &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK},
		Attributes: []*commonpb.KeyValue{
			models.StringAttr("test.case.name", "github.com/x/p/TestA"),
			models.StringAttr("defrost.run_id", run.RunID),
		},
	}
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{root, span})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("github.com/x/p/TestA")
	if err != nil {
		t.Fatalf("GetTestHistory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 span, got %d", len(got))
	}
	gotSpan := SpanFromResourceSpans(got[0])
	if gotSpan == nil || gotSpan.Name != "github.com/x/p/TestA" {
		t.Errorf("name: %v", gotSpan)
	}
	if rev := models.ResourceString(got[0].Resource, "vcs.repository.ref.revision"); rev != "deadbeefcafebabe" {
		t.Errorf("resource not inlined: revision=%q", rev)
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

func TestPushWithRetry_RebasesOnConflict(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	seedRun := newRunContext("run-seed", "1111111111111111", "main")
	seedTraces := WrapSpansInResource(seedRun.Resource, []*tracepb.Span{
		NewRootSpan(seedRun),
		makeTestSpan(seedRun, "p/TestA", tracepb.Status_STATUS_CODE_OK),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(seedTraces, nil); err != nil {
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
	w1Traces := WrapSpansInResource(w1Run.Resource, []*tracepb.Span{
		NewRootSpan(w1Run),
		makeTestSpan(w1Run, "p/TestA", tracepb.Status_STATUS_CODE_ERROR),
	})
	if err := appendSpans(w1Dir, w1Traces); err != nil {
		t.Fatalf("w1 appendSpans: %v", err)
	}
	if err := commitAll(w1Dir, "writer 1"); err != nil {
		t.Fatalf("w1 commit: %v", err)
	}

	racerRun := newRunContext("run-racer", "3333333333333333", "main")
	racerTraces := WrapSpansInResource(racerRun.Resource, []*tracepb.Span{
		NewRootSpan(racerRun),
		makeTestSpan(racerRun, "p/TestA", tracepb.Status_STATUS_CODE_OK),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(racerTraces, nil); err != nil {
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
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{NewRootSpan(run), makeTestSpan(run, "p/TestA", tracepb.Status_STATUS_CODE_OK)})
	err := New(Options{RepoDir: dir}).InsertNewRun(traces, nil)
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
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{NewRootSpan(run), makeTestSpan(run, "p/TestA", tracepb.Status_STATUS_CODE_OK)})
	if err := New(Options{RepoDir: dir, NoRemote: true}).InsertNewRun(traces, nil); err != nil {
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

func TestPersist_LocalOnlyNoRemote_RelativeRepoDir(t *testing.T) {
	requireGit(t)
	parent := t.TempDir()
	gitMust(t, "", "init", "-b", "main", filepath.Join(parent, "repo"))
	gitMust(t, filepath.Join(parent, "repo"), "config", "user.email", "t@example.com")
	gitMust(t, filepath.Join(parent, "repo"), "config", "user.name", "t")
	gitMust(t, filepath.Join(parent, "repo"), "commit", "--allow-empty", "-m", "init")

	t.Chdir(parent)

	run := newRunContext("rel-run", "abc123", "main")
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{NewRootSpan(run), makeTestSpan(run, "p/TestA", tracepb.Status_STATUS_CODE_OK)})
	if err := New(Options{RepoDir: "repo", NoRemote: true}).InsertNewRun(traces, nil); err != nil {
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
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{NewRootSpan(run), makeTestSpan(run, "p/TestA", tracepb.Status_STATUS_CODE_OK)})
	if err := New(Options{RepoDir: dir, Dev: true}).InsertNewRun(traces, nil); err != nil {
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

func TestLoadAll_ReturnsRootSpansAndTestSpansGroupedByEncodedName(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	run1 := newRunContext("run-1", "1111111111111111", "main")
	traces1 := WrapSpansInResource(run1.Resource, []*tracepb.Span{
		NewRootSpan(run1),
		makeTestSpan(run1, "github.com/x/p/TestA", tracepb.Status_STATUS_CODE_OK),
		makeTestSpan(run1, "github.com/x/p/TestB", tracepb.Status_STATUS_CODE_ERROR),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces1, nil); err != nil {
		t.Fatalf("persist run1: %v", err)
	}

	run2 := newRunContext("run-2", "2222222222222222", "main")
	traces2 := WrapSpansInResource(run2.Resource, []*tracepb.Span{
		NewRootSpan(run2),
		makeTestSpan(run2, "github.com/x/p/TestA", tracepb.Status_STATUS_CODE_OK),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces2, nil); err != nil {
		t.Fatalf("persist run2: %v", err)
	}

	roots, byEncodedName, err := New(Options{RepoDir: repoDir}).LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("want 2 root spans, got %d", len(roots))
	}
	gotRunIDs := map[string]bool{}
	for _, rs := range roots {
		if id := models.ResourceString(rs.Resource, "defrost.run_id"); id != "" {
			gotRunIDs[id] = true
		}
	}
	if !gotRunIDs["run-1"] || !gotRunIDs["run-2"] {
		t.Errorf("missing run IDs in %v", gotRunIDs)
	}

	idA := EncodeName("github.com/x/p/TestA")
	idB := EncodeName("github.com/x/p/TestB")
	if len(byEncodedName[idA]) != 2 {
		t.Errorf("TestA: want 2 spans, got %d", len(byEncodedName[idA]))
	}
	if len(byEncodedName[idB]) != 1 {
		t.Errorf("TestB: want 1 span, got %d", len(byEncodedName[idB]))
	}
}

func TestLoadAllMetrics_RoundTripsEveryDataPoint(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	run := newRunContext("run-1", "1111111111111111", "main")
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{NewRootSpan(run)})

	gauge := 0.93
	metrics := []*metricspb.Metric{
		{
			Name: "eval.factuality",
			Unit: "{score}",
			Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
				DataPoints: []*metricspb.NumberDataPoint{{
					TimeUnixNano: uint64(run.StartTimeUnixNano) + 1,
					Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: gauge},
				}},
			}},
		},
		{
			Name: "http.server.request.count",
			Unit: "{request}",
			Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
				IsMonotonic:            true,
				AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
				DataPoints: []*metricspb.NumberDataPoint{{
					TimeUnixNano: uint64(run.StartTimeUnixNano) + 1,
					Value:        &metricspb.NumberDataPoint_AsInt{AsInt: 42},
				}},
			}},
		},
	}
	wrapped := WrapMetricsInResource(MetricResource(run), metrics)
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, wrapped); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	loaded, err := New(Options{RepoDir: repoDir}).LoadAllMetrics()
	if err != nil {
		t.Fatalf("LoadAllMetrics: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("want 2 ResourceMetrics, got %d", len(loaded))
	}
	gotNames := map[string]bool{}
	for _, rm := range loaded {
		m := MetricFromResourceMetrics(rm)
		if m == nil {
			continue
		}
		gotNames[m.Name] = true
	}
	if !gotNames["eval.factuality"] || !gotNames["http.server.request.count"] {
		t.Errorf("missing metric names in %v", gotNames)
	}
}

func TestLoadAllMetrics_NoBranch_ReturnsEmpty(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	loaded, err := New(Options{RepoDir: repoDir}).LoadAllMetrics()
	if err != nil {
		t.Fatalf("LoadAllMetrics: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 metrics, got %d", len(loaded))
	}
}

func TestLoadAll_NoBranch_ReturnsEmpty(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	roots, byEncodedName, err := New(Options{RepoDir: repoDir}).LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(roots) != 0 {
		t.Errorf("expected 0 root spans, got %d", len(roots))
	}
	if len(byEncodedName) != 0 {
		t.Errorf("expected 0 test groups, got %d", len(byEncodedName))
	}
}

func TestMetricResource_StripsHighCardinalityFields(t *testing.T) {
	run := newRunContext("run-mr", "deadbeefcafebabe", "main")
	mr := MetricResource(run)
	if mr == nil {
		t.Fatal("MetricResource returned nil")
	}
	for _, kv := range mr.Attributes {
		switch kv.Key {
		case "defrost.run_id", "cicd.pipeline.run.id", "defrost.cmd",
			"vcs.repository.ref.revision", "defrost.dirty_hash",
			"defrost.author_email", "defrost.author_name", "defrost.parent_commit":
			t.Errorf("metric resource should not contain %q", kv.Key)
		}
	}
	// Stable identity attrs should still be there.
	if got := models.ResourceString(mr, "service.name"); got != "defrost" {
		t.Errorf("service.name: %q", got)
	}
	if got := models.ResourceString(mr, "vcs.repository.ref.name"); got != "main" {
		t.Errorf("vcs.repository.ref.name: %q", got)
	}
}

// --- helpers ---

func newRunContext(runID, commit, branch string) models.RunContext {
	attrs := []*commonpb.KeyValue{
		models.StringAttr("service.name", "defrost"),
		models.StringAttr("vcs.repository.ref.revision", commit),
		models.StringAttr("vcs.repository.ref.name", branch),
		models.StringAttr("defrost.run_id", runID),
		models.StringAttr("cicd.pipeline.run.id", runID),
	}
	return models.RunContext{
		RunID:             runID,
		TraceID:           models.DeriveTraceID(runID),
		RootSpanID:        models.NewSpanID(),
		StartTimeUnixNano: 1,
		Resource:          &resourcepb.Resource{Attributes: attrs},
	}
}

func makeTestSpan(run models.RunContext, name string, statusCode tracepb.Status_StatusCode) *tracepb.Span {
	return &tracepb.Span{
		TraceId:           run.TraceID,
		SpanId:            mustHex8(),
		ParentSpanId:      run.RootSpanID,
		Name:              name,
		StartTimeUnixNano: uint64(run.StartTimeUnixNano + 1),
		EndTimeUnixNano:   uint64(run.StartTimeUnixNano + 5),
		Status:            &tracepb.Status{Code: statusCode},
		Attributes: []*commonpb.KeyValue{
			models.StringAttr("test.case.name", name),
			models.StringAttr("defrost.run_id", run.RunID),
		},
	}
}

func mustHex8() []byte {
	return models.NewSpanID()
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
