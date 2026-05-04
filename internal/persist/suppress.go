package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
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

// readSuppressions reads <repoDir>/.defrost/suppressions.json. An
// absent file is not an error — it returns an empty slice. A
// present-but-malformed file IS an error: defrost must not silently
// treat corruption as "no suppressions" (which would un-suppress
// everything).
func readSuppressions(opts Options) ([]string, error) {
	path := LocalSuppressionsPath(opts)
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

// writeSuppressions sorts and dedupes ids, then writes the JSON
// document with two-space indent and a trailing newline. Sorting on
// every write keeps diffs minimal regardless of input order.
//
// Suppressions live in the user's working tree at
// <repoDir>/.defrost/suppressions.json. The user is responsible for
// `git add` + `git commit` afterwards as part of their normal
// workflow — we deliberately don't auto-commit so suppression
// changes are reviewable in PRs.
func writeSuppressions(opts Options, ids []string) error {
	if err := EnsureLocalDir(opts); err != nil {
		return err
	}
	doc := suppressionsDoc{Schema: suppressionsSchema, TestIDs: sortAndDedupe(ids)}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal suppressions: %w", err)
	}
	return os.WriteFile(LocalSuppressionsPath(opts), append(b, '\n'), 0o644)
}

// Both backends share the same suppressions implementation: read/
// write <repoDir>/.defrost/suppressions.json. There's no git here.
//
// Was this on the data branch in a prior iteration? Yes. Why move it?
// Three reasons: (1) suppressions are project-scoped knowledge that
// belongs in the same review process as the code itself; (2) reads are
// hot-path (every `defrost exec` checks the list to decide exit-code
// rewrite) and a clone-per-read is wasteful; (3) the gitBackend's
// commit/push/retry machinery for suppressions was the most complex
// code in this package for a feature with no concurrency requirement.

func (b *fileBackend) GetSuppressions() ([]string, error) { return readSuppressions(b.opts) }

func (b *fileBackend) UpdateSuppressions(mutate func([]string) []string, _ string) error {
	return updateSuppressions(b.opts, mutate)
}

func (b *gitBackend) GetSuppressions() ([]string, error) { return readSuppressions(b.opts) }

func (b *gitBackend) UpdateSuppressions(mutate func([]string) []string, _ string) error {
	return updateSuppressions(b.opts, mutate)
}

// updateSuppressions reads the current list, applies mutate, and
// writes the result. No-ops are skipped (no file write when the
// list is unchanged).
func updateSuppressions(opts Options, mutate func([]string) []string) error {
	cur, err := readSuppressions(opts)
	if err != nil {
		return err
	}
	next := sortAndDedupe(mutate(cur))
	if stringSlicesEqual(cur, next) {
		return nil
	}
	return writeSuppressions(opts, next)
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
