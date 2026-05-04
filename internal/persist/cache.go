package persist

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Snapshot is what CloneForRead returns: the on-disk worktree of the
// data branch and the snapshot's identity for caching.
//
// Dir is the worktree root (<repo>/.defrost) — also the signals
// directory, since traces/, metrics/, logs/, and suppressions.json
// all live at the branch root. "" means no data (branch missing on
// origin, or scratch dir absent in dev mode); SHA is "" in that case
// too.
//
// SHA is the commit at the tip of the data branch (40-char hex).
// Used by callers as the key against which to compare a previously-
// recorded "last hydrated SHA" — when they match, no walk is needed.
// "" in dev mode (file backend has no commit identity).
//
// Reset is true when the local persistent worktree was force-reset to
// match a rewritten remote — typically after `defrost drop history`
// rewrites the data branch via orphan commit. Callers that maintain
// derived state (DuckDB rows, hydration_state) MUST drop and rebuild
// that state when Reset is true.
type Snapshot struct {
	Dir   string
	SHA   string
	Reset bool
}

// LocalDir is the path inside the user's repo where defrost lives.
// The directory itself IS a clone of the data branch — same model
// as .git/, defrost-managed and excluded from the main repo's
// commits.
//
// Layout under <repoDir>/.defrost/ (i.e. the data branch's worktree):
//
//	.git/              worktree's git directory (clone of _defrost)
//	.gitignore         committed; ignores cache.duckdb
//	README.md          committed
//	traces/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst   committed
//	metrics/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst  committed
//	logs/<YYYY>/<MM>/<DD>/<trace-id>.otlp.pb.zst     committed
//	suppressions.json  committed
//	cache.duckdb       local-only, gitignored on the data branch
const LocalDir = ".defrost"

// dataBranchGitignore is the .gitignore committed at the data branch
// root. It excludes the local-only DuckDB cache files that share the
// worktree with the committed data files.
const dataBranchGitignore = `# .gitignore on the _defrost branch — keeps the per-machine DuckDB
# read cache out of commits while letting it share the worktree at
# <repo>/.defrost/.
/cache.duckdb
/cache.duckdb.wal
/cache.duckdb.tmp
`

// userRepoIgnoreLine is the line we add to the user's main-repo
// .gitignore so .defrost/ doesn't show up as untracked. Matches a
// directory at the repo root only.
const userRepoIgnoreLine = "/.defrost/"

// userRepoIgnoreBlock is the comment + line we append to the user's
// main-repo .gitignore. The comment makes the addition self-
// documenting so a reviewer doesn't have to guess where it came from.
const userRepoIgnoreBlock = `
# defrost: data branch worktree (test/eval history). Safe to remove
# this entry if you uninstall defrost.
` + userRepoIgnoreLine + "\n"

// LocalRoot returns <repoDir>/.defrost — the path of the data
// branch's worktree on this machine. Both signal files and
// suppressions.json live directly under this root, so it doubles as
// the "where to read OTLP files" path.
func LocalRoot(opts Options) string {
	repo := opts.RepoDir
	if repo == "" {
		repo = "."
	}
	return filepath.Join(repo, LocalDir)
}

// CacheRoot is kept as an alias for LocalRoot for callers that
// adopted the older name.
func CacheRoot(opts Options) (string, error) { return LocalRoot(opts), nil }

// LocalSuppressionsPath is the path to suppressions.json at the data
// branch's worktree root.
func LocalSuppressionsPath(opts Options) string {
	return filepath.Join(LocalRoot(opts), suppressionsFile)
}

// LocalCacheDBPath is the local-only DuckDB cache file. Lives inside
// the worktree but is excluded from commits via the data branch's own
// .gitignore.
func LocalCacheDBPath(opts Options) string {
	return filepath.Join(LocalRoot(opts), "cache.duckdb")
}

// EnsureUserRepoIgnoresDefrost appends "/.defrost/" to <repoDir>/.gitignore
// if it isn't already present. Idempotent — does nothing on subsequent
// calls. Skipped silently when --repo-dir doesn't have a .gitignore at
// all (e.g. a non-git directory invoking defrost-ci); the caller
// shouldn't fail on its return.
//
// Why we do this: <repo>/.defrost/ is a worktree of a separate branch
// (similar to .git/). Without ignoring it, every git status in the
// user's main repo would show .defrost/ as untracked, and a careless
// `git add .` would commit megabytes of OTLP files into the source
// tree.
func EnsureUserRepoIgnoresDefrost(opts Options) error {
	repo := opts.RepoDir
	if repo == "" {
		repo = "."
	}
	gi := filepath.Join(repo, ".gitignore")
	existing, err := os.ReadFile(gi)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// No .gitignore at all → write one with just our entry.
		return os.WriteFile(gi, []byte(strings.TrimLeft(userRepoIgnoreBlock, "\n")), 0o644)
	case err != nil:
		return fmt.Errorf("read %s: %w", gi, err)
	}
	// Skip if the line is already present in any form (`/.defrost/`,
	// `/.defrost`, `.defrost/`, `.defrost`). All of these keep the
	// directory out of commits in practice.
	for _, line := range strings.Split(string(existing), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		switch t {
		case "/.defrost/", "/.defrost", ".defrost/", ".defrost":
			return nil
		}
	}
	// Append. Ensure a separating newline if the file didn't end with one.
	out := existing
	if len(out) > 0 && !strings.HasSuffix(string(out), "\n") {
		out = append(out, '\n')
	}
	out = append(out, []byte(userRepoIgnoreBlock)...)
	return os.WriteFile(gi, out, 0o644)
}

// dataCacheDir is the worktree root the gitBackend clones / fetches
// into. Returns <repo>/.defrost — the directory IS the worktree.
func (b *gitBackend) dataCacheDir() (string, error) {
	return LocalRoot(b.opts), nil
}

// RemoteHeadSHA returns the commit SHA at the tip of the data branch
// on origin. Returns ("", nil) when the branch doesn't exist on
// origin — the caller treats that as "no data yet."
//
// Cheap (one HTTPS round-trip via `git ls-remote`); intended as the
// freshness probe before any clone/fetch.
func (b *gitBackend) RemoteHeadSHA() (string, error) {
	branch := b.dataBranch()
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return "", err
	}
	out, err := runGit("", "ls-remote", remoteURL, "refs/heads/"+branch)
	if err != nil {
		var ge *gitErr
		if errors.As(err, &ge) && ge.code == 2 {
			return "", nil
		}
		return "", fmt.Errorf("ls-remote %s: %w", remoteURL, err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", nil
	}
	parts := strings.Fields(out)
	if len(parts) < 1 {
		return "", nil
	}
	return parts[0], nil
}

// fileBackend has no remote, so the SHA probe is always empty.
func (b *fileBackend) RemoteHeadSHA() (string, error) { return "", nil }

// localHeadSHA returns the SHA at HEAD inside the persistent worktree
// at dir, or "" if dir doesn't yet contain a checkout.
func localHeadSHA(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return ""
	}
	out, err := runGit(dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// removeAllPreservingParent rm -rf's path. Keeps the function name for
// backward source compatibility with earlier iterations.
func removeAllPreservingParent(path string) error {
	if path == "" {
		return nil
	}
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
