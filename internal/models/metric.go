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

// HistogramBucket is one entry in a histogram's bucket list.
// A nil UpperBound represents the +Inf overflow bucket.
type HistogramBucket struct {
	UpperBound *float64 `json:"upper_bound"` // nil for the +Inf bucket
	Count      uint64   `json:"count"`
}
