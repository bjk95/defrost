package pytest

import (
	"strings"
	"testing"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    []models.TestResult
	}{
		{
			name:    "single passing test",
			fixture: "testdata/pass.xml",
			want: []models.TestResult{
				{Id: "tests.test_a::test_pass", Ran: true, Passed: true, Duration: time.Millisecond},
			},
		},
		{
			name:    "single failing test",
			fixture: "testdata/fail.xml",
			want: []models.TestResult{
				{
					Id: "tests.test_a::test_fail", Ran: true, Passed: false,
					Duration: 2 * time.Millisecond,
					Output:   "assert 1 == 2\nTraceback line",
				},
			},
		},
		{
			name:    "single errored test",
			fixture: "testdata/error.xml",
			want: []models.TestResult{
				{
					Id: "tests.test_a::test_error", Ran: true, Passed: false,
					Duration: 3 * time.Millisecond,
					Output:   "setup failure\nboom",
				},
			},
		},
		{
			name:    "single skipped test",
			fixture: "testdata/skip.xml",
			want: []models.TestResult{
				{Id: "tests.test_a::test_skip", Ran: false, Passed: false, Duration: 0},
			},
		},
		{
			name:    "mixed suite with system-out and system-err",
			fixture: "testdata/mixed.xml",
			want: []models.TestResult{
				{
					Id: "tests.test_a::test_pass", Ran: true, Passed: true,
					Duration: time.Millisecond,
					Output:   "hello stdout",
				},
				{
					Id: "tests.test_a::test_fail", Ran: true, Passed: false,
					Duration: 2 * time.Millisecond,
					Output:   "assert 1 == 2\nstack\ncaptured stdout\ncaptured stderr",
				},
				{
					Id: "tests.test_a::test_skip", Ran: false, Passed: false, Duration: 0,
				},
			},
		},
		{
			name:    "empty suite",
			fixture: "testdata/empty.xml",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseFile(tc.fixture)
			if err != nil {
				t.Fatalf("ParseFile(%s) error: %v", tc.fixture, err)
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
				if strings.TrimSpace(got[i].Output) != strings.TrimSpace(tc.want[i].Output) {
					t.Errorf("[%d] Output: got %q, want %q", i, got[i].Output, tc.want[i].Output)
				}
				if !got[i].StartTime.IsZero() {
					t.Errorf("[%d] StartTime: got %v, want zero", i, got[i].StartTime)
				}
			}
		})
	}
}
