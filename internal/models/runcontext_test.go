package models

import (
	"bytes"
	"testing"
)

func TestDeriveTraceID_Deterministic(t *testing.T) {
	a := DeriveTraceID("run-001")
	b := DeriveTraceID("run-001")
	if !bytes.Equal(a[:], b[:]) {
		t.Errorf("DeriveTraceID is not deterministic: %x vs %x", a, b)
	}
	if len(a) != 16 {
		t.Errorf("trace id must be 16 bytes, got %d: %x", len(a), a)
	}
}

func TestDeriveTraceID_DifferentInputs(t *testing.T) {
	a, b := DeriveTraceID("a"), DeriveTraceID("b")
	if bytes.Equal(a[:], b[:]) {
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
	a, b := NewSpanID(), NewSpanID()
	if bytes.Equal(a[:], b[:]) {
		t.Errorf("expected unique span ids, got %x twice", a)
	}
}

func TestStringAttr_RoundTrip(t *testing.T) {
	kv := StringAttr("test.case.name", "pkg.TestFoo")
	if kv.Key != "test.case.name" {
		t.Errorf("key: %q", kv.Key)
	}
	if kv.Value.(string) != "pkg.TestFoo" {
		t.Errorf("value: %v", kv.Value)
	}
}

func TestAttrString_FoundAndAbsent(t *testing.T) {
	attrs := []Attr{StringAttr("test.case.name", "pkg.TestFoo")}
	if got := AttrString(attrs, "test.case.name"); got != "pkg.TestFoo" {
		t.Errorf("found: %q", got)
	}
	if got := AttrString(attrs, "missing"); got != "" {
		t.Errorf("absent: %q", got)
	}
}

func TestDoubleAttr(t *testing.T) {
	kv := DoubleAttr("eval.score", 0.87)
	if kv.Key != "eval.score" {
		t.Fatalf("expected key eval.score, got %q", kv.Key)
	}
	if kv.Value.(float64) != 0.87 {
		t.Fatalf("expected 0.87, got %v", kv.Value)
	}
}

func TestStringArrayAttr_DefensiveCopy(t *testing.T) {
	src := []string{"a", "b"}
	kv := StringArrayAttr("defrost.cmd", src)
	src[0] = "X"
	got := kv.Value.([]string)
	if got[0] != "a" {
		t.Errorf("expected defensive copy, got %v", got)
	}
}
