package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/persist"
)

func HandleHistory(testName, repoDir, dataBranch string, noRemote bool) int {
	entries, err := persist.History(persist.Options{
		RepoDir:    repoDir,
		DataBranch: dataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   noRemote,
	}, testName)
	if err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			fmt.Fprintln(os.Stderr, "history: no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --no-remote to read from the local repo.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "history:", err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			fmt.Fprintln(os.Stderr, "history:", err)
			return 1
		}
	}
	return 0
}
