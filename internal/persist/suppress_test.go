package persist

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSuppressions_FileBased exercises the new file-based suppression
// implementation: both git-mode and dev-mode backends read/write the
// same canonical location at <repoDir>/.defrost/suppressions.json.
//
// Was previously git-backed for non-dev mode (commit + push to the
// data branch); now it's just local file IO. The user is expected to
// review and commit the diff via their normal workflow.
func TestSuppressions_FileBased(t *testing.T) {
	for _, tc := range []struct {
		name string
		dev  bool
	}{
		{"file_backend", true},
		{"git_backend", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			be := New(Options{RepoDir: repo, Dev: tc.dev})

			// Empty initial state.
			ids, err := be.GetSuppressions()
			if err != nil {
				t.Fatalf("get (empty): %v", err)
			}
			if len(ids) != 0 {
				t.Errorf("expected empty list initially, got %v", ids)
			}

			// Add two ids; verify deduplication and sorting.
			err = be.UpdateSuppressions(func(cur []string) []string {
				return append(cur, "pkg/TestB", "pkg/TestA", "pkg/TestB")
			}, "")
			if err != nil {
				t.Fatalf("add: %v", err)
			}
			ids, err = be.GetSuppressions()
			if err != nil {
				t.Fatalf("get (after add): %v", err)
			}
			want := []string{"pkg/TestA", "pkg/TestB"}
			if !reflect.DeepEqual(ids, want) {
				t.Errorf("after add: got %v, want %v", ids, want)
			}

			// File should have been created at the canonical path.
			expected := filepath.Join(repo, ".defrost", "suppressions.json")
			if _, err := os.Stat(expected); err != nil {
				t.Errorf("expected suppressions.json at %s: %v", expected, err)
			}

			// .gitignore should also have been auto-created.
			gi := filepath.Join(repo, ".defrost", ".gitignore")
			if _, err := os.Stat(gi); err != nil {
				t.Errorf("expected .gitignore at %s: %v", gi, err)
			}

			// Remove one id.
			err = be.UpdateSuppressions(func(cur []string) []string {
				out := cur[:0]
				for _, s := range cur {
					if s != "pkg/TestA" {
						out = append(out, s)
					}
				}
				return out
			}, "")
			if err != nil {
				t.Fatalf("remove: %v", err)
			}
			ids, err = be.GetSuppressions()
			if err != nil {
				t.Fatalf("get (after remove): %v", err)
			}
			want = []string{"pkg/TestB"}
			if !reflect.DeepEqual(ids, want) {
				t.Errorf("after remove: got %v, want %v", ids, want)
			}
		})
	}
}
