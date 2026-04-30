package persist

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestReadSuppressionsFile_AbsentReturnsEmpty(t *testing.T) {
	got, err := readSuppressionsFile(t.TempDir())
	if err != nil {
		t.Fatalf("readSuppressionsFile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestWriteSuppressionsFile_SortsAndDedupes(t *testing.T) {
	dir := t.TempDir()
	if err := writeSuppressionsFile(dir, []string{"b", "a", "a", "c"}); err != nil {
		t.Fatalf("writeSuppressionsFile: %v", err)
	}

	got, err := readSuppressionsFile(dir)
	if err != nil {
		t.Fatalf("readSuppressionsFile: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip: want %v got %v", want, got)
	}
}

func TestWriteSuppressionsFile_StableSortAcrossWrites(t *testing.T) {
	dir := t.TempDir()
	if err := writeSuppressionsFile(dir, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "suppressions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSuppressionsFile(dir, []string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "suppressions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("write order should not affect bytes:\n first:  %s\n second: %s", first, second)
	}
}

func TestReadSuppressionsFile_MalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "suppressions.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSuppressionsFile(dir); err == nil {
		t.Errorf("expected error reading malformed file, got nil")
	}
}

func TestSortAndDedupe(t *testing.T) {
	got := sortAndDedupe([]string{"b", "a", "b", "c", "a"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("not sorted: %v", got)
	}
}

func TestFileBackend_SuppressionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	b := &fileBackend{dir: filepath.Join(dir, "scratch")}

	got, err := b.GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions on empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}

	addX := func(cur []string) []string { return append(cur, "x") }
	if err := b.UpdateSuppressions(addX, "ignored"); err != nil {
		t.Fatalf("UpdateSuppressions add x: %v", err)
	}
	addY := func(cur []string) []string { return append(cur, "y") }
	if err := b.UpdateSuppressions(addY, "ignored"); err != nil {
		t.Fatalf("UpdateSuppressions add y: %v", err)
	}

	got, err = b.GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions after writes: %v", err)
	}
	want := []string{"x", "y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}
}

func TestGitBackend_Suppressions_EmptyWhenBranchAbsent(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	got, err := New(Options{RepoDir: repoDir}).GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty (no data branch yet), got %v", got)
	}
}

func TestGitBackend_Suppressions_RoundTrip(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	addX := func(cur []string) []string { return append(cur, "github.com/x/p/TestX") }
	if err := New(Options{RepoDir: repoDir}).UpdateSuppressions(addX, "suppress: add X"); err != nil {
		t.Fatalf("UpdateSuppressions add X: %v", err)
	}

	got, err := New(Options{RepoDir: repoDir}).GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions: %v", err)
	}
	want := []string{"github.com/x/p/TestX"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}

	// Verify the file landed on the data branch.
	verify := cloneDataBranch(t, originURL)
	b, err := os.ReadFile(filepath.Join(verify, "suppressions.json"))
	if err != nil {
		t.Fatalf("read suppressions.json: %v", err)
	}
	if !strings.Contains(string(b), "github.com/x/p/TestX") {
		t.Errorf("file does not contain test id:\n%s", b)
	}
}

func TestGitBackend_Suppressions_IdempotentAdd(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	addX := func(cur []string) []string { return append(cur, "X") }
	be := New(Options{RepoDir: repoDir})
	if err := be.UpdateSuppressions(addX, "suppress: add X"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := be.UpdateSuppressions(addX, "suppress: add X"); err != nil {
		t.Fatalf("second add: %v", err)
	}

	// Two adds of the same ID should produce one suppression entry...
	got, err := be.GetSuppressions()
	if err != nil {
		t.Fatalf("GetSuppressions: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"X"}) {
		t.Errorf("want [X], got %v", got)
	}

	// ...and only ONE commit on the data branch (the second should be a
	// no-op because the file content didn't change).
	verify := cloneDataBranch(t, originURL)
	out, err := exec.Command("git", "-C", verify, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v: %s", err, out)
	}
	commits := strings.Count(strings.TrimSpace(string(out)), "\n") + 1
	// Expected: seed commit (initial branch creation) + one suppress commit = 2.
	// If second add added a commit, we'd see 3.
	if commits > 2 {
		t.Errorf("expected at most 2 commits on data branch, got %d:\n%s", commits, out)
	}
}

func TestGitBackend_Suppressions_DevModeUsesScratchDir(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	be := New(Options{RepoDir: repoDir, Dev: true})
	add := func(cur []string) []string { return append(cur, "id1") }
	if err := be.UpdateSuppressions(add, "n/a"); err != nil {
		t.Fatalf("UpdateSuppressions: %v", err)
	}
	scratchPath := filepath.Join(repoDir, DevDir, "suppressions.json")
	if _, err := os.Stat(scratchPath); err != nil {
		t.Errorf("expected %s to exist: %v", scratchPath, err)
	}
}
