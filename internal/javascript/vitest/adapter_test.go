package vitest

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
			name:              "npm test with scripts.test=vitest",
			scripts:           map[string]string{"test": "vitest"},
			cmd:               []string{"npm", "test"},
			want:              true,
			wantScriptOK:      false,
			wantWatchInScript: true,
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
			name:              "yarn test with scripts.test=vitest",
			scripts:           map[string]string{"test": "vitest"},
			cmd:               []string{"yarn", "test"},
			want:              true,
			wantScriptOK:      false,
			wantWatchInScript: true,
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

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns whatever
// was written to stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	w.Close()
	os.Stderr = old
	return <-done
}

func TestRunRejectsWatchArgvForms(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
	}{
		{"bare vitest (no run)", []string{"vitest"}},
		{"vitest --watch", []string{"vitest", "--watch"}},
		{"vitest --watchAll", []string{"vitest", "--watchAll"}},
		{"vitest --ui", []string{"vitest", "--ui"}},
		{"vitest watch", []string{"vitest", "watch"}},
		{"npx vitest (no run)", []string{"npx", "vitest"}},
		{"npx vitest --watch", []string{"npx", "vitest", "--watch"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adapter{}
			a.Matches(tc.cmd) // populate state
			var code int
			stderr := captureStderr(t, func() {
				_, _, code = a.Run(tc.cmd)
			})
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr, "watch") {
				t.Errorf("stderr should mention watch; got %q", stderr)
			}
			if !strings.Contains(stderr, "vitest run") {
				t.Errorf("stderr should suggest 'vitest run'; got %q", stderr)
			}
		})
	}
}

func TestRunRejectsWatchInScriptFormD(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writePackageJSON(t, map[string]string{"test": "vitest --watch"})

	a := &Adapter{}
	if !a.Matches([]string{"npm", "test"}) {
		t.Fatal("expected matcher to recognize form D")
	}
	var code int
	stderr := captureStderr(t, func() {
		_, _, code = a.Run([]string{"npm", "test"})
	})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "scripts.test") {
		t.Errorf("stderr should reference scripts.test; got %q", stderr)
	}
	if !strings.Contains(stderr, "watch") {
		t.Errorf("stderr should mention watch; got %q", stderr)
	}
}

func TestRunAcceptsExplicitRun(t *testing.T) {
	// Sanity check: vitest run is NOT a watch trigger. Run will fall
	// through to the rest of the pipeline (which we stub later); for now
	// it must NOT exit 2 with a watch message.
	a := &Adapter{}
	a.Matches([]string{"vitest", "run"})
	stderr := captureStderr(t, func() {
		_, _, _ = a.Run([]string{"vitest", "run"})
	})
	if strings.Contains(stderr, "watch") && strings.Contains(stderr, "use 'vitest run") {
		t.Errorf("'vitest run' should not trigger watch rejection; got %q", stderr)
	}
}

func TestDetectUserOutputPath(t *testing.T) {
	cases := []struct {
		name     string
		cmd      []string
		wantPath string
		wantOK   bool
	}{
		{
			name:   "no output flags",
			cmd:    []string{"vitest", "run"},
			wantOK: false,
		},
		{
			name:     "--outputFile.json=mine.json wins regardless",
			cmd:      []string{"vitest", "run", "--outputFile.json=mine.json"},
			wantPath: "mine.json",
			wantOK:   true,
		},
		{
			name:     "--outputFile=results.json with --reporter=json",
			cmd:      []string{"vitest", "run", "--outputFile=results.json", "--reporter=json"},
			wantPath: "results.json",
			wantOK:   true,
		},
		{
			name:   "--outputFile=junit.xml with --reporter=junit (not piggybacked)",
			cmd:    []string{"vitest", "run", "--outputFile=junit.xml", "--reporter=junit"},
			wantOK: false,
		},
		{
			name:   "--outputFile=foo with --reporter=json AND --reporter=junit (ambiguous)",
			cmd:    []string{"vitest", "run", "--outputFile=foo", "--reporter=json", "--reporter=junit"},
			wantOK: false,
		},
		{
			name:   "--outputFile=foo with --reporter=html (not piggybacked)",
			cmd:    []string{"vitest", "run", "--outputFile=foo", "--reporter=html"},
			wantOK: false,
		},
		{
			name:   "--outputFile=results.json without explicit --reporter=json",
			cmd:    []string{"vitest", "run", "--outputFile=results.json"},
			wantOK: false,
		},
		{
			name:     "--outputFile.json=x.json wins even with another file-emitting reporter",
			cmd:      []string{"vitest", "run", "--outputFile.json=x.json", "--outputFile.junit=y.xml", "--reporter=junit", "--reporter=json"},
			wantPath: "x.json",
			wantOK:   true,
		},
		{
			name:     "--outputFile foo (space form)",
			cmd:      []string{"vitest", "run", "--outputFile", "foo.json", "--reporter=json"},
			wantPath: "foo.json",
			wantOK:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, ok := detectUserOutputPath(tc.cmd)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (path=%q)", ok, tc.wantOK, path)
			}
			if ok && path != tc.wantPath {
				t.Errorf("path = %q, want %q", path, tc.wantPath)
			}
		})
	}
}

