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

import "github.com/alecthomas/kong"

// CLI is the top-level Kong struct. Both binaries share this exact
// shape (so `defrost --help` and `defrost-ci --help` differ only in
// the binary name printed in usage), and dispatch the parsed command
// to a binary-specific handler.
type CLI struct {
	// --- Global flags ---

	RepoDir    string `name:"repo-dir" short:"C" default:"." env:"DEFROST_REPO_DIR" help:"Path to the git repo." placeholder:"<path>"`
	DataBranch string `name:"data-branch" default:"_defrost" env:"DEFROST_DATA_BRANCH" help:"Branch where results are stored." placeholder:"<name>"`
	Dev        bool   `name:"dev" short:"d" env:"DEFROST_DEV" help:"Use the local scratch dir instead of the data branch."`

	NoColor bool `name:"no-color" env:"NO_COLOR" help:"Disable colored output."`
	Verbose int  `name:"verbose" short:"v" type:"counter" help:"More detailed output (repeat for more, -vv)."`
	Quiet   bool `name:"quiet" short:"q" help:"Suppress informational output."`

	Version kong.VersionFlag `name:"version" short:"V" help:"Print version and exit."`

	Exec ExecCmd `cmd:"" group:"core" help:"Execute test command and persist results.\n\nEXAMPLES:\n  # Go\n  $ defrost exec go test ./...\n\n  # Python\n  $ defrost exec pytest tests/\n\n  # Node\n  $ defrost exec npm test"`

	History HistoryCmd `cmd:"" group:"inspection" help:"Print recorded history for a single test as NDJSON.\n\nEXAMPLES:\n  $ defrost history github.com/you/pkg.TestThing"`

	Suppress struct {
		Add    SuppressAddCmd    `cmd:"" help:"Add one or more test IDs to the suppression list.\n\nEXAMPLES:\n  $ defrost suppress add github.com/you/pkg.TestFlaky"`
		Remove SuppressRemoveCmd `cmd:"" help:"Remove a test from the suppression list.\n\nEXAMPLES:\n  $ defrost suppress remove github.com/you/pkg.TestFlaky"`
		List   SuppressListCmd   `cmd:"" help:"List the suppressed test IDs, one per line.\n\nEXAMPLES:\n  $ defrost suppress list"`
	} `cmd:"" group:"management" help:"Manage the suppression list.\n\nEXAMPLES:\n  $ defrost suppress add <id>\n  $ defrost suppress list"`

	Drop struct {
		History DropHistoryCmd `cmd:"" help:"Drop persisted traces and/or metrics. Suppressions are preserved.\n\nEXAMPLES:\n  $ defrost drop history\n  $ defrost drop history --traces-only\n  $ defrost drop history --yes"`
	} `cmd:"" group:"management" help:"Destructive operations on the data branch."`

	Serve ServeCmd `cmd:"" group:"core" help:"Serve a local UI for inspecting test history.\n\nEXAMPLES:\n  $ defrost serve\n  $ defrost serve --port 8080"`

	Reset ResetCmd `cmd:"" group:"management" help:"Wipe the local <repo>/.defrost/ cache so the next read clones fresh from origin. The data branch on origin is untouched.\n\nEXAMPLES:\n  $ defrost reset\n  $ defrost reset --yes"`
}

// RootOpts bundles the values from CLI's global flags so handlers
// don't each need to receive five parameters. Construct in the
// binary's main.go from the parsed CLI, then pass into each handler
// alongside its subcommand struct.
type RootOpts struct {
	RepoDir    string
	DataBranch string
	Dev        bool
}

// ExecCmd is the `defrost exec` subcommand.
type ExecCmd struct {
	Cmd       []string `arg:"" name:"cmd" passthrough:"" help:"Test command to run."`
	NoPersist bool     `name:"no-persist" help:"Run tests without persisting results."`
}

// HistoryCmd is the `defrost history` subcommand.
type HistoryCmd struct {
	Test string `arg:"" name:"test" help:"Full test name (package + test, e.g. github.com/x/p/TestA)."`
}

// SuppressAddCmd is `defrost suppress add`.
type SuppressAddCmd struct {
	Tests []string `arg:"" name:"tests" help:"One or more test IDs to suppress (same form as 'defrost history'). All IDs land in a single commit on the data branch."`
}

// SuppressRemoveCmd is `defrost suppress remove`.
type SuppressRemoveCmd struct {
	Test string `arg:"" name:"test" help:"Full test ID to remove from the suppression list."`
}

// SuppressListCmd is `defrost suppress list`.
type SuppressListCmd struct{}

// DropHistoryCmd is `defrost drop history`.
type DropHistoryCmd struct {
	TracesOnly  bool `name:"traces-only" help:"Drop only traces; keep metrics + logs."`
	MetricsOnly bool `name:"metrics-only" help:"Drop only metrics; keep traces + logs."`
	LogsOnly    bool `name:"logs-only" help:"Drop only logs; keep traces + metrics."`
	Yes         bool `name:"yes" short:"y" help:"Skip the confirmation prompt."`
}

// ServeCmd is `defrost serve`. The handler differs between full and
// CI binaries; see cmd/defrost/main.go and cmd/defrost-ci/main.go.
type ServeCmd struct {
	Port int `name:"port" default:"6969" help:"Port to bind on 127.0.0.1."`
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
	Yes bool `name:"yes" short:"y" help:"Skip the confirmation prompt."`
}

// DefrostVersion is the value stamped into the run's
// service.version Resource attribute. Declared as a var (not const)
// so the Makefile's -ldflags -X can override it at build time:
//
//	-X github.com/bjk95/defrost/internal/cli.DefrostVersion=<tag>
var DefrostVersion = "0.0.0-dev"
