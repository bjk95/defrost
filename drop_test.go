package main

import (
	"strings"
	"testing"

	"github.com/bjk95/defrost/internal/persist"
)

func TestSanitizeOriginURL_StripsCredentials(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"https with token-as-user", "https://ghp_AbcDef@github.com/foo/bar.git", "https://github.com/foo/bar.git"},
		{"https with user:password", "https://user:pass@github.com/foo/bar.git", "https://github.com/foo/bar.git"},
		{"https with x-access-token", "https://x-access-token:ghp_xyz@github.com/foo/bar.git", "https://github.com/foo/bar.git"},
		{"plain https unchanged", "https://github.com/foo/bar.git", "https://github.com/foo/bar.git"},
		{"scp-style ssh unchanged", "git@github.com:foo/bar.git", "git@github.com:foo/bar.git"},
		{"file url unchanged", "file:///tmp/origin.git", "file:///tmp/origin.git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeOriginURL(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeOriginURL(%q) = %q; want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "ghp_") || strings.Contains(got, "pass") {
				t.Errorf("sanitized URL still contains credential-like substring: %q", got)
			}
		})
	}
}

func TestDropLocation_RedactsCredentialsInPrompt(t *testing.T) {
	plan := persist.DropPlan{
		Branch:    "_defrost",
		OriginURL: "https://ghp_secrettoken@github.com/foo/bar.git",
	}
	got := dropLocation(plan)
	if strings.Contains(got, "ghp_secrettoken") {
		t.Errorf("dropLocation leaked token: %q", got)
	}
	if !strings.Contains(got, "github.com/foo/bar.git") {
		t.Errorf("dropLocation lost the host/path: %q", got)
	}
}
