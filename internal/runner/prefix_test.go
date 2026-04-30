package runner

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bjk95/defrost/internal/models"
)

func TestRepoRelCwdAtRepoRoot(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	t.Chdir(dir)

	if got := RepoRelCwd(); got != "" {
		t.Errorf("RepoRelCwd at repo root: got %q, want %q", got, "")
	}
}

func TestRepoRelCwdInSubdir(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	sub := filepath.Join(root, "examples", "typescript")
	mkdirAll(t, sub)
	t.Chdir(sub)

	want := filepath.Join("examples", "typescript")
	if got := RepoRelCwd(); got != want {
		t.Errorf("RepoRelCwd in subdir: got %q, want %q", got, want)
	}
}

func TestRepoRelCwdNotInRepo(t *testing.T) {
	dir := t.TempDir()
	// Block git from walking above dir's parent. With no .git in dir or
	// its (test-temp) parent, `git rev-parse --show-toplevel` will fail
	// and RepoRelCwd must return "".
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)

	if got := RepoRelCwd(); got != "" {
		t.Errorf("RepoRelCwd outside repo: got %q, want %q", got, "")
	}
}

func TestApplyRepoPrefixEmptyPrefix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)

	results := []models.TestResult{{Id: "a"}, {Id: "b"}}
	ApplyRepoPrefix(results)

	if results[0].Id != "a" || results[1].Id != "b" {
		t.Errorf("ApplyRepoPrefix with empty prefix mutated IDs: %+v", results)
	}
}

func TestApplyRepoPrefixDecorates(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	sub := filepath.Join(root, "examples", "typescript")
	mkdirAll(t, sub)
	t.Chdir(sub)

	results := []models.TestResult{
		{Id: "basics.test.ts::adds correctly"},
		{Id: "basics.test.ts::subtracts"},
	}
	ApplyRepoPrefix(results)

	prefix := filepath.Join("examples", "typescript") + "¬"
	if results[0].Id != prefix+"basics.test.ts::adds correctly" {
		t.Errorf("results[0].Id = %q, want %q", results[0].Id, prefix+"basics.test.ts::adds correctly")
	}
	if results[1].Id != prefix+"basics.test.ts::subtracts" {
		t.Errorf("results[1].Id = %q, want %q", results[1].Id, prefix+"basics.test.ts::subtracts")
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "--quiet", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := exec.Command("mkdir", "-p", dir).Run(); err != nil {
		t.Fatalf("mkdir -p %s: %v", dir, err)
	}
}
