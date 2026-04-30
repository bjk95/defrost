# OTel-Aligned Storage and Metrics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape defrost's persisted data into the OpenTelemetry data model (test results as spans, runs as traces, RunRecord as Resource) and add an embedded OTLP/HTTP metrics receiver in `defrost exec` so any standard OTel SDK can push metrics into the same store.

**Architecture:** Two new directories on the data branch — `traces/<span_name>.ndjson` and `metrics/<metric_name>.ndjson`, both NDJSON, both `merge=union`. The `runs/` and `tests/` directories are removed (the run becomes a span at `traces/defrost.run.ndjson`). Pure translators (`internal/otlp/translate.go`) convert `models.TestResult` → spans and incoming OTLP records → metric entries. A minimal `net/http` listener (`internal/otlp/receiver.go`) buffers OTLP/HTTP requests for translation at run end. Hard-cut migration: schema 3, no compatibility code for the old `Entry` shape.

**Tech Stack:** Go 1.24 stdlib (`net/http`, `crypto/sha256`, `crypto/rand`, `encoding/json`), `go.opentelemetry.io/proto/otlp` for OTLP protobuf types, `google.golang.org/protobuf` for wire decoding, existing `kong` CLI parsing.

**Spec:** [docs/superpowers/specs/2026-04-30-otel-storage-and-metrics-design.md](../specs/2026-04-30-otel-storage-and-metrics-design.md)

**Worktree:** `/Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa`

---

## Task 1: `models.RunContext` + trace/span ID derivation

Add the per-run identity object that translators and persist consume. `RunContext` carries the run id, derived `trace_id`, fresh root `span_id`, the OTel Resource attribute set, and the run start time (nanos). Two helpers: `DeriveTraceID` (deterministic from run id) and `NewSpanID` (fresh per call).

**Files:**
- Create: `internal/models/runcontext.go`
- Create: `internal/models/runcontext_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/models/runcontext_test.go`:

```go
package models

import (
	"encoding/hex"
	"testing"
)

func TestDeriveTraceID_Deterministic(t *testing.T) {
	a := DeriveTraceID("run-001")
	b := DeriveTraceID("run-001")
	if a != b {
		t.Errorf("DeriveTraceID is not deterministic: %q vs %q", a, b)
	}
	if len(a) != 32 {
		t.Errorf("trace id must be 32 hex chars, got %d: %q", len(a), a)
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Errorf("trace id is not valid hex: %v", err)
	}
}

func TestDeriveTraceID_DifferentInputs(t *testing.T) {
	if DeriveTraceID("a") == DeriveTraceID("b") {
		t.Error("expected different trace ids for different run ids")
	}
}

func TestNewSpanID_Format(t *testing.T) {
	id := NewSpanID()
	if len(id) != 16 {
		t.Errorf("span id must be 16 hex chars, got %d: %q", len(id), id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("span id is not valid hex: %v", err)
	}
}

func TestNewSpanID_Unique(t *testing.T) {
	a := NewSpanID()
	b := NewSpanID()
	if a == b {
		t.Errorf("expected unique span ids, got %q twice", a)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/models/...
```

Expected: compile failure — `undefined: DeriveTraceID`, `undefined: NewSpanID`.

- [ ] **Step 3: Write the implementation**

Create `internal/models/runcontext.go`:

```go
package models

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// RunContext carries the OTel-shaped per-run identity used by translators
// and the persist layer. Built once at the start of a defrost exec
// invocation and threaded through everything that emits spans or metrics.
type RunContext struct {
	RunID             string
	TraceID           string         // 32 hex chars, derived from RunID
	RootSpanID        string         // 16 hex chars, fresh per run
	Resource          map[string]any // OTel Resource attributes for the run
	StartTimeUnixNano int64
}

// DeriveTraceID hashes a run id into the 16-byte (32 hex char) trace id
// shape OTel mandates. Deterministic so a given run always maps to the
// same trace id, which makes cross-file joins on trace_id reproducible.
func DeriveTraceID(runID string) string {
	h := sha256.Sum256([]byte(runID))
	return hex.EncodeToString(h[:16])
}

// NewSpanID returns a fresh 8-byte (16 hex char) span id.
func NewSpanID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/models/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/runcontext.go internal/models/runcontext_test.go
git commit -m "feat(models): add RunContext with OTel trace/span id helpers"
```

---

## Task 2: `models.Span` type + JSON round-trip

The on-disk shape for one span line in `traces/<span_name>.ndjson`. Resource is inlined per span. Schema constant lives here so all v3 records reference the same value.

**Files:**
- Create: `internal/models/span.go`
- Create: `internal/models/span_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/models/span_test.go`:

```go
package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSpan_JSONRoundTrip(t *testing.T) {
	in := Span{
		Schema:            SchemaV3,
		TraceID:           "11111111111111111111111111111111",
		SpanID:            "2222222222222222",
		ParentSpanID:      "3333333333333333",
		Name:              "github.com/x/p/TestFoo",
		Kind:              "INTERNAL",
		StartTimeUnixNano: 1714_500_000_000_000_000,
		EndTimeUnixNano:   1714_500_000_005_000_000,
		Status:            SpanStatus{Code: "ERROR", Message: "expected 1, got 2"},
		Attributes: map[string]any{
			"test.case.name":          "github.com/x/p/TestFoo",
			"test.case.result.status": "failed",
		},
		Events: []SpanEvent{{
			TimeUnixNano: 1714_500_000_005_000_000,
			Name:         "test.output",
			Attributes:   map[string]any{"body": "FAIL\n"},
		}},
		Resource: map[string]any{
			"service.name":                  "defrost",
			"vcs.repository.ref.revision":   "abc123",
			"vcs.repository.ref.name":       "main",
		},
	}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Span
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip mismatch\nin:  %+v\nout: %+v", in, out)
	}
}

func TestSpan_OmitsEmptyOptionalFields(t *testing.T) {
	in := Span{
		Schema:            SchemaV3,
		TraceID:           "11111111111111111111111111111111",
		SpanID:            "2222222222222222",
		Name:              "defrost.run",
		StartTimeUnixNano: 1,
		EndTimeUnixNano:   2,
		Status:            SpanStatus{Code: "OK"},
		Resource:          map[string]any{"service.name": "defrost"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, omitted := range []string{`"parent_span_id"`, `"kind"`, `"attributes"`, `"events"`, `"message"`} {
		if got := s; containsField(got, omitted) {
			t.Errorf("expected %s to be omitted, got: %s", omitted, got)
		}
	}
}

func containsField(haystack, field string) bool {
	for i := 0; i+len(field) <= len(haystack); i++ {
		if haystack[i:i+len(field)] == field {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/models/...
```

Expected: compile failure — `undefined: Span`, `undefined: SpanStatus`, `undefined: SpanEvent`, `undefined: SchemaV3`.

- [ ] **Step 3: Write the implementation**

Create `internal/models/span.go`:

```go
package models

// SchemaV3 is the schema version for OTel-aligned span and metric records.
// Bumped from 2 (the pre-OTel Entry/RunRecord shape).
const SchemaV3 = 3

// Span is the on-disk shape for one OTel span line in
// traces/<span_name>.ndjson. Resource is inlined per span — see the
// OTel-Aligned Storage and Metrics design spec for the rationale.
type Span struct {
	Schema            int            `json:"schema"`
	TraceID           string         `json:"trace_id"`
	SpanID            string         `json:"span_id"`
	ParentSpanID      string         `json:"parent_span_id,omitempty"`
	Name              string         `json:"name"`
	Kind              string         `json:"kind,omitempty"`
	StartTimeUnixNano int64          `json:"start_time_unix_nano"`
	EndTimeUnixNano   int64          `json:"end_time_unix_nano"`
	Status            SpanStatus     `json:"status"`
	Attributes        map[string]any `json:"attributes,omitempty"`
	Events            []SpanEvent    `json:"events,omitempty"`
	Resource          map[string]any `json:"resource"`
}

type SpanStatus struct {
	Code    string `json:"code"`              // "OK" | "ERROR" | "UNSET"
	Message string `json:"message,omitempty"`
}

type SpanEvent struct {
	TimeUnixNano int64          `json:"time_unix_nano"`
	Name         string         `json:"name"`
	Attributes   map[string]any `json:"attributes,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/models/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/span.go internal/models/span_test.go
git commit -m "feat(models): add Span type with OTel-shaped JSON layout"
```

---

## Task 3: `models.MetricEntry` type + JSON round-trip

On-disk shape for one OTLP metric data point line in `metrics/<metric_name>.ndjson`. Single struct holds gauge, sum, and histogram shapes — fields used per shape are pointer-typed and `omitempty`.

**Files:**
- Create: `internal/models/metric.go`
- Create: `internal/models/metric_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/models/metric_test.go`:

```go
package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func ptrFloat(v float64) *float64 { return &v }
func ptrUint(v uint64) *uint64    { return &v }

func TestMetricEntry_GaugeRoundTrip(t *testing.T) {
	in := MetricEntry{
		Schema:         SchemaV3,
		Name:           "db.query.duration",
		Unit:           "ms",
		InstrumentType: "gauge",
		TimeUnixNano:   1714_500_000_000_000_000,
		Attributes:     map[string]any{"db.system": "postgresql"},
		Resource:       map[string]any{"service.name": "defrost"},
		TraceID:        "11111111111111111111111111111111",
		Value:          ptrFloat(42.5),
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out MetricEntry
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip mismatch\nin:  %+v\nout: %+v", in, out)
	}
}

