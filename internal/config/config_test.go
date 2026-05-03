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
