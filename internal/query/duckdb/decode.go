// Package duckdb hydrates a local DuckDB from the data branch's
// canonical OTLP/protobuf files. The hydration pipeline is:
//
//  1. Walk traces/, metrics/, logs/ under the cloned data branch
//     (or scratch dir in dev mode).
//  2. For each new file (not in hydration_state), zstd-decompress
//     and decode into pdata via ptraceotlp/pmetricotlp/plogotlp.
//  3. Project pdata into the schema in schema.go and INSERT.
//  4. Record the file in hydration_state.
//
// Decoding is done entirely in Go so we don't need the community
// `duckdb-otlp` extension (which isn't reachable from many CI
// environments and pins to a specific DuckDB version anyway).
package duckdb

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/bjk95/defrost/internal/persist"
)

func decodeTraces(raw []byte) (ptrace.Traces, error) {
	req := ptraceotlp.NewExportRequest()
	if err := req.UnmarshalProto(raw); err != nil {
		return ptrace.Traces{}, fmt.Errorf("unmarshal traces: %w", err)
	}
	return req.Traces(), nil
}

func decodeMetrics(raw []byte) (pmetric.Metrics, error) {
	req := pmetricotlp.NewExportRequest()
	if err := req.UnmarshalProto(raw); err != nil {
		return pmetric.Metrics{}, fmt.Errorf("unmarshal metrics: %w", err)
	}
	return req.Metrics(), nil
}

func decodeLogs(raw []byte) (plog.Logs, error) {
	req := plogotlp.NewExportRequest()
	if err := req.UnmarshalProto(raw); err != nil {
		return plog.Logs{}, fmt.Errorf("unmarshal logs: %w", err)
	}
	return req.Logs(), nil
}

// insertTraces decomposes a ptrace.Traces into one row per span and
// INSERTs them in a single transaction.
func insertTraces(ctx context.Context, tx *sql.Tx, td ptrace.Traces) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO traces
        (trace_id, span_id, parent_span, span_name, service_name,
         start_time, end_time, duration_ns, status_code, status_msg,
         attrs, resource, output)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	rs := td.ResourceSpans()
	for i := 0; i < rs.Len(); i++ {
		r := rs.At(i)
		resJSON := mapToJSON(r.Resource().Attributes())
		serviceName := r.Resource().Attributes().AsRaw()["service.name"]
		serviceNameStr := ""
		if s, ok := serviceName.(string); ok {
			serviceNameStr = s
		}
		ss := r.ScopeSpans()
		for j := 0; j < ss.Len(); j++ {
			scope := ss.At(j)
			for k := 0; k < scope.Spans().Len(); k++ {
				span := scope.Spans().At(k)
				start := time.Unix(0, int64(span.StartTimestamp()))
				end := time.Unix(0, int64(span.EndTimestamp()))
				durNs := int64(span.EndTimestamp() - span.StartTimestamp())
				output := extractTestOutput(span)
				if _, err := stmt.ExecContext(ctx,
					hex.EncodeToString(traceIDBytes(span.TraceID())),
					hex.EncodeToString(spanIDBytes(span.SpanID())),
					hex.EncodeToString(spanIDBytes(span.ParentSpanID())),
					span.Name(),
					serviceNameStr,
					start.UTC(),
					end.UTC(),
					durNs,
					int32(span.Status().Code()),
					span.Status().Message(),
					mapToJSON(span.Attributes()),
					resJSON,
					output,
				); err != nil {
					return fmt.Errorf("insert span: %w", err)
				}
			}
		}
	}
	return nil
}

