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
