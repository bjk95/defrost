package persist

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Snapshot is what CloneForRead returns: the on-disk working tree
// directory and the snapshot's identity for caching.
//
// Dir is the local working tree. "" means no data (branch missing on
// origin, or scratch dir absent in dev mode); SHA is "" in that case
// too.
//
// SHA is the commit at the tip of the data branch (40-char hex).
// Used by callers as the key against which to compare a previously-
// recorded "last hydrated SHA" — when they match, no walk is needed.
// "" in dev mode (file backend has no commit identity).
//
// Reset is true when the local persistent cache was force-reset to
// match a rewritten remote — typically after `defrost drop history`
// rewrites the data branch via orphan commit. Callers that maintain
// derived state (DuckDB rows, hydration_state) MUST drop and rebuild
// that state when Reset is true; the file paths they previously
// recorded may now refer to deleted blobs, and rows in the materialised
// tables may no longer correspond to anything on disk.
type Snapshot struct {
	Dir   string
	SHA   string
	Reset bool
}

// CacheRoot returns the per-repo cache root used for the persistent
// data-branch worktree (and, in the duckdb Querier, for cache.duckdb).
//
// Layout:
//
//	$UserCacheDir/defrost/<repo-hash>/
//	  ├─ data/         persistent worktree of the data branch
//	  └─ cache.duckdb  (owned by the query/duckdb package)
//
// repo-hash is sha256(originURL)[:8] in normal mode, or
// sha256("dev:"+repoDir)[:8] in --dev mode. Stable across branch
// switches, isolated per remote.
func CacheRoot(opts Options) (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	var seed string
	if opts.Dev {
		seed = "dev:" + opts.RepoDir
	} else {
		// Best-effort: if origin lookup fails, fall back to repo path.
		// The hash is just a per-cache identifier, not security-relevant.
		origin, err := readOriginURL(opts.RepoDir)
		if err != nil || origin == "" {
			seed = "repo:" + opts.RepoDir
		} else {
			seed = origin
		}
	}
	h := sha256.Sum256([]byte(seed))
	id := hex.EncodeToString(h[:8])
	return filepath.Join(root, "defrost", id), nil
}

// dataCacheDir is the worktree path inside CacheRoot.
func (b *gitBackend) dataCacheDir() (string, error) {
	root, err := CacheRoot(b.opts)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "data"), nil
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
	// `ls-remote` output is "<sha>\t<ref>". Take the SHA half.
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

// removeAllPreservingParent rm -rf's path but keeps its parent dir.
func removeAllPreservingParent(path string) error {
	if path == "" {
		return nil
	}
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
