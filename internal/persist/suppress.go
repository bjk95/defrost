package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const suppressionsFile = "suppressions.json"

// suppressionsDoc is the on-disk shape of suppressions.json. The schema
// field future-proofs the move to richer entries (reason/expiry/etc.) if
// a later iteration needs them.
type suppressionsDoc struct {
	Schema  int      `json:"schema"`
	TestIDs []string `json:"test_ids"`
}

const suppressionsSchema = 1

// readSuppressionsFile returns the suppression list at dir/suppressions.json.
// An absent file is not an error: it returns an empty slice. A present-but-
// malformed file IS an error — defrost must not silently treat corruption as
// "no suppressions" (which would un-suppress everything).
func readSuppressionsFile(dir string) ([]string, error) {
	path := filepath.Join(dir, suppressionsFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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

// writeSuppressionsFile sorts and dedupes ids, then writes the JSON
// document to dir/suppressions.json with two-space indent and a trailing
// newline. Sorting on every write keeps diffs minimal regardless of input
// order.
func writeSuppressionsFile(dir string, ids []string) error {
	doc := suppressionsDoc{Schema: suppressionsSchema, TestIDs: sortAndDedupe(ids)}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal suppressions: %w", err)
	}
	path := filepath.Join(dir, suppressionsFile)
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func (b *fileBackend) GetSuppressions() ([]string, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	return readSuppressionsFile(b.dir)
}

func (b *fileBackend) UpdateSuppressions(mutate func([]string) []string, _ string) error {
	if err := b.InitialisePersistence(); err != nil {
		return err
	}
	cur, err := readSuppressionsFile(b.dir)
	if err != nil {
		return err
	}
	return writeSuppressionsFile(b.dir, mutate(cur))
}

func (b *gitBackend) GetSuppressions() ([]string, error) {
	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}

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
	_ = os.Remove(workDir) // clone wants the path missing

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, fmt.Errorf("clone data branch: %w", err)
	}
	return readSuppressionsFile(workDir)
}

func (b *gitBackend) UpdateSuppressions(mutate func([]string) []string, msg string) error {
	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}

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

	apply := func() (changed bool, err error) {
		cur, err := readSuppressionsFile(workDir)
		if err != nil {
			return false, err
		}
		next := mutate(cur)
		next = sortAndDedupe(next)

		prevBytes, _ := os.ReadFile(filepath.Join(workDir, suppressionsFile))
		if err := writeSuppressionsFile(workDir, next); err != nil {
			return false, err
		}
		newBytes, err := os.ReadFile(filepath.Join(workDir, suppressionsFile))
		if err != nil {
			return false, err
		}
		return string(prevBytes) != string(newBytes), nil
	}

	changed, err := apply()
	if err != nil {
		return err
	}
	if !changed && branchExisted {
		// No-op: file content unchanged and the branch already exists,
		// so there is nothing to commit or push.
		return nil
	}

	if err := commitAll(workDir, msg); err != nil {
		return err
	}

	// Retry-on-conflict: discard the local commit, fetch the remote tip,
	// hard-reset to it, re-apply the mutation closure, and re-commit. This
	// works for suppressions.json (single canonical file, NOT covered by
	// the merge=union driver in .gitattributes) — a three-way merge of
	// JSON would corrupt the file, so we replay the user's intent against
	// the latest tree instead. Two concurrent add calls for different IDs
	// both land in the final list this way.
	var lastErr error
	for attempt := 1; attempt <= maxPushAttempts; attempt++ {
		err := pushBranch(workDir, branch)
		if err == nil {
			return nil
		}
		lastErr = err
		if !branchExisted || !isNonFastForward(err) {
			return err
		}
		refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
		if _, err := runGit(workDir, "fetch", "--quiet", "origin", refspec); err != nil {
			return fmt.Errorf("fetch after push conflict (attempt %d): %w", attempt, err)
		}
		if _, err := runGit(workDir, "reset", "--hard", "refs/remotes/origin/"+branch); err != nil {
			return fmt.Errorf("reset to remote tip (attempt %d): %w", attempt, err)
		}
		if _, err := apply(); err != nil {
			return err
		}
		if err := commitAll(workDir, msg); err != nil {
			return err
		}
	}
	return fmt.Errorf("push failed after %d retries: %w", maxPushAttempts, lastErr)
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
