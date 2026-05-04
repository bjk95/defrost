package cli

import (
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/persist"
)

func suppressOpts(repoDir, dataBranch string, dev bool) persist.Options {
	return persist.Options{
		RepoDir:    repoDir,
		DataBranch: dataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        dev,
	}
}

// HandleSuppressAdd appends each test ID in c.Tests to the suppression
// list in a single commit. Empty input is an error; empty individual
// IDs are skipped silently.
func HandleSuppressAdd(c SuppressAddCmd) int {
	if len(c.Tests) == 0 {
		fmt.Fprintln(os.Stderr, "suppress add: no test ids provided")
		return 2
	}
	cleaned := make([]string, 0, len(c.Tests))
	for _, id := range c.Tests {
		if id == "" {
			continue
		}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		fmt.Fprintln(os.Stderr, "suppress add: all provided test ids were empty")
		return 2
	}
	be := persist.New(suppressOpts(c.RepoDir, c.DataBranch, c.Dev))
	mutate := func(cur []string) []string { return append(cur, cleaned...) }
	if err := be.UpdateSuppressions(mutate, commitMessageForAdd(cleaned)); err != nil {
		fmt.Fprintln(os.Stderr, "suppress add:", err)
		return 1
	}
	return 0
}

// commitMessageForAdd returns a one-line commit subject for an add of
// ids.
func commitMessageForAdd(ids []string) string {
	if len(ids) == 1 {
		return "suppress: add " + ids[0]
	}
	return fmt.Sprintf("suppress: add %d tests", len(ids))
}

// HandleSuppressRemove removes a test ID from the suppression list.
func HandleSuppressRemove(c SuppressRemoveCmd) int {
	if c.Test == "" {
		fmt.Fprintln(os.Stderr, "suppress remove: empty test id")
		return 2
	}
	be := persist.New(suppressOpts(c.RepoDir, c.DataBranch, c.Dev))
	mutate := func(cur []string) []string {
		out := make([]string, 0, len(cur))
		for _, id := range cur {
			if id != c.Test {
				out = append(out, id)
			}
		}
		return out
	}
	if err := be.UpdateSuppressions(mutate, "suppress: remove "+c.Test); err != nil {
		fmt.Fprintln(os.Stderr, "suppress remove:", err)
		return 1
	}
	return 0
}

// HandleSuppressList prints every suppressed test id on its own line.
func HandleSuppressList(c SuppressListCmd) int {
	be := persist.New(suppressOpts(c.RepoDir, c.DataBranch, c.Dev))
	ids, err := be.GetSuppressions()
	if err != nil {
		fmt.Fprintln(os.Stderr, "suppress list:", err)
		return 1
	}
	for _, id := range ids {
		fmt.Println(id)
	}
	return 0
}