func TestRunPassthroughForFormDShapeFailure(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writePackageJSON(t, map[string]string{"test": "vitest && eslint ."})

	a := &Adapter{}
	if !a.Matches([]string{"npm", "test"}) {
		t.Fatal("expected matcher to recognize form D")
	}
	if a.scriptOK {
		t.Fatal("expected scriptOK=false for composite shell")
	}
	if a.watchInScript {
		t.Fatal("expected watchInScript=false for composite-shell case")
	}
	// We can't easily invoke `npm test` for real in this test, so we just
	// assert that Run prints a passthrough warning and does NOT exit 2.
	var code int
	stderr := captureStderr(t, func() {
		_, _, code = a.Run([]string{"npm", "test"})
	})
	if code == 2 {
		t.Errorf("expected non-2 exit (passthrough), got %d", code)
	}
	if !strings.Contains(stderr, "scripts.test") || !strings.Contains(stderr, "passthrough") {
		t.Errorf("stderr should explain passthrough; got %q", stderr)
	}
}

func TestBuildArgs(t *testing.T) {
	cases := []struct {
		name      string
		cmd       []string
		path      string
		piggyback bool
		want      []string
	}{
		{
			name: "direct vitest run",
			cmd:  []string{"vitest", "run"},
			path: "/tmp/x.json",
			want: []string{"run", "--reporter=json", "--outputFile.json=/tmp/x.json"},
		},
		{
			name: "npx vitest run",
			cmd:  []string{"npx", "vitest", "run"},
			path: "/tmp/x.json",
			want: []string{"vitest", "run", "--reporter=json", "--outputFile.json=/tmp/x.json"},
		},
		{
			name: "yarn vitest run (no separator)",
			cmd:  []string{"yarn", "vitest", "run"},
			path: "/tmp/x.json",
			want: []string{"vitest", "run", "--reporter=json", "--outputFile.json=/tmp/x.json"},
		},
		{
			name: "pnpm vitest run (no separator)",
			cmd:  []string{"pnpm", "vitest", "run"},
			path: "/tmp/x.json",
			want: []string{"vitest", "run", "--reporter=json", "--outputFile.json=/tmp/x.json"},
		},
		{
			name: "npm test (needs separator)",
			cmd:  []string{"npm", "test"},
			path: "/tmp/x.json",
			want: []string{"test", "--", "--reporter=json", "--outputFile.json=/tmp/x.json"},
		},
		{
			name: "npm test with existing -- (no double separator)",
			cmd:  []string{"npm", "test", "--", "tests/foo"},
			path: "/tmp/x.json",
			want: []string{"test", "--", "tests/foo", "--reporter=json", "--outputFile.json=/tmp/x.json"},
		},
		{
			name: "pnpm test (needs separator)",
			cmd:  []string{"pnpm", "test"},
			path: "/tmp/x.json",
			want: []string{"test", "--", "--reporter=json", "--outputFile.json=/tmp/x.json"},
		},
		{
			name: "pnpm run x (needs separator)",
			cmd:  []string{"pnpm", "run", "x"},
			path: "/tmp/x.json",
			want: []string{"run", "x", "--", "--reporter=json", "--outputFile.json=/tmp/x.json"},
		},
		{
			name:      "piggyback skips injection (direct)",
			cmd:       []string{"vitest", "run", "--reporter=json", "--outputFile=mine.json"},
			path:      "mine.json",
			piggyback: true,
			want:      []string{"run", "--reporter=json", "--outputFile=mine.json"},
		},
		{
			name:      "piggyback skips injection (npm)",
			cmd:       []string{"npm", "test", "--", "--reporter=json", "--outputFile=mine.json"},
			path:      "mine.json",
			piggyback: true,
			want:      []string{"test", "--", "--reporter=json", "--outputFile=mine.json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildArgs(tc.cmd, tc.path, tc.piggyback)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("\ngot:  %v\nwant: %v", got, tc.want)
			}
		})
	}
}

