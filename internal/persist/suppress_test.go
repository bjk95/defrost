package persist

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSuppressions_FileBased exercises the file-based suppressions
// flow used by --dev mode. Reads/writes go directly to
// <repo>/.defrost/suppressions.json with no git involvement.
func TestSuppressions_FileBased(t *testing.T) {
	repo := t.TempDir()
	be := New(Options{RepoDir: repo, Dev: true})

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
}

// TestSuppressions_GitBased exercises the gitBackend's data-branch
// flow against a local bare repo: add (creates branch), read, remove,
// read.
func TestSuppressions_GitBased(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bare := emptyBareRemote(t)
	cwd := setupRepoWithOrigin(t, bare)

	be := New(Options{RepoDir: cwd})

	// Empty initial state — branch doesn't exist on origin yet.
	ids, err := be.GetSuppressions()
	if err != nil {
		t.Fatalf("get (empty): %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list initially, got %v", ids)
	}

	// Add — this creates the data branch from scratch (writeSeed writes
	// README + .gitignore; UpdateSuppressions appends suppressions.json
	// in the same commit).
	if err := be.UpdateSuppressions(func(cur []string) []string {
		return append(cur, "pkg/TestB", "pkg/TestA")
	}, "test: seed suppressions"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Read again — picks up the just-pushed list via clone fallback
	// (the persistent worktree at <repo>/.defrost/ doesn't exist yet
	// because we haven't called CloneForRead).
	ids, err = be.GetSuppressions()
	if err != nil {
		t.Fatalf("get (after add): %v", err)
	}
	want := []string{"pkg/TestA", "pkg/TestB"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("after add: got %v, want %v", ids, want)
	}

	// Now materialise the persistent worktree (simulating a serve session).
	gb := be.(*gitBackend)
	if _, err := gb.CloneForRead(); err != nil {
		t.Fatalf("CloneForRead: %v", err)
	}

	// Read again — fast path via the worktree.
	ids, err = be.GetSuppressions()
	if err != nil {
		t.Fatalf("get (after CloneForRead): %v", err)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("worktree fast path: got %v, want %v", ids, want)
	}

	// Remove.
	if err := be.UpdateSuppressions(func(cur []string) []string {
		out := cur[:0]
		for _, s := range cur {
			if s != "pkg/TestA" {
				out = append(out, s)
			}
		}
		return out
	}, "test: drop pkg/TestA"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// The worktree we cloned earlier is now stale; the next read
	// returns its (cached) value. Force a refresh by re-cloning.
	if _, err := gb.CloneForRead(); err != nil {
		t.Fatalf("CloneForRead (refresh): %v", err)
	}
	ids, err = be.GetSuppressions()
	if err != nil {
		t.Fatalf("get (after remove): %v", err)
	}
	want = []string{"pkg/TestB"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("after remove: got %v, want %v", ids, want)
	}
}
