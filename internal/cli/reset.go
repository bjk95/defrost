package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjk95/defrost/internal/cliout"
	"github.com/bjk95/defrost/internal/persist"
)

// HandleReset implements `defrost reset` — the escape hatch when the
// local <repo>/.defrost/ cache is in a state CloneForRead can't
// refresh from. Deletes the directory and immediately re-clones the
// data branch from origin so the user is left with a fresh, usable
// worktree (not an absent one that the next command has to bootstrap).
// Local-only; the data branch on origin is untouched.
//
// Confirmation prompt is on by default because deleting `.defrost/`
// also removes cache.duckdb (the dashboard's read cache). Pass --yes
// to skip the prompt in scripts.
func HandleReset(c ResetCmd, root RootOpts, out *cliout.Printer) int {
	opts := persist.Options{
		RepoDir:   root.RepoDir,
		AuthToken: os.Getenv("GITHUB_TOKEN"),
	}
	localRoot := persist.LocalRoot(opts)
	info, err := os.Stat(localRoot)
	missing := errors.Is(err, fs.ErrNotExist)
	switch {
	case missing:
		// Nothing to wipe, but we still re-clone below so the user
		// ends up with a populated .defrost/ either way.
	case err != nil:
		out.Failf("defrost reset: %v", err)
		return 1
	case !info.IsDir():
		out.Failf("defrost reset: %s exists but is not a directory; remove it manually.", localRoot)
		return 1
	}

	if !missing && !c.Yes && !confirmReset(os.Stdin, os.Stderr, localRoot) {
		return 0
	}
	if !missing {
		if err := os.RemoveAll(localRoot); err != nil {
			out.Failf("defrost reset: %v", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "defrost reset: removed %s.\n", localRoot)
	}

	// Re-clone immediately so the user is left with a usable worktree.
	// CloneForRead does the cold-path init+fetch+checkout against
	// origin/<data-branch>; if the branch doesn't exist on origin yet
	// (a brand new repo), it returns Snapshot{} cleanly and we just
	// note it.
	snap, err := persist.New(opts).CloneForRead()
	if err != nil {
		out.Failf("defrost reset: re-clone failed: %v", err)
		fmt.Fprintf(os.Stderr, "  the local cache is wiped; the next read will retry the clone.\n")
		return 1
	}
	if snap.Dir == "" {
		fmt.Fprintf(os.Stderr, "defrost reset: data branch does not exist on origin yet; nothing to clone.\n")
		return 0
	}
	fmt.Fprintf(os.Stderr, "defrost reset: cloned origin/<data-branch> into %s (HEAD: %s).\n", snap.Dir, snap.SHA[:min(8, len(snap.SHA))])
	return 0
}

func confirmReset(in *os.File, out *os.File, root string) bool {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	fmt.Fprintf(out, "About to wipe %s\n", abs)
	fmt.Fprintln(out, "This deletes the local data-branch worktree and DuckDB cache.")
	fmt.Fprintln(out, "It does NOT touch origin/_defrost.")
	fmt.Fprint(out, `Type "reset" to confirm: `)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		fmt.Fprintln(out, "cancelled.")
		return false
	}
	if strings.TrimSpace(scanner.Text()) != "reset" {
		fmt.Fprintln(out, "cancelled.")
		return false
	}
	return true
}
