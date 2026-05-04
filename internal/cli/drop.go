package cli

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bjk95/defrost/internal/persist"
)

// HandleDropHistory implements `defrost drop history`.
func HandleDropHistory(c DropHistoryCmd) int {
	exclusive := 0
	if c.TracesOnly {
		exclusive++
	}
	if c.MetricsOnly {
		exclusive++
	}
	if c.LogsOnly {
		exclusive++
	}
	if exclusive > 1 {
		fmt.Fprintln(os.Stderr, "drop history: --traces-only / --metrics-only / --logs-only are mutually exclusive")
		return 2
	}
	sel := persist.DropSelector{
		DropTraces:  !c.MetricsOnly && !c.LogsOnly,
		DropMetrics: !c.TracesOnly && !c.LogsOnly,
		DropLogs:    !c.TracesOnly && !c.MetricsOnly,
	}
	be := persist.New(persist.Options{
		RepoDir:    c.RepoDir,
		DataBranch: c.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        c.Dev,
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
		if c.Yes {
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
	parts := []string{}
	if plan.Sel.DropTraces {
		parts = append(parts, "traces")
	}
	if plan.Sel.DropMetrics {
		parts = append(parts, "metrics")
	}
	if plan.Sel.DropLogs {
		parts = append(parts, "logs")
	}
	if len(parts) == 0 {
		return fmt.Sprintf("nothing to drop: %s.", loc)
	}
	return fmt.Sprintf("nothing to drop: %s has no %s persisted.", loc, strings.Join(parts, "/"))
}

func dropLocation(plan persist.DropPlan) string {
	if plan.Dev {
		return ".defrost/data/"
	}
	if plan.OriginURL != "" {
		return fmt.Sprintf("branch %s (origin: %s)", plan.Branch, sanitizeOriginURL(plan.OriginURL))
	}
	return fmt.Sprintf("branch %s", plan.Branch)
}

// SanitizeOriginURL strips userinfo from an origin URL so an embedded
// token (https://<pat>@github.com/foo/bar.git, https://user:pass@host/...)
// doesn't leak into the confirmation prompt or, with --yes, into CI
// logs. SCP-style SSH remotes (git@host:path) don't parse as URLs and
// pass through unchanged.
func SanitizeOriginURL(raw string) string { return sanitizeOriginURL(raw) }

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
	signalLine(w, "traces ", plan.Sel.DropTraces, plan.TraceFiles, plan.TraceBytes, plan.OldestRunUTC, plan.NewestRunUTC)
	signalLine(w, "metrics", plan.Sel.DropMetrics, plan.MetricFiles, plan.MetricBytes, plan.OldestRunUTC, plan.NewestRunUTC)
	signalLine(w, "logs   ", plan.Sel.DropLogs, plan.LogFiles, plan.LogBytes, plan.OldestRunUTC, plan.NewestRunUTC)
	fmt.Fprintln(w)
	if plan.Dev {
		fmt.Fprintln(w, "This permanently deletes the files. Suppressions and README are preserved.")
	} else {
		fmt.Fprintln(w, "This rewrites the branch via orphan commit + force-push and is irreversible.")
	}
	fmt.Fprintf(w, "Preserved: suppressions.json (%d entries), README.md.\n", plan.SuppressionsN)
	fmt.Fprintln(w)
}

func signalLine(w *os.File, label string, drop bool, files int, bytes int64, oldest, newest time.Time) {
	if drop {
		fmt.Fprintf(w, "  %s: %s\n", label, formatSignalLine(files, bytes, oldest, newest))
	} else {
		fmt.Fprintf(w, "  %s: preserved (%d files, %s)\n", label, files, humanBytes(bytes))
	}
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
