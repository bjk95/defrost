package pytest

import "testing"

func TestAdapterMatches(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"bare pytest", []string{"pytest"}, true},
		{"pytest with args", []string{"pytest", "tests/"}, true},
		{"python -m pytest", []string{"python", "-m", "pytest", "tests/"}, true},
		{"python3 -m pytest", []string{"python3", "-m", "pytest", "tests/"}, true},
		{"python3.12 -m pytest", []string{"python3.12", "-m", "pytest", "tests/"}, true},
		{"poetry run pytest", []string{"poetry", "run", "pytest", "tests/"}, true},
		{"uv run pytest", []string{"uv", "run", "pytest", "tests/"}, true},
		{"pipenv run pytest", []string{"pipenv", "run", "pytest", "tests/"}, true},
		{"pytest with junitxml (collision handled in Run)", []string{"pytest", "--junitxml=foo.xml"}, true},

		{"go test", []string{"go", "test", "./..."}, false},
		{"empty", []string{}, false},
		{"python alone", []string{"python"}, false},
		{"python -m other", []string{"python", "-m", "unittest"}, false},
		{"poetry run other", []string{"poetry", "run", "ruff"}, false},
		{"poetry without run", []string{"poetry", "install"}, false},
		{"python4", []string{"python4", "-m", "pytest"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Adapter{}.Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}
