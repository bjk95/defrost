package models

import (
	"bytes"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func TestDeriveTraceID_Deterministic(t *testing.T) {
	a := DeriveTraceID("run-001")
	b := DeriveTraceID("run-001")
	if !bytes.Equal(a, b) {
		t.Errorf("DeriveTraceID is not deterministic: %x vs %x", a, b)
	}
	if len(a) != 16 {
		t.Errorf("trace id must be 16 bytes, got %d: %x", len(a), a)
	}
}

func TestDeriveTraceID_DifferentInputs(t *testing.T) {
	if bytes.Equal(DeriveTraceID("a"), DeriveTraceID("b")) {
		t.Error("expected different trace ids for different run ids")
	}
}

func TestNewSpanID_Format(t *testing.T) {
	id := NewSpanID()
	if len(id) != 8 {
		t.Errorf("span id must be 8 bytes, got %d: %x", len(id), id)
	}
}

func TestNewSpanID_Unique(t *testing.T) {
	a := NewSpanID()
	b := NewSpanID()
	if bytes.Equal(a, b) {
		t.Errorf("expected unique span ids, got %x twice", a)
	}
}

func TestStringAttr_RoundTrip(t *testing.T) {
	kv := StringAttr("test.case.name", "pkg.TestFoo")
	if kv.Key != "test.case.name" {
		t.Errorf("key: %q", kv.Key)
	}
	v, ok := kv.Value.GetValue().(*commonpb.AnyValue_StringValue)
	if !ok || v.StringValue != "pkg.TestFoo" {
		t.Errorf("value: %v", kv.Value)
	}
}

func TestResourceString_FoundAndAbsent(t *testing.T) {
	res := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{StringAttr("vcs.repository.ref.name", "main")}}
	if got := ResourceString(res, "vcs.repository.ref.name"); got != "main" {
		t.Errorf("found: %q", got)
	}
	if got := ResourceString(res, "missing.key"); got != "" {
		t.Errorf("absent: %q", got)
	}
	if got := ResourceString(nil, "anything"); got != "" {
		t.Errorf("nil resource: %q", got)
	}
}

func TestAttrString_FoundAndAbsent(t *testing.T) {
	attrs := []*commonpb.KeyValue{StringAttr("test.case.name", "pkg.TestFoo")}
	if got := AttrString(attrs, "test.case.name"); got != "pkg.TestFoo" {
		t.Errorf("found: %q", got)
	}
	if got := AttrString(attrs, "missing"); got != "" {
		t.Errorf("absent: %q", got)
	}
}