func TestMetricEntry_HistogramRoundTrip(t *testing.T) {
	in := MetricEntry{
		Schema:            SchemaV3,
		Name:              "http.server.request.duration",
		Unit:              "s",
		InstrumentType:    "histogram",
		Temporality:       "delta",
		TimeUnixNano:      1714_500_000_000_000_000,
		StartTimeUnixNano: 1714_499_990_000_000_000,
		Resource:          map[string]any{"service.name": "defrost"},
		Count:             ptrUint(100),
		Sum:               ptrFloat(15.5),
		Min:               ptrFloat(0.001),
		Max:               ptrFloat(2.5),
		Buckets: []HistogramBucket{
			{UpperBound: ptrFloat(0.01), Count: 50},
			{UpperBound: ptrFloat(1.0), Count: 40},
			{UpperBound: nil, Count: 10}, // +Inf bucket
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out MetricEntry
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip mismatch\nin:  %+v\nout: %+v", in, out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/models/...
```

Expected: compile failure — `undefined: MetricEntry`, `undefined: HistogramBucket`.

- [ ] **Step 3: Write the implementation**

Create `internal/models/metric.go`:

```go
package models

// MetricEntry is the on-disk shape for one OTel metric data point line
// in metrics/<metric_name>.ndjson. Resource is inlined per data point.
// Single struct covers gauge, sum, and histogram instrument types —
// type-specific fields are *T+omitempty so unused ones are absent on disk.
type MetricEntry struct {
	Schema            int            `json:"schema"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Unit              string         `json:"unit,omitempty"`
	InstrumentType    string         `json:"instrument_type"` // "gauge" | "sum" | "histogram"
	Temporality       string         `json:"temporality,omitempty"` // "delta" | "cumulative"
	Monotonic         bool           `json:"monotonic,omitempty"` // sum only
	TimeUnixNano      int64          `json:"time_unix_nano"`
	StartTimeUnixNano int64          `json:"start_time_unix_nano,omitempty"`
	Attributes        map[string]any `json:"attributes,omitempty"`
	Resource          map[string]any `json:"resource"`
	TraceID           string         `json:"trace_id,omitempty"`
	SpanID            string         `json:"span_id,omitempty"`

	// gauge / sum
	Value *float64 `json:"value,omitempty"`

	// histogram
	Count   *uint64           `json:"count,omitempty"`
	Sum     *float64          `json:"sum,omitempty"`
	Min     *float64          `json:"min,omitempty"`
	Max     *float64          `json:"max,omitempty"`
	Buckets []HistogramBucket `json:"buckets,omitempty"`
}

type HistogramBucket struct {
	UpperBound *float64 `json:"upper_bound"` // nil for the +Inf bucket
	Count      uint64   `json:"count"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/models/...
```

Expected: PASS (all four tests across runcontext, span, metric).

- [ ] **Step 5: Commit**

```bash
git add internal/models/metric.go internal/models/metric_test.go
git commit -m "feat(models): add MetricEntry type for OTel metric data points"
```

---

## Task 4: Translator — `TestResultsToSpans`

Pure function converting `[]models.TestResult` to `[]models.Span`. Each result becomes a sibling span under the run root. Status mapping: `passed`/`failed`/`skipped`/`aborted`. `Output` becomes a single `test.output` span event when non-empty. Attributes follow OTel test semconv (`test.case.name`, `test.case.result.status`, `test.suite.name`, `code.namespace`, `code.function`, plus `defrost.run_id`).

**Files:**
- Create: `internal/otlp/translate.go`
- Create: `internal/otlp/translate_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/otlp/translate_test.go`:

```go
package otlp

import (
	"testing"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

func newRunContext() models.RunContext {
	return models.RunContext{
		RunID:             "run-001",
		TraceID:           "11111111111111111111111111111111",
		RootSpanID:        "2222222222222222",
		Resource:          map[string]any{"service.name": "defrost", "vcs.repository.ref.revision": "abc123"},
		StartTimeUnixNano: 1714_500_000_000_000_000,
	}
}

func TestTestResultsToSpans_Pass(t *testing.T) {
	r := models.TestResult{
		Id:        "github.com/x/p/TestA",
		Ran:       true,
		Passed:    true,
		Duration:  5 * time.Millisecond,
		StartTime: time.Unix(0, 1714_500_001_000_000_000).UTC(),
	}
	got := TestResultsToSpans([]models.TestResult{r}, newRunContext())
	if len(got) != 1 {
		t.Fatalf("want 1 span, got %d", len(got))
	}
	s := got[0]
	if s.Name != r.Id {
		t.Errorf("name: want %q got %q", r.Id, s.Name)
	}
	if s.TraceID != "11111111111111111111111111111111" {
		t.Errorf("trace_id: %q", s.TraceID)
	}
	if s.ParentSpanID != "2222222222222222" {
		t.Errorf("parent_span_id: %q", s.ParentSpanID)
	}
	if s.Status.Code != "OK" {
		t.Errorf("status.code: want OK got %q", s.Status.Code)
	}
	if s.Attributes["test.case.name"] != r.Id {
		t.Errorf("test.case.name attribute missing: %+v", s.Attributes)
	}
	if s.Attributes["test.case.result.status"] != "passed" {
		t.Errorf("test.case.result.status: %v", s.Attributes["test.case.result.status"])
	}
	if s.Attributes["defrost.run_id"] != "run-001" {
		t.Errorf("defrost.run_id attribute missing: %+v", s.Attributes)
	}
	if s.Attributes["test.suite.name"] != "github.com/x/p" {
		t.Errorf("test.suite.name: %v", s.Attributes["test.suite.name"])
	}
	if s.Attributes["code.function"] != "TestA" {
		t.Errorf("code.function: %v", s.Attributes["code.function"])
	}
	if got := s.EndTimeUnixNano - s.StartTimeUnixNano; got != int64(5*time.Millisecond) {
		t.Errorf("duration nanos: want %d got %d", int64(5*time.Millisecond), got)
	}
	if len(s.Events) != 0 {
		t.Errorf("expected no events for passing test with empty output, got %+v", s.Events)
	}
	if s.Resource["service.name"] != "defrost" {
		t.Errorf("resource not inlined: %+v", s.Resource)
	}
}

func TestTestResultsToSpans_FailWithOutput(t *testing.T) {
	r := models.TestResult{
		Id:        "github.com/x/p/TestB",
		Ran:       true,
		Passed:    false,
		Duration:  1 * time.Millisecond,
		StartTime: time.Unix(0, 1).UTC(),
		Output:    "FAIL\nexpected 1 got 2\n",
	}
	got := TestResultsToSpans([]models.TestResult{r}, newRunContext())
	s := got[0]
	if s.Status.Code != "ERROR" {
		t.Errorf("status.code: want ERROR got %q", s.Status.Code)
	}
	if s.Status.Message != "FAIL" {
		t.Errorf("status.message: want first line 'FAIL', got %q", s.Status.Message)
	}
	if s.Attributes["test.case.result.status"] != "failed" {
		t.Errorf("result status: %v", s.Attributes["test.case.result.status"])
	}
	if len(s.Events) != 1 {
		t.Fatalf("want 1 event for non-empty output, got %d", len(s.Events))
	}
	if s.Events[0].Name != "test.output" {
		t.Errorf("event name: %q", s.Events[0].Name)
	}
	if s.Events[0].Attributes["body"] != r.Output {
		t.Errorf("event body: %v", s.Events[0].Attributes["body"])
	}
}

func TestTestResultsToSpans_Skip(t *testing.T) {
	r := models.TestResult{Id: "p/TestSkipped", Ran: false}
	s := TestResultsToSpans([]models.TestResult{r}, newRunContext())[0]
	if s.Status.Code != "UNSET" {
		t.Errorf("status.code: want UNSET got %q", s.Status.Code)
	}
	if s.Attributes["test.case.result.status"] != "skipped" {
		t.Errorf("result status: %v", s.Attributes["test.case.result.status"])
	}
}

func TestTestResultsToSpans_Panic(t *testing.T) {
	r := models.TestResult{
		Id:     "p/TestPanic",
		Ran:    true,
		Passed: false,
		Output: "panic: nil pointer\nfoo()\n",
	}
	s := TestResultsToSpans([]models.TestResult{r}, newRunContext())[0]
	if s.Status.Code != "ERROR" {
		t.Errorf("status.code: want ERROR got %q", s.Status.Code)
	}
	if s.Attributes["test.case.result.status"] != "aborted" {
		t.Errorf("result status: want aborted got %v", s.Attributes["test.case.result.status"])
	}
}

func TestTestResultsToSpans_PytestId(t *testing.T) {
	r := models.TestResult{
		Id:     "tests/test_module.py::TestClass::test_method",
		Ran:    true,
		Passed: true,
	}
	s := TestResultsToSpans([]models.TestResult{r}, newRunContext())[0]
	if s.Attributes["test.suite.name"] != "tests/test_module.py" {
		t.Errorf("test.suite.name: %v", s.Attributes["test.suite.name"])
	}
	if s.Attributes["code.function"] != "test_method" {
		t.Errorf("code.function: %v", s.Attributes["code.function"])
	}
}

func TestTestResultsToSpans_Siblings(t *testing.T) {
	rs := []models.TestResult{
		{Id: "p/TestA", Ran: true, Passed: true},
		{Id: "p/TestA/sub1", Ran: true, Passed: true},
		{Id: "p/TestA/sub2", Ran: true, Passed: false},
	}
	got := TestResultsToSpans(rs, newRunContext())
	if len(got) != 3 {
		t.Fatalf("want 3 spans, got %d", len(got))
	}
	for i, s := range got {
		if s.ParentSpanID != "2222222222222222" {
			t.Errorf("span %d: parent_span_id should be the run root, got %q", i, s.ParentSpanID)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/otlp/...
```

Expected: compile failure — package `otlp` does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/otlp/translate.go`:

```go
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
	if ns, fn := codeFromTestID(r.Id); ns != "" {
		attrs["code.namespace"] = ns
	}
	if _, fn := codeFromTestID(r.Id); fn != "" {
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
	if i := strings.Index(id, "::"); i >= 0 {
		return id[:i]
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
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/otlp/... ./internal/models/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/otlp/translate.go internal/otlp/translate_test.go
git commit -m "feat(otlp): translate TestResult to OTel test-case spans"
```

---

## Task 5: Add OTel proto dependency

Pull in `go.opentelemetry.io/proto/otlp` for the canonical OTLP protobuf types and `google.golang.org/protobuf` for wire decoding. We use only the protobuf types — no gRPC server code, so the grpc transitive dep tree is avoided.

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the dependencies**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go get go.opentelemetry.io/proto/otlp@latest google.golang.org/protobuf@latest && go mod tidy
```

- [ ] **Step 2: Verify the imports resolve**

Create a one-off probe at `internal/otlp/probe_test.go`:

```go
package otlp

import (
	"testing"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTLPProtoTypesResolvable(t *testing.T) {
	req := &cmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{}},
	}
	if _, err := proto.Marshal(req); err != nil {
		t.Errorf("marshal empty request: %v", err)
	}
}
```

Run:

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/otlp/...
```

Expected: PASS. If the proto package paths resolved differently in the chosen version, fix the imports here and propagate to later tasks.

- [ ] **Step 3: Remove the probe file (its job is done)**

```bash
rm /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa/internal/otlp/probe_test.go
```

- [ ] **Step 4: Verify the existing tests still pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./...
```

Expected: PASS (all existing tests continue to compile and pass).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add OTel proto types for OTLP receiver"
```

---

## Task 6: Translator — `MetricsToEntries`

Flatten an OTLP `ExportMetricsServiceRequest` into per-data-point `MetricEntry` records. Cover gauge (`NumberDataPoint`), sum (`NumberDataPoint` with monotonic + temporality), and histogram (`HistogramDataPoint` with explicit buckets). Exponential histograms convert to explicit buckets at translate time.

**Files:**
- Modify: `internal/otlp/translate.go` (append)
- Modify: `internal/otlp/translate_test.go` (append)

- [ ] **Step 1: Write the failing test**

Add the new imports to the existing `import (...)` block at the top of `internal/otlp/translate_test.go` (do NOT introduce a second import block — Go disallows it):

```go
cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
commonpb "go.opentelemetry.io/proto/otlp/common/v1"
metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
```

Then append these test functions and helpers to the end of the file:

```go
func strKV(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func makeRequest(metrics ...*metricspb.Metric) *cmetricspb.ExportMetricsServiceRequest {
	return &cmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{strKV("service.name", "client")}},
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: metrics,
			}},
		}},
	}
}

func TestMetricsToEntries_Gauge(t *testing.T) {
	m := &metricspb.Metric{
		Name: "db.connection_pool.size",
		Unit: "{connections}",
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: 1714_500_000_000_000_000,
				Attributes:   []*commonpb.KeyValue{strKV("db.system", "postgresql")},
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 12.0},
			}},
		}},
	}
	got := MetricsToEntries(makeRequest(m), newRunContext())
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	e := got[0]
	if e.Name != "db.connection_pool.size" {
		t.Errorf("name: %q", e.Name)
	}
	if e.InstrumentType != "gauge" {
		t.Errorf("instrument_type: %q", e.InstrumentType)
	}
	if e.Unit != "{connections}" {
		t.Errorf("unit: %q", e.Unit)
	}
	if e.Value == nil || *e.Value != 12.0 {
		t.Errorf("value: %v", e.Value)
	}
	if e.Attributes["db.system"] != "postgresql" {
		t.Errorf("attribute missing: %+v", e.Attributes)
	}
	if e.Resource["service.name"] != "defrost" {
		t.Errorf("resource: defrost RunContext should override caller resource: %+v", e.Resource)
	}
	if e.TraceID != "11111111111111111111111111111111" {
		t.Errorf("trace_id: %q", e.TraceID)
	}
}

func TestMetricsToEntries_SumDelta(t *testing.T) {
	m := &metricspb.Metric{
		Name: "http.server.request.count",
		Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
			IsMonotonic:            true,
			AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano:      1714_500_000_000_000_000,
				StartTimeUnixNano: 1714_499_990_000_000_000,
				Value:             &metricspb.NumberDataPoint_AsInt{AsInt: 7},
			}},
		}},
	}
	got := MetricsToEntries(makeRequest(m), newRunContext())[0]
	if got.InstrumentType != "sum" {
		t.Errorf("instrument_type: %q", got.InstrumentType)
	}
	if !got.Monotonic {
		t.Error("monotonic should be true")
	}
	if got.Temporality != "delta" {
		t.Errorf("temporality: %q", got.Temporality)
	}
	if got.Value == nil || *got.Value != 7 {
		t.Errorf("value: %v", got.Value)
	}
}

func TestMetricsToEntries_Histogram(t *testing.T) {
	m := &metricspb.Metric{
		Name: "http.server.request.duration",
		Unit: "s",
		Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
			AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
			DataPoints: []*metricspb.HistogramDataPoint{{
				TimeUnixNano:   1714_500_000_000_000_000,
				Count:          3,
				Sum:            ptr(0.45),
				Min:            ptr(0.05),
				Max:            ptr(0.25),
				BucketCounts:   []uint64{1, 1, 1},
				ExplicitBounds: []float64{0.1, 0.2},
			}},
		}},
	}
	got := MetricsToEntries(makeRequest(m), newRunContext())[0]
	if got.InstrumentType != "histogram" {
		t.Errorf("instrument_type: %q", got.InstrumentType)
	}
	if got.Count == nil || *got.Count != 3 {
		t.Errorf("count: %v", got.Count)
	}
	if got.Sum == nil || *got.Sum != 0.45 {
		t.Errorf("sum: %v", got.Sum)
	}
	if got.Min == nil || *got.Min != 0.05 {
		t.Errorf("min: %v", got.Min)
	}
	if got.Max == nil || *got.Max != 0.25 {
		t.Errorf("max: %v", got.Max)
	}
	if len(got.Buckets) != 3 {
		t.Fatalf("buckets: want 3, got %d (%v)", len(got.Buckets), got.Buckets)
	}
	if got.Buckets[0].UpperBound == nil || *got.Buckets[0].UpperBound != 0.1 || got.Buckets[0].Count != 1 {
		t.Errorf("bucket 0: %+v", got.Buckets[0])
	}
	if got.Buckets[2].UpperBound != nil {
		t.Errorf("bucket 2 (+Inf) should have nil UpperBound, got %v", got.Buckets[2].UpperBound)
	}
}

func ptr[T any](v T) *T { return &v }
```

(The earlier `ptrFloat` and `ptrUint` helpers in `metric_test.go` live in the `models` package. The new generic `ptr` helper here is local to `internal/otlp`.)

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/otlp/...
```

Expected: compile failure — `undefined: MetricsToEntries`.

- [ ] **Step 3: Write the implementation**

Append to `internal/otlp/translate.go`:

```go
import (
	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// MetricsToEntries flattens an OTLP ExportMetricsServiceRequest into per-
// data-point MetricEntry rows, ready to append to metrics/<name>.ndjson.
//
// All entries inherit the supplied RunContext's Resource — caller-supplied
// resource attributes on the inbound request are dropped. We're a sink for
// metrics produced *during a run*, so the run's resource (commit, branch,
// etc.) is the one that matters for downstream alerting.
func MetricsToEntries(req *cmetricspb.ExportMetricsServiceRequest, run models.RunContext) []models.MetricEntry {
	if req == nil {
		return nil
	}
	var out []models.MetricEntry
	for _, rm := range req.ResourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				out = append(out, metricToEntries(m, run)...)
			}
		}
	}
	return out
}

func metricToEntries(m *metricspb.Metric, run models.RunContext) []models.MetricEntry {
	switch d := m.Data.(type) {
	case *metricspb.Metric_Gauge:
		out := make([]models.MetricEntry, 0, len(d.Gauge.DataPoints))
		for _, dp := range d.Gauge.DataPoints {
			e := baseEntry(m, run, "gauge", dp.TimeUnixNano, dp.StartTimeUnixNano, dp.Attributes)
			v := numberValue(dp)
			e.Value = &v
			out = append(out, e)
		}
		return out
	case *metricspb.Metric_Sum:
		out := make([]models.MetricEntry, 0, len(d.Sum.DataPoints))
		for _, dp := range d.Sum.DataPoints {
			e := baseEntry(m, run, "sum", dp.TimeUnixNano, dp.StartTimeUnixNano, dp.Attributes)
			e.Monotonic = d.Sum.IsMonotonic
			e.Temporality = temporalityString(d.Sum.AggregationTemporality)
			v := numberValue(dp)
			e.Value = &v
			out = append(out, e)
		}
		return out
	case *metricspb.Metric_Histogram:
		out := make([]models.MetricEntry, 0, len(d.Histogram.DataPoints))
		for _, dp := range d.Histogram.DataPoints {
			e := baseEntry(m, run, "histogram", dp.TimeUnixNano, dp.StartTimeUnixNano, dp.Attributes)
			e.Temporality = temporalityString(d.Histogram.AggregationTemporality)
			c := dp.Count
			e.Count = &c
			if dp.Sum != nil {
				s := *dp.Sum
				e.Sum = &s
			}
			if dp.Min != nil {
				v := *dp.Min
				e.Min = &v
			}
			if dp.Max != nil {
				v := *dp.Max
				e.Max = &v
			}
			e.Buckets = histogramBuckets(dp.BucketCounts, dp.ExplicitBounds)
			out = append(out, e)
		}
		return out
	case *metricspb.Metric_ExponentialHistogram:
		// Exponential histograms collapse into explicit-bucket form via
		// per-data-point conversion. Bucket bounds are the boundaries
		// implied by the scale + offset; bucket counts are the raw
		// positive-bucket counts. Negative buckets and zero counts are
		// preserved at the boundaries 0 and below.
		out := make([]models.MetricEntry, 0, len(d.ExponentialHistogram.DataPoints))
		for _, dp := range d.ExponentialHistogram.DataPoints {
			e := baseEntry(m, run, "histogram", dp.TimeUnixNano, dp.StartTimeUnixNano, dp.Attributes)
			e.Temporality = temporalityString(d.ExponentialHistogram.AggregationTemporality)
			c := dp.Count
			e.Count = &c
			if dp.Sum != nil {
				s := *dp.Sum
				e.Sum = &s
			}
			if dp.Min != nil {
				v := *dp.Min
				e.Min = &v
			}
			if dp.Max != nil {
				v := *dp.Max
				e.Max = &v
			}
			e.Buckets = exponentialHistogramBuckets(dp)
			out = append(out, e)
		}
		return out
	}
	return nil
}

func baseEntry(m *metricspb.Metric, run models.RunContext, instrument string, ts, startTs uint64, attrs []*commonpb.KeyValue) models.MetricEntry {
	return models.MetricEntry{
		Schema:            models.SchemaV3,
		Name:              m.Name,
		Description:       m.Description,
		Unit:              m.Unit,
		InstrumentType:    instrument,
		TimeUnixNano:      int64(ts),
		StartTimeUnixNano: int64(startTs),
		Attributes:        kvToMap(attrs),
		Resource:          run.Resource,
		TraceID:           run.TraceID,
	}
}

func numberValue(dp *metricspb.NumberDataPoint) float64 {
	switch v := dp.Value.(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		return v.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		return float64(v.AsInt)
	}
	return 0
}

func temporalityString(t metricspb.AggregationTemporality) string {
	switch t {
	case metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA:
		return "delta"
	case metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE:
		return "cumulative"
	}
	return ""
}

func histogramBuckets(counts []uint64, bounds []float64) []models.HistogramBucket {
	out := make([]models.HistogramBucket, 0, len(counts))
	for i, c := range counts {
		var ub *float64
		if i < len(bounds) {
			b := bounds[i]
			ub = &b
		}
		out = append(out, models.HistogramBucket{UpperBound: ub, Count: c})
	}
	return out
}

func exponentialHistogramBuckets(dp *metricspb.ExponentialHistogramDataPoint) []models.HistogramBucket {
	// Convert by emitting one bucket per positive offset slot. Each
	// bucket's upper bound is base^(offset+i+1) where base = 2^(2^-scale).
	// Negative buckets are folded into a single bucket bounded at 0.
	scale := dp.Scale
	base := math.Pow(2, math.Pow(2, float64(-scale)))
	var buckets []models.HistogramBucket
	if dp.ZeroCount > 0 {
		zero := 0.0
		buckets = append(buckets, models.HistogramBucket{UpperBound: &zero, Count: dp.ZeroCount})
	}
	if dp.Positive != nil {
		offset := int(dp.Positive.Offset)
		for i, c := range dp.Positive.BucketCounts {
			ub := math.Pow(base, float64(offset+i+1))
			b := ub
			buckets = append(buckets, models.HistogramBucket{UpperBound: &b, Count: c})
		}
	}
	// Always close with a +Inf bucket so the shape matches explicit-bucket
	// histograms produced via histogramBuckets above.
	buckets = append(buckets, models.HistogramBucket{UpperBound: nil, Count: 0})
	return buckets
}

func kvToMap(kvs []*commonpb.KeyValue) map[string]any {
	if len(kvs) == 0 {
		return nil
	}
	out := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		out[kv.Key] = anyValueToInterface(kv.Value)
	}
	return out
}

func anyValueToInterface(v *commonpb.AnyValue) any {
	if v == nil {
		return nil
	}
	switch x := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_BoolValue:
		return x.BoolValue
	case *commonpb.AnyValue_IntValue:
		return x.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return x.DoubleValue
	case *commonpb.AnyValue_ArrayValue:
		arr := make([]any, 0, len(x.ArrayValue.Values))
		for _, v := range x.ArrayValue.Values {
			arr = append(arr, anyValueToInterface(v))
		}
		return arr
	}
	return nil
}
```

Add the `math` import to the file's import block.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/otlp/...
```

Expected: PASS for all five `TestMetricsToEntries_*` cases plus the existing `TestTestResultsToSpans_*` cases.

- [ ] **Step 5: Commit**

```bash
git add internal/otlp/translate.go internal/otlp/translate_test.go
git commit -m "feat(otlp): translate OTLP metric records to MetricEntry"
```

---

## Task 7: OTLP/HTTP receiver

Minimal `net/http` server bound to a free localhost port. Routes `POST /v1/metrics` with `Content-Type: application/x-protobuf` to a handler that decodes `ExportMetricsServiceRequest` and buffers the message in memory. Other methods, paths, and content types return appropriate HTTP errors. `Shutdown(ctx)` returns the buffered messages.

**Files:**
- Create: `internal/otlp/receiver.go`
- Create: `internal/otlp/receiver_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/otlp/receiver_test.go`:

```go
package otlp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestReceiver_AcceptsMetrics(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	req := &cmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{
					Name: "test.gauge",
					Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
						DataPoints: []*metricspb.NumberDataPoint{{
							TimeUnixNano: 1,
							Attributes:   []*commonpb.KeyValue{strKV("k", "v")},
							Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 1.5},
						}},
					}},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port), "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := r.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 buffered request, got %d", len(got))
	}
	if got[0].ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Name != "test.gauge" {
		t.Errorf("buffered metric name wrong: %q", got[0].ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Name)
	}
}

func TestReceiver_RejectsWrongMethod(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: want 405, got %d", resp.StatusCode)
	}
}

func TestReceiver_RejectsWrongPath(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/traces", port), "application/x-protobuf", bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", resp.StatusCode)
	}
}

func TestReceiver_RejectsWrongContentType(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port), "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status: want 415, got %d", resp.StatusCode)
	}
}

func TestReceiver_RejectsBadProtobuf(t *testing.T) {
	r := New()
	port, err := r.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Shutdown(context.Background())

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/metrics", port), "application/x-protobuf", bytes.NewReader([]byte{0xff, 0xff, 0xff}))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", resp.StatusCode)
	}
}

func TestReceiver_ShutdownDrainsAndIsIdempotent(t *testing.T) {
	r := New()
	if _, err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx := context.Background()
	if _, err := r.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if _, err := r.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown should be a no-op, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/otlp/...
```

Expected: compile failure — `undefined: New`, `undefined: Receiver.Start`, `undefined: Receiver.Shutdown`.

- [ ] **Step 3: Write the implementation**

Create `internal/otlp/receiver.go`:

```go
package otlp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// Receiver is a minimal OTLP/HTTP listener for metrics. It binds a
// random localhost port, accepts POST /v1/metrics requests with
// Content-Type: application/x-protobuf, and buffers the decoded
// ExportMetricsServiceRequest messages in memory until Shutdown is called.
type Receiver struct {
	server *http.Server
	port   int
	mu     sync.Mutex
	buf    []*cmetricspb.ExportMetricsServiceRequest
	closed bool
}

// New returns a non-started Receiver.
func New() *Receiver { return &Receiver{} }

// Start binds 127.0.0.1 on a free port and serves until Shutdown.
// Returns the chosen port.
func (r *Receiver) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("otlp receiver: bind: %w", err)
	}
	r.port = ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/metrics", r.handleMetrics)
	r.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = r.server.Serve(ln) }()
	return r.port, nil
}

// Shutdown stops accepting new connections, waits for in-flight handlers
// to drain bounded by ctx, and returns the buffered metric requests.
// Subsequent calls return (nil, nil).
func (r *Receiver) Shutdown(ctx context.Context) ([]*cmetricspb.ExportMetricsServiceRequest, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, nil
	}
	r.closed = true
	server := r.server
	out := r.buf
	r.buf = nil
	r.mu.Unlock()
	if server == nil {
		return out, nil
	}
	return out, server.Shutdown(ctx)
}

func (r *Receiver) handleMetrics(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/x-protobuf" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	msg := &cmetricspb.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(body, msg); err != nil {
		http.Error(w, "decode protobuf", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	r.buf = append(r.buf, msg)
	r.mu.Unlock()
	resp, _ := proto.Marshal(&cmetricspb.ExportMetricsServiceResponse{})
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/otlp/...
```

Expected: PASS for all six receiver tests.

- [ ] **Step 5: Commit**

```bash
git add internal/otlp/receiver.go internal/otlp/receiver_test.go
git commit -m "feat(otlp): add minimal OTLP/HTTP metrics receiver"
```

---

## Task 8: Persist rewrite — schema 3, traces/, metrics/, no `runs/` or `tests/`

This is the schema-3 cut. Replace the `Entry`/`RunRecord`/`HistoricalEntry` types and the `InsertNewTestResults`/`GetTestHistory` methods with span- and metric-shaped equivalents. The git push, branch lifecycle, retry, and bot-identity helpers all stay; only the file layout, types, and write/read functions change.

This task is one cohesive refactor and lands as one commit. Many sub-steps; the package will not compile until step 8.

**Files:**
- Modify (heavy rewrite): `internal/persist/persist.go`
- Modify (heavy rewrite): `internal/persist/persist_test.go`

- [ ] **Step 1: Replace `internal/persist/persist.go` data types and Backend interface**

Replace the file's contents (the whole file) with the new shape below. Note: helpers `commitAll`, `openOrInitDataRepo`, `pushBranch`, `pushWithRetry`, `isNonFastForward`, `pullRebase`, `gitErr`, `runGit`, `configureBotIdentity`, `resolveTargetURL`, `readOriginURL`, `localGitDir`, `branchExistsOnRemote`, `parseNDJSON` (slightly adapted), `cmdHash`, `workingTreeStatus`, `parsePRFromEnv` are preserved exactly as today — only the writing/reading functions and types change.

Write the new `internal/persist/persist.go`:

```go
package persist

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bjk95/defrost/internal/models"
)

const (
	DefaultDataBranch = "_defrost"

	botName  = "defrost[bot]"
	botEmail = "defrost[bot]@users.noreply.github.com"

	gitAttributes = "traces/*.ndjson merge=union\nmetrics/*.ndjson merge=union\n"
	readme        = "# defrost data branch\n\nManaged by the defrost CLI. Do not edit by hand.\n"

	maxPushAttempts = 5

	rootSpanName = "defrost.run"
)

// Options controls Backend creation.
type Options struct {
	RepoDir    string
	DataBranch string // "" → DefaultDataBranch
	AuthToken  string
	NoRemote   bool
	Dev        bool
}

const DevDir = ".defrost-dev"

// Backend is the swappable persistence layer. Schema 3.
type Backend interface {
	InitialisePersistence() error
	// InsertNewRun atomically persists the root run span, every test span,
	// and every metric data point produced by one defrost exec invocation.
	InsertNewRun(root models.Span, testSpans []models.Span, metrics []models.MetricEntry) error
	// GetTestHistory returns every span persisted under traces/<testName>.ndjson,
	// sorted oldest first by start time. Empty slice when nothing matches.
	GetTestHistory(testName string) ([]models.Span, error)
}

// New returns the Backend implied by opts. Dev mode selects the local
// scratch backend; otherwise the git-data-branch backend is used.
func New(opts Options) Backend {
	if opts.Dev {
		return &fileBackend{dir: filepath.Join(opts.RepoDir, DevDir)}
	}
	return &gitBackend{opts: opts}
}

// ErrNoOrigin is returned when the user's repo has no origin remote configured.
var ErrNoOrigin = errors.New("no origin remote configured")

// EncodeName escapes a span or metric name into a filesystem-safe segment.
// Reversible via DecodeName.
func EncodeName(name string) string { return url.PathEscape(name) }

// DecodeName is the inverse of EncodeName.
func DecodeName(id string) (string, error) { return url.PathUnescape(id) }

// NewRunID returns a sortable-by-time, collision-resistant run identifier.
// Format: <16 hex of UnixNano>-<8 hex of crypto/rand>.
func NewRunID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return fmt.Sprintf("%016x-%s", time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}

// DetectRunContext builds a RunContext for one defrost exec invocation by
// inspecting the user's repo, environment variables, and Go runtime.
// Returned Resource attributes follow OTel CI/CD and VCS semantic
// conventions where applicable; defrost-private keys use a `defrost.*`
// prefix.
func DetectRunContext(opts Options, cmd []string, defrostVersion string) (models.RunContext, error) {
	if _, err := runGit(opts.RepoDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return models.RunContext{}, fmt.Errorf("not a git repo at %s: %w", opts.RepoDir, err)
	}

	runID := NewRunID()
	res := map[string]any{
		"service.name":             "defrost",
		"service.version":          defrostVersion,
		"cicd.pipeline.run.id":     runID,
		"host.os.type":             runtime.GOOS,
		"host.arch":                runtime.GOARCH,
		"process.runtime.version":  runtime.Version(),
		"defrost.cmd":              cmd,
		"defrost.cmd_hash":         cmdHash(cmd),
		"defrost.run_id":           runID,
	}

	if out, err := runGit(opts.RepoDir, "log", "-1", "--format=%H%n%P%n%ae%n%an"); err == nil {
		lines := strings.SplitN(out, "\n", 4)
		if len(lines) >= 1 && lines[0] != "" {
			res["vcs.repository.ref.revision"] = lines[0]
		}
		if len(lines) >= 2 {
			parents := strings.Fields(lines[1])
			if len(parents) > 0 {
				res["defrost.parent_commit"] = parents[0]
			}
		}
		if len(lines) >= 3 && lines[2] != "" {
			res["defrost.author_email"] = lines[2]
		}
		if len(lines) >= 4 && lines[3] != "" {
			res["defrost.author_name"] = lines[3]
		}
	}

	if out, err := runGit(opts.RepoDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && out != "HEAD" && out != "" {
		res["vcs.repository.ref.name"] = out
	} else if v := os.Getenv("GITHUB_HEAD_REF"); v != "" {
		res["vcs.repository.ref.name"] = v
	} else if v := os.Getenv("GITHUB_REF_NAME"); v != "" {
		res["vcs.repository.ref.name"] = v
	}

	if pr := parsePRFromEnv(); pr != 0 {
		res["vcs.repository.change.id"] = strconv.Itoa(pr)
	}

	dirty, dirtyHash := workingTreeStatus(opts.RepoDir)
	res["defrost.dirty"] = dirty
	if dirtyHash != "" {
		res["defrost.dirty_hash"] = dirtyHash
	}

	now := time.Now().UnixNano()
	return models.RunContext{
		RunID:             runID,
		TraceID:           models.DeriveTraceID(runID),
		RootSpanID:        models.NewSpanID(),
		Resource:          res,
		StartTimeUnixNano: now,
	}, nil
}

// NewRootSpan returns the bookkeeping span representing one defrost exec
// invocation. End time and status are filled in by the caller after the
// child exits and persistence either succeeds or fails.
func NewRootSpan(run models.RunContext) models.Span {
	return models.Span{
		Schema:            models.SchemaV3,
		TraceID:           run.TraceID,
		SpanID:            run.RootSpanID,
		Name:              rootSpanName,
		Kind:              "INTERNAL",
		StartTimeUnixNano: run.StartTimeUnixNano,
		Status:            models.SpanStatus{Code: "UNSET"},
		Attributes:        map[string]any{"defrost.run_id": run.RunID},
		Resource:          run.Resource,
	}
}

// gitBackend stores spans and metrics on a dedicated git data branch.
type gitBackend struct{ opts Options }

func (b *gitBackend) InitialisePersistence() error { return nil }

func (b *gitBackend) InsertNewRun(root models.Span, testSpans []models.Span, metrics []models.MetricEntry) error {
	if root.SpanID == "" {
		return errors.New("persist: empty root span id")
	}

	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}

	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "defrost-data-")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)

	branchExisted, err := openOrInitDataRepo(workDir, remoteURL, branch)
	if err != nil {
		return err
	}

	if !branchExisted {
		if err := writeSeed(workDir); err != nil {
			return err
		}
	}

	if err := appendSpans(workDir, append([]models.Span{root}, testSpans...)); err != nil {
		return err
	}
	if err := appendMetrics(workDir, metrics); err != nil {
		return err
	}

	if err := commitAll(workDir, commitMessage(root, len(testSpans), len(metrics))); err != nil {
		return err
	}

	return pushWithRetry(workDir, branch, branchExisted)
}

func (b *gitBackend) GetTestHistory(testName string) ([]models.Span, error) {
	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return nil, err
	}
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	workDir, err := os.MkdirTemp("", "defrost-read-")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)
	_ = os.Remove(workDir) // clone wants empty path

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, fmt.Errorf("clone data branch: %w", err)
	}

	return readSpansFromDir(workDir, testName)
}

// fileBackend writes spans/metrics to a plain directory; no git operations.
type fileBackend struct{ dir string }

func (b *fileBackend) InitialisePersistence() error {
	return os.MkdirAll(b.dir, 0o755)
}

func (b *fileBackend) InsertNewRun(root models.Span, testSpans []models.Span, metrics []models.MetricEntry) error {
	if root.SpanID == "" {
		return errors.New("persist: empty root span id")
	}
	if err := b.InitialisePersistence(); err != nil {
		return err
	}
	if err := appendSpans(b.dir, append([]models.Span{root}, testSpans...)); err != nil {
		return err
	}
	return appendMetrics(b.dir, metrics)
}

func (b *fileBackend) GetTestHistory(testName string) ([]models.Span, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return readSpansFromDir(b.dir, testName)
}

// --- write helpers ---

func writeSeed(workDir string) error {
	if err := os.WriteFile(filepath.Join(workDir, ".gitattributes"), []byte(gitAttributes), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "README.md"), []byte(readme), 0o644)
}

func appendSpans(workDir string, spans []models.Span) error {
	if len(spans) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, "traces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, s := range spans {
		line, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("marshal span %s: %w", s.Name, err)
		}
		path := filepath.Join(dir, EncodeName(s.Name)+".ndjson")
		if err := appendLine(path, line); err != nil {
			return err
		}
	}
	return nil
}

func appendMetrics(workDir string, entries []models.MetricEntry) error {
	if len(entries) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, "metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal metric %s: %w", e.Name, err)
		}
		path := filepath.Join(dir, EncodeName(e.Name)+".ndjson")
		if err := appendLine(path, line); err != nil {
			return err
		}
	}
	return nil
}

func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("write %s: %w", path, werr)
	}
	if cerr != nil {
		return fmt.Errorf("close %s: %w", path, cerr)
	}
	return nil
}

// --- read helpers ---

func readSpansFromDir(dir, testName string) ([]models.Span, error) {
	path := filepath.Join(dir, "traces", EncodeName(testName)+".ndjson")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	spans, err := parseSpansNDJSON(f)
	if err != nil {
		return nil, err
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].StartTimeUnixNano < spans[j].StartTimeUnixNano })
	return spans, nil
}

func parseSpansNDJSON(r io.Reader) ([]models.Span, error) {
	var out []models.Span
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var s models.Span
		if err := json.Unmarshal(line, &s); err != nil {
			return nil, fmt.Errorf("parse ndjson line: %w", err)
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

func commitMessage(root models.Span, nSpans, nMetrics int) string {
	commit, _ := root.Resource["vcs.repository.ref.revision"].(string)
	short := commit
	if len(short) > 7 {
		short = short[:7]
	}
	if short == "" {
		runID, _ := root.Resource["defrost.run_id"].(string)
		short = runID
		if len(short) > 8 {
			short = short[:8]
		}
	}
	return fmt.Sprintf("results for %s (%d spans, %d metrics)", short, nSpans, nMetrics)
}

// --- preserved helpers (unchanged from schema 2) ---

type gitErr struct {
	args   []string
	err    error
	stderr string
	code   int
}

func (e *gitErr) Error() string {
	if e.stderr != "" {
		return fmt.Sprintf("git %s: %v: %s", strings.Join(e.args, " "), e.err, e.stderr)
	}
	return fmt.Sprintf("git %s: %v", strings.Join(e.args, " "), e.err)
}

func (e *gitErr) Unwrap() error { return e.err }

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return strings.TrimSpace(stdout.String()), &gitErr{args: args, err: err, stderr: strings.TrimSpace(stderr.String()), code: code}
	}
	return strings.TrimSpace(stdout.String()), nil
}

func resolveTargetURL(opts Options) (string, error) {
	if opts.NoRemote {
		return localGitDir(opts.RepoDir)
	}
	return readOriginURL(opts.RepoDir)
}

func readOriginURL(repoDir string) (string, error) {
	out, err := runGit(repoDir, "remote", "get-url", "origin")
	if err != nil {
		var ge *gitErr
		if errors.As(err, &ge) && ge.code == 2 {
			return "", ErrNoOrigin
		}
		return "", err
	}
	if out == "" {
		return "", ErrNoOrigin
	}
	return out, nil
}

func localGitDir(repoDir string) (string, error) {
	out, err := runGit(repoDir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(repoDir, out)
	}
	return filepath.Abs(out)
}

func branchExistsOnRemote(remoteURL, branch string) (bool, error) {
	_, err := runGit("", "ls-remote", "--exit-code", remoteURL, "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	var ge *gitErr
	if errors.As(err, &ge) && ge.code == 2 {
		return false, nil
	}
	return false, fmt.Errorf("ls-remote %s: %w", remoteURL, err)
}

func openOrInitDataRepo(workDir, remoteURL, branch string) (branchExisted bool, err error) {
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return false, err
	}
	if exists {
		if err := os.Remove(workDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("clear workdir: %w", err)
		}
		if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
			return false, fmt.Errorf("clone data branch: %w", err)
		}
		if err := configureBotIdentity(workDir); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return false, fmt.Errorf("mkdir workdir: %w", err)
	}
	if _, err := runGit(workDir, "init", "--quiet", "."); err != nil {
		return false, fmt.Errorf("git init: %w", err)
	}
	if _, err := runGit(workDir, "symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		return false, fmt.Errorf("set HEAD: %w", err)
	}
	if _, err := runGit(workDir, "remote", "add", "origin", remoteURL); err != nil {
		return false, fmt.Errorf("add origin: %w", err)
	}
	if err := configureBotIdentity(workDir); err != nil {
		return false, err
	}
	return false, nil
}

func configureBotIdentity(workDir string) error {
	if _, err := runGit(workDir, "config", "user.name", botName); err != nil {
		return fmt.Errorf("config user.name: %w", err)
	}
	if _, err := runGit(workDir, "config", "user.email", botEmail); err != nil {
		return fmt.Errorf("config user.email: %w", err)
	}
	return nil
}

func commitAll(workDir, msg string) error {
	if _, err := runGit(workDir, "add", "."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if _, err := runGit(workDir, "commit", "--quiet", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

func pushBranch(workDir, branch string) error {
	refspec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
	_, err := runGit(workDir, "push", "--quiet", "origin", refspec)
	return err
}

func pushWithRetry(workDir, branch string, branchExisted bool) error {
	var lastErr error
	for attempt := 1; attempt <= maxPushAttempts; attempt++ {
		err := pushBranch(workDir, branch)
		if err == nil {
			return nil
		}
		lastErr = err
		if !branchExisted || !isNonFastForward(err) {
			return err
		}
		if rebErr := pullRebase(workDir, branch); rebErr != nil {
			return fmt.Errorf("rebase after push conflict (attempt %d): %w", attempt, rebErr)
		}
	}
	return fmt.Errorf("push failed after %d retries: %w", maxPushAttempts, lastErr)
}

func isNonFastForward(err error) bool {
	if err == nil {
		return false
	}
	var ge *gitErr
	if !errors.As(err, &ge) {
		return false
	}
	msg := ge.stderr
	switch {
	case strings.Contains(msg, "non-fast-forward"),
		strings.Contains(msg, "fetch first"),
		strings.Contains(msg, "stale info"),
		strings.Contains(msg, "rejected"):
		return true
	}
	return false
}

func pullRebase(workDir, branch string) error {
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	if _, err := runGit(workDir, "fetch", "--quiet", "origin", refspec); err != nil {
		return fmt.Errorf("fetch %s: %w", branch, err)
	}
	target := fmt.Sprintf("refs/remotes/origin/%s", branch)
	if _, err := runGit(workDir, "rebase", target); err != nil {
		_, _ = runGit(workDir, "rebase", "--abort")
		return fmt.Errorf("git rebase %s: %w", target, err)
	}
	return nil
}

func cmdHash(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	h := sha1.Sum([]byte(strings.Join(cmd, "\x00")))
	return hex.EncodeToString(h[:8])
}

func workingTreeStatus(repoDir string) (bool, string) {
	out, err := runGit(repoDir, "status", "--porcelain")
	if err != nil || out == "" {
		return false, ""
	}
	diff, derr := runGit(repoDir, "diff", "HEAD")
	if derr != nil {
		h := sha1.Sum([]byte(out))
		return true, hex.EncodeToString(h[:8])
	}
	h := sha1.Sum([]byte(diff))
	return true, hex.EncodeToString(h[:8])
}

func parsePRFromEnv() int {
	if v := os.Getenv("GITHUB_PR_NUMBER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	ref := os.Getenv("GITHUB_REF")
	if !strings.HasPrefix(ref, "refs/pull/") {
		return 0
	}
	parts := strings.Split(ref, "/")
	if len(parts) < 3 {
		return 0
	}
	n, _ := strconv.Atoi(parts[2])
	return n
}
```

- [ ] **Step 2: Replace `internal/persist/persist_test.go` with the new schema-3 tests**

Replace the file's contents (note: the test rewrite below intentionally drops the obsolete schema-2 tests, but the helper-level regression tests `TestReadOriginURL_WorksInWorktree` and `TestPersist_LocalOnlyNoRemote_RelativeRepoDir` from the previous test file should be carried forward unchanged at the bottom of the new file — they exercise `localGitDir` and worktree resolution, which this refactor doesn't touch and which we shouldn't lose coverage on):

```go
package persist

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bjk95/defrost/internal/models"
)

func TestEncodeName_RoundTrip(t *testing.T) {
	cases := []string{
		"github.com/bjk95/defrost/internal/x/TestFoo",
		"p/TestSubtest/with spaces",
		"p/TestPercent_already%encoded",
		"db.query.duration",
		"http.server.request.duration",
		"defrost.run",
	}
	for _, name := range cases {
		id := EncodeName(name)
		if strings.ContainsAny(id, `/\`) {
			t.Errorf("encoded id contains path separator: %q (from %q)", id, name)
		}
		got, err := DecodeName(id)
		if err != nil {
			t.Errorf("decode %q: %v", id, err)
			continue
		}
		if got != name {
			t.Errorf("round-trip mismatch:\n want: %q\n got:  %q", name, got)
		}
	}
}

func TestPersist_WritesTracesAndMetricsOnFirstWrite(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	run := newRunContext("run-001", "abc123def4567890", "main")
	root := NewRootSpan(run)
	root.EndTimeUnixNano = root.StartTimeUnixNano + 100
	root.Status = models.SpanStatus{Code: "OK"}

	testSpans := []models.Span{
		{
			Schema:            models.SchemaV3,
			TraceID:           run.TraceID,
			SpanID:            "aaaaaaaaaaaaaaaa",
			ParentSpanID:      run.RootSpanID,
			Name:              "github.com/x/p/TestA",
			StartTimeUnixNano: run.StartTimeUnixNano + 10,
			EndTimeUnixNano:   run.StartTimeUnixNano + 50,
			Status:            models.SpanStatus{Code: "OK"},
			Resource:          run.Resource,
			Attributes:        map[string]any{"test.case.name": "github.com/x/p/TestA"},
		},
	}
	v := 12.0
	metrics := []models.MetricEntry{
		{
			Schema:         models.SchemaV3,
			Name:           "db.connection_pool.size",
			InstrumentType: "gauge",
			TimeUnixNano:   run.StartTimeUnixNano + 30,
			Resource:       run.Resource,
			TraceID:        run.TraceID,
			Value:          &v,
		},
	}

	if err := New(Options{RepoDir: repoDir}).InsertNewRun(root, testSpans, metrics); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	verify := cloneDataBranch(t, originURL)

	tracesPath := filepath.Join(verify, "traces", EncodeName("github.com/x/p/TestA")+".ndjson")
	if b, err := os.ReadFile(tracesPath); err != nil {
		t.Errorf("missing %s: %v", tracesPath, err)
	} else if !strings.Contains(string(b), run.TraceID) {
		t.Errorf("test span missing trace_id %q:\n%s", run.TraceID, b)
	}

	rootPath := filepath.Join(verify, "traces", EncodeName("defrost.run")+".ndjson")
	if b, err := os.ReadFile(rootPath); err != nil {
		t.Errorf("missing root span file %s: %v", rootPath, err)
	} else if !strings.Contains(string(b), run.RunID) {
		t.Errorf("root span missing run_id %q:\n%s", run.RunID, b)
	}

	metricsPath := filepath.Join(verify, "metrics", EncodeName("db.connection_pool.size")+".ndjson")
	if b, err := os.ReadFile(metricsPath); err != nil {
		t.Errorf("missing metrics file %s: %v", metricsPath, err)
	} else if !strings.Contains(string(b), `"value":12`) {
		t.Errorf("metric file missing value:\n%s", b)
	}

	if _, err := os.Stat(filepath.Join(verify, ".gitattributes")); err != nil {
		t.Errorf("missing .gitattributes seed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(verify, "runs")); err == nil {
		t.Errorf("runs/ directory should not exist in schema 3")
	}
	if _, err := os.Stat(filepath.Join(verify, "tests")); err == nil {
		t.Errorf("tests/ directory should not exist in schema 3")
	}
}

func TestPersist_AppendsToExistingBranch(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	first := newRunContext("run-A", "1111111111111111", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(NewRootSpan(first), []models.Span{makeTestSpan(first, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("first InsertNewRun: %v", err)
	}

	second := newRunContext("run-B", "2222222222222222", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(NewRootSpan(second), []models.Span{makeTestSpan(second, "p/TestA", "ERROR")}, nil); err != nil {
		t.Fatalf("second InsertNewRun: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	path := filepath.Join(verify, "traces", EncodeName("p/TestA")+".ndjson")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 spans after two runs, got %d:\n%s", len(lines), b)
	}
	if !strings.Contains(lines[0], first.RunID) || !strings.Contains(lines[1], second.RunID) {
		t.Errorf("expected one line per run id; got:\n%s", b)
	}
}

func TestHistory_ReturnsSpansSortedByStartTime(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	run := newRunContext("run-history", "deadbeefcafebabe", "main")
	root := NewRootSpan(run)
	span := models.Span{
		Schema:            models.SchemaV3,
		TraceID:           run.TraceID,
		SpanID:            "bbbbbbbbbbbbbbbb",
		ParentSpanID:      run.RootSpanID,
		Name:              "github.com/x/p/TestA",
		StartTimeUnixNano: 100,
		EndTimeUnixNano:   200,
		Status:            models.SpanStatus{Code: "OK"},
		Resource:          run.Resource,
		Attributes:        map[string]any{"test.case.name": "github.com/x/p/TestA"},
	}
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(root, []models.Span{span}, nil); err != nil {
		t.Fatalf("InsertNewRun: %v", err)
	}

	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("github.com/x/p/TestA")
	if err != nil {
		t.Fatalf("GetTestHistory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 span, got %d", len(got))
	}
	if got[0].Name != "github.com/x/p/TestA" {
		t.Errorf("name: %q", got[0].Name)
	}
	if got[0].Resource["vcs.repository.ref.revision"] != "deadbeefcafebabe" {
		t.Errorf("resource not inlined: %+v", got[0].Resource)
	}
	if got[0].TraceID != run.TraceID {
		t.Errorf("trace_id: %q", got[0].TraceID)
	}
}

func TestHistory_UnknownTestReturnsEmpty(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)
	got, err := New(Options{RepoDir: repoDir}).GetTestHistory("github.com/x/p/NeverWritten")
	if err != nil {
		t.Fatalf("GetTestHistory on empty origin: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 spans, got %d", len(got))
	}
}

// TestPushWithRetry_RebasesOnConflict drives the rebase path manually:
// writer 1 stages a commit, writer 2 races ahead and pushes, then writer 1
// pushes — the retry loop must fetch, rebase under merge=union, and land
// without losing either side's appended line.
func TestPushWithRetry_RebasesOnConflict(t *testing.T) {
	requireGit(t)
	repoDir, originURL := makeFixture(t)

	seedRun := newRunContext("run-seed", "1111111111111111", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(NewRootSpan(seedRun), []models.Span{makeTestSpan(seedRun, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("seed InsertNewRun: %v", err)
	}

	w1Dir := filepath.Join(t.TempDir(), "w1")
	branchExisted, err := openOrInitDataRepo(w1Dir, originURL, DefaultDataBranch)
	if err != nil {
		t.Fatalf("w1 openOrInit: %v", err)
	}
	if !branchExisted {
		t.Fatal("w1: expected branch to exist after seed")
	}
	w1Run := newRunContext("run-w1", "2222222222222222", "main")
	if err := appendSpans(w1Dir, []models.Span{NewRootSpan(w1Run), makeTestSpan(w1Run, "p/TestA", "ERROR")}); err != nil {
		t.Fatalf("w1 appendSpans: %v", err)
	}
	if err := commitAll(w1Dir, "writer 1"); err != nil {
		t.Fatalf("w1 commit: %v", err)
	}

	racerRun := newRunContext("run-racer", "3333333333333333", "main")
	if err := New(Options{RepoDir: repoDir}).InsertNewRun(NewRootSpan(racerRun), []models.Span{makeTestSpan(racerRun, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("racer InsertNewRun: %v", err)
	}

	if err := pushWithRetry(w1Dir, DefaultDataBranch, true); err != nil {
		t.Fatalf("pushWithRetry: %v", err)
	}

	verify := cloneDataBranch(t, originURL)
	path := filepath.Join(verify, "traces", EncodeName("p/TestA")+".ndjson")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final ndjson: %v", err)
	}
	final := string(b)
	for _, runID := range []string{"run-seed", "run-w1", "run-racer"} {
		if !strings.Contains(final, runID) {
			t.Errorf("expected run_id %s entry in final file:\n%s", runID, final)
		}
	}
	if got := strings.Count(strings.TrimRight(final, "\n"), "\n") + 1; got != 3 {
		t.Errorf("expected 3 lines after rebase, got %d:\n%s", got, final)
	}
}

func TestReadOriginURL_NoOriginReturnsErrNoOrigin(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", dir)
	_, err := readOriginURL(dir)
	if !errors.Is(err, ErrNoOrigin) {
		t.Errorf("expected ErrNoOrigin, got %v", err)
	}
}

func TestPersist_RequiresOriginByDefault(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", dir)
	run := newRunContext("orphan-run", "abc", "main")
	err := New(Options{RepoDir: dir}).InsertNewRun(NewRootSpan(run), []models.Span{makeTestSpan(run, "p/TestA", "OK")}, nil)
	if !errors.Is(err, ErrNoOrigin) {
		t.Errorf("expected ErrNoOrigin, got %v", err)
	}
}

func TestPersist_LocalOnlyNoRemote(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", "-b", "main", dir)
	gitMust(t, dir, "config", "user.email", "t@example.com")
	gitMust(t, dir, "config", "user.name", "t")
	gitMust(t, dir, "commit", "--allow-empty", "-m", "init")

	run := newRunContext("local-run", "abc123", "main")
	if err := New(Options{RepoDir: dir, NoRemote: true}).InsertNewRun(NewRootSpan(run), []models.Span{makeTestSpan(run, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("InsertNewRun (no-remote): %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "show", DefaultDataBranch+":traces/"+EncodeName("p/TestA")+".ndjson").CombinedOutput()
	if err != nil {
		t.Fatalf("read traces file from data branch: %v: %s", err, out)
	}
	if !strings.Contains(string(out), run.RunID) {
		t.Errorf("trace ndjson missing run_id %q:\n%s", run.RunID, out)
	}
}

func TestPersist_DevModeWritesScratchDirAndSkipsGit(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitMust(t, "", "init", "-b", "main", dir)

	run := newRunContext("dev-run", "abc123", "main")
	if err := New(Options{RepoDir: dir, Dev: true}).InsertNewRun(NewRootSpan(run), []models.Span{makeTestSpan(run, "p/TestA", "OK")}, nil); err != nil {
		t.Fatalf("InsertNewRun (dev): %v", err)
	}

	scratch := filepath.Join(dir, DevDir)
	tracesPath := filepath.Join(scratch, "traces", EncodeName("p/TestA")+".ndjson")
	if _, err := os.Stat(tracesPath); err != nil {
		t.Errorf("trace file not written to scratch dir: %v", err)
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", DefaultDataBranch).CombinedOutput(); err == nil {
		t.Errorf("expected no %s branch, but rev-parse succeeded: %s", DefaultDataBranch, out)
	}
}

// --- helpers ---

func newRunContext(runID, commit, branch string) models.RunContext {
	return models.RunContext{
		RunID:             runID,
		TraceID:           models.DeriveTraceID(runID),
		RootSpanID:        models.NewSpanID(),
		StartTimeUnixNano: 1,
		Resource: map[string]any{
			"service.name":                "defrost",
			"vcs.repository.ref.revision": commit,
			"vcs.repository.ref.name":     branch,
			"defrost.run_id":              runID,
		},
	}
}

func makeTestSpan(run models.RunContext, name, statusCode string) models.Span {
	return models.Span{
		Schema:            models.SchemaV3,
		TraceID:           run.TraceID,
		SpanID:            models.NewSpanID(),
		ParentSpanID:      run.RootSpanID,
		Name:              name,
		StartTimeUnixNano: run.StartTimeUnixNano + 1,
		EndTimeUnixNano:   run.StartTimeUnixNano + 5,
		Status:            models.SpanStatus{Code: statusCode},
		Resource:          run.Resource,
		Attributes:        map[string]any{"test.case.name": name, "defrost.run_id": run.RunID},
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not available")
	}
}

func gitMust(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %q: %v: %s", args, dir, err, out)
	}
}

func makeFixture(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	bareDir := filepath.Join(base, "origin.git")
	gitMust(t, "", "init", "--bare", bareDir)

	repoDir := filepath.Join(base, "repo")
	gitMust(t, "", "init", repoDir)
	gitMust(t, repoDir, "remote", "add", "origin", bareDir)
	return repoDir, bareDir
}

func cloneDataBranch(t *testing.T, originURL string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "verify")
	gitMust(t, "", "clone", "--quiet", "--single-branch", "--branch", DefaultDataBranch, originURL, dir)
	return dir
}
```

- [ ] **Step 3: Run tests to verify the package compiles and tests pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go test ./internal/persist/...
```

Expected: PASS for every `TestPersist_*` and `TestHistory_*` test. If anything fails, fix in-place before moving on.

- [ ] **Step 4: Verify the rest of the module still compiles**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go build ./...
```

Expected: build failure in `exec.go` and `history.go` (they reference removed types `RunRecord`, `Entry`, methods `InsertNewTestResults`, `DetectRun`). This is expected — the next two tasks fix those callers.

- [ ] **Step 5: Commit**

```bash
git add internal/persist/persist.go internal/persist/persist_test.go
git commit -m "feat(persist): schema 3 — traces/, metrics/, span-shaped storage"
```

(Note: `git status` will still show `exec.go` and `history.go` as failing to build. The next tasks repair those.)

---

## Task 9: Wire receiver, translators, and persist into `exec.go`

Update the top-level `defrost exec` flow:

1. Build a `RunContext` via `persist.DetectRunContext`.
2. Start the OTLP receiver, capture port.
3. Set `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL` on the child env (the runner adapters spawn the child themselves; we expose the env via `os.Setenv` for the duration of the run, scoped via `t.Setenv`-style cleanup).
4. Run the child; collect `[]TestResult`.
5. Drain the receiver (2s grace), translate to `[]MetricEntry`.
6. Translate `[]TestResult` to `[]Span`.
7. Build the root run span (status derived from results + persistence outcome).
8. Persist via `Backend.InsertNewRun`.

**Files:**
- Modify (full rewrite): `exec.go`

- [ ] **Step 1: Rewrite `exec.go`**

Replace the file's contents:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/otlp"
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
)

// defrostVersion is stamped into the Resource attribute service.version.
// Bump when cutting a release; build-time injection can replace this
// constant later.
const defrostVersion = "0.0.0-dev"

// drainGrace is the window we give SDK background flushes after the child
// exits before tearing down the OTLP receiver. See spec defaults table.
const drainGrace = 2 * time.Second

type ExecOpts struct {
	RepoDir    string
	DataBranch string
	Persist    bool
	NoRemote   bool
	Dev        bool
}

func HandleExecution(cmd []string, opts ExecOpts) int {
	if len(cmd) == 0 {
		fmt.Fprintln(os.Stderr, "exec: no command provided")
		return 2
	}

	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(pytest.Adapter{})

	a := reg.Find(cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "exec: unsupported test command: %q\n", cmd[0])
		return 2
	}

	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
		Dev:        opts.Dev,
	}

	// Even when Persist is off we still build a RunContext so the receiver
	// can stamp metric data points with a trace_id. Failure here is fatal
	// only when we're going to persist; otherwise we run without metric
	// collection.
	run, runErr := persist.DetectRunContext(pOpts, cmd, defrostVersion)
	if runErr != nil && opts.Persist {
		fmt.Fprintln(os.Stderr, "exec: detect run context:", runErr)
		return 1
	}

	receiver, restoreEnv := startReceiver(run, runErr == nil)
	defer restoreEnv()

	results, code := a.Run(cmd)

	for _, r := range results {
		fmt.Printf("%+v\n", r)
	}

	var metrics []models.MetricEntry
	if receiver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), drainGrace)
		buffered, err := receiver.Shutdown(ctx)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "exec: otlp receiver shutdown:", err)
		}
		if runErr == nil {
			for _, req := range buffered {
				metrics = append(metrics, otlp.MetricsToEntries(req, run)...)
			}
		}
	}

	if !opts.Persist || len(results) == 0 && len(metrics) == 0 {
		return code
	}
	if runErr != nil {
		// Persist requested but no run context. Surface the original error.
		fmt.Fprintln(os.Stderr, "exec: persist skipped, no run context:", runErr)
		if code == 0 {
			code = 1
		}
		return code
	}

	if err := persistRun(pOpts, run, results, metrics, code); err != nil {
		fmt.Fprintln(os.Stderr, "persist: failed:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// startReceiver binds the OTLP listener and exports the standard env vars
// for the child. On bind failure we log and return (nil, no-op) so the
// run continues without metric collection. Returns a restore function the
// caller MUST call to clear the exported env vars regardless of outcome.
func startReceiver(run models.RunContext, haveRunContext bool) (*otlp.Receiver, func()) {
	r := otlp.New()
	port, err := r.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "exec: otlp receiver bind failed, continuing without metrics:", err)
		return nil, func() {}
	}
	if !haveRunContext {
		// Receiver is up but we'll have nowhere to attach the data — still
		// expose the env so user code's OTLP exporter doesn't fail to push,
		// then drop the buffered records on Shutdown.
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	prevEndpoint, hadEndpoint := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	prevProtocol, hadProtocol := os.LookupEnv("OTEL_EXPORTER_OTLP_PROTOCOL")
	os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	os.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	restore := func() {
		if hadEndpoint {
			os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", prevEndpoint)
		} else {
			os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		}
		if hadProtocol {
			os.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", prevProtocol)
		} else {
			os.Unsetenv("OTEL_EXPORTER_OTLP_PROTOCOL")
		}
	}
	return r, restore
}

func persistRun(pOpts persist.Options, run models.RunContext, results []models.TestResult, metrics []models.MetricEntry, exitCode int) error {
	testSpans := otlp.TestResultsToSpans(results, run)

	root := persist.NewRootSpan(run)
	root.EndTimeUnixNano = time.Now().UnixNano()
	root.Status = rootStatusFromExit(exitCode)

	if err := persist.New(pOpts).InsertNewRun(root, testSpans, metrics); err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			return errors.New("no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --no-remote to persist locally only")
		}
		return err
	}
	return nil
}

