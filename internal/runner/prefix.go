package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bjk95/defrost/internal/models"
)

// RepoRelCwd returns the path from the git repo root to the current
// working directory, suitable for prefixing test IDs to make them
// globally unique within the repository. Returns "" when:
//   - not inside a git repo,
//   - cwd is the repo root,
//   - any git or filesystem call fails.
//
// The empty return is the "no decoration" signal — callers must treat
// it as such, not as an error.
//
// Both cwd and the git toplevel are passed through filepath.EvalSymlinks
// so callers don't see spurious "../" segments on systems where one
// path is symlinked and the other isn't (notably macOS, where the
// default temp dir is /var/folders/... → /private/var/folders/...).
func RepoRelCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return ""
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ""
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || rel == "." {
		return ""
	}
	return rel
}

// ApplyRepoPrefix decorates each result's Id with RepoRelCwd() + "¬".
// No-op when RepoRelCwd returns "" (run from repo root, or not in a
// repo). Mutates results in place.
//
// `¬` (U+00AC) is used as the separator because it never appears in
// path separators, Python module names, JS test descriptions, or shell
// metacharacters defrost touches — collision-free as a sentinel.
func ApplyRepoPrefix(results []models.TestResult) {
	prefix := RepoRelCwd()
	if prefix == "" {
		return
	}
	for i := range results {
		results[i].Id = prefix + "¬" + results[i].Id
	}
}
