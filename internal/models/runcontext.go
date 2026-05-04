package models

import (
	"crypto/rand"
	"crypto/sha256"
)

// RunContext carries the per-run identity used by translators and the
// gitExporter. Built once at the start of a defrost exec invocation
// and threaded through everything that emits spans, metrics, or logs.
//
// TraceID is the raw 16-byte OTel trace id; RootSpanID is the raw
// 8-byte OTel span id. Hex stringification happens at projection time.
//
// Attrs is a primitive list of key/value pairs — by design RunContext
// does NOT depend on pdata, so DetectRunContext (in internal/persist)
// stays independent of the OTel pdata package. The conversion to
// pcommon.Map happens at the boundary in internal/runner/spans.go.
type RunContext struct {
	RunID             string
	TraceID           [16]byte
	RootSpanID        [8]byte
	Attrs             []Attr
	StartTimeUnixNano int64
}

// Attr is one resource-level attribute. Value is one of:
// string | bool | int64 | float64 | []string. Other types are not
// supported — defrost only uses these.
type Attr struct {
	Key   string
	Value any
}

// DeriveTraceID hashes a run id into the 16-byte trace id shape OTel
// mandates. Deterministic so a given run always maps to the same trace
// id, which makes cross-file joins on trace_id reproducible.
func DeriveTraceID(runID string) [16]byte {
	h := sha256.Sum256([]byte(runID))
	var out [16]byte
	copy(out[:], h[:16])
	return out
}

// NewSpanID returns a fresh 8-byte span id.
func NewSpanID() [8]byte {
	var out [8]byte
	if _, err := rand.Read(out[:]); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return out
}

// StringAttr is a tiny helper for building Attr lists with a string value.
func StringAttr(key, value string) Attr {
	return Attr{Key: key, Value: value}
}

// BoolAttr builds an Attr with a bool value.
func BoolAttr(key string, value bool) Attr {
	return Attr{Key: key, Value: value}
}

// IntAttr builds an Attr with an int64 value.
func IntAttr(key string, value int64) Attr {
	return Attr{Key: key, Value: value}
}

// DoubleAttr builds an Attr with a float64 value.
func DoubleAttr(key string, value float64) Attr {
	return Attr{Key: key, Value: value}
}

// StringArrayAttr builds an Attr with a string-slice value.
func StringArrayAttr(key string, values []string) Attr {
	cp := make([]string, len(values))
	copy(cp, values)
	return Attr{Key: key, Value: cp}
}

// AttrString reads a string-typed attribute by key, or "" if absent or
// not a string.
func AttrString(attrs []Attr, key string) string {
	for _, a := range attrs {
		if a.Key != key {
			continue
		}
		if s, ok := a.Value.(string); ok {
			return s
		}
	}
	return ""
}

// AttrBool reads a bool-typed attribute by key, or false if absent.
func AttrBool(attrs []Attr, key string) bool {
	for _, a := range attrs {
		if a.Key != key {
			continue
		}
		if b, ok := a.Value.(bool); ok {
			return b
		}
	}
	return false
}

// AttrStrings reads a string-array-typed attribute by key.
func AttrStrings(attrs []Attr, key string) []string {
	for _, a := range attrs {
		if a.Key != key {
			continue
		}
		if v, ok := a.Value.([]string); ok {
			return v
		}
	}
	return nil
}
