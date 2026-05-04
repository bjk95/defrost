package persist

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

func TestFileBackend_InsertNewRun_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	be := New(Options{RepoDir: dir, Dev: true})

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "defrost")
	rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("defrost.run")

	traceBytes, err := ptraceotlp.NewExportRequestFromTraces(td).MarshalProto()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var traceID [16]byte
	for i := range traceID {
		traceID[i] = byte(i + 1)
	}
	now := time.Now().UTC()
	if err := be.InsertNewRun(Run{
		TraceID:     traceID,
		RunStartUTC: now,
		TraceBytes:  traceBytes,
	}); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}
	files, err := ListSignalFiles(LocalRoot(Options{RepoDir: dir, Dev: true}), "traces")
	if err != nil {
		t.Fatalf("ListSignalFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file under traces/, got %d", len(files))
	}
	raw, err := ReadSignalBytes(files[0])
	if err != nil {
		t.Fatalf("ReadSignalBytes: %v", err)
	}
	req := ptraceotlp.NewExportRequest()
	if err := req.UnmarshalProto(raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Traces().ResourceSpans().Len() != 1 {
		t.Errorf("decoded ResourceSpans: got %d, want 1", req.Traces().ResourceSpans().Len())
	}
}

func TestFileBackend_InsertNewRun_SkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	be := New(Options{RepoDir: dir, Dev: true})
	if err := be.InsertNewRun(Run{}); err != nil {
		t.Fatalf("InsertNewRun(empty): %v", err)
	}
	if _, err := os.Stat(LocalRoot(Options{RepoDir: dir, Dev: true})); !os.IsNotExist(err) {
		t.Errorf("expected data dir absent for empty run, got err=%v", err)
	}
}

func TestDetectRunContext_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := DetectRunContext(Options{RepoDir: dir}, []string{"go", "test"}, "0.0.0"); err == nil {
		t.Errorf("expected error for non-git dir, got nil")
	}
}

func TestDetectRunContext_HappyPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=t",
			"-c", "commit.gpgsign=false", "commit", "--allow-empty",
			"-q", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Skipf("git setup failed (sandbox limitation): %v: %s", err, out)
		}
	}
	rc, err := DetectRunContext(Options{RepoDir: dir}, []string{"go", "test", "./..."}, "0.0.0-test")
	if err != nil {
		t.Fatalf("DetectRunContext: %v", err)
	}
	if rc.RunID == "" {
		t.Errorf("RunID empty")
	}
	if len(rc.TraceID) != 16 {
		t.Errorf("TraceID len: got %d, want 16", len(rc.TraceID))
	}
}

// TestFileBackend_DropHistory_TriggersReset proves that after a
// fileBackend drop deletes signal files, the next CloneForRead
// reports Reset=true so the Querier wipes derived state. Without
// this, dashboards keep rendering rows whose underlying files are
// gone — Hydrate's incremental walk only INSERTs, it doesn't DELETE.
func TestFileBackend_DropHistory_TriggersReset(t *testing.T) {
	dir := t.TempDir()
	be := New(Options{RepoDir: dir, Dev: true})

	// Seed one run so dropSignalFiles has something to remove.
	td := ptrace.NewTraces()
	td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("defrost.run")
	traceBytes, err := ptraceotlp.NewExportRequestFromTraces(td).MarshalProto()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var traceID [16]byte
	for i := range traceID {
		traceID[i] = byte(i + 1)
	}
	if err := be.InsertNewRun(Run{
		TraceID:     traceID,
		RunStartUTC: time.Now().UTC(),
		TraceBytes:  traceBytes,
	}); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	// First read: no Reset, baseline.
	snap1, err := be.CloneForRead()
	if err != nil {
		t.Fatalf("CloneForRead (baseline): %v", err)
	}
	if snap1.Reset {
		t.Errorf("snap1.Reset = true; expected false on first read")
	}

	// Drop. The fileBackend deletes signal files in place AND writes
	// the .defrost-dropped sentinel.
	if err := be.DropHistory(DropSelector{DropTraces: true, DropMetrics: true, DropLogs: true}, nil); err != nil {
		t.Fatalf("DropHistory: %v", err)
	}

	// Second read: Reset=true so the Querier knows to wipe.
	snap2, err := be.CloneForRead()
	if err != nil {
		t.Fatalf("CloneForRead (post-drop): %v", err)
	}
	if !snap2.Reset {
		t.Error("snap2.Reset = false; expected true after drop")
	}

	// Third read: Reset back to false (the sentinel is consumed by
	// the previous CloneForRead).
	snap3, err := be.CloneForRead()
	if err != nil {
		t.Fatalf("CloneForRead (steady state): %v", err)
	}
	if snap3.Reset {
		t.Error("snap3.Reset = true; expected false (sentinel should be one-shot)")
	}
}