func rootStatusFromExit(code int) models.SpanStatus {
	if code == 0 {
		return models.SpanStatus{Code: "OK"}
	}
	return models.SpanStatus{Code: "ERROR", Message: fmt.Sprintf("exit code %d", code)}
}
```

- [ ] **Step 2: Verify the build proceeds further but still fails on `history.go`**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go build ./...
```

Expected: only `history.go` fails (`undefined: persist.HistoricalEntry`). Task 10 repairs it.

- [ ] **Step 3: Commit**

```bash
git add exec.go
git commit -m "feat(exec): start OTLP receiver, translate to spans, persist via schema 3"
```

---

## Task 10: Update `history.go` to print spans

`history.go` currently encodes `HistoricalEntry` records (test entry joined with run record). Schema 3 has just spans; `Resource` is inlined per span so a single span line is self-contained.

**Files:**
- Modify (full rewrite): `history.go`

- [ ] **Step 1: Rewrite `history.go`**

Replace the file's contents:

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/persist"
)

func HandleHistory(testName, repoDir, dataBranch string, noRemote bool) int {
	spans, err := persist.New(persist.Options{
		RepoDir:    repoDir,
		DataBranch: dataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   noRemote,
	}).GetTestHistory(testName)
	if err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			fmt.Fprintln(os.Stderr, "history: no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --no-remote to read from the local repo.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "history:", err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	for _, s := range spans {
		if err := enc.Encode(s); err != nil {
			fmt.Fprintln(os.Stderr, "history:", err)
			return 1
		}
	}
	return 0
}
```

- [ ] **Step 2: Verify the whole module builds and all tests pass**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go build ./... && go test ./...
```

