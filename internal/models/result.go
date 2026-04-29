package models

import "time"

type TestResult struct {
	Id        string
	Ran       bool
	Passed    bool
	StartTime time.Time
	Duration  time.Duration
	Output    string
}
