package flake

import (
	"math"
	"testing"
)

func TestTransitionRate(t *testing.T) {
	tests := []struct {
		name     string
		outcomes []Outcome
		want     float64
	}{
		{"empty", nil, 0},
		{"single pass", []Outcome{Pass}, 0},
		{"single fail", []Outcome{Fail}, 0},
		{"all skips", []Outcome{Skip, Skip, Skip}, 0},
		{"stable pass", []Outcome{Pass, Pass, Pass, Pass}, 0},
		{"stable fail", []Outcome{Fail, Fail, Fail, Fail}, 0},
		{"alternating", []Outcome{Pass, Fail, Pass, Fail}, 1.0},
		{"one flip in five", []Outcome{Pass, Pass, Fail, Pass, Pass}, 0.5},
		{"two flips in five", []Outcome{Pass, Fail, Pass, Pass, Fail}, 0.75},
		{"skips removed yields all-flip", []Outcome{Pass, Skip, Fail, Skip, Pass}, 1.0},
		{"skips around single pass yields zero", []Outcome{Skip, Pass, Skip}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TransitionRate(tt.outcomes)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("TransitionRate(%v) = %f, want %f", tt.outcomes, got, tt.want)
			}
		})
	}
}
