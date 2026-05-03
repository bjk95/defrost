package cli

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-isatty"

	"github.com/bjk95/defrost/internal/cliout"
	"github.com/bjk95/defrost/internal/config"
)

// LoadConfigResolver returns a Kong Resolver that injects values from
// <repo>/.defrost.toml as defaults for the global flags. The resolver
// only fills values Kong's flag-and-env layers haven't already
// populated (Kong precedence: flag > env > resolver > tag default).
//
// If the config file doesn't exist or is malformed, the returned
// resolver applies no overrides; the caller may inspect warnings
// (via the second return value) and surface them through the printer.
// A genuine read error (parse failure, IO error) returns a non-nil err
// — callers should fail fast on that.
func LoadConfigResolver(startDir string) (kong.Resolver, []string, error) {
	cfg, warnings, err := config.Load(startDir)
	if err != nil {
		return nil, nil, err
	}
	resolver := kong.ResolverFunc(func(_ *kong.Context, _ *kong.Path, flag *kong.Flag) (any, error) {
		switch flag.Name {
		case "repo-dir":
			if cfg.RepoDir != "" {
				return cfg.RepoDir, nil
			}
		case "data-branch":
			if cfg.DataBranch != "" {
				return cfg.DataBranch, nil
			}
		case "dev":
			if cfg.Dev {
				return cfg.Dev, nil
			}
		case "port":
			if cfg.Serve.Port != 0 {
				return cfg.Serve.Port, nil
			}
		}
		return nil, nil
	})
	return resolver, warnings, nil
}

// NewPrinter constructs a *cliout.Printer from the parsed CLI's
// global verbosity / color flags, plus environment detection. Both
// binaries (cmd/defrost and cmd/defrost-ci) call this immediately
// after kong.Parse so all subsequent output flows through one
// consistent printer.
//
// Verbosity precedence: --quiet wins (returns -1), then --verbose
// count, then 0 (default). Mutual-exclusion enforcement happens at
// the call site so we can return an exit code; this function just
// computes the effective verbosity int.
//
// Color decision uses cliout.ShouldUseColor against stderr's TTY
// status — chrome goes to stderr, so colorising based on stdout would
// mis-detect when stderr is a TTY but stdout is redirected (the
// common case for `defrost history > out.ndjson`).
func NewPrinter(c *CLI) *cliout.Printer {
	verbosity := c.Verbose
	if c.Quiet {
		verbosity = -1
	}
	color := cliout.ShouldUseColor(
		isatty.IsTerminal(os.Stderr.Fd()),
		c.NoColor,
		os.Getenv("NO_COLOR"),
		os.Getenv("FORCE_COLOR"),
	)
	return cliout.New(os.Stderr, os.Stdout, verbosity, color)
}

// ValidateGlobalFlags returns an error message and exit code if -q
// and -v are both set. Caller pattern:
//
//	if msg, code := cli.ValidateGlobalFlags(&c); msg != "" {
//	    fmt.Fprintln(os.Stderr, msg)
//	    return code
//	}
func ValidateGlobalFlags(c *CLI) (string, int) {
	if c.Quiet && c.Verbose > 0 {
		return "defrost: --quiet and --verbose are mutually exclusive", 2
	}
	return "", 0
}

// Root returns the global flag values in a struct convenient for
// passing into per-subcommand handlers.
func (c *CLI) Root() RootOpts {
	return RootOpts{RepoDir: c.RepoDir, DataBranch: c.DataBranch, Dev: c.Dev}
}

// VersionString builds the --version output. Format:
//
//	defrost <DefrostVersion> (commit <sha>, go<runtime>, <goos>/<goarch>)
//
// <commit> comes from runtime/debug.ReadBuildInfo's vcs.revision —
// Go embeds this in every build done from a git repo, no -ldflags
// required. Truncated to 7 chars to match git's short-SHA convention.
func VersionString() string {
	commit := "unknown"
	if info, ok := debugReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				commit = s.Value
				if len(commit) > 7 {
					commit = commit[:7]
				}
				break
			}
		}
	}
	return fmt.Sprintf("defrost %s (commit %s, %s, %s/%s)",
		DefrostVersion, commit, runtimeVersion(), runtimeGOOS(), runtimeGOARCH())
}
