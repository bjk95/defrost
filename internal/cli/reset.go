package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjk95/defrost/internal/persist"
)

// HandleReset implements `defrost reset` — the escape hatch when the
// local <repo>/.defrost/ cache is in a state CloneForRead can't
// refresh from. Deletes the directory entirely so the next read
// clones fresh from origin. Local-only; the data branch on origin
// is untouched.
//
// Confirmation prompt is on by default because deleting `.defrost/`
// also removes cache.duckdb (the dashboard's read cache). Pass --yes
// to skip the prompt in scripts.
func HandleReset(c ResetCmd) int {
	root := persist.LocalRoot(persist.Options{RepoDir: c.RepoDir})
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "defrost reset: nothing to do — %s does not exist.\n", root)
			return 0
		}
		fmt.Fprintln(os.Stderr, "defrost reset:", err)
		return 1
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "defrost reset: %s exists but is not a directory; remove it manually.\n", root)
		return 1
	}

	if !c.Yes && !confirmReset(os.Stdin, os.Stderr, root) {
		return 0
	}

	if err := os.RemoveAll(root); err != nil {
		fmt.Fprintln(os.Stderr, "defrost reset:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "defrost reset: removed %s. Next read will clone fresh from origin.\n", root)
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
