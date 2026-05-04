# Changelog

All notable changes to this project are documented here.

## [0.2.0] — 2026-05-04

### Added
- `defrost --version` (alias `-V`) prints version, commit, Go version, and OS/arch.
- `defrost --no-color` and `NO_COLOR` / `FORCE_COLOR` env-var support.
- `defrost --verbose` (`-v`, `-vv`) and `--quiet` (`-q`) verbosity flags.
- Per-repo configuration via `.defrost.toml` at the repo root. Recognised
  keys: `repo-dir`, `data-branch`, `dev`, and `[serve] port`. Precedence:
  flag > env var > `.defrost.toml` > built-in default.
- Environment-variable defaults: `DEFROST_REPO_DIR`, `DEFROST_DATA_BRANCH`,
  `DEFROST_DEV`, `DEFROST_SERVE_PORT`.
- gh-style help with command groups (`CORE`, `INSPECTION`, `MANAGEMENT`)
  and per-command `EXAMPLES`.
- Symbol-led runtime output (`✓`, `✗`, `→`, `!`, `·`) with auto color
  when stderr is a TTY.
- `defrost exec` now lists each failed test ID under the summary; at
  `-v` it also shows the first few non-blank lines of the captured
  failure message.
- The result-summary line now includes a `✱ N suppressed` column so
  failures already on the suppression list are visible without
  scrolling for the rewrite line.

### Changed (breaking)
- `--repo-dir`, `--data-branch`, and `--dev` are now **global** flags and
  must appear before the subcommand:
  - Old: `defrost exec --repo-dir=. go test ./...`
  - New: `defrost --repo-dir=. exec go test ./...`
- `defrost exec` result summary changed format from
  `defrost: results: P pass, F fail, S skip` to
  `→ P passed   ✗ F failed   ⊘ S skipped   ✱ N suppressed` —
  CI grep patterns updated.
