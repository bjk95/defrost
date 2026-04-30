package models

import (
	"encoding/json"
	"reflect"
	"strings"
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
		if got := s; strings.Contains(got, omitted) {
			t.Errorf("expected %s to be omitted, got: %s", omitted, got)
		}
	}
}

