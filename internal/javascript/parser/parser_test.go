package parser

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

// loadFixture reads a fixture file, rewrites the __CWD__ placeholder to
// the given cwd, and returns the resulting bytes.
func loadFixture(t *testing.T, name, cwd string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return bytes.ReplaceAll(raw, []byte("__CWD__"), []byte(cwd))
}

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    []models.TestResult
	}{
		{
			name:    "single passing test",
			fixture: "pass.json",
			want: []models.TestResult{
				{
					Id:       "basics.test.js::adds correctly",
					Ran:      true,
					Passed:   true,
					Duration: time.Millisecond,
				},
			},
		},
		{
			name:    "single failing test",
			fixture: "fail.json",
			want: []models.TestResult{
				{
					Id:       "basics.test.js::adds correctly",
					Ran:      true,
					Passed:   false,
					Duration: 2 * time.Millisecond,
					Output:   "Error: expected 2 but got 3\n    at Object.<anonymous>",
				},
			},
		},
		{
			name:    "pending status (skip)",
			fixture: "pending.json",
			want: []models.TestResult{
				{Id: "skip.test.js::skipped test", Ran: false, Passed: false},
			},
		},
		{
			name:    "todo status",
			fixture: "todo.json",
			want: []models.TestResult{
				{Id: "skip.test.js::todo test", Ran: false, Passed: false},
			},
		},
		{
			name:    "ancestor titles joined with ' > '",
			fixture: "ancestors.json",
			want: []models.TestResult{
				{
					Id:       "describe.test.js::math > addition > adds correctly",
					Ran:      true,
					Passed:   true,
					Duration: time.Millisecond,
				},
			},
		},
		{
			name:    "nested describes with same title produce distinct IDs",
			fixture: "nested-describes.json",
			want: []models.TestResult{
				{
					Id:       "nested.test.js::outer > A > same",
					Ran:      true,
					Passed:   true,
					Duration: time.Millisecond,
				},
				{
					Id:       "nested.test.js::outer > B > same",
					Ran:      true,
					Passed:   true,
					Duration: 2 * time.Millisecond,
				},
			},
		},
		{
			name:    "multi-file mixed results",
			fixture: "multi-file.json",
			want: []models.TestResult{
				{
					Id:       "a.test.js::passes",
					Ran:      true,
					Passed:   true,
					Duration: time.Millisecond,
				},
				{
					Id:       "sub/b.test.js::fails",
					Ran:      true,
					Passed:   false,
					Duration: 2 * time.Millisecond,
					Output:   "boom",
				},
				{
					Id:     "sub/b.test.js::skipped",
					Ran:    false,
					Passed: false,
				},
			},
		},
		{
			name:    "testExecError emits synthetic file-error result",
			fixture: "exec-error.json",
			want: []models.TestResult{
				{
					Id:     "broken.test.js::<file-error>",
					Ran:    true,
					Passed: false,
					Output: "SyntaxError: Unexpected token\n    at Module._compile",
				},
			},
		},
		{
			name:    "null duration / startAt yield zero values",
			fixture: "null-duration.json",
			want: []models.TestResult{
				{Id: "x.test.js::no timing info", Ran: true, Passed: true},
			},
		},
		{
			name:    "empty results",
			fixture: "empty.json",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			data := loadFixture(t, tc.fixture, cwd)

			got, err := Parse(bytes.NewReader(data), cwd)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d, len(want)=%d\ngot: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i].Id != tc.want[i].Id {
					t.Errorf("[%d] Id: got %q, want %q", i, got[i].Id, tc.want[i].Id)
				}
				if got[i].Ran != tc.want[i].Ran {
					t.Errorf("[%d] Ran: got %v, want %v", i, got[i].Ran, tc.want[i].Ran)
				}
				if got[i].Passed != tc.want[i].Passed {
					t.Errorf("[%d] Passed: got %v, want %v", i, got[i].Passed, tc.want[i].Passed)
				}
				if got[i].Duration != tc.want[i].Duration {
					t.Errorf("[%d] Duration: got %v, want %v", i, got[i].Duration, tc.want[i].Duration)
				}
				if !got[i].StartTime.Equal(tc.want[i].StartTime) {
					t.Errorf("[%d] StartTime: got %v, want %v", i, got[i].StartTime, tc.want[i].StartTime)
				}
				if strings.TrimSpace(got[i].Output) != strings.TrimSpace(tc.want[i].Output) {
					t.Errorf("[%d] Output: got %q, want %q", i, got[i].Output, tc.want[i].Output)
				}
			}
		})
	}
}
