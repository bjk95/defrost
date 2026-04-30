package main

import (
	"errors"
	"fmt"
	"os"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bjk95/defrost/internal/persist"
)

// historyMarshal emits one ResourceSpans per line as canonical OTLP/JSON.
var historyMarshal = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}

func HandleHistory(testName, repoDir, dataBranch string, noRemote bool) int {
	traces, err := persist.New(persist.Options{
		RepoDir:    repoDir,
		DataBranch: dataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   noRemote,
	}).GetTestHistory(testName)
	if err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			fmt.Fprintln(os.Stderr, "history: no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --no-remote to read from the local repo.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "history:", err)
		return 1
	}
	for _, rs := range traces {
		line, err := historyMarshal.Marshal(rs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "history:", err)
			return 1
		}
		// Strip embedded newlines so each ResourceSpans is one NDJSON line.
		fmt.Println(string(replaceBytes(line, '\n', ' ')))
	}
	return 0
}

func replaceBytes(s []byte, from, to byte) []byte {
	out := make([]byte, len(s))
	for i, c := range s {
		if c == from {
			out[i] = to
		} else {
			out[i] = c
		}
	}
	return out
}
