package promptfoo

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// silence unused imports until later tasks introduce real uses
var _ = io.Discard
var _ = exec.Command
var _ = os.CreateTemp
var _ = filepath.Join
var _ = runtime.GOOS

func TestMatches(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"direct", []string{"promptfoo", "eval"}, true},
		{"direct with config", []string{"promptfoo", "eval", "-c", "promptfooconfig.yaml"}, true},
		{"npx", []string{"npx", "promptfoo", "eval"}, true},
		{"npx with -y", []string{"npx", "-y", "promptfoo", "eval"}, true},
		{"npx with @latest", []string{"npx", "promptfoo@latest", "eval"}, true},
		{"pnpm", []string{"pnpm", "promptfoo", "eval"}, true},
		{"pnpm dlx", []string{"pnpm", "dlx", "promptfoo", "eval"}, true},
		{"yarn", []string{"yarn", "promptfoo", "eval"}, true},
		{"node_modules bin", []string{"./node_modules/.bin/promptfoo", "eval"}, true},
		{"missing eval subcommand", []string{"promptfoo"}, false},
		{"different subcommand", []string{"promptfoo", "view"}, false},
		{"different subcommand between promptfoo and eval", []string{"promptfoo", "init", "eval"}, false},
		{"unrelated cmd", []string{"jest"}, false},
		{"empty", []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adapter{}
			got := a.Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestBuildArgsInjectsOutputFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "-c", "promptfooconfig.yaml"}, "/tmp/results.json")
	want := []string{"eval", "-c", "promptfooconfig.yaml", "--output", "/tmp/results.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsUserLongFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "--output", "user.json"}, "/tmp/results.json")
	want := []string{"eval", "--output", "user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsUserShortFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "-o", "user.json"}, "/tmp/results.json")
	want := []string{"eval", "-o", "user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsLongFlagWithEquals(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "--output=user.json"}, "/tmp/results.json")
	want := []string{"eval", "--output=user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestUserOutputPath(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"eval", "--output", "user.json"}, "user.json"},
		{[]string{"eval", "-o", "user.json"}, "user.json"},
		{[]string{"eval", "--output=user.json"}, "user.json"},
		{[]string{"eval", "-c", "x.yaml"}, ""},
	}
	for _, tc := range cases {
		got := userOutputPath(tc.args)
		if got != tc.want {
			t.Fatalf("userOutputPath(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
