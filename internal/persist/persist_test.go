package persist

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestPersist_WritesOneFilePerSignal(t *testing.T) {
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

	traceFiles := listFilesWithSuffix(t, filepath.Join(verify, "traces"), fileSuffix)
	if len(traceFiles) != 1 {
		t.Fatalf("want exactly 1 trace file, got %d: %v", len(traceFiles), traceFiles)
	}
	metricFiles := listFilesWithSuffix(t, filepath.Join(verify, "metrics"), fileSuffix)
	if len(metricFiles) != 1 {
		t.Fatalf("want exactly 1 metrics file, got %d: %v", len(metricFiles), metricFiles)
	}
	if _, err := os.Stat(filepath.Join(verify, ".gitattributes")); err == nil {
		t.Errorf(".gitattributes should not be written")
	}
	if _, err := os.Stat(filepath.Join(verify, "runs")); err == nil {
		t.Errorf("runs/ directory should not exist")
	}
}

func TestPersist_RoundTrip(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	run := newRunContext("run-rt", "deadbeefcafebabe", "main")
	root := NewRootSpan(run)
	root.EndTimeUnixNano = uint64(int64(run.StartTimeUnixNano) + 100)
	root.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}

	tests := []*tracepb.Span{
		makeTestSpan(run, "github.com/x/p/TestA", tracepb.Status_STATUS_CODE_OK),
		makeTestSpan(run, "github.com/x/p/TestB", tracepb.Status_STATUS_CODE_ERROR),
	}
	traces := WrapSpansInResource(run.Resource, append([]*tracepb.Span{root}, tests...))

	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("github.com/x/p/TestA")
	if err != nil {
		t.Fatalf("GetTestHistory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 span for TestA, got %d", len(got))
	}
	gotSpan := SpanFromResourceSpans(got[0])
	if gotSpan == nil {
		t.Fatal("got nil span")
	}
	if gotSpan.Name != "github.com/x/p/TestA" {
		t.Errorf("name: %q", gotSpan.Name)
	}
	if rev := models.ResourceString(got[0].Resource, "vcs.repository.ref.revision"); rev != "deadbeefcafebabe" {
		t.Errorf("resource not preserved through round-trip: revision=%q", rev)
	}
	if rid := models.ResourceString(got[0].Resource, "cicd.pipeline.run.id"); rid != run.RunID {
		t.Errorf("cicd.pipeline.run.id missing on round-trip: %q", rid)
	}
}

func TestPersist_AtomicWrite_NoTmpLeft(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	run := newRunContext("run-tmp", "1234abcd5678ef00", "main")
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{NewRootSpan(run)})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}
	verify := cloneDataBranch(t, originURL)

	var leftovers []string
	if err := filepath.Walk(verify, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if strings.HasSuffix(info.Name(), ".tmp") {
			leftovers = append(leftovers, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(leftovers) != 0 {
		t.Errorf("expected no .tmp files left, got: %v", leftovers)
	}
}

func TestPersist_AtomicWrite_FailureLeavesNoPartial(t *testing.T) {
	// Create a directory at the path we're about to write to. The .tmp
	// write succeeds, but the final rename(tmp, target) fails because
	// you can't rename a regular file over a non-empty directory. The
	// deferred cleanup must remove the .tmp so no orphan is left.
	dir := t.TempDir()
	target := filepath.Join(dir, "blocker")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	// Put something inside so rename can't succeed even on Linux's
	// directory-overwrite-when-empty semantics.
	if err := os.WriteFile(filepath.Join(target, "in-use"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := writeFileAtomic(target, []byte("payload")); err == nil {
		t.Fatal("expected writeFileAtomic to fail when target path is a non-empty directory")
	}

	// No leftover .tmp anywhere under dir — proves the defer cleaned up.
	var leftovers []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		if strings.HasSuffix(info.Name(), ".tmp") {
			leftovers = append(leftovers, p)
		}
		return nil
	})
	if len(leftovers) != 0 {
		t.Errorf("expected no .tmp orphans, got: %v", leftovers)
	}
}

func TestPersist_SpanAttributesSlim(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	run := newRunContext("run-slim", "11111111aaaaaaaa", "main")
	root := NewRootSpan(run)
	test := makeTestSpan(run, "p/TestA", tracepb.Status_STATUS_CODE_OK)
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{root, test})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("p/TestA")
	if err != nil || len(got) != 1 {
		t.Fatalf("get test history: %v len=%d", err, len(got))
	}
	gotSpan := SpanFromResourceSpans(got[0])
	if gotSpan == nil {
		t.Fatal("nil span")
	}
	wantKeys := map[string]struct{}{"test.case.result.status": {}}
	if len(gotSpan.Attributes) != len(wantKeys) {
		t.Errorf("test span attribute count: want %d, got %d (%v)", len(wantKeys), len(gotSpan.Attributes), gotSpan.Attributes)
	}
	for _, kv := range gotSpan.Attributes {
		if _, ok := wantKeys[kv.Key]; !ok {
			t.Errorf("unexpected test span attribute: %q", kv.Key)
		}
	}

	roots, _, err := New(Options{RepoDir: repoDir}).LoadAll()
	if err != nil || len(roots) != 1 {
		t.Fatalf("load all roots: %v len=%d", err, len(roots))
	}
	if rs := SpanFromResourceSpans(roots[0]); rs == nil {
		t.Fatal("nil root span")
	} else if len(rs.Attributes) != 0 {
		t.Errorf("root span should have no attributes, got %v", rs.Attributes)
	}
}

func TestPersist_FQNPreserved(t *testing.T) {
	requireGit(t)
	cases := []string{
		"github.com/bjk95/defrost/internal/x/TestFoo",
		"tests/test_module.py::TestClass::test_method",
		"tests/test_module.py::test_top_level",
		"src/components/Button.test.tsx > Button > renders",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			repoDir, _ := makeFixture(t)
			run := newRunContext("run-"+EncodeName(name), "abc123def4567890", "main")
			traces := WrapSpansInResource(run.Resource, []*tracepb.Span{
				NewRootSpan(run),
				makeTestSpan(run, name, tracepb.Status_STATUS_CODE_OK),
			})
			if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, nil); err != nil {
				t.Fatalf("InsertNewRun: %v", err)
			}
			got, err := New(Options{RepoDir: repoDir}).GetTestHistory(name)
			if err != nil || len(got) != 1 {
				t.Fatalf("get history: %v len=%d", err, len(got))
			}
			s := SpanFromResourceSpans(got[0])
			if s == nil || s.Name != name {
				t.Errorf("FQN not preserved: want %q got %q", name, s.GetName())
			}
		})
	}
}