// --- Bug fixes from PR #30 codex bot review ---

func TestAdapterMatchesFormDBareVitestIsWatchTrigger(t *testing.T) {
	// Bug P1: scripts.test = "vitest" (bare, no run) hangs in TTY because
	// vitest defaults to watch mode. Treat as watchInScript so Run exits 2.
	dir := t.TempDir()
	t.Chdir(dir)
	writePackageJSON(t, map[string]string{"test": "vitest"})

	a := &Adapter{}
	if !a.Matches([]string{"npm", "test"}) {
		t.Fatal("expected matcher to recognize form D")
	}
	if a.scriptOK {
		t.Errorf("scriptOK = true; bare 'vitest' should be treated as watch trigger")
	}
	if !a.watchInScript {
		t.Errorf("watchInScript = false; want true for bare vitest")
	}
}

func TestAdapterMatchesFormDExplicitDisableFlags(t *testing.T) {
	// Bug P2: --watch=false / --no-watch should NOT trigger watch detection.
	cases := []struct {
		name    string
		script  string
		wantOK  bool
		wantWIS bool
	}{
		{"vitest --watch=false", "vitest --watch=false", true, false},
		{"vitest --watchAll=false", "vitest --watchAll=false", true, false},
		{"vitest --ui=false", "vitest --ui=false", true, false},
		{"vitest --no-watch", "vitest --no-watch", true, false},
		{"vitest --no-watchAll", "vitest --no-watchAll", true, false},
		{"vitest run --watch=false", "vitest run --watch=false", true, false},
		// True is still a trigger.
		{"vitest --watch=true (still a trigger)", "vitest --watch=true", false, true},
		{"vitest --watch (still a trigger)", "vitest --watch", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writePackageJSON(t, map[string]string{"test": tc.script})
			a := &Adapter{}
			if !a.Matches([]string{"npm", "test"}) {
				t.Fatal("expected matcher to recognize form D")
			}
			if a.scriptOK != tc.wantOK {
				t.Errorf("scriptOK = %v, want %v", a.scriptOK, tc.wantOK)
			}
			if a.watchInScript != tc.wantWIS {
				t.Errorf("watchInScript = %v, want %v", a.watchInScript, tc.wantWIS)
			}
		})
	}
}

func TestRunArgvWatchDisableFlags(t *testing.T) {
	// Argv-side bug P2: --watch=false / --no-watch should NOT trigger watch
	// rejection in Run.
	cases := []struct {
		name string
		cmd  []string
	}{
		{"vitest --watch=false", []string{"vitest", "--watch=false"}},
		{"vitest --watchAll=false", []string{"vitest", "--watchAll=false"}},
		{"vitest --ui=false", []string{"vitest", "--ui=false"}},
		{"vitest --no-watch", []string{"vitest", "--no-watch"}},
		{"vitest run --watch=false", []string{"vitest", "run", "--watch=false"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adapter{}
			a.Matches(tc.cmd)
			stderr := captureStderr(t, func() {
				_, _, _ = a.Run(tc.cmd)
			})
			// We don't care about exit code (the spawn will fail in test env)
			// — just that we don't print the watch-rejection message.
			if strings.Contains(stderr, "vitest in watch/UI mode can't be wrapped") {
				t.Errorf("argv %v incorrectly rejected as watch mode; stderr: %q", tc.cmd, stderr)
			}
		})
	}
}

// --- Bug fixes from PR #30 codex bot review (round 2) ---

func TestRunArgvShortWatchFlag(t *testing.T) {
	// P2: -w is the vitest short-form alias for --watch.
	cases := []struct {
		name string
		cmd  []string
	}{
		{"vitest -w", []string{"vitest", "-w"}},
		{"vitest run -w", []string{"vitest", "run", "-w"}},
		{"vitest -w=true", []string{"vitest", "-w=true"}},
		{"npx vitest -w", []string{"npx", "vitest", "-w"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adapter{}
			a.Matches(tc.cmd)
			var code int
			stderr := captureStderr(t, func() {
				_, _, code = a.Run(tc.cmd)
			})
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr, "watch") {
				t.Errorf("stderr should mention watch; got %q", stderr)
			}
		})
	}
}

func TestRunArgvShortWatchDisable(t *testing.T) {
	// P2 negative: -w=false explicitly disables watch.
	a := &Adapter{}
	a.Matches([]string{"vitest", "run", "-w=false"})
	stderr := captureStderr(t, func() {
		_, _, _ = a.Run([]string{"vitest", "run", "-w=false"})
	})
	if strings.Contains(stderr, "vitest in watch/UI mode can't be wrapped") {
		t.Errorf("'-w=false' should not trigger watch rejection; got %q", stderr)
	}
}

