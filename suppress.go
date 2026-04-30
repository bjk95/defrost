package main

import (
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/persist"
)

type SuppressOpts struct {
	RepoDir    string
	DataBranch string
	NoRemote   bool
	Dev        bool
}

func (s SuppressOpts) toPersist() persist.Options {
	return persist.Options{
		RepoDir:    s.RepoDir,
		DataBranch: s.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   s.NoRemote,
		Dev:        s.Dev,
	}
}

func HandleSuppressAdd(testID string, opts SuppressOpts) int {
	if testID == "" {
		fmt.Fprintln(os.Stderr, "suppress add: empty test id")
		return 2
	}
	be := persist.New(opts.toPersist())
	mutate := func(cur []string) []string { return append(cur, testID) }
	if err := be.UpdateSuppressions(mutate, "suppress: add "+testID); err != nil {
		fmt.Fprintln(os.Stderr, "suppress add:", err)
		return 1
	}
	return 0
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