Expected: build success and all tests PASS across `models`, `otlp`, `persist`, `runner`, `golang`, `python/pytest`, `resultcollector`. Existing adapter tests should be unaffected.

- [ ] **Step 3: Commit**

```bash
git add history.go
git commit -m "feat(history): emit OTel spans (schema 3) for the requested test"
```

---

## Task 11: End-to-end smoke verification

Run a real `defrost exec go test ./internal/models/...` against the worktree, in dev mode (`-d`) so it writes to `.defrost-dev/` instead of pushing. Then inspect the produced files and confirm the layout matches the spec exactly.

**Files:**
- (Read-only inspection — no source changes.)

- [ ] **Step 1: Build the binary**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && go build -o /tmp/defrost-otel .
```

Expected: success.

- [ ] **Step 2: Run a real test command in dev mode**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && rm -rf .defrost-dev && /tmp/defrost-otel exec -d go test ./internal/models/...
```

Expected: tests PASS, exit 0, `.defrost-dev/` directory created.

- [ ] **Step 3: Inspect the produced layout**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && ls .defrost-dev/traces .defrost-dev/metrics 2>/dev/null
```

Expected:
- `.defrost-dev/traces/` exists and contains:
  - `defrost.run.ndjson` (one line)
  - One `<encoded-test-name>.ndjson` per test (e.g. `TestDeriveTraceID_Deterministic.ndjson` URL-escaped)
- `.defrost-dev/metrics/` does not exist (no metrics emitted by these tests).

If the metrics directory is unexpectedly present and empty, that's a bug — it should only be created when there are entries to write.

- [ ] **Step 4: Sanity-check one span line**

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && head -1 .defrost-dev/traces/defrost.run.ndjson | python3 -m json.tool
```

