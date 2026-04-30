package models

import (
	"strings"
	"time"
)

type TestResult struct {
	Id        string
	Ran       bool
	Passed    bool
	StartTime time.Time
	Duration  time.Duration
	Output    string
}

// FileErrorSuffix marks results that aren't real test failures but rather
// the test runner's report of a file/import/build error at the module
// level (e.g. jest's "could not load test file"). Adapters synthesise
// IDs with this suffix when no individual test executed but a failure
// surfaced at the file level. The exec exit-code rewrite never suppresses
// these — non-test failures must always force a non-zero exit.
const FileErrorSuffix = "::<file-error>"

// IsFileError reports whether r is a synthetic file-level failure (see
// FileErrorSuffix), as opposed to an actual test that ran and failed.
func (r TestResult) IsFileError() bool {
	return strings.HasSuffix(r.Id, FileErrorSuffix)
}
