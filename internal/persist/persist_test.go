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