func TestPersist_ResourceCarriesCicdRunID(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)
	run := newRunContext("run-cicd", "1234567890abcdef", "main")
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{
		NewRootSpan(run),
		makeTestSpan(run, "p/TestA", tracepb.Status_STATUS_CODE_OK),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}
	roots, _, err := New(Options{RepoDir: repoDir}).LoadAll()
	if err != nil || len(roots) != 1 {
		t.Fatalf("load: %v len=%d", err, len(roots))
	}
	if id := models.ResourceString(roots[0].Resource, "cicd.pipeline.run.id"); id != run.RunID {
		t.Errorf("cicd.pipeline.run.id: want %q got %q", run.RunID, id)
	}
	if id := models.ResourceString(roots[0].Resource, "defrost.run_id"); id != "" {
		t.Errorf("defrost.run_id should be absent, got %q", id)
	}
}

func TestPersist_ConcurrentWritersDontConflict(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	// Seed first run.
	first := newRunContext("run-A", "1111111111111111", "main")
	t1 := WrapSpansInResource(first.Resource, []*tracepb.Span{
		NewRootSpan(first),
		makeTestSpan(first, "p/TestA", tracepb.Status_STATUS_CODE_OK),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(t1, nil); err != nil {
		t.Fatalf("first InsertNewRun: %v", err)
	}

	// Second run from a different trace_id (different run id).
	second := newRunContext("run-B", "2222222222222222", "main")
	t2 := WrapSpansInResource(second.Resource, []*tracepb.Span{
		NewRootSpan(second),
		makeTestSpan(second, "p/TestA", tracepb.Status_STATUS_CODE_ERROR),
	})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(t2, nil); err != nil {
		t.Fatalf("second InsertNewRun: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	files := listFilesWithSuffix(t, filepath.Join(verify, "traces"), fileSuffix)
	if len(files) != 2 {
		t.Fatalf("want 2 trace files (one per run), got %d: %v", len(files), files)
	}

	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("p/TestA")
	if err != nil {
		t.Fatalf("GetTestHistory: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 history rows for TestA, got %d", len(got))
	}
}

func TestPersist_SizeBudget(t *testing.T) {
	// Pin the headline win: a 100-test run should land well under 10 KB
	// on disk (proto + zstd). Catches regressions where someone inflates
	// per-span attributes again.
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	run := newRunContext("run-size", "deadbeefcafebabe", "main")
	spans := []*tracepb.Span{NewRootSpan(run)}
	for i := 0; i < 100; i++ {
		spans = append(spans, makeTestSpan(run, "github.com/x/p/TestSize"+itoa(i), tracepb.Status_STATUS_CODE_OK))
	}
	traces := WrapSpansInResource(run.Resource, spans)
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	files := listFilesWithSuffix(t, filepath.Join(verify, "traces"), fileSuffix)
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d", len(files))
	}
	st, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	const budget = 10 * 1024 // 10 KB hard ceiling for 100 spans.
	if st.Size() > budget {
		t.Errorf("100-span trace file too large: %d bytes (budget %d)", st.Size(), budget)
	}
}

func TestPersist_DatePartitioning(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	run := newRunContext("run-date", "abcdefabcdefabcd", "main")
	// Pin the start time to a known UTC day so we can predict the path.
	known := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	run.StartTimeUnixNano = known.UnixNano()
	root := NewRootSpan(run)
	root.StartTimeUnixNano = uint64(run.StartTimeUnixNano)
	traces := WrapSpansInResource(run.Resource, []*tracepb.Span{root})
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(traces, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}
	verify := cloneDataBranch(t, originURL)
	wantDir := filepath.Join(verify, "traces", "2026", "04", "15")
	if _, err := os.Stat(wantDir); err != nil {
		t.Errorf("expected date-partitioned dir %s: %v", wantDir, err)
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
	files := listFilesWithSuffix(t, filepath.Join(scratch, "traces"), fileSuffix)
	if len(files) != 1 {
		t.Errorf("want 1 dev trace file, got %d", len(files))
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
		if id := models.ResourceString(rs.Resource, "cicd.pipeline.run.id"); id != "" {
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

func TestRunDurationMetric(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoDir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	sub := filepath.Join(repoDir, "subpkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	startNs := int64(1_000_000_000)
	run := models.RunContext{StartTimeUnixNano: startNs}
	end := time.Unix(0, startNs+1_500_000_000) // +1.5s

	cases := []struct {
		name    string
		repoDir string
		want    string
	}{
		{"root", repoDir, "defrost.run.go test ./..."},
		{"subdir", sub, "defrost.run.subpkg¬go test ./..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := RunDurationMetric(run, []string{"go", "test", "./..."}, tc.repoDir, end)
			if m.Name != tc.want {
				t.Errorf("name: got %q want %q", m.Name, tc.want)
			}
			if m.Unit != "ms" {
				t.Errorf("unit: got %q want %q", m.Unit, "ms")
			}
			g, ok := m.Data.(*metricspb.Metric_Gauge)
			if !ok {
				t.Fatalf("data: got %T want *Metric_Gauge", m.Data)
			}
			if len(g.Gauge.DataPoints) != 1 {
				t.Fatalf("data points: got %d want 1", len(g.Gauge.DataPoints))
			}
			dp := g.Gauge.DataPoints[0]
			dv, ok := dp.Value.(*metricspb.NumberDataPoint_AsDouble)
			if !ok {
				t.Fatalf("value: got %T want AsDouble", dp.Value)
			}
			if dv.AsDouble != 1500.0 {
				t.Errorf("value ms: got %v want 1500", dv.AsDouble)
			}
			if dp.StartTimeUnixNano != uint64(startNs) {
				t.Errorf("start nano: got %d want %d", dp.StartTimeUnixNano, startNs)
			}
			if dp.TimeUnixNano != uint64(end.UnixNano()) {
				t.Errorf("end nano: got %d want %d", dp.TimeUnixNano, end.UnixNano())
			}
		})
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
		case "cicd.pipeline.run.id", "defrost.cmd",
			"vcs.repository.ref.revision", "defrost.dirty_hash",
			"defrost.author_email", "defrost.author_name", "defrost.parent_commit":
			t.Errorf("metric resource should not contain %q", kv.Key)
		}
	}
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
			models.StringAttr("test.case.result.status", statusToText(statusCode)),
		},
	}
}

func statusToText(c tracepb.Status_StatusCode) string {
	switch c {
	case tracepb.Status_STATUS_CODE_OK:
		return "passed"
	case tracepb.Status_STATUS_CODE_ERROR:
		return "failed"
	default:
		return "skipped"
	}
}

func mustHex8() []byte { return models.NewSpanID() }

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

func listFilesWithSuffix(t *testing.T, root, suffix string) []string {
	t.Helper()
	var out []string
	if _, err := os.Stat(root); err != nil {
		return out
	}
	if err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if strings.HasSuffix(info.Name(), suffix) {
			out = append(out, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := "0123456789"
	out := ""
	for i > 0 {
		out = string(digits[i%10]) + out
		i /= 10
	}
	return out
}
