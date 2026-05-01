package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bjk95/defrost/internal/persist"
)

type DropOpts struct {
	RepoDir     string
	DataBranch  string
	TracesOnly  bool
	MetricsOnly bool
	Yes         bool
	Dev         bool
}

func HandleDropHistory(opts DropOpts) int {
	if opts.TracesOnly && opts.MetricsOnly {
		fmt.Fprintln(os.Stderr, "drop history: --traces-only and --metrics-only are mutually exclusive")
		return 2
	}
	sel := persist.DropSelector{
		DropTraces:  !opts.MetricsOnly,
		DropMetrics: !opts.TracesOnly,
	}

	be := persist.New(persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        opts.Dev,
	})

	confirm := func(plan persist.DropPlan) bool {
		if plan.BranchMissing {
			fmt.Fprintf(os.Stderr, "nothing to drop: data branch %q does not exist on origin.\n", plan.Branch)
			return false
		}
		if plan.Nothing() {
			fmt.Fprintln(os.Stderr, nothingToDropMessage(plan))
			return false
		}
		printDropPlan(os.Stderr, plan)
		if opts.Yes {
			return true
		}
		return askDropConfirmation(os.Stdin, os.Stderr)
	}

	if err := be.DropHistory(sel, confirm); err != nil {
		fmt.Fprintln(os.Stderr, "drop history:", err)
		return 1
	}
	return 0
}

func nothingToDropMessage(plan persist.DropPlan) string {
	loc := dropLocation(plan)
	switch {
	case plan.Sel.DropTraces && plan.Sel.DropMetrics:
		return fmt.Sprintf("nothing to drop: %s has no traces or metrics persisted.", loc)
	case plan.Sel.DropTraces:
		return fmt.Sprintf("nothing to drop: %s has no traces persisted.", loc)
	case plan.Sel.DropMetrics:
		return fmt.Sprintf("nothing to drop: %s has no metrics persisted.", loc)
	}
	return fmt.Sprintf("nothing to drop: %s.", loc)
}

func dropLocation(plan persist.DropPlan) string {
	if plan.Dev {
		return ".defrost-dev/"
	}
	if plan.OriginURL != "" {
		return fmt.Sprintf("branch %s (origin: %s)", plan.Branch, sanitizeOriginURL(plan.OriginURL))
	}
	return fmt.Sprintf("branch %s", plan.Branch)
}

// sanitizeOriginURL strips userinfo from the origin URL so an embedded
// token in an HTTPS remote (e.g. https://<pat>@github.com/foo/bar.git or
// https://user:pass@host/...) doesn't leak into the confirmation prompt
// or, with --yes, into CI logs. SCP-style SSH remotes (git@host:path)
// don't parse as URLs and pass through unchanged; they carry no secret.
func sanitizeOriginURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func printDropPlan(w *os.File, plan persist.DropPlan) {
	fmt.Fprintf(w, "About to drop history on %s:\n", dropLocation(plan))
	if plan.Sel.DropTraces {
		fmt.Fprintf(w, "  traces:  %s\n", formatSignalLine(plan.TraceFiles, plan.TraceBytes, plan.OldestRunUTC, plan.NewestRunUTC))
	} else {
		fmt.Fprintf(w, "  traces:  preserved (%d files, %s)\n", plan.TraceFiles, humanBytes(plan.TraceBytes))
	}
	if plan.Sel.DropMetrics {
		fmt.Fprintf(w, "  metrics: %s\n", formatSignalLine(plan.MetricFiles, plan.MetricBytes, plan.OldestRunUTC, plan.NewestRunUTC))
	} else {
		fmt.Fprintf(w, "  metrics: preserved (%d files, %s)\n", plan.MetricFiles, humanBytes(plan.MetricBytes))
	}
	fmt.Fprintln(w)
	if plan.Dev {
		fmt.Fprintln(w, "This permanently deletes the files. Suppressions and README are preserved.")
	} else {
		fmt.Fprintln(w, "This rewrites the branch via orphan commit + force-push and is irreversible.")
	}
	fmt.Fprintf(w, "Preserved: suppressions.json (%d entries), README.md.\n", plan.SuppressionsN)
	fmt.Fprintln(w)
}

func formatSignalLine(files int, bytes int64, oldest, newest time.Time) string {
	dateRange := ""
	if !oldest.IsZero() && !newest.IsZero() {
		dateRange = fmt.Sprintf("  (%s → %s)", oldest.Format("2006-01-02"), newest.Format("2006-01-02"))
	}
	return fmt.Sprintf("%d files, %s%s", files, humanBytes(bytes), dateRange)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for c := n / unit; c >= unit; c /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func askDropConfirmation(in *os.File, out *os.File) bool {
	fmt.Fprint(out, `Type "drop" to confirm: `)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		fmt.Fprintln(out, "cancelled.")
		return false
	}
	if strings.TrimSpace(scanner.Text()) != "drop" {
		fmt.Fprintln(out, "cancelled.")
		return false
	}
	return true
}
