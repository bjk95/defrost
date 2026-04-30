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
