package persist

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGitBackend_CloneForRead_PersistentWorktree exercises the warm-
// path / cold-path / force-reset trio:
//
//  1. Cold: first call clones into the persistent cache dir.
//  2. Warm: second call fast-forwards via fetch; same dir, no Reset.
//  3. Force-reset: a `--force` push that rewrites the branch causes
//     the next call to detect non-fast-forward → wipe + reclone, with
//     Snapshot.Reset=true.
//
// Uses a bare repo on the local filesystem as the "remote" so the test
// has no network dependency and runs in a few hundred milliseconds.
func TestGitBackend_CloneForRead_PersistentWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	bare := setupBareRemote(t)
	cwd := setupRepoWithOrigin(t, bare)
	cacheRoot := setupCacheRedirect(t)

	be := New(Options{RepoDir: cwd}).(*gitBackend)

	// (1) Cold call: nothing on disk yet.
	snap1, err := be.CloneForRead()
	if err != nil {
		t.Fatalf("first CloneForRead: %v", err)
	}
	if snap1.Dir == "" {
		t.Fatal("expected snap1.Dir to be set, got empty")
	}
	expectedDir := filepath.Join(cacheRoot, "defrost")
	if !filepathHasPrefix(snap1.Dir, expectedDir) {
		t.Errorf("snap1.Dir = %q; expected to be under %q", snap1.Dir, expectedDir)
	}
	if snap1.SHA == "" {
		t.Error("snap1.SHA empty; expected commit SHA")
	}
	if snap1.Reset {
		t.Error("snap1.Reset = true; expected false on first clone")
	}

	// (2) Warm call: should reuse the same worktree, fetch with no new
	// commits → SHA unchanged, Reset still false.
	snap2, err := be.CloneForRead()
	if err != nil {
		t.Fatalf("second CloneForRead: %v", err)
	}
	if snap2.Dir != snap1.Dir {
		t.Errorf("warm call returned a different dir: snap1=%q snap2=%q", snap1.Dir, snap2.Dir)
	}
	if snap2.SHA != snap1.SHA {
		t.Errorf("warm call SHA changed without remote write: snap1.SHA=%q snap2.SHA=%q", snap1.SHA, snap2.SHA)
	}
	if snap2.Reset {
		t.Error("snap2.Reset = true; expected false (no force-push)")
	}

	// (3) Push a new commit on the same branch. Warm call should fast-
	// forward; Reset still false.
	addCommitOnBranch(t, bare, branchOf(be), "second.txt", "second")
	snap3, err := be.CloneForRead()
	if err != nil {
		t.Fatalf("third CloneForRead: %v", err)
	}
	if snap3.Dir != snap1.Dir {
		t.Errorf("after fetch dir changed: snap3.Dir=%q expected %q", snap3.Dir, snap1.Dir)
	}
	if snap3.SHA == snap1.SHA {
		t.Error("snap3.SHA didn't advance after a new remote commit")
	}
	if snap3.Reset {
		t.Error("snap3.Reset = true; expected false on fast-forward")
	}

	// (4) Force-push an unrelated history (orphan commit) onto the same
	// branch. Next CloneForRead should detect non-fast-forward and
	// return Reset=true with a fresh SHA.
	forceResetBranch(t, bare, branchOf(be), "rewritten.txt", "rewritten")
	snap4, err := be.CloneForRead()
	if err != nil {
		t.Fatalf("fourth CloneForRead: %v", err)
	}
	if !snap4.Reset {
		t.Error("snap4.Reset = false; expected true after force-push")
	}
	if snap4.SHA == snap3.SHA {
		t.Error("snap4.SHA matches old tip; expected new SHA after force-push")
	}
	if snap4.Dir != snap1.Dir {
		t.Errorf("after force-reset Dir changed: snap4.Dir=%q expected %q", snap4.Dir, snap1.Dir)
	}
}

// TestGitBackend_RemoteHeadSHA_MatchesCloneSHA confirms that the
// cheap `ls-remote` probe returns the same SHA that the working tree
// would have after CloneForRead — that's the invariant the read path
// relies on for short-circuiting.
func TestGitBackend_RemoteHeadSHA_MatchesCloneSHA(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bare := setupBareRemote(t)
	cwd := setupRepoWithOrigin(t, bare)
	setupCacheRedirect(t)
	be := New(Options{RepoDir: cwd}).(*gitBackend)

	remoteSHA, err := be.RemoteHeadSHA()
	if err != nil {
		t.Fatalf("RemoteHeadSHA: %v", err)
	}
	if remoteSHA == "" {
		t.Fatal("RemoteHeadSHA returned empty when branch exists on remote")
	}
	snap, err := be.CloneForRead()
	if err != nil {
		t.Fatalf("CloneForRead: %v", err)
	}
	if snap.SHA != remoteSHA {
		t.Errorf("ls-remote SHA %q != snap.SHA %q", remoteSHA, snap.SHA)
	}
}

