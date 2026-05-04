// Package cli holds the Kong CLI structure and subcommand handlers
// shared by `cmd/defrost` (the full binary, with serve + DuckDB + the
// embedded web bundle) and `cmd/defrost-ci` (the slim CI-focused
// binary, no serve / DuckDB / web assets).
//
// The serve subcommand is registered on the Kong struct in both
// binaries so the help text matches; the actual handler differs:
// `cmd/defrost/main.go` wires up the dashboard, while
// `cmd/defrost-ci/main.go` prints the install hint and exits 1.
package cli

// CLI is the top-level Kong struct. Both binaries share this exact
// shape (so `defrost --help` and `defrost-ci --help` differ only in
// the binary name printed in usage), and dispatch the parsed command
// to a binary-specific handler.
type CLI struct {
	Exec ExecCmd `cmd:"" help:"Execute test command and persist results."`

	History HistoryCmd `cmd:"" help:"Print recorded history for a single test as NDJSON."`

	Suppress struct {
		Add    SuppressAddCmd    `cmd:"" help:"Add one or more test IDs to the suppression list."`
		Remove SuppressRemoveCmd `cmd:"" help:"Remove a test from the suppression list."`
		List   SuppressListCmd   `cmd:"" help:"List the suppressed test IDs, one per line."`
	} `cmd:"" help:"Manage the suppression list. When every failing test in 'defrost exec' is suppressed, the exit code is rewritten to 0."`

	Drop struct {
		History DropHistoryCmd `cmd:"" help:"Drop persisted traces and/or metrics, rewriting the data branch via orphan commit + force-push so old objects can be garbage-collected. Suppressions are preserved."`
	} `cmd:"" help:"Destructive operations on the data branch."`

	Serve ServeCmd `cmd:"" help:"Serve a local UI for inspecting test history."`

	Reset ResetCmd `cmd:"" help:"Wipe the local <repo>/.defrost/ cache so the next read clones fresh from origin. The data branch on origin is untouched."`
}

// ExecCmd is the `defrost exec` subcommand.
type ExecCmd struct {
	Cmd        []string `arg:"" name:"cmd" passthrough:"" help:"Test command to run."`
	RepoDir    string   `name:"repo-dir" default:"." help:"Path to the git repo to persist into."`
	DataBranch string   `name:"data-branch" default:"_defrost" help:"Branch name where results are stored."`
	NoPersist  bool     `name:"no-persist" help:"Run tests without persisting results."`
	Dev        bool     `name:"dev" short:"d" help:"Dev mode: write run results locally only (no push to the data branch). Files still land at <repo-dir>/.defrost/data/. For developing defrost itself."`
}

// HistoryCmd is the `defrost history` subcommand.
type HistoryCmd struct {
	Test       string `arg:"" name:"test" help:"Full test name (package + test, e.g. github.com/x/p/TestA)."`
	RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
	DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
	Dev        bool   `name:"dev" short:"d" help:"Dev mode: read only from the local <repo-dir>/.defrost/data/ tree; no remote operations."`
}

// SuppressAddCmd is `defrost suppress add`.
type SuppressAddCmd struct {
	Tests      []string `arg:"" name:"tests" help:"One or more test IDs to suppress (same form as 'defrost history'). All IDs land in a single commit on the data branch."`
	RepoDir    string   `name:"repo-dir" default:"." help:"Path to the git repo."`
	DataBranch string   `name:"data-branch" default:"_defrost" help:"Branch name where suppressions are stored."`
	Dev        bool     `name:"dev" short:"d" help:"Dev mode: read/write only the local <repo-dir>/.defrost/ tree; no remote operations."`
}

// SuppressRemoveCmd is `defrost suppress remove`.
type SuppressRemoveCmd struct {
	Test       string `arg:"" name:"test" help:"Full test ID to remove from the suppression list."`
	RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
	DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name where suppressions are stored."`
	Dev        bool   `name:"dev" short:"d" help:"Dev mode: read/write only the local <repo-dir>/.defrost/ tree; no remote operations."`
}

// SuppressListCmd is `defrost suppress list`.
type SuppressListCmd struct {
	RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
	DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
	Dev        bool   `name:"dev" short:"d" help:"Dev mode: read only from the local <repo-dir>/.defrost/data/ tree; no remote operations."`
}

// DropHistoryCmd is `defrost drop history`.
type DropHistoryCmd struct {
	TracesOnly  bool   `name:"traces-only" help:"Drop only traces; keep metrics + logs."`
	MetricsOnly bool   `name:"metrics-only" help:"Drop only metrics; keep traces + logs."`
	LogsOnly    bool   `name:"logs-only" help:"Drop only logs; keep traces + metrics."`
	Yes         bool   `name:"yes" short:"y" help:"Skip the confirmation prompt."`
	RepoDir     string `name:"repo-dir" default:"." help:"Path to the git repo."`
	DataBranch  string `name:"data-branch" default:"_defrost" help:"Branch name to rewrite."`
	Dev         bool   `name:"dev" short:"d" help:"Dev mode: drop only the local <repo-dir>/.defrost/data/ tree; no remote operations."`
}

// ServeCmd is `defrost serve`. The handler differs between full and
// CI binaries; see cmd/defrost/main.go and cmd/defrost-ci/main.go.
type ServeCmd struct {
	Port       int    `name:"port" default:"6969" help:"Port to bind on 127.0.0.1."`
	RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
	DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
	Dev        bool   `name:"dev" short:"d" help:"Dev mode: read only from the local <repo-dir>/.defrost/data/ tree; no remote operations."`
}

// ResetCmd is `defrost reset` — the escape hatch for when the local
// .defrost/ cache is in a state CloneForRead can't refresh from
// (corrupt .git, dir-without-.git from a half-failed prior clone,
// network-induced ref skew, etc.). Wipes <repo>/.defrost/ and lets
// the next read clone fresh.
//
// Local-only: the data branch on origin is untouched. Re-cloning is
// safe because the worktree is a derived view of origin/<branch>;
// nothing is lost that wasn't already on origin.
type ResetCmd struct {
	RepoDir string `name:"repo-dir" default:"." help:"Path to the git repo whose .defrost/ tree should be wiped."`
	Yes     bool   `name:"yes" short:"y" help:"Skip the confirmation prompt."`
}

// DefrostVersion is the value stamped into the run's
// service.version Resource attribute. Bump when cutting a release;
// build-time injection can replace this constant later.
const DefrostVersion = "0.0.0-dev"
