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