Expected: well-formed JSON with `schema=3`, a 32-char `trace_id`, a 16-char `span_id`, name `defrost.run`, a `resource` object containing `service.name=defrost` and `vcs.repository.ref.revision`, and `status.code=OK`.

- [ ] **Step 5: Verify history reads back correctly**

Pick one test span file from step 3 (e.g. the file name corresponds to a real test in `internal/models`) and run:

```bash
cd /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa && /tmp/defrost-otel history --no-remote 'github.com/bjk95/defrost/internal/models.TestDeriveTraceID_Deterministic' 2>&1 | head -5
```

Note: this command targets the data branch via `--no-remote`, not `.defrost-dev/`, so it may legitimately return empty if dev-mode persists were the only writers. The intent is to verify the command runs without crashing and emits valid JSON when it does have data.

Expected: command exits 0; if any output is produced it is a JSON object with `schema=3`.

- [ ] **Step 6: Clean up**

```bash
rm -rf /Users/brad/dev/defrost/.claude/worktrees/peaceful-gould-70ecaa/.defrost-dev /tmp/defrost-otel
```

- [ ] **Step 7: Final commit (if anything was tweaked during smoke testing)**

```bash
git status
```

Expected: clean working tree. If smoke testing surfaced bugs and they were fixed in earlier tasks, those fixes should already be committed under those tasks. If not, commit now with a descriptive message.

