package vitest

import (
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
