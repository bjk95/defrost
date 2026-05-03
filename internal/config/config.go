// Package config loads .defrost.toml from the repo root. Missing file
// is not an error — Load returns a zero-valued Config so callers can
// uniformly fall through to env vars and built-in defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config mirrors the subset of CLI fields that may be set from the
// config file. Field tags use the same kebab-case names as the CLI
// flags so the file reads identically to a flag list.
type Config struct {
	RepoDir    string      `toml:"repo-dir"`
	DataBranch string      `toml:"data-branch"`
	Dev        bool        `toml:"dev"`
	Serve      ServeConfig `toml:"serve"`
}

// ServeConfig holds defrost serve specific defaults.
type ServeConfig struct {
	Port int `toml:"port"`
}

// Load walks up from startDir looking for a directory containing .git.
// It then reads .defrost.toml from that directory if present. Returns:
//   - cfg: a populated Config (or zero-valued struct if no file found)
//   - warnings: human-readable notes (e.g. unknown keys); never nil-vs-empty
//     significant — callers print each line via cliout.Warnf
//   - err: only set on TOML parse errors or unreadable files
func Load(startDir string) (*Config, []string, error) {
	root, ok := findRepoRoot(startDir)
	if !ok {
		return &Config{}, nil, nil
	}
	path := filepath.Join(root, ".defrost.toml")
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	meta, err := toml.Decode(string(body), &cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	var warnings []string
	for _, key := range meta.Undecoded() {
		warnings = append(warnings, fmt.Sprintf("%s: unknown key %q", path, key.String()))
	}
	return &cfg, warnings, nil
}

// findRepoRoot walks up from start until it finds a directory
// containing .git (file or dir, to support submodule worktrees) or
// hits the filesystem root.
func findRepoRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
