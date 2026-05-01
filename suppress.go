package main

import (
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/persist"
)

type SuppressOpts struct {
	RepoDir    string
	DataBranch string
	Dev        bool
}

func (s SuppressOpts) toPersist() persist.Options {
	return persist.Options{
		RepoDir:    s.RepoDir,
		DataBranch: s.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        s.Dev,
	}
}

// HandleSuppressAdd appends each test ID in testIDs to the suppression
// list in a single commit. Empty input is an error; empty individual
// IDs are skipped silently.
func HandleSuppressAdd(testIDs []string, opts SuppressOpts) int {
	if len(testIDs) == 0 {
		fmt.Fprintln(os.Stderr, "suppress add: no test ids provided")
		return 2
	}
	// Filter empties so a stray empty string in the args doesn't pollute
	// the list.
	cleaned := make([]string, 0, len(testIDs))
	for _, id := range testIDs {
		if id == "" {
			continue
		}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		fmt.Fprintln(os.Stderr, "suppress add: all provided test ids were empty")
		return 2
	}
	be := persist.New(opts.toPersist())
	mutate := func(cur []string) []string { return append(cur, cleaned...) }
	msg := commitMessageForAdd(cleaned)
	if err := be.UpdateSuppressions(mutate, msg); err != nil {
		fmt.Fprintln(os.Stderr, "suppress add:", err)
		return 1
	}
	return 0
}

// commitMessageForAdd returns a one-line commit subject for an add of
// ids. For a single ID the message is "suppress: add <id>"; for many,
// it's "suppress: add N tests".
func commitMessageForAdd(ids []string) string {
	if len(ids) == 1 {
		return "suppress: add " + ids[0]
	}
	return fmt.Sprintf("suppress: add %d tests", len(ids))
}

func HandleSuppressRemove(testID string, opts SuppressOpts) int {
	if testID == "" {
		fmt.Fprintln(os.Stderr, "suppress remove: empty test id")
		return 2
	}
	be := persist.New(opts.toPersist())
	mutate := func(cur []string) []string {
		out := make([]string, 0, len(cur))
		for _, id := range cur {
			if id != testID {
				out = append(out, id)
			}
		}
		return out
	}
	if err := be.UpdateSuppressions(mutate, "suppress: remove "+testID); err != nil {
		fmt.Fprintln(os.Stderr, "suppress remove:", err)
		return 1
	}
	return 0
}

func HandleSuppressList(opts SuppressOpts) int {
	be := persist.New(opts.toPersist())
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
