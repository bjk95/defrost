package jest

import (
	"os"
	"path/filepath"
	"testing"
)

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
		// crude JSON-string escaping is fine here: tests control inputs
		body += `"` + k + `": "` + v + `"`
	}
	body += `}}`
	if err := os.WriteFile("package.json", []byte(body), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
}

func TestAdapterMatchesPureArgv(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"bare jest", []string{"jest"}, true},
		{"jest with args", []string{"jest", "tests/"}, true},
		{"jest with --json (collision handled in Run)", []string{"jest", "--json"}, true},
		{"jest with --watch (collision handled in Run)", []string{"jest", "--watch"}, true},
		{"npx jest", []string{"npx", "jest", "tests/"}, true},
		{"npx with flags before jest", []string{"npx", "-y", "jest"}, true},
		{"npx --no-install jest", []string{"npx", "--no-install", "jest"}, true},
		{"npx --package=jest jest (= form does not consume next token)", []string{"npx", "--package=jest", "jest"}, true},
		{"npx --package foo jest (foo is package value, jest is exec)", []string{"npx", "--package", "foo", "jest"}, true},
		{"bunx jest", []string{"bunx", "jest"}, true},
		{"yarn jest", []string{"yarn", "jest", "tests/"}, true},
		{"pnpm jest", []string{"pnpm", "jest", "tests/"}, true},
		{"node_modules/.bin/jest", []string{"node_modules/.bin/jest"}, true},
		{"./node_modules/.bin/jest", []string{"./node_modules/.bin/jest"}, true},
		{"/abs/path/node_modules/.bin/jest", []string{"/repo/node_modules/.bin/jest"}, true},

		{"empty", []string{}, false},
		{"go test", []string{"go", "test", "./..."}, false},
		{"pytest", []string{"pytest", "tests/"}, false},
		{"npx other", []string{"npx", "vitest"}, false},
		{"npx --package jest vitest (jest is value of --package, vitest is exec)", []string{"npx", "--package", "jest", "vitest"}, false},
		{"npx -p jest vitest (short --package form)", []string{"npx", "-p", "jest", "vitest"}, false},
		{"npx --call jest other (jest is value of --call)", []string{"npx", "--call", "jest", "other"}, false},
		{"bunx other", []string{"bunx", "vitest"}, false},
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

func TestAdapterMatchesFormD(t *testing.T) {
	cases := []struct {
		name      string
		scripts   map[string]string
		cmd       []string
		want      bool
		wantOK    bool   // value of scriptOK if matched
		wantName  string // value of scriptName if matched
	}{
		{
			name:     "npm test with scripts.test=jest",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "npm test with scripts.test=jest --config=foo.js",
			scripts:  map[string]string{"test": "jest --config=foo.js"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "env-var prefix accepted",
			scripts:  map[string]string{"test": "NODE_ENV=test jest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "multiple env-var prefix tokens accepted",
			scripts:  map[string]string{"test": "FOO=1 BAR=2 jest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "cross-env wrapper rejected by shape (matches form, scriptOK=false)",
			scripts:  map[string]string{"test": "cross-env NODE_ENV=test jest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:     "composite && rejected",
			scripts:  map[string]string{"test": "jest && eslint ."},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:     "react-scripts rejected (no jest token in head)",
			scripts:  map[string]string{"test": "react-scripts test"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:     "vitest rejected",
			scripts:  map[string]string{"test": "vitest"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:     "node escape form rejected",
			scripts:  map[string]string{"test": "node --experimental-vm-modules node_modules/jest/bin/jest.js"},
			cmd:      []string{"npm", "test"},
			want:     true,
			wantOK:   false,
			wantName: "test",
		},
		{
			name:    "scripts.test missing",
			scripts: map[string]string{"build": "tsc"},
			cmd:     []string{"npm", "test"},
			want:    false,
		},
		{
			name:     "npm run jest with scripts.jest=jest",
			scripts:  map[string]string{"jest": "jest"},
			cmd:      []string{"npm", "run", "jest"},
			want:     true,
			wantOK:   true,
			wantName: "jest",
		},
		{
			name:    "npm run lint with non-jest script",
			scripts: map[string]string{"lint": "eslint ."},
			cmd:     []string{"npm", "run", "lint"},
			want:    true, // form recognized; script shape failure surfaced in Run
			wantOK:  false,
			wantName: "lint",
		},
		{
			name:     "yarn test",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"yarn", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "yarn run test",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"yarn", "run", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "pnpm test",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"pnpm", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "pnpm run test",
			scripts:  map[string]string{"test": "jest"},
			cmd:      []string{"pnpm", "run", "test"},
			want:     true,
			wantOK:   true,
			wantName: "test",
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
			if got {
				if a.scriptName != tc.wantName {
					t.Errorf("scriptName: got %q, want %q", a.scriptName, tc.wantName)
				}
				if a.scriptOK != tc.wantOK {
					t.Errorf("scriptOK: got %v, want %v", a.scriptOK, tc.wantOK)
				}
			}
		})
	}
}

func TestAdapterMatchesFormDMissingPackageJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// no package.json written

	if (&Adapter{}).Matches([]string{"npm", "test"}) {
		t.Fatal("expected no match when package.json is missing")
	}
}

func TestAdapterMatchesFormDInvalidPackageJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	if (&Adapter{}).Matches([]string{"npm", "test"}) {
		t.Fatal("expected no match when package.json is invalid")
	}
}

func TestLooksLikeJestScript(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"jest", true},
		{"jest --config=foo.js", true},
		{"NODE_ENV=test jest", true},
		{"FOO=1 BAR=2 jest --watch=false", true},
		{"cross-env NODE_ENV=test jest", false},
		{"jest && eslint .", false},
		{"jest || true", false},
		{"jest ; true", false},
		{"jest | tee out.txt", false},
		{"jest > out.txt", false},
		{"jest < in.txt", false},
		{"jest &", false},
		{"jest --runInBand&&echo ok", false},
		{"jest --config=foo.js>out.txt", false},
		{"jest --foo|bar", false},
		{"jest --testNamePattern=foo;rm", false},
		{"jest `whoami`", false},
		{"jest $(whoami)", false},
		{"jest (subshell)", false},
		{"react-scripts test", false},
		{"vitest", false},
		{"node node_modules/jest/bin/jest.js", false},
		{"", false},
		{"   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if got := looksLikeJestScript(tc.value); got != tc.want {
				t.Errorf("looksLikeJestScript(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestHasUserJSONFlag(t *testing.T) {
	cases := []struct {
		cmd  []string
		want bool
	}{
		{[]string{"jest"}, false},
		{[]string{"jest", "tests/"}, false},
		{[]string{"jest", "--json"}, true},
		{[]string{"jest", "--json=true"}, true},
		{[]string{"jest", "--outputFile"}, true},
		{[]string{"jest", "--outputFile=foo.json"}, true},
	}
	for _, tc := range cases {
		if got := hasUserJSONFlag(tc.cmd); got != tc.want {
			t.Errorf("hasUserJSONFlag(%v) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestHasUserWatchFlag(t *testing.T) {
	cases := []struct {
		cmd  []string
		want bool
	}{
		{[]string{"jest"}, false},
		{[]string{"jest", "tests/"}, false},
		{[]string{"jest", "--watch"}, true},
		{[]string{"jest", "--watchAll"}, true},
		{[]string{"jest", "--watch=true"}, true},
		{[]string{"jest", "--watchAll=true"}, true},
	}
	for _, tc := range cases {
		if got := hasUserWatchFlag(tc.cmd); got != tc.want {
			t.Errorf("hasUserWatchFlag(%v) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}
