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
