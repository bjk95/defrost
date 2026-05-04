package duckdb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bjk95/defrost/internal/persist"
)

// TestHydrate_ShortCircuitWhenSHAUnchanged verifies the cheap-probe
// path: when ls-remote returns the same SHA we've already hydrated
// against, Hydrate must NOT touch the working tree. We assert this
// indirectly — the `last_sha` cache_meta entry remains stable, and
// no rows are added beyond what we seeded.
func TestHydrate_ShortCircuitWhenSHAUnchanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bare := setupBareRemote(t)
	cwd := setupRepoWithOrigin(t, bare)

	q, err := New(persist.Options{RepoDir: cwd})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer q.Close()

	// First Hydrate: clones the worktree, walks zero traces (none
	// committed yet — the bare only has a non-OTLP seed file). After
	// this, last_sha should be the bare's tip.
	if err := q.Hydrate(); err != nil {
		t.Fatalf("first Hydrate: %v", err)
	}
	first, err := q.cacheMeta(context.Background(), "last_sha")
	if err != nil {
		t.Fatalf("cacheMeta after first hydrate: %v", err)
	}
	if first == "" {
		t.Fatal("expected last_sha to be recorded after first hydrate")
	}

	// Mutate the worktree dir mtime so any signal of "did we walk?"
	// is detectable. Then call Hydrate again — the SHA short-circuit
	// should fire and the worktree should NOT be touched.
	dataDir, err := persist.CacheRoot(persist.Options{RepoDir: cwd})
	if err != nil {
		t.Fatalf("CacheRoot: %v", err)
	}
	worktree := filepath.Join(dataDir, "data")
	statBefore, err := os.Stat(worktree)
	if err != nil {
		t.Fatalf("stat worktree: %v", err)
	}

	if err := q.Hydrate(); err != nil {
		t.Fatalf("second Hydrate: %v", err)
	}

	statAfter, err := os.Stat(worktree)
	if err != nil {
		t.Fatalf("stat worktree (post): %v", err)
	}
	// Modtime is the cheapest "was the dir touched" check. A short-
	// circuit must skip git fetch/reset, so worktree mtime stays put.
	if !statAfter.ModTime().Equal(statBefore.ModTime()) {
		t.Errorf("worktree mtime advanced — short-circuit didn't fire. before=%v after=%v",
			statBefore.ModTime(), statAfter.ModTime())
	}

	second, err := q.cacheMeta(context.Background(), "last_sha")
	if err != nil {
		t.Fatalf("cacheMeta after second hydrate: %v", err)
	}
	if second != first {
		t.Errorf("last_sha changed without a remote update: %q -> %q", first, second)
	}
}

// TestHydrate_WipesDerivedStateOnForceReset verifies the drop-history
// reconciliation: when CloneForRead reports Reset=true, Hydrate must
// truncate traces/metrics/logs/hydration_state before re-walking.
//
// Approach: seed materialised rows that mimic an old hydrate, force-
// push a fresh history on the remote, call Hydrate, assert the seeded
// rows are gone (they would otherwise survive because the file paths
// they correspond to no longer exist in the new history).
func TestHydrate_WipesDerivedStateOnForceReset(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bare := setupBareRemote(t)
	cwd := setupRepoWithOrigin(t, bare)

	q, err := New(persist.Options{RepoDir: cwd})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer q.Close()

	// Initial hydrate to lay down cache_meta.last_sha.
	if err := q.Hydrate(); err != nil {
		t.Fatalf("initial Hydrate: %v", err)
	}

	// Seed a stale row in each materialised table — these rows simulate
	// runs from the old history that drop just rewrote away.
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO traces (trace_id, span_name, start_time, status_code) VALUES ('stale', 'old.run', NOW(), 0)`,
		`INSERT INTO metrics (metric_name, ts) VALUES ('stale', NOW())`,
		`INSERT INTO logs (trace_id, ts, severity, body) VALUES ('stale', NOW(), 'INFO', 'old')`,
		`INSERT INTO hydration_state (file_path, file_size, file_mtime) VALUES ('/tmp/stale', 1, 1)`,
	} {
		if _, err := q.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Force-push a new orphan branch on the remote — same shape as
	// what `defrost drop history` does.
	forceResetBranch(t, bare, persist.DefaultDataBranch)

	if err := q.Hydrate(); err != nil {
		t.Fatalf("post-reset Hydrate: %v", err)
	}

	// All four seed rows should be gone.
	for _, c := range []struct {
		name, sql string
	}{
		{"traces", `SELECT COUNT(*) FROM traces WHERE trace_id = 'stale'`},
		{"metrics", `SELECT COUNT(*) FROM metrics WHERE metric_name = 'stale'`},
		{"logs", `SELECT COUNT(*) FROM logs WHERE trace_id = 'stale'`},
		{"hydration_state", `SELECT COUNT(*) FROM hydration_state WHERE file_path = '/tmp/stale'`},
	} {
		var n int
		if err := q.db.QueryRowContext(ctx, c.sql).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", c.name, err)
		}
		if n != 0 {
			t.Errorf("%s: expected 0 stale rows after force-reset, got %d", c.name, n)
		}
	}
}

// --- helpers (deliberately tiny — full git plumbing lives in the
// persist package's cache_test.go; these mirror the minimum needed
// here without importing test code across packages).

func setupBareRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "--bare", "-b", persist.DefaultDataBranch, bare)
	work := filepath.Join(t.TempDir(), "seed-work")
	mustGit(t, "", "init", "-q", "-b", persist.DefaultDataBranch, work)
	configBot(t, work)
	if err := os.WriteFile(filepath.Join(work, "first.txt"), []byte("first"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-q", "-m", "first")
	mustGit(t, work, "remote", "add", "origin", bare)
	mustGit(t, work, "push", "-q", "origin", persist.DefaultDataBranch)
	return bare
}

func setupRepoWithOrigin(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, "", "init", "-q", dir)
	configBot(t, dir)
	mustGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")
	mustGit(t, dir, "remote", "add", "origin", originURL)
	return dir
}

func configBot(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "test")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", full, err, out)
	}
	return string(out)
}

func forceResetBranch(t *testing.T, bare, branch string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "force-work")
	mustGit(t, "", "init", "-q", "-b", branch, work)
	configBot(t, work)
	if err := os.WriteFile(filepath.Join(work, "rewritten.txt"), []byte("rewritten"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-q", "-m", "rewritten")
	mustGit(t, work, "remote", "add", "origin", bare)
	mustGit(t, work, "push", "--force", "origin", branch)
}