// TestGitBackend_RemoteHeadSHA_NoBranch returns "" for a remote that
// exists but has no data branch yet (first run on a new repo).
func TestGitBackend_RemoteHeadSHA_NoBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bare := emptyBareRemote(t) // bare repo with no branches
	cwd := setupRepoWithOriginNoData(t, bare)
	setupCacheRedirect(t)
	be := New(Options{RepoDir: cwd}).(*gitBackend)

	remoteSHA, err := be.RemoteHeadSHA()
	if err != nil {
		t.Fatalf("RemoteHeadSHA: %v", err)
	}
	if remoteSHA != "" {
		t.Errorf("RemoteHeadSHA = %q; expected empty for a remote with no data branch", remoteSHA)
	}
}

// --- helpers ---

func branchOf(b *gitBackend) string { return b.dataBranch() }

func filepathHasPrefix(p, prefix string) bool {
	pa, _ := filepath.Abs(p)
	pra, _ := filepath.Abs(prefix)
	return len(pa) >= len(pra) && pa[:len(pra)] == pra
}

// setupBareRemote builds a bare repo at $TMP/remote.git with one
// commit on the default _defrost data branch. Returns the bare path.
func setupBareRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "--bare", "-b", DefaultDataBranch, bare)

	// Stage a commit on the data branch via a temporary working copy
	// (the bare has no branch ref yet, so a direct `git clone -b
	// <branch>` would fail). Init the working repo with the same
	// branch name as default, commit, then push.
	work := filepath.Join(t.TempDir(), "seed-work")
	mustGit(t, "", "init", "-q", "-b", DefaultDataBranch, work)
	configBot(t, work)
	if err := os.WriteFile(filepath.Join(work, "first.txt"), []byte("first"), 0o644); err != nil {
		t.Fatalf("write first.txt: %v", err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-q", "-m", "first")
	mustGit(t, work, "remote", "add", "origin", bare)
	mustGit(t, work, "push", "-q", "origin", DefaultDataBranch)
	return bare
}

// emptyBareRemote returns a bare repo with no branches at all — what
// you'd see on a brand-new origin where `defrost exec` hasn't yet
// pushed.
func emptyBareRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "empty.git")
	mustGit(t, "", "init", "--bare", bare)
	return bare
}

// setupRepoWithOrigin builds a working repo whose origin points at the
// bare repo. The persist backend reads `origin` from the working repo
// to find the data branch, so this is what gitBackend resolves.
func setupRepoWithOrigin(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, "", "init", "-q", dir)
	configBot(t, dir)
	mustGit(t, dir, "commit", "--allow-empty", "-q", "-m", "init")
	mustGit(t, dir, "remote", "add", "origin", originURL)
	return dir
}

func setupRepoWithOriginNoData(t *testing.T, originURL string) string {
	t.Helper()
	return setupRepoWithOrigin(t, originURL)
}

func setupCacheRedirect(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "xdg-cache")
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("HOME", filepath.Dir(root)) // macOS UserCacheDir falls back here
	return root
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

// addCommitOnBranch clones the bare remote, adds a commit on branch,
// pushes (fast-forward).
func addCommitOnBranch(t *testing.T, bare, branch, name, content string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "ff-work")
	mustGit(t, "", "clone", "--branch", branch, bare, work)
	configBot(t, work)
	if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-q", "-m", "ff")
	mustGit(t, work, "push", "-q", "origin", branch)
}

// forceResetBranch builds an orphan commit on the same branch name
// and force-pushes it. Mirrors what `defrost drop history` does to
// the data branch.
func forceResetBranch(t *testing.T, bare, branch, name, content string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "force-work")
	mustGit(t, "", "init", "-q", "-b", branch, work)
	configBot(t, work)
	if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-q", "-m", "rewritten")
	mustGit(t, work, "remote", "add", "origin", bare)
	mustGit(t, work, "push", "--force", "origin", branch)
}
