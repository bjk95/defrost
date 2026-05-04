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
// Schema mirrors the duckdb-otlp extension exactly so the same SQL
// queries work against this cache and against the extension's
// table-valued functions over raw OTLP files.
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

// serviceTriple returns (service.name, service.namespace,
// service.instance.id) from a resource attribute map. duckdb-otlp
// pulls these out into dedicated columns alongside leaving them in
// the resource_attributes JSON.
func serviceTriple(res pcommon.Map) (string, string, string) {
	raw := res.AsRaw()
	get := func(k string) string {
		if v, ok := raw[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	return get("service.name"), get("service.namespace"), get("service.instance.id")
}

// insertTraces decomposes a ptrace.Traces into one row per span and
// INSERTs them into otel_traces in a single transaction.
func insertTraces(ctx context.Context, tx *sql.Tx, td ptrace.Traces) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO otel_traces (
        timestamp, end_timestamp, duration,
        trace_id, span_id, parent_span_id, trace_state,
        service_name, service_namespace, service_instance_id,
        span_name, span_kind, status_code, status_message,
        resource_attributes, scope_name, scope_version, scope_attributes,
        span_attributes, events_json, links_json,
        dropped_attributes_count, dropped_events_count, dropped_links_count,
        flags
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	rs := td.ResourceSpans()
	for i := 0; i < rs.Len(); i++ {
		r := rs.At(i)
		resJSON := mapToJSON(r.Resource().Attributes())
		svcName, svcNs, svcID := serviceTriple(r.Resource().Attributes())
		ss := r.ScopeSpans()
		for j := 0; j < ss.Len(); j++ {
			scope := ss.At(j)
			scopeName := scope.Scope().Name()
			scopeVersion := scope.Scope().Version()
			scopeAttrs := mapToJSON(scope.Scope().Attributes())
			for k := 0; k < scope.Spans().Len(); k++ {
				span := scope.Spans().At(k)
				startNs := int64(span.StartTimestamp())
				endNs := int64(span.EndTimestamp())
				if _, err := stmt.ExecContext(ctx,
					nsToTimeMS(startNs),
					endNs,
					endNs-startNs,
					hex.EncodeToString(traceIDBytes(span.TraceID())),
					hex.EncodeToString(spanIDBytes(span.SpanID())),
					hex.EncodeToString(spanIDBytes(span.ParentSpanID())),
					span.TraceState().AsRaw(),
					svcName, svcNs, svcID,
					span.Name(),
					int32(span.Kind()),
					int32(span.Status().Code()),
					span.Status().Message(),
					resJSON,
					scopeName, scopeVersion, scopeAttrs,
					mapToJSON(span.Attributes()),
					encodeSpanEvents(span.Events()),
					encodeSpanLinks(span.Links()),
					int32(span.DroppedAttributesCount()),
					int32(span.DroppedEventsCount()),
					int32(span.DroppedLinksCount()),
					int32(span.Flags()),
				); err != nil {
					return fmt.Errorf("insert span: %w", err)
				}
			}
		}
	}
	return nil
}

// insertMetrics dispatches each pmetric data point to the table that
// matches its type. duckdb-otlp uses one table per metric type
// (gauge, sum, histogram, exp_histogram); we follow the same
// convention so cross-table queries match the extension's docs.
func insertMetrics(ctx context.Context, tx *sql.Tx, md pmetric.Metrics) error {
	gaugeStmt, err := tx.PrepareContext(ctx, `INSERT INTO otel_metrics_gauge (
        timestamp, start_timestamp, metric_name, metric_description, metric_unit,
        value, service_name, service_namespace, service_instance_id,
        resource_attributes, scope_name, scope_version, scope_attributes,
        metric_attributes, flags, exemplars_json
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer gaugeStmt.Close()
	sumStmt, err := tx.PrepareContext(ctx, `INSERT INTO otel_metrics_sum (
        timestamp, start_timestamp, metric_name, metric_description, metric_unit,
        value, service_name, service_namespace, service_instance_id,
        resource_attributes, scope_name, scope_version, scope_attributes,
        metric_attributes, flags, exemplars_json,
        aggregation_temporality, is_monotonic
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer sumStmt.Close()
	histStmt, err := tx.PrepareContext(ctx, `INSERT INTO otel_metrics_histogram (
        timestamp, start_timestamp, metric_name, metric_description, metric_unit,
        count, sum, min, max, bucket_counts, explicit_bounds,
        service_name, service_namespace, service_instance_id,
        resource_attributes, scope_name, scope_version, scope_attributes,
        metric_attributes, flags, exemplars_json, aggregation_temporality
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer histStmt.Close()
	expHistStmt, err := tx.PrepareContext(ctx, `INSERT INTO otel_metrics_exp_histogram (
        timestamp, start_timestamp, metric_name, metric_description, metric_unit,
        count, sum, min, max, scale, zero_count, zero_threshold,
        positive_offset, positive_bucket_counts, negative_offset, negative_bucket_counts,
        service_name, service_namespace, service_instance_id,
        resource_attributes, scope_name, scope_version, scope_attributes,
        metric_attributes, flags, exemplars_json, aggregation_temporality
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer expHistStmt.Close()

	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		resJSON := mapToJSON(rm.Resource().Attributes())
		svcName, svcNs, svcID := serviceTriple(rm.Resource().Attributes())
		sms := rm.ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			sm := sms.At(j)
			scopeName := sm.Scope().Name()
			scopeVersion := sm.Scope().Version()
			scopeAttrs := mapToJSON(sm.Scope().Attributes())
			for k := 0; k < sm.Metrics().Len(); k++ {
				m := sm.Metrics().At(k)
				switch m.Type() {
				case pmetric.MetricTypeGauge:
					dps := m.Gauge().DataPoints()
					for d := 0; d < dps.Len(); d++ {
						dp := dps.At(d)
						if _, err := gaugeStmt.ExecContext(ctx,
							nsToTimeMS(int64(dp.Timestamp())),
							int64(dp.StartTimestamp()),
							m.Name(), m.Description(), m.Unit(),
							numberDPValue(dp),
							svcName, svcNs, svcID,
							resJSON, scopeName, scopeVersion, scopeAttrs,
							mapToJSON(dp.Attributes()),
							int32(dp.Flags()),
							encodeExemplars(dp.Exemplars()),
						); err != nil {
							return fmt.Errorf("insert gauge: %w", err)
						}
					}
				case pmetric.MetricTypeSum:
					dps := m.Sum().DataPoints()
					for d := 0; d < dps.Len(); d++ {
						dp := dps.At(d)
						if _, err := sumStmt.ExecContext(ctx,
							nsToTimeMS(int64(dp.Timestamp())),
							int64(dp.StartTimestamp()),
							m.Name(), m.Description(), m.Unit(),
							numberDPValue(dp),
							svcName, svcNs, svcID,
							resJSON, scopeName, scopeVersion, scopeAttrs,
							mapToJSON(dp.Attributes()),
							int32(dp.Flags()),
							encodeExemplars(dp.Exemplars()),
							int32(m.Sum().AggregationTemporality()),
							m.Sum().IsMonotonic(),
						); err != nil {
							return fmt.Errorf("insert sum: %w", err)
						}
					}
				case pmetric.MetricTypeHistogram:
					dps := m.Histogram().DataPoints()
					for d := 0; d < dps.Len(); d++ {
						dp := dps.At(d)
						if _, err := histStmt.ExecContext(ctx,
							nsToTimeMS(int64(dp.Timestamp())),
							int64(dp.StartTimestamp()),
							m.Name(), m.Description(), m.Unit(),
							int64(dp.Count()),
							optionalSum(dp.HasSum, dp.Sum),
							optionalSum(dp.HasMin, dp.Min),
							optionalSum(dp.HasMax, dp.Max),
							jsonUint64Slice(dp.BucketCounts().AsRaw()),
							jsonFloat64Slice(dp.ExplicitBounds().AsRaw()),
							svcName, svcNs, svcID,
							resJSON, scopeName, scopeVersion, scopeAttrs,
							mapToJSON(dp.Attributes()),
							int32(dp.Flags()),
							encodeExemplars(dp.Exemplars()),
							int32(m.Histogram().AggregationTemporality()),
						); err != nil {
							return fmt.Errorf("insert histogram: %w", err)
						}
					}
				case pmetric.MetricTypeExponentialHistogram:
					dps := m.ExponentialHistogram().DataPoints()
					for d := 0; d < dps.Len(); d++ {
						dp := dps.At(d)
						if _, err := expHistStmt.ExecContext(ctx,
							nsToTimeMS(int64(dp.Timestamp())),
							int64(dp.StartTimestamp()),
							m.Name(), m.Description(), m.Unit(),
							int64(dp.Count()),
							optionalSum(dp.HasSum, dp.Sum),
							optionalSum(dp.HasMin, dp.Min),
							optionalSum(dp.HasMax, dp.Max),
							int32(dp.Scale()),
							int64(dp.ZeroCount()),
							dp.ZeroThreshold(),
							dp.Positive().Offset(),
							jsonUint64Slice(dp.Positive().BucketCounts().AsRaw()),
							dp.Negative().Offset(),
							jsonUint64Slice(dp.Negative().BucketCounts().AsRaw()),
							svcName, svcNs, svcID,
							resJSON, scopeName, scopeVersion, scopeAttrs,
							mapToJSON(dp.Attributes()),
							int32(dp.Flags()),
							encodeExemplars(dp.Exemplars()),
							int32(m.ExponentialHistogram().AggregationTemporality()),
						); err != nil {
							return fmt.Errorf("insert exp_histogram: %w", err)
						}
					}
				}
			}
		}
	}
	return nil
}

func insertLogs(ctx context.Context, tx *sql.Tx, ld plog.Logs) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO otel_logs (
        timestamp, observed_timestamp, trace_id, span_id,
        service_name, service_namespace, service_instance_id,
        severity_number, severity_text, body,
        resource_attributes, scope_name, scope_version, scope_attributes,
        log_attributes
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	rls := ld.ResourceLogs()
	for i := 0; i < rls.Len(); i++ {
		rl := rls.At(i)
		resJSON := mapToJSON(rl.Resource().Attributes())
		svcName, svcNs, svcID := serviceTriple(rl.Resource().Attributes())
		sls := rl.ScopeLogs()
		for j := 0; j < sls.Len(); j++ {
			sl := sls.At(j)
			scopeName := sl.Scope().Name()
			scopeVersion := sl.Scope().Version()
			scopeAttrs := mapToJSON(sl.Scope().Attributes())
			for k := 0; k < sl.LogRecords().Len(); k++ {
				rec := sl.LogRecords().At(k)
				if _, err := stmt.ExecContext(ctx,
					nsToTimeMS(int64(rec.Timestamp())),
					int64(rec.ObservedTimestamp()),
					hex.EncodeToString(traceIDBytes(rec.TraceID())),
					hex.EncodeToString(spanIDBytes(rec.SpanID())),
					svcName, svcNs, svcID,
					int32(rec.SeverityNumber()),
					rec.SeverityText(),
					rec.Body().AsString(),
					resJSON, scopeName, scopeVersion, scopeAttrs,
					mapToJSON(rec.Attributes()),
				); err != nil {
					return fmt.Errorf("insert log record: %w", err)
				}
			}
		}
	}
	return nil
}

// nsToTimeMS converts an OTLP nanosecond Unix timestamp to a Go
// time.Time at millisecond resolution. The timestamp column type is
// TIMESTAMP_MS, so go-duckdb truncates anyway; pre-truncating in Go
// makes the insert side reflect what'll actually be stored.
func nsToTimeMS(ns int64) time.Time {
	return time.UnixMilli(ns / int64(time.Millisecond)).UTC()
}

func numberDPValue(dp pmetric.NumberDataPoint) float64 {
	switch dp.ValueType() {
	case pmetric.NumberDataPointValueTypeDouble:
		return dp.DoubleValue()
	case pmetric.NumberDataPointValueTypeInt:
		return float64(dp.IntValue())
	}
	return 0
}

func optionalSum(has func() bool, get func() float64) any {
	if !has() {
		return nil
	}
	return get()
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

func jsonUint64Slice(s []uint64) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func jsonFloat64Slice(s []float64) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// encodeSpanEvents serialises a span event slice as a JSON array
// where each entry has timestamp (ns), name, attributes, and
// dropped_attributes_count. Matches duckdb-otlp's events_json shape.
func encodeSpanEvents(events ptrace.SpanEventSlice) string {
	if events.Len() == 0 {
		return "[]"
	}
	type entry struct {
		Timestamp              int64          `json:"timestamp"`
		Name                   string         `json:"name"`
		Attributes             map[string]any `json:"attributes"`
		DroppedAttributesCount uint32         `json:"dropped_attributes_count"`
	}
	out := make([]entry, 0, events.Len())
	for i := 0; i < events.Len(); i++ {
		e := events.At(i)
		out = append(out, entry{
			Timestamp:              int64(e.Timestamp()),
			Name:                   e.Name(),
			Attributes:             e.Attributes().AsRaw(),
			DroppedAttributesCount: e.DroppedAttributesCount(),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// encodeSpanLinks serialises a span link slice as a JSON array.
func encodeSpanLinks(links ptrace.SpanLinkSlice) string {
	if links.Len() == 0 {
		return "[]"
	}
	type entry struct {
		TraceID                string         `json:"trace_id"`
		SpanID                 string         `json:"span_id"`
		TraceState             string         `json:"trace_state"`
		Attributes             map[string]any `json:"attributes"`
		DroppedAttributesCount uint32         `json:"dropped_attributes_count"`
		Flags                  uint32         `json:"flags"`
	}
	out := make([]entry, 0, links.Len())
	for i := 0; i < links.Len(); i++ {
		l := links.At(i)
		out = append(out, entry{
			TraceID:                hex.EncodeToString(traceIDBytes(l.TraceID())),
			SpanID:                 hex.EncodeToString(spanIDBytes(l.SpanID())),
			TraceState:             l.TraceState().AsRaw(),
			Attributes:             l.Attributes().AsRaw(),
			DroppedAttributesCount: l.DroppedAttributesCount(),
			Flags:                  l.Flags(),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// encodeExemplars serialises a metric data point's exemplar slice as
// a JSON array. Each entry has timestamp, value, trace_id, span_id,
// and filtered_attributes — matching duckdb-otlp.
func encodeExemplars(ex pmetric.ExemplarSlice) string {
	if ex.Len() == 0 {
		return "[]"
	}
	type entry struct {
		Timestamp          int64          `json:"timestamp"`
		Value              float64        `json:"value"`
		TraceID            string         `json:"trace_id"`
		SpanID             string         `json:"span_id"`
		FilteredAttributes map[string]any `json:"filtered_attributes"`
	}
	out := make([]entry, 0, ex.Len())
	for i := 0; i < ex.Len(); i++ {
		e := ex.At(i)
		v := 0.0
		switch e.ValueType() {
		case pmetric.ExemplarValueTypeDouble:
			v = e.DoubleValue()
		case pmetric.ExemplarValueTypeInt:
			v = float64(e.IntValue())
		}
		out = append(out, entry{
			Timestamp:          int64(e.Timestamp()),
			Value:              v,
			TraceID:            hex.EncodeToString(traceIDBytes(e.TraceID())),
			SpanID:             hex.EncodeToString(spanIDBytes(e.SpanID())),
			FilteredAttributes: e.FilteredAttributes().AsRaw(),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
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
