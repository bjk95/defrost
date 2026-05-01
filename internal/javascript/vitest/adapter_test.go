package vitest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdapterMatchesPureArgv(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"bare vitest", []string{"vitest"}, true},
		{"vitest run", []string{"vitest", "run"}, true},
		{"vitest with args", []string{"vitest", "run", "tests/"}, true},
		{"vitest with --watch (collision handled in Run)", []string{"vitest", "--watch"}, true},
		{"vitest with --ui (collision handled in Run)", []string{"vitest", "--ui"}, true},
		{"npx vitest", []string{"npx", "vitest", "run"}, true},
		{"npx with flags before vitest", []string{"npx", "-y", "vitest", "run"}, true},
		{"npx --no-install vitest", []string{"npx", "--no-install", "vitest"}, true},
		{"npx --package=vitest@latest vitest", []string{"npx", "--package=vitest@latest", "vitest"}, true},
		{"npx --package vitest@latest vitest", []string{"npx", "--package", "vitest@latest", "vitest"}, true},
		{"bunx vitest", []string{"bunx", "vitest"}, true},
		{"yarn vitest", []string{"yarn", "vitest", "run"}, true},
		{"pnpm vitest", []string{"pnpm", "vitest", "run"}, true},
		{"node_modules/.bin/vitest", []string{"node_modules/.bin/vitest"}, true},
		{"./node_modules/.bin/vitest", []string{"./node_modules/.bin/vitest"}, true},
		{"/abs/path/node_modules/.bin/vitest", []string{"/repo/node_modules/.bin/vitest"}, true},

		{"empty", []string{}, false},
		{"go test", []string{"go", "test", "./..."}, false},
		{"pytest", []string{"pytest", "tests/"}, false},
		{"jest", []string{"jest", "tests/"}, false},
		{"npx jest", []string{"npx", "jest"}, false},
		{"npx --package vitest jest (jest is exec, vitest is value)", []string{"npx", "--package", "vitest", "jest"}, false},
		{"npx -p vitest jest", []string{"npx", "-p", "vitest", "jest"}, false},
		{"bunx jest", []string{"bunx", "jest"}, false},
		{"yarn alone", []string{"yarn"}, false},
		{"pnpm alone", []string{"pnpm"}, false},
		{"npm alone", []string{"npm"}, false},
		{"node alone", []string{"node", "script.js"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (&Adapter{}).Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

// writePackageJSON drops a package.json with the given scripts map into
// the current directory.
func writePackageJSON(t *testing.T, scripts map[string]string) {
	t.Helper()
	body := `{"scripts": {`
	first := true
	for k, v := range scripts {
		if !first {
			body += ", "
		}
		first = false
		body += `"` + k + `": "` + v + `"`
	}
	body += `}}`
	if err := os.WriteFile("package.json", []byte(body), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
}

func TestAdapterMatchesFormD(t *testing.T) {
	cases := []struct {
		name              string
		scripts           map[string]string
		cmd               []string
		want              bool
		wantScriptOK      bool
		wantWatchInScript bool
	}{
		{
			name:         "npm test with scripts.test=vitest",
			scripts:      map[string]string{"test": "vitest"},
			cmd:          []string{"npm", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "npm test with scripts.test=vitest run",
			scripts:      map[string]string{"test": "vitest run"},
			cmd:          []string{"npm", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "npm test with scripts.test=vitest run --coverage",
			scripts:      map[string]string{"test": "vitest run --coverage"},
			cmd:          []string{"npm", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "npm test with NODE_ENV=test prefix",
			scripts:      map[string]string{"test": "NODE_ENV=test vitest run"},
			cmd:          []string{"npm", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "npm test with two env-var prefix tokens",
			scripts:      map[string]string{"test": "NODE_ENV=test CI=1 vitest run"},
			cmd:          []string{"npm", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:              "npm test with scripts.test=vitest --watch (watch trigger)",
			scripts:           map[string]string{"test": "vitest --watch"},
			cmd:               []string{"npm", "test"},
			want:              true,
			wantScriptOK:      false,
			wantWatchInScript: true,
		},
		{
			name:              "npm test with scripts.test=vitest --ui (watch trigger)",
			scripts:           map[string]string{"test": "vitest --ui"},
			cmd:               []string{"npm", "test"},
			want:              true,
			wantScriptOK:      false,
			wantWatchInScript: true,
		},
		{
			name:              "npm test with scripts.test=vitest watch (subcommand watch)",
			scripts:           map[string]string{"test": "vitest watch"},
			cmd:               []string{"npm", "test"},
			want:              true,
			wantScriptOK:      false,
			wantWatchInScript: true,
		},
		{
			name:         "npm test with composite shell command",
			scripts:      map[string]string{"test": "vitest && eslint ."},
			cmd:          []string{"npm", "test"},
			want:         true,
			wantScriptOK: false,
		},
		{
			name:    "npm test with non-vitest head token",
			scripts: map[string]string{"test": "react-scripts test"},
			cmd:     []string{"npm", "test"},
			want:    false,
		},
		{
			name:    "npm test with jest (rejected)",
			scripts: map[string]string{"test": "jest"},
			cmd:     []string{"npm", "test"},
			want:    false,
		},
		{
			name:    "npm test with cross-env wrapper (rejected)",
			scripts: map[string]string{"test": "cross-env NODE_ENV=test vitest run"},
			cmd:     []string{"npm", "test"},
			want:    false,
		},
		{
			name:         "npm run script with scripts.test=vitest",
			scripts:      map[string]string{"vitest-only": "vitest run"},
			cmd:          []string{"npm", "run", "vitest-only"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "yarn test with scripts.test=vitest",
			scripts:      map[string]string{"test": "vitest"},
			cmd:          []string{"yarn", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "yarn run test",
			scripts:      map[string]string{"test": "vitest run"},
			cmd:          []string{"yarn", "run", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "yarn <script> direct (no run)",
			scripts:      map[string]string{"check": "vitest run"},
			cmd:          []string{"yarn", "check"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "pnpm test",
			scripts:      map[string]string{"test": "vitest run"},
			cmd:          []string{"pnpm", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:         "pnpm run test",
			scripts:      map[string]string{"test": "vitest run"},
			cmd:          []string{"pnpm", "run", "test"},
			want:         true,
			wantScriptOK: true,
		},
		{
			name:    "npm test with no scripts.test",
			scripts: map[string]string{"build": "vite build"},
			cmd:     []string{"npm", "test"},
			want:    false,
		},
		{
			name:    "npm run unknown",
			scripts: map[string]string{"test": "vitest"},
			cmd:     []string{"npm", "run", "build"},
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writePackageJSON(t, tc.scripts)

			a := &Adapter{}
			got := a.Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
			if !tc.want {
				return
			}
			if a.scriptOK != tc.wantScriptOK {
				t.Errorf("scriptOK = %v, want %v", a.scriptOK, tc.wantScriptOK)
			}
			if a.watchInScript != tc.wantWatchInScript {
				t.Errorf("watchInScript = %v, want %v", a.watchInScript, tc.wantWatchInScript)
			}
		})
	}
}

func TestAdapterMatchesFormDMissingPackageJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if (&Adapter{}).Matches([]string{"npm", "test"}) {
		t.Fatal("expected no match without package.json")
	}
}

func TestAdapterMatchesFormDInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if (&Adapter{}).Matches([]string{"npm", "test"}) {
		t.Fatal("expected no match with invalid package.json")
	}
}
