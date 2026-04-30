package serve

import (
	"math"
	"sort"
	"strconv"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// metricSeriesDTO is the wire shape consumed by the frontend metrics
// page. It mirrors web/src/lib/metrics.ts:MetricSeries.
type metricSeriesDTO struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Unit        string           `json:"unit,omitempty"`
	Instrument  string           `json:"instrument"`
	Temporality string           `json:"temporality,omitempty"`
	Monotonic   bool             `json:"monotonic,omitempty"`
	Points      []metricPointDTO `json:"points"`
}

type metricPointDTO struct {
	RunID   string            `json:"run_id"`
	TS      string            `json:"ts"`
	Attrs   map[string]string `json:"attrs"`
	Value   *float64          `json:"value,omitempty"`
	Count   *uint64           `json:"count,omitempty"`
	Sum     *float64          `json:"sum,omitempty"`
	Min     *float64          `json:"min,omitempty"`
	Max     *float64          `json:"max,omitempty"`
	Buckets []bucketDTO       `json:"buckets,omitempty"`
}

type bucketDTO struct {
	LE    *float64 `json:"le"` // null encodes +Inf
	Count uint64   `json:"count"`
}

// runWindow defaults bound how far before/after a run's recorded start
// time we'll attribute a metric data point to that run when no exemplar
// is present. Most run-scoped metrics (suite duration, peak heap, etc.)
// land at end-of-run; the next run typically starts seconds-to-minutes
// later, so a generous window is fine.
const (
	runWindowGraceBeforeNs int64 = 5 * 1_000_000_000        // 5s
	runWindowFallbackNs    int64 = int64(2 * 60 * 60) * 1e9 // 2h
)

