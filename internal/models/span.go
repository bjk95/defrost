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
