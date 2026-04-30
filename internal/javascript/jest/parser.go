package jest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

// jestDoc is the top-level JSON shape jest writes when invoked with
// --json --outputFile=<path>. We decode only the fields we actually use.
type jestDoc struct {
	TestResults []jestFileResult `json:"testResults"`
}

type jestFileResult struct {
	Name             string          `json:"name"`
	Status           string          `json:"status"`
	Message          string          `json:"message"`
	AssertionResults []jestAssertion `json:"assertionResults"`
}

type jestAssertion struct {
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	AncestorTitles  []string `json:"ancestorTitles"`
	FailureMessages []string `json:"failureMessages"`
	Duration        *float64 `json:"duration"`
}

// Parse reads a jest --json --outputFile document from r and returns one
// models.TestResult per assertion. Test file paths are made relative to
// cwd. Returns nil and an error only on JSON decode failure.
func Parse(r io.Reader, cwd string) ([]models.TestResult, error) {
	var doc jestDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse jest json: %w", err)
	}
	var out []models.TestResult
	for _, f := range doc.TestResults {
		rel := relPath(f.Name, cwd)
		if len(f.AssertionResults) == 0 {
			if f.Status == "failed" && f.Message != "" {
				out = append(out, fileExecError(rel, f.Message))
			}
			continue
		}
		for _, a := range f.AssertionResults {
			out = append(out, mapAssertion(rel, a))
		}
	}
	return out, nil
}

// ParseFile is a convenience wrapper around Parse for a file on disk.
func ParseFile(path, cwd string) ([]models.TestResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, cwd)
}

func relPath(abs, cwd string) string {
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return abs
	}
	return rel
}

func mapAssertion(relFile string, a jestAssertion) models.TestResult {
	id := relFile + "::"
	if len(a.AncestorTitles) > 0 {
		id += strings.Join(a.AncestorTitles, " > ") + " > " + a.Title
	} else {
		id += a.Title
	}
	ran := a.Status == "passed" || a.Status == "failed"
	passed := a.Status == "passed"

	var duration time.Duration
	if a.Duration != nil {
		duration = time.Duration(*a.Duration * float64(time.Millisecond))
	}

	output := ""
	if a.Status == "failed" {
		output = strings.Join(a.FailureMessages, "\n")
	}

	return models.TestResult{
		Id:       id,
		Ran:      ran,
		Passed:   passed,
		Duration: duration,
		Output:   output,
	}
}

func fileExecError(relFile, message string) models.TestResult {
	return models.TestResult{
		Id:     relFile + models.FileErrorSuffix,
		Ran:    true,
		Passed: false,
		Output: message,
	}
}