func TestAdapterMatchesFormDShortWatchFlag(t *testing.T) {
	// P2 in script context: scripts.test = "vitest -w" should be classified
	// as watch.
	dir := t.TempDir()
	t.Chdir(dir)
	writePackageJSON(t, map[string]string{"test": "vitest -w"})

	a := &Adapter{}
	if !a.Matches([]string{"npm", "test"}) {
		t.Fatal("expected matcher to recognize form D")
	}
	if a.scriptOK {
		t.Errorf("scriptOK = true; expected false for 'vitest -w'")
	}
	if !a.watchInScript {
		t.Errorf("watchInScript = false; expected true for 'vitest -w'")
	}
}

func TestRunFormDForwardedWatchFlag(t *testing.T) {
	// P1: user passes --watch through script-runner.
	// scripts.test = "vitest run" is fine on its own, but
	// `npm test -- --watch` forwards --watch to vitest.
	cases := []struct {
		name string
		cmd  []string
	}{
		{"npm test -- --watch", []string{"npm", "test", "--", "--watch"}},
		{"npm test --watch (conservative)", []string{"npm", "test", "--watch"}},
		{"npm test -- -w", []string{"npm", "test", "--", "-w"}},
		{"yarn test --watch (yarn forwards directly)", []string{"yarn", "test", "--watch"}},
		{"pnpm test -- --watch", []string{"pnpm", "test", "--", "--watch"}},
		{"npm test -- --ui", []string{"npm", "test", "--", "--ui"}},
		{"npm run test -- --watchAll", []string{"npm", "run", "test", "--", "--watchAll"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writePackageJSON(t, map[string]string{"test": "vitest run"})

			a := &Adapter{}
			if !a.Matches(tc.cmd) {
				t.Fatalf("expected matcher to recognize form D for %v", tc.cmd)
			}
			if !a.scriptOK {
				t.Fatalf("expected scriptOK=true for clean script; got watchInScript=%v", a.watchInScript)
			}
			var code int
			stderr := captureStderr(t, func() {
				_, _, code = a.Run(tc.cmd)
			})
			if code != 2 {
				t.Errorf("exit code = %d, want 2 for %v", code, tc.cmd)
			}
			if !strings.Contains(stderr, "watch") {
				t.Errorf("stderr should mention watch for %v; got %q", tc.cmd, stderr)
			}
		})
	}
}

func TestRunFormDForwardedDisableFlagOK(t *testing.T) {
	// Negative for P1: user explicitly disables watch via forwarded flag.
	dir := t.TempDir()
	t.Chdir(dir)
	writePackageJSON(t, map[string]string{"test": "vitest run"})

	a := &Adapter{}
	a.Matches([]string{"npm", "test", "--", "--watch=false"})
	stderr := captureStderr(t, func() {
		_, _, _ = a.Run([]string{"npm", "test", "--", "--watch=false"})
	})
	if strings.Contains(stderr, "vitest in watch/UI mode") || strings.Contains(stderr, "watch/UI flag passed through") {
		t.Errorf("'--watch=false' should not be rejected; got %q", stderr)
	}
}

func TestRunPiggybackParsesUserOutputFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	outputPath := filepath.Join(dir, "results.json")

	// Pre-write the json fixture as if vitest had produced it.
	fixture := `{
		"testResults": [{
			"name": "` + dir + `/basics.test.js",
			"status": "passed",
			"assertionResults": [{
				"title": "adds correctly",
				"status": "passed",
				"ancestorTitles": [],
				"failureMessages": [],
				"duration": 1
			}]
		}]
	}`
	if err := os.WriteFile(outputPath, []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Use a no-op binary so Run's child returns exit 0 quickly without
	// actually running vitest. detectUserOutputPath returns the pre-written
	// fixture path; Run skips injection and parses straight from there.
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Fatalf("LookPath true: %v", err)
	}
	a := &Adapter{}
	cmd := []string{trueBin, "--reporter=json", "--outputFile=" + outputPath}
	results, _, code := a.Run(cmd)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1; got %+v", len(results), results)
	}
	if results[0].Id != "basics.test.js::adds correctly" {
		t.Errorf("Id = %q, want %q", results[0].Id, "basics.test.js::adds correctly")
	}
	if !results[0].Passed {
		t.Errorf("Passed = false, want true")
	}
}
