package models

import (
	"crypto/rand"
	"crypto/sha256"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

// RunContext carries the OTel-shaped per-run identity used by translators
// and the persist layer. Built once at the start of a defrost exec
// invocation and threaded through everything that emits spans or metrics.
//
// TraceID and RootSpanID are raw bytes — 16 and 8 respectively — because
// that's what the OTel proto types consume directly. Hex stringification
// happens at projection / display time only.
type RunContext struct {
	RunID             string
	TraceID           []byte // 16 raw bytes, derived from RunID via SHA256
	RootSpanID        []byte // 8 raw bytes, fresh per run
	Resource          *resourcepb.Resource
	StartTimeUnixNano int64
}

// DeriveTraceID hashes a run id into the 16-byte trace id shape OTel
// mandates. Deterministic so a given run always maps to the same trace
// id, which makes cross-file joins on trace_id reproducible.
func DeriveTraceID(runID string) []byte {
	h := sha256.Sum256([]byte(runID))
	out := make([]byte, 16)
	copy(out, h[:16])
	return out
}

// NewSpanID returns a fresh 8-byte span id.
func NewSpanID() []byte {
	out := make([]byte, 8)
	if _, err := rand.Read(out); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return out
}

// StringAttr is a tiny helper for building Resource / Attribute KeyValue
// lists with a string value. Most defrost-emitted attributes are strings.
func StringAttr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
	}
}

// BoolAttr builds a KeyValue with a bool value.
func BoolAttr(key string, value bool) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: value}},
	}
}

// IntAttr builds a KeyValue with an int64 value.
func IntAttr(key string, value int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: value}},
	}
}

// DoubleAttr returns a *commonpb.KeyValue carrying a float64.
func DoubleAttr(key string, value float64) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: value}},
	}
}

// StringArrayAttr builds a KeyValue with a string-array value (used for
// `defrost.cmd` which is the wrapped argv).
func StringArrayAttr(key string, values []string) *commonpb.KeyValue {
	arr := make([]*commonpb.AnyValue, 0, len(values))
	for _, v := range values {
		arr = append(arr, &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}})
	}
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: arr}}},
	}
}

// ResourceString reads a string-typed attribute from a Resource by key,
// returning "" when the key is absent or the value is not a string.
func ResourceString(r *resourcepb.Resource, key string) string {
	if r == nil {
		return ""
	}
	for _, kv := range r.Attributes {
		if kv.Key == key {
			if v, ok := kv.Value.GetValue().(*commonpb.AnyValue_StringValue); ok {
				return v.StringValue
			}
		}
	}
	return ""
}

// AttrString reads a string-typed attribute from a KeyValue list by key,
// returning "" when absent.
func AttrString(attrs []*commonpb.KeyValue, key string) string {
	for _, kv := range attrs {
		if kv.Key == key {
			if v, ok := kv.Value.GetValue().(*commonpb.AnyValue_StringValue); ok {
				return v.StringValue
			}
		}
	}
	return ""
}
