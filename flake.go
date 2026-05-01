package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/bjk95/defrost/internal/flake"
	"github.com/bjk95/defrost/internal/persist"
)

type FlakeOpts struct {
	RepoDir    string
	DataBranch string
	Dev        bool
	Window     int
	Branch     string
	Threshold  float64
}

func (o FlakeOpts) toPersist() persist.Options {
	return persist.Options{
		RepoDir:    o.RepoDir,
		DataBranch: o.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        o.Dev,
	}
}

// HandleFlakeList prints every test with its transition rate over the
// requested window, sorted highest first. When opts.Threshold > 0 only
// tests with rate >= threshold are emitted.
//
// Output columns: rate (3-decimal), n (window size used), outcome string,
// test name. Tab-separated for easy piping into awk/cut.
func HandleFlakeList(opts FlakeOpts) int {
	be := persist.New(opts.toPersist())
	_, byEncodedName, err := be.LoadAll()
	if err != nil {
		fmt.Fprintln(os.Stderr, "flake list:", err)
		return 1
	}
	results := flake.Compute(byEncodedName, flake.Options{
		Window: opts.Window,
		Branch: opts.Branch,
	})
	for _, r := range results {
		if r.Rate < opts.Threshold {
			continue
		}
		fmt.Printf("%.3f\t%d\t%s\t%s\n", r.Rate, len(r.Outcomes), renderOutcomes(r.Outcomes), r.TestName)
	}
	return 0
}

// renderOutcomes turns a list of outcomes into a compact run-history
// string. P pass, F fail, S skip. Used by `defrost flake list` so the
// reader can audit the call: a test rated 0.50 with sequence "PPFP" is
// believable; a test rated 0.50 with sequence "P" is not.
func renderOutcomes(outcomes []flake.Outcome) string {
	var sb strings.Builder
	sb.Grow(len(outcomes))
	for _, o := range outcomes {
		switch o {
		case flake.Pass:
			sb.WriteByte('P')
		case flake.Fail:
			sb.WriteByte('F')
		default:
			sb.WriteByte('S')
		}
	}
	return sb.String()
}
