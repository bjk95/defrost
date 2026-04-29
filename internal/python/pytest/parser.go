package pytest

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

type junitDoc struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Cases []junitCase `xml:"testcase"`
}

type junitCase struct {
	ClassName string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitMessage `xml:"failure"`
	Error     *junitMessage `xml:"error"`
	Skipped   *junitMessage `xml:"skipped"`
	SystemOut string        `xml:"system-out"`
	SystemErr string        `xml:"system-err"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// Parse reads a JUnit XML document (xunit2 family) from r and returns one
// models.TestResult per <testcase>. Returns nil and an error only on XML decode
// failure.
func Parse(r io.Reader) ([]models.TestResult, error) {
	var doc junitDoc
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse junit xml: %w", err)
	}
	var out []models.TestResult
	for _, s := range doc.Suites {
		for _, c := range s.Cases {
			out = append(out, mapCase(c))
		}
	}
	return out, nil
}

// ParseFile is a convenience wrapper around Parse for a file on disk.
func ParseFile(path string) ([]models.TestResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

func mapCase(c junitCase) models.TestResult {
	id := c.ClassName + "::" + c.Name
	ran := c.Skipped == nil
	passed := c.Failure == nil && c.Error == nil && c.Skipped == nil
	duration := time.Duration(c.Time * float64(time.Second))

	var parts []string
	if c.Failure != nil {
		if s := formatMessage(*c.Failure); s != "" {
			parts = append(parts, s)
		}
	}
	if c.Error != nil {
		if s := formatMessage(*c.Error); s != "" {
			parts = append(parts, s)
		}
	}
	if c.SystemOut != "" {
		parts = append(parts, c.SystemOut)
	}
	if c.SystemErr != "" {
		parts = append(parts, c.SystemErr)
	}

	return models.TestResult{
		Id:       id,
		Ran:      ran,
		Passed:   passed,
		Duration: duration,
		Output:   strings.Join(parts, "\n"),
	}
}

func formatMessage(m junitMessage) string {
	body := strings.TrimSpace(m.Body)
	switch {
	case m.Message == "" && body == "":
		return ""
	case m.Message == "":
		return body
	case body == "":
		return m.Message
	default:
		return m.Message + "\n" + body
	}
}