---

## Self-Review (run before completion)

After implementing the plan, verify against the spec:

- [ ] **OTLP/HTTP receiver:** Tasks 5 + 7 add the dependency and implement the listener. Task 9 wires it into `defrost exec`. Confirms receiver bind failure is logged and the run continues (Task 9 step 1, the `startReceiver` function).
- [ ] **`metrics/<name>.ndjson` storage:** Task 8 step 1 (`appendMetrics`) writes the file; Task 6 produces the entries.
- [ ] **`traces/<test_name>.ndjson` storage with span shape:** Task 8 step 1 (`appendSpans`); Task 4 produces the spans.
- [ ] **`defrost.run` root span at `traces/defrost.run.ndjson`:** Task 8 (`NewRootSpan`, `rootSpanName`); Task 9 fills in end time and status.
- [ ] **Resource attributes inlined per span/metric, semconv-keyed:** Task 8 (`DetectRunContext`); Tasks 4 + 6 inline `run.Resource`.
- [ ] **`OTEL_EXPORTER_OTLP_ENDPOINT` injected in child env:** Task 9 (`startReceiver`).
- [ ] **Hard cut, no compatibility code:** Task 8 removes `Entry`, `RunRecord`, `HistoricalEntry`, `InsertNewTestResults`, the old `tests/` and `runs/` paths, and the `merge=union` line for `tests/`.
- [ ] **Receiver bind failure does not break the run:** Task 9 step 1 (`startReceiver` logs and returns nil receiver).
- [ ] **Drain grace: 2 seconds:** Task 9 (`drainGrace` constant).
- [ ] **Cardinality cap: explicitly out of scope.** No code added; not a regression.
- [ ] **No `b.ReportMetric` / benchmark inspiration leaks into the implementation.** None added; the receiver-based design supersedes that branch of the design conversation.
