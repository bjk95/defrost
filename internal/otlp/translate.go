package otlp

import (
	"strings"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

// TestResultsToSpans converts runner-adapter output to OTel test-case
// spans under the supplied RunContext's trace and root span. Each result
// is a sibling — schema 3 does not model subtest hierarchy beyond the run
// root.
func TestResultsToSpans(results []models.TestResult, run models.RunContext) []models.Span {
	spans := make([]models.Span, 0, len(results))
	for _, r := range results {
		spans = append(spans, testResultToSpan(r, run))
	}
	return spans
}

func testResultToSpan(r models.TestResult, run models.RunContext) models.Span {
	start := r.StartTime
	if start.IsZero() {
		start = time.Now()
	}
	startNs := start.UnixNano()
	endNs := startNs + r.Duration.Nanoseconds()

	resultStatus := mapResultStatus(r)
	attrs := map[string]any{
		"test.case.name":          r.Id,
		"test.case.result.status": resultStatus,
		"defrost.run_id":          run.RunID,
	}
	if suite := suiteFromTestID(r.Id); suite != "" {
		attrs["test.suite.name"] = suite
	}
	ns, fn := codeFromTestID(r.Id)
	if ns != "" {
		attrs["code.namespace"] = ns
	}
	if fn != "" {
		attrs["code.function"] = fn
	}

	var events []models.SpanEvent
	if r.Output != "" {
		events = []models.SpanEvent{{
			TimeUnixNano: endNs,
			Name:         "test.output",
			Attributes:   map[string]any{"body": r.Output},
		}}
	}

	return models.Span{
		Schema:            models.SchemaV3,
		TraceID:           run.TraceID,
		SpanID:            models.NewSpanID(),
		ParentSpanID:      run.RootSpanID,
		Name:              r.Id,
		Kind:              "INTERNAL",
		StartTimeUnixNano: startNs,
		EndTimeUnixNano:   endNs,
		Status:            resultToSpanStatus(resultStatus, r.Output),
		Attributes:        attrs,
		Events:            events,
		Resource:          run.Resource,
	}
}

// mapResultStatus translates a TestResult into the OTel test semconv
// `test.case.result.status` enum value: passed/failed/skipped/aborted.
func mapResultStatus(r models.TestResult) string {
	if !r.Ran {
		return "skipped"
	}
	if r.Passed {
		return "passed"
	}
	if strings.Contains(r.Output, "panic:") {
		return "aborted"
	}
	return "failed"
}

func resultToSpanStatus(resultStatus, output string) models.SpanStatus {
	switch resultStatus {
	case "passed":
		return models.SpanStatus{Code: "OK"}
	case "skipped":
		return models.SpanStatus{Code: "UNSET"}
	case "failed", "aborted":
		return models.SpanStatus{Code: "ERROR", Message: firstLine(output)}
	}
	return models.SpanStatus{Code: "UNSET"}
}

// suiteFromTestID extracts the suite identifier:
// "github.com/x/p/TestFoo" → "github.com/x/p"
// "tests/test_foo.py::TestClass::test_method" → "tests/test_foo.py"
func suiteFromTestID(id string) string {
	if suite, _, ok := strings.Cut(id, "::"); ok {
		return suite
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[:i]
	}
	return ""
}

// codeFromTestID returns (namespace, function) for a test id. For Go
// "<pkg>/TestFoo" the namespace is the package and function is the test
// name. For pytest "<file>::<class>::<method>" the namespace is the file
// path and function is the trailing token.
func codeFromTestID(id string) (string, string) {
	if i := strings.LastIndex(id, "::"); i >= 0 {
		return suiteFromTestID(id), id[i+2:]
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
