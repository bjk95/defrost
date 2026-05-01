package main

var CLI struct {
	Exec struct {
		Cmd        []string `arg:"" name:"cmd" passthrough:"" help:"Test command to run."`
		RepoDir    string   `name:"repo-dir" default:"." help:"Path to the git repo to persist into."`
		DataBranch string   `name:"data-branch" default:"_defrost" help:"Branch name where results are stored."`
		NoPersist  bool     `name:"no-persist" help:"Run tests without persisting results."`
		Dev        bool     `name:"dev" short:"d" help:"Dev mode: write results to <repo-dir>/.defrost-dev (gitignored scratch dir) instead of committing/pushing. For developing defrost itself."`
	} `cmd:"" help:"Execute test command and persist results."`

	History struct {
		Test       string `arg:"" name:"test" help:"Full test name (package + test, e.g. github.com/x/p/TestA)."`
		RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
		DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
		Dev        bool   `name:"dev" short:"d" help:"Dev mode: read the local scratch dir instead of the data branch."`
	} `cmd:"" help:"Print recorded history for a single test as NDJSON."`

	Suppress struct {
		Add struct {
			Tests      []string `arg:"" name:"tests" help:"One or more test IDs to suppress (same form as 'defrost history'). All IDs land in a single commit on the data branch."`
			RepoDir    string   `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string   `name:"data-branch" default:"_defrost" help:"Branch name where suppressions are stored."`
			Dev        bool     `name:"dev" short:"d" help:"Dev mode: read/write the local scratch dir instead of the data branch."`
		} `cmd:"" help:"Add one or more test IDs to the suppression list."`

		Remove struct {
			Test       string `arg:"" name:"test" help:"Full test ID to remove from the suppression list."`
			RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name where suppressions are stored."`
			Dev        bool   `name:"dev" short:"d" help:"Dev mode: read/write the local scratch dir instead of the data branch."`
		} `cmd:"" help:"Remove a test from the suppression list."`

		List struct {
			RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
			Dev        bool   `name:"dev" short:"d" help:"Dev mode: read the local scratch dir instead of the data branch."`
		} `cmd:"" help:"List the suppressed test IDs, one per line."`
	} `cmd:"" help:"Manage the suppression list. When every failing test in 'defrost exec' is suppressed, the exit code is rewritten to 0."`

	Drop struct {
		History struct {
			TracesOnly  bool   `name:"traces-only" help:"Drop only traces; keep metrics."`
			MetricsOnly bool   `name:"metrics-only" help:"Drop only metrics; keep traces."`
			Yes         bool   `name:"yes" short:"y" help:"Skip the confirmation prompt."`
			RepoDir     string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch  string `name:"data-branch" default:"_defrost" help:"Branch name to rewrite."`
			Dev         bool   `name:"dev" short:"d" help:"Dev mode: drop from the local scratch dir instead of the data branch."`
		} `cmd:"" help:"Drop persisted traces and/or metrics, rewriting the data branch via orphan commit + force-push so old objects can be garbage-collected. Suppressions are preserved."`
	} `cmd:"" help:"Destructive operations on the data branch."`

	Serve struct {
		Port       int    `name:"port" default:"6969" help:"Port to bind on 127.0.0.1."`
		RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
		DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
		Dev        bool   `name:"dev" short:"d" help:"Dev mode: read the local scratch dir instead of the data branch."`
	} `cmd:"" help:"Serve a local UI for inspecting test history."`

	Flake struct {
		List struct {
			Window     int     `name:"window" default:"20" help:"Most-recent runs per test to consider. 0 means use the full history."`
			Threshold  float64 `name:"threshold" default:"0" help:"Only show tests with transition rate >= this value (0..1). 0 shows every test."`
			Branch     string  `name:"branch" default:"" help:"Only consider runs from this branch (vcs.repository.ref.name). Empty means all branches."`
			RepoDir    string  `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string  `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
			Dev        bool    `name:"dev" short:"d" help:"Dev mode: read the local scratch dir instead of the data branch."`
		} `cmd:"" help:"List every test ranked by recent pass\u2194fail transition rate (transition-state flake metric)."`
	} `cmd:"" help:"Detect flaky tests using transition-rate analysis on persisted history."`
}
