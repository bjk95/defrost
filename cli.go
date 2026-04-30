package main

var CLI struct {
	Exec struct {
		Cmd        []string `arg:"" name:"cmd" passthrough:"" help:"Test command to run."`
		RepoDir    string   `name:"repo-dir" default:"." help:"Path to the git repo to persist into."`
		DataBranch string   `name:"data-branch" default:"_defrost" help:"Branch name where results are stored."`
		NoPersist  bool     `name:"no-persist" help:"Run tests without persisting results."`
		NoRemote   bool     `name:"no-remote" help:"Persist locally only — store the data branch in the local repo and do not push."`
		Dev        bool     `name:"dev" short:"d" help:"Dev mode: write results to <repo-dir>/.defrost-dev (gitignored scratch dir) instead of committing/pushing. For developing defrost itself."`
	} `cmd:"" help:"Execute test command and persist results."`

	History struct {
		Test       string `arg:"" name:"test" help:"Full test name (package + test, e.g. github.com/x/p/TestA)."`
		RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
		DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
		NoRemote   bool   `name:"no-remote" help:"Read from the local repo only — do not consult origin."`
	} `cmd:"" help:"Print recorded history for a single test as NDJSON."`

	Suppress struct {
		Add struct {
			Test       string `arg:"" name:"test" help:"Full test ID to suppress (same form as 'defrost history')."`
			RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name where suppressions are stored."`
			NoRemote   bool   `name:"no-remote" help:"Write to the local repo only — do not push."`
			Dev        bool   `name:"dev" short:"d" help:"Dev mode: read/write the local scratch dir instead of the data branch."`
		} `cmd:"" help:"Add a test to the suppression list."`

		Remove struct {
			Test       string `arg:"" name:"test" help:"Full test ID to remove from the suppression list."`
			RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name where suppressions are stored."`
			NoRemote   bool   `name:"no-remote" help:"Write to the local repo only — do not push."`
			Dev        bool   `name:"dev" short:"d" help:"Dev mode: read/write the local scratch dir instead of the data branch."`
		} `cmd:"" help:"Remove a test from the suppression list."`

		List struct {
			RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
			DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
			NoRemote   bool   `name:"no-remote" help:"Read from the local repo only — do not consult origin."`
			Dev        bool   `name:"dev" short:"d" help:"Dev mode: read the local scratch dir instead of the data branch."`
		} `cmd:"" help:"List the suppressed test IDs, one per line."`
	} `cmd:"" help:"Manage the suppression list. When every failing test in 'defrost exec' is suppressed, the exit code is rewritten to 0."`
}
