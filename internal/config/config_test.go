package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFileReturnsZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error for missing config: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config; want zero-valued struct")
	}
	if cfg.RepoDir != "" || cfg.DataBranch != "" || cfg.Dev || cfg.Serve.Port != 0 {
		t.Errorf("expected zero-valued config, got %+v", cfg)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestLoad_FoundFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `repo-dir    = "/some/path"
data-branch = "_my-defrost"
dev         = true

[serve]
port = 7000
`
	if err := os.WriteFile(filepath.Join(dir, ".defrost.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoDir != "/some/path" {
		t.Errorf("RepoDir = %q; want /some/path", cfg.RepoDir)
	}
	if cfg.DataBranch != "_my-defrost" {
		t.Errorf("DataBranch = %q; want _my-defrost", cfg.DataBranch)
	}
	if !cfg.Dev {
		t.Errorf("Dev = false; want true")
	}
	if cfg.Serve.Port != 7000 {
		t.Errorf("Serve.Port = %d; want 7000", cfg.Serve.Port)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings on a clean config, got %v", warnings)
	}
}

func TestLoad_UnknownKeyWarns(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `repo-dir = "."
typo-key = "oops"

[serve]
port    = 1234
nonsense = true
`
	if err := os.WriteFile(filepath.Join(dir, ".defrost.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoDir != "." {
		t.Errorf("RepoDir = %q; want .", cfg.RepoDir)
	}
	if cfg.Serve.Port != 1234 {
		t.Errorf("Serve.Port = %d; want 1234", cfg.Serve.Port)
	}
	if len(warnings) != 2 {
		t.Errorf("want 2 warnings (typo-key, serve.nonsense), got %d: %v", len(warnings), warnings)
	}
}

func TestLoad_TypeErrorReturnsErr(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[serve]
port = "six thousand"
`
	if err := os.WriteFile(filepath.Join(dir, ".defrost.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := Load(dir)
	if err == nil {
		t.Fatal("expected error from type mismatch, got nil")
	}
}

func TestLoad_WalksUpFromSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".defrost.toml"),
		[]byte(`data-branch = "_walked"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(sub)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DataBranch != "_walked" {
		t.Errorf("DataBranch = %q; want _walked", cfg.DataBranch)
	}
}

func TestLoad_NoGitMeansNoConfig(t *testing.T) {
	dir := t.TempDir()
	// no .git, no .defrost.toml — even if we drop a config file in a
	// non-repo dir, Load should ignore it.
	if err := os.WriteFile(filepath.Join(dir, ".defrost.toml"),
		[]byte(`data-branch = "_ignored"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DataBranch != "" {
		t.Errorf("DataBranch = %q; want empty (no .git anchor)", cfg.DataBranch)
	}
}