// buildMetricsResponse translates the kept ResourceMetrics into the
// frontend wire shape, grouping by metric name. Run association is
// resolved per data point via:
//  1. exemplar trace_id matching DeriveTraceID(run_id), or
//  2. time_unix_nano falling inside the run's [start, end] window
//     (with a small grace period both sides). The window allows
//     legacy/external metrics that don't stamp exemplars (e.g. the
//     auto-emitted defrost.run.<cmd> gauge) to be displayed.
func buildMetricsResponse(
	rms []*metricspb.ResourceMetrics,
	roots []*tracepb.ResourceSpans,
) []metricSeriesDTO {
	if len(rms) == 0 {
		return []metricSeriesDTO{}
	}
	resolver := newRunResolver(roots)

	byName := make(map[string]*seriesAcc)
	for _, rm := range rms {
		m := persist.MetricFromResourceMetrics(rm)
		if m == nil {
			continue
		}
		acc, ok := byName[m.Name]
		if !ok {
			acc = &seriesAcc{
				dto: metricSeriesDTO{
					Name:        m.Name,
					Description: m.Description,
					Unit:        m.Unit,
				},
			}
			byName[m.Name] = acc
		}
		extractDataPoints(acc, m, resolver)
	}

	out := make([]metricSeriesDTO, 0, len(byName))
	for _, acc := range byName {
		if len(acc.points) == 0 {
			continue
		}
		acc.dto.Points = acc.points
		out = append(out, acc.dto)
	}

	// Gauges first, then sums, then histograms; alpha within group.
	sort.Slice(out, func(i, j int) bool {
		oi := instrumentOrder(out[i].Instrument)
		oj := instrumentOrder(out[j].Instrument)
		if oi != oj {
			return oi < oj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

type runRef struct {
	runID string
	ts    string
}

type seriesAcc struct {
	dto    metricSeriesDTO
	points []metricPointDTO
}

// runResolver maps a metric data point to the run it belongs to. It
// supports two strategies: trace_id exemplar lookup (preferred — an
// exact match) and time-window lookup (fallback for metrics that didn't
// stamp exemplars).
type runResolver struct {
	traceIDToRun map[string]runRef
	// windows are sorted ascending by start. Each entry tracks the
	// run's [start, end] interval so we can pick the run containing a
	// given timestamp via binary search.
	windows []runWindow
}

type runWindow struct {
	start int64
	end   int64
	ref   runRef
}

func newRunResolver(roots []*tracepb.ResourceSpans) *runResolver {
	r := &runResolver{traceIDToRun: make(map[string]runRef, len(roots))}
	for _, rs := range roots {
		id := runIDOf(rs)
		if id == "" {
			continue
		}
		span := persist.SpanFromResourceSpans(rs)
		if span == nil {
			continue
		}
		ref := runRef{
			runID: id,
			ts:    nanosToRFC3339(int64(span.StartTimeUnixNano)),
		}
		r.traceIDToRun[string(models.DeriveTraceID(id))] = ref

		start := int64(span.StartTimeUnixNano)
		end := int64(span.EndTimeUnixNano)
		if end <= start {
			// Root span hasn't been finalised, or the span lacks an
			// end time; pick a generous fallback so late-arriving
			// metrics still attach.
			end = start + runWindowFallbackNs
		}
		r.windows = append(r.windows, runWindow{
			start: start - runWindowGraceBeforeNs,
			end:   end + runWindowGraceBeforeNs,
			ref:   ref,
		})
	}
	sort.Slice(r.windows, func(i, j int) bool {
		return r.windows[i].start < r.windows[j].start
	})
	return r
}

// resolveDataPoint picks the run for a data point, preferring exemplar
// match. ts is the data point's time_unix_nano, used as the time-window
// fallback key.
func (r *runResolver) resolveDataPoint(exemplars []*metricspb.Exemplar, ts uint64) (runRef, bool) {
	for _, ex := range exemplars {
		if ref, ok := r.traceIDToRun[string(ex.GetTraceId())]; ok {
			return ref, true
		}
	}
	if ts == 0 || len(r.windows) == 0 {
		return runRef{}, false
	}
	t := int64(ts)
	// Binary search for the latest window whose start <= t.
	idx := sort.Search(len(r.windows), func(i int) bool {
		return r.windows[i].start > t
	}) - 1
	if idx < 0 {
		return runRef{}, false
	}
	w := r.windows[idx]
	if t > w.end {
		return runRef{}, false
	}
	return w.ref, true
}

func extractDataPoints(acc *seriesAcc, m *metricspb.Metric, resolver *runResolver) {
	switch d := m.Data.(type) {
	case *metricspb.Metric_Gauge:
		acc.dto.Instrument = "gauge"
		for _, dp := range d.Gauge.GetDataPoints() {
			run, ok := resolver.resolveDataPoint(dp.GetExemplars(), dp.GetTimeUnixNano())
			if !ok {
				continue
			}
			acc.points = append(acc.points, metricPointDTO{
				RunID: run.runID,
				TS:    run.ts,
				Attrs: attrsToMap(dp.GetAttributes()),
				Value: floatPtr(numberValue(dp)),
			})
		}
	case *metricspb.Metric_Sum:
		acc.dto.Instrument = "sum"
		acc.dto.Temporality = temporalityString(d.Sum.GetAggregationTemporality())
		acc.dto.Monotonic = d.Sum.GetIsMonotonic()
		for _, dp := range d.Sum.GetDataPoints() {
			run, ok := resolver.resolveDataPoint(dp.GetExemplars(), dp.GetTimeUnixNano())
			if !ok {
				continue
			}
			acc.points = append(acc.points, metricPointDTO{
				RunID: run.runID,
				TS:    run.ts,
				Attrs: attrsToMap(dp.GetAttributes()),
				Value: floatPtr(numberValue(dp)),
			})
		}
	case *metricspb.Metric_Histogram:
		acc.dto.Instrument = "histogram"
		acc.dto.Temporality = temporalityString(d.Histogram.GetAggregationTemporality())
		for _, dp := range d.Histogram.GetDataPoints() {
			run, ok := resolver.resolveDataPoint(dp.GetExemplars(), dp.GetTimeUnixNano())
			if !ok {
				continue
			}
			count := dp.Count
			pt := metricPointDTO{
				RunID:   run.runID,
				TS:      run.ts,
				Attrs:   attrsToMap(dp.GetAttributes()),
				Count:   &count,
				Buckets: bucketsFromHistogram(dp),
			}
			if dp.Sum != nil {
				pt.Sum = dp.Sum
			}
			if dp.Min != nil {
				pt.Min = dp.Min
			}
			if dp.Max != nil {
				pt.Max = dp.Max
			}
			acc.points = append(acc.points, pt)
		}
	}
}

func numberValue(dp *metricspb.NumberDataPoint) float64 {
	switch v := dp.Value.(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		return v.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		return float64(v.AsInt)
	}
	return 0
}

func bucketsFromHistogram(dp *metricspb.HistogramDataPoint) []bucketDTO {
	bounds := dp.GetExplicitBounds()
	counts := dp.GetBucketCounts()
	if len(counts) == 0 || len(bounds)+1 != len(counts) {
		return nil
	}
	out := make([]bucketDTO, 0, len(counts))
	for i, le := range bounds {
		v := le
		out = append(out, bucketDTO{LE: &v, Count: counts[i]})
	}
	out = append(out, bucketDTO{LE: nil, Count: counts[len(counts)-1]})
	return out
}

func attrsToMap(kvs []*commonpb.KeyValue) map[string]string {
	if len(kvs) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		if kv == nil || kv.Value == nil {
			continue
		}
		out[kv.Key] = stringifyAnyValue(kv.Value)
	}
	return out
}

func stringifyAnyValue(v *commonpb.AnyValue) string {
	switch x := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(x.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		if math.IsNaN(x.DoubleValue) || math.IsInf(x.DoubleValue, 0) {
			return ""
		}
		return strconv.FormatFloat(x.DoubleValue, 'g', -1, 64)
	case *commonpb.AnyValue_BoolValue:
		if x.BoolValue {
			return "true"
		}
		return "false"
	}
	return ""
}

func temporalityString(t metricspb.AggregationTemporality) string {
	switch t {
	case metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA:
		return "delta"
	case metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE:
		return "cumulative"
	}
	return ""
}

func instrumentOrder(s string) int {
	switch s {
	case "gauge":
		return 0
	case "sum":
		return 1
	case "histogram":
		return 2
	}
	return 3
}

func floatPtr(v float64) *float64 { return &v }