func insertMetrics(ctx context.Context, tx *sql.Tx, md pmetric.Metrics) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO metrics
        (metric_name, metric_unit, metric_type, value, ts, start_ts,
         trace_id, attrs, resource, histogram)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		resJSON := mapToJSON(rm.Resource().Attributes())
		sms := rm.ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			sm := sms.At(j)
			for k := 0; k < sm.Metrics().Len(); k++ {
				m := sm.Metrics().At(k)
				switch m.Type() {
				case pmetric.MetricTypeGauge:
					for d := 0; d < m.Gauge().DataPoints().Len(); d++ {
						dp := m.Gauge().DataPoints().At(d)
						if err := insertNumberDP(ctx, stmt, m, "gauge", dp, resJSON); err != nil {
							return err
						}
					}
				case pmetric.MetricTypeSum:
					for d := 0; d < m.Sum().DataPoints().Len(); d++ {
						dp := m.Sum().DataPoints().At(d)
						if err := insertNumberDP(ctx, stmt, m, "sum", dp, resJSON); err != nil {
							return err
						}
					}
				case pmetric.MetricTypeHistogram:
					for d := 0; d < m.Histogram().DataPoints().Len(); d++ {
						dp := m.Histogram().DataPoints().At(d)
						if err := insertHistogramDP(ctx, stmt, m, "histogram", dp.Timestamp(), dp.StartTimestamp(), histogramMean(dp.Sum(), dp.Count()), dp.Attributes(), exemplarTraceID(dp.Exemplars()), resJSON, encodeHistogramDP(dp)); err != nil {
							return err
						}
					}
				case pmetric.MetricTypeExponentialHistogram:
					for d := 0; d < m.ExponentialHistogram().DataPoints().Len(); d++ {
						dp := m.ExponentialHistogram().DataPoints().At(d)
						if err := insertHistogramDP(ctx, stmt, m, "exp_histogram", dp.Timestamp(), dp.StartTimestamp(), histogramMean(dp.Sum(), dp.Count()), dp.Attributes(), exemplarTraceID(dp.Exemplars()), resJSON, encodeExpHistogramDP(dp)); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func insertNumberDP(ctx context.Context, stmt *sql.Stmt, m pmetric.Metric, metricType string, dp pmetric.NumberDataPoint, resJSON string) error {
	val := 0.0
	switch dp.ValueType() {
	case pmetric.NumberDataPointValueTypeDouble:
		val = dp.DoubleValue()
	case pmetric.NumberDataPointValueTypeInt:
		val = float64(dp.IntValue())
	}
	traceID := exemplarTraceID(dp.Exemplars())
	_, err := stmt.ExecContext(ctx,
		m.Name(),
		m.Unit(),
		metricType,
		val,
		time.Unix(0, int64(dp.Timestamp())).UTC(),
		time.Unix(0, int64(dp.StartTimestamp())).UTC(),
		traceID,
		mapToJSON(dp.Attributes()),
		resJSON,
		nil, // histogram payload — only set for histogram/exp_histogram
	)
	if err != nil {
		return fmt.Errorf("insert metric data point: %w", err)
	}
	return nil
}

// insertHistogramDP persists a histogram (or exponential histogram)
// data point. The summary `value` is a lossy mean (sum/count) kept
// for legacy single-scalar queries; the full bucket counts, bounds,
// min/max, and (for exponential) scale + zero count live in the
// histogram JSON column so quantile/heatmap queries can reconstruct
// the distribution faithfully.
func insertHistogramDP(ctx context.Context, stmt *sql.Stmt, m pmetric.Metric, metricType string, ts, start pcommon.Timestamp, value float64, attrs pcommon.Map, traceID, resJSON, histogramJSON string) error {
	_, err := stmt.ExecContext(ctx,
		m.Name(),
		m.Unit(),
		metricType,
		value,
		time.Unix(0, int64(ts)).UTC(),
		time.Unix(0, int64(start)).UTC(),
		traceID,
		mapToJSON(attrs),
		resJSON,
		histogramJSON,
	)
	if err != nil {
		return fmt.Errorf("insert histogram data point: %w", err)
	}
	return nil
}

// encodeHistogramDP serializes an explicit-bucket histogram data
// point as JSON: count, sum, min/max (when present), bucket_counts,
// and explicit_bounds. Aligns with the OTel Collector's ClickHouse
// exporter so a hosted backend storing the same JSON shape can serve
// the same dashboard queries without translation.
func encodeHistogramDP(dp pmetric.HistogramDataPoint) string {
	type out struct {
		Type           string    `json:"type"`
		Count          uint64    `json:"count"`
		Sum            *float64  `json:"sum,omitempty"`
		Min            *float64  `json:"min,omitempty"`
		Max            *float64  `json:"max,omitempty"`
		BucketCounts   []uint64  `json:"bucket_counts"`
		ExplicitBounds []float64 `json:"explicit_bounds"`
	}
	o := out{
		Type:           "histogram",
		Count:          dp.Count(),
		BucketCounts:   dp.BucketCounts().AsRaw(),
		ExplicitBounds: dp.ExplicitBounds().AsRaw(),
	}
	if dp.HasSum() {
		v := dp.Sum()
		o.Sum = &v
	}
	if dp.HasMin() {
		v := dp.Min()
		o.Min = &v
	}
	if dp.HasMax() {
		v := dp.Max()
		o.Max = &v
	}
	b, err := json.Marshal(o)
	if err != nil {
		return ""
	}
	return string(b)
}

// encodeExpHistogramDP serializes an exponential-histogram data
// point: count, sum, min/max, scale, zero_count, plus the positive
// and negative bucket runs (offset + counts each).
func encodeExpHistogramDP(dp pmetric.ExponentialHistogramDataPoint) string {
	type buckets struct {
		Offset       int32    `json:"offset"`
		BucketCounts []uint64 `json:"bucket_counts"`
	}
	type out struct {
		Type      string   `json:"type"`
		Count     uint64   `json:"count"`
		Sum       *float64 `json:"sum,omitempty"`
		Min       *float64 `json:"min,omitempty"`
		Max       *float64 `json:"max,omitempty"`
		Scale     int32    `json:"scale"`
		ZeroCount uint64   `json:"zero_count"`
		Positive  buckets  `json:"positive"`
		Negative  buckets  `json:"negative"`
	}
	o := out{
		Type:      "exp_histogram",
		Count:     dp.Count(),
		Scale:     dp.Scale(),
		ZeroCount: dp.ZeroCount(),
		Positive: buckets{
			Offset:       dp.Positive().Offset(),
			BucketCounts: dp.Positive().BucketCounts().AsRaw(),
		},
		Negative: buckets{
			Offset:       dp.Negative().Offset(),
			BucketCounts: dp.Negative().BucketCounts().AsRaw(),
		},
	}
	if dp.HasSum() {
		v := dp.Sum()
		o.Sum = &v
	}
	if dp.HasMin() {
		v := dp.Min()
		o.Min = &v
	}
	if dp.HasMax() {
		v := dp.Max()
		o.Max = &v
	}
	b, err := json.Marshal(o)
	if err != nil {
		return ""
	}
	return string(b)
}

func insertLogs(ctx context.Context, tx *sql.Tx, ld plog.Logs) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO logs
        (trace_id, span_id, ts, severity, body, attrs, resource)
        VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	rls := ld.ResourceLogs()
	for i := 0; i < rls.Len(); i++ {
		rl := rls.At(i)
		resJSON := mapToJSON(rl.Resource().Attributes())
		sls := rl.ScopeLogs()
		for j := 0; j < sls.Len(); j++ {
			sl := sls.At(j)
			for k := 0; k < sl.LogRecords().Len(); k++ {
				rec := sl.LogRecords().At(k)
				if _, err := stmt.ExecContext(ctx,
					hex.EncodeToString(traceIDBytes(rec.TraceID())),
					hex.EncodeToString(spanIDBytes(rec.SpanID())),
					time.Unix(0, int64(rec.Timestamp())).UTC(),
					rec.SeverityText(),
					rec.Body().AsString(),
					mapToJSON(rec.Attributes()),
					resJSON,
				); err != nil {
					return fmt.Errorf("insert log record: %w", err)
				}
			}
		}
	}
	return nil
}

func mapToJSON(m pcommon.Map) string {
	if m.Len() == 0 {
		return "{}"
	}
	raw := m.AsRaw()
	b, err := json.Marshal(raw)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// extractTestOutput pulls the captured stdout/stderr from a span event
// named "test.output" with attribute "body". This mirrors the schema-2
// shape the dashboard's run-detail page expects.
func extractTestOutput(span ptrace.Span) string {
	events := span.Events()
	for i := 0; i < events.Len(); i++ {
		e := events.At(i)
		if e.Name() != "test.output" {
			continue
		}
		v, ok := e.Attributes().Get("body")
		if !ok {
			continue
		}
		if v.Type() == pcommon.ValueTypeStr {
			return v.Str()
		}
	}
	return ""
}

// traceIDBytes returns the raw 16-byte slice of a pcommon.TraceID.
// pdata exposes TraceID as a [16]byte; .Bytes() does not exist.
func traceIDBytes(id pcommon.TraceID) []byte {
	return id[:]
}

// spanIDBytes is the SpanID counterpart of traceIDBytes.
func spanIDBytes(id pcommon.SpanID) []byte {
	return id[:]
}

func histogramMean(sum float64, count uint64) float64 {
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func exemplarTraceID(ex pmetric.ExemplarSlice) string {
	for i := 0; i < ex.Len(); i++ {
		tid := ex.At(i).TraceID()
		if !tid.IsEmpty() {
			return hex.EncodeToString(traceIDBytes(tid))
		}
	}
	return ""
}

// pathStat is the minimum metadata we record in hydration_state to
// detect file changes between runs. Mtime + size is plenty: defrost
// files are immutable per trace_id, so any change is treated as a
// rewrite (e.g. drop + re-clone) and we can safely re-INSERT.
type pathStat struct {
	size  int64
	mtime int64
}

func statFile(path string) (pathStat, error) {
	info, err := os.Stat(path)
	if err != nil {
		return pathStat{}, err
	}
	return pathStat{size: info.Size(), mtime: info.ModTime().UnixNano()}, nil
}

// signalDirs is the set of subdirectories a defrost data branch
// contains. Order matters only for predictable progress reporting.
var signalDirs = []string{"traces", "metrics", "logs"}

// listFiles is a small wrapper around persist.ListSignalFiles that
// returns paths absolute under root.
func listFiles(root, signal string) ([]string, error) {
	files, err := persist.ListSignalFiles(root, signal)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(files))
	for i, p := range files {
		if filepath.IsAbs(p) {
			out[i] = p
		} else {
			out[i] = filepath.Join(root, p)
		}
	}
	return out, nil
}
