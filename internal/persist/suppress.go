package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const suppressionsFile = "suppressions.json"

// suppressionsDoc is the on-disk shape of suppressions.json. The
// schema field future-proofs the move to richer entries
// (reason/expiry/etc.) if a later iteration needs them.
type suppressionsDoc struct {
	Schema  int      `json:"schema"`
	TestIDs []string `json:"test_ids"`
}

const suppressionsSchema = 1

// readSuppressionsFromDir parses dir/suppressions.json. An absent file
// is not an error: it returns an empty slice. A present-but-malformed
// file IS an error — defrost must not silently treat corruption as
// "no suppressions" (which would un-suppress everything).
func readSuppressionsFromDir(dir string) ([]string, error) {
	path := filepath.Join(dir, suppressionsFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	var doc suppressionsDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return doc.TestIDs, nil
}

// writeSuppressionsToDir sorts and dedupes ids, then writes the JSON
// document with two-space indent and a trailing newline. Sorting on
// every write keeps diffs minimal regardless of input order.
func writeSuppressionsToDir(dir string, ids []string) error {
	doc := suppressionsDoc{Schema: suppressionsSchema, TestIDs: sortAndDedupe(ids)}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal suppressions: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, suppressionsFile), append(b, '\n'), 0o644)
}

// fileBackend's suppressions are just a local file — no git involved.
// Lives at <repo>/.defrost/suppressions.json (same path the gitBackend
// reads from, so a switch from --dev to prod doesn't move the file).

func (b *fileBackend) GetSuppressions() ([]string, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	return readSuppressionsFromDir(b.dir)
}

func (b *fileBackend) UpdateSuppressions(mutate func([]string) []string, _ string) error {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return err
	}
	cur, err := readSuppressionsFromDir(b.dir)
	if err != nil {
		return err
	}
	next := sortAndDedupe(mutate(cur))
	if stringSlicesEqual(cur, next) {
		return nil
	}
	return writeSuppressionsToDir(b.dir, next)
}

// gitBackend's suppressions live at the data branch root, alongside
// traces/, metrics/, and logs/. Reads use the persistent worktree at
// <repo>/.defrost/ when available (no git roundtrip needed); writes
// go through a temp clone so they don't race the worktree's
// fetch+reset cycle.

func (b *gitBackend) GetSuppressions() ([]string, error) {
	// Fast path: persistent worktree has the file. The worktree may be
	// stale by minutes (we don't fetch on every read), but for
	// suppression-list reads — which can tolerate seconds-to-minutes
	// of staleness — that's the right trade-off.
	worktreePath := LocalSuppressionsPath(b.opts)
	if _, err := os.Stat(worktreePath); err == nil {
		return readSuppressionsFromDir(LocalRoot(b.opts))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	// Slow path: no worktree yet. Temp-clone, read, drop. This is the
	// first-time-on-this-machine case, e.g. a fresh CI job that
	// never ran `defrost serve`.
	branch := b.dataBranch()
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return nil, err
	}
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []string{}, nil
	}
	workDir, err := os.MkdirTemp("", "defrost-suppress-read-")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)
	_ = os.Remove(workDir) // git clone wants the path absent
	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, fmt.Errorf("clone data branch: %w", err)
	}
	return readSuppressionsFromDir(workDir)
}

func (b *gitBackend) UpdateSuppressions(mutate func([]string) []string, msg string) error {
	branch := b.dataBranch()
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return err
	}
	workDir, err := os.MkdirTemp("", "defrost-suppress-write-")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)

	branchExisted, err := openOrInitDataRepo(workDir, remoteURL, branch)
	if err != nil {
		return err
	}
	if !branchExisted {
		if err := writeSeed(workDir); err != nil {
			return err
		}
	}
	return updateSuppressionsInWorkDir(workDir, branch, mutate, msg)
}

// updateSuppressionsInWorkDir handles the apply/commit/push/retry
// cycle against a workdir that already holds a checkout of the data
// branch. Two concurrent `defrost suppress add` calls land both IDs
// in the final list via fetch-rebase-replay rather than three-way
// merging the JSON (which would corrupt the file).
func updateSuppressionsInWorkDir(workDir, branch string, mutate func([]string) []string, msg string) error {
	apply := func() (changed bool, err error) {
		cur, err := readSuppressionsFromDir(workDir)
		if err != nil {
			return false, err
		}
		next := sortAndDedupe(mutate(cur))
		// cur is already canonical because we always write
		// sorted+deduped (or it's [] for an absent file). Compare
		// lists, not file bytes — otherwise a no-op mutation on a
		// fresh branch (where prevBytes is empty but newBytes is the
		// empty-list JSON) would falsely report "changed" and create
		// the data branch for a no-op operation.
		if stringSlicesEqual(cur, next) {
			return false, nil
		}
		if err := writeSuppressionsToDir(workDir, next); err != nil {
			return false, err
		}
		return true, nil
	}

	changed, err := apply()
	if err != nil {
		return err
	}
	if !changed {
		// Genuine no-op (e.g. add of an existing ID, or remove of a
		// missing ID). Skip commit + push so we don't pollute history
		// or create an empty branch on the remote.
		return nil
	}

	if err := commitAll(workDir, msg); err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= maxPushAttempts; attempt++ {
		err := pushBranch(workDir, branch)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isNonFastForward(err) {
			return err
		}
		// Non-fast-forward at this point means another writer raced
		// us. Recover by fetching the winner's tip, hard-resetting
		// to it, and replaying the user's intent (the mutate
		// closure) against the new state.
		refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
		if _, err := runGit(workDir, "fetch", "--quiet", "origin", refspec); err != nil {
			return fmt.Errorf("fetch after push conflict (attempt %d): %w", attempt, err)
		}
		if _, err := runGit(workDir, "reset", "--hard", "refs/remotes/origin/"+branch); err != nil {
			return fmt.Errorf("reset to remote tip (attempt %d): %w", attempt, err)
		}
		changed, err := apply()
		if err != nil {
			return err
		}
		if !changed {
			// The rebased tip already reflects the user's intent
			// (e.g. two concurrent `add X` calls where the winner
			// already added X). Treat as success rather than calling
			// commitAll on an empty change.
			return nil
		}
		if err := commitAll(workDir, msg); err != nil {
			return err
		}
	}
	return fmt.Errorf("push failed after %d retries: %w", maxPushAttempts, lastErr)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortAndDedupe(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
