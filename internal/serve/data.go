package serve

import (
	"sort"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/persist"
)

// MaxRuns is the cap on grid columns. Runs older than the latest 50 are
// dropped from the in-memory Dataset.
const MaxRuns = 50

// Dataset is a request-scoped view of the data branch sized for the grid
// UI. Roots are root run ResourceSpans (column axis, newest first).
// TestSpans are the per-test span time series, keyed by encoded span name
// (file basename in traces/), filtered to only those whose run_id still
// appears in Roots after capping. Metrics are every persisted metric
// ResourceMetrics record; the /api/metrics handler resolves each data
// point to a kept run by exemplar trace_id or by time-window fallback.
type Dataset struct {
	Roots     []*tracepb.ResourceSpans
	TestSpans map[string][]*tracepb.ResourceSpans
	Metrics   []*metricspb.ResourceMetrics
}

// persistLoadAll is a package-level seam so tests can stub the data
// source without going through git. Default impl calls through the
// Backend selected by the persist.Options. The progress callback is
// forwarded so the boot-screen SSE feed can stream real phase events.
var persistLoadAll = func(opts persist.Options, progress persist.ProgressFn) ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	return persist.New(opts).LoadAllWithProgress(progress)
}

// persistLoadAllMetrics is the metrics seam, parallel to persistLoadAll.
var persistLoadAllMetrics = func(opts persist.Options, progress persist.ProgressFn) ([]*metricspb.ResourceMetrics, error) {
	return persist.New(opts).LoadAllMetricsWithProgress(progress)
}

// Load reads the data branch and returns a sorted, capped Dataset suitable
// for serving over HTTP.
func Load(opts persist.Options) (Dataset, error) {
	return LoadWithProgress(opts, nil)
}

// LoadWithProgress is Load with phase boundary events streamed to emit
// (connect → clone → spans → parse → metrics → index → ready). Pass nil
// for a silent load (equivalent to Load). Used by the boot-screen SSE
// handler.
func LoadWithProgress(opts persist.Options, emit ProgressEmitter) (Dataset, error) {
	if emit == nil {
		emit = func(ProgressEvent) {}
	}
	emit(ProgressEvent{Phase: "connect", Detail: "git ls-remote origin"})
	progressFn := func(phase, detail, stream string) {
		emit(ProgressEvent{Phase: phase, Detail: detail, Stream: stream})
	}
	roots, byName, err := persistLoadAll(opts, progressFn)
	if err != nil {
		return Dataset{}, err
	}

	// Sort newest-first by start time so the grid columns flow most-
	// recent to oldest after the filter.
	sort.Slice(roots, func(i, j int) bool {
		ai := persist.SpanFromResourceSpans(roots[i])
		aj := persist.SpanFromResourceSpans(roots[j])
		if ai == nil || aj == nil {
			return false
		}
		return ai.StartTimeUnixNano > aj.StartTimeUnixNano
	})
	if len(roots) > MaxRuns {
		roots = roots[:MaxRuns]
	}

	keep := make(map[string]struct{}, len(roots))
	for _, rs := range roots {
		if id := runIDOf(rs); id != "" {
			keep[id] = struct{}{}
		}
	}

	filtered := make(map[string][]*tracepb.ResourceSpans, len(byName))
	for tid, spans := range byName {
		var kept []*tracepb.ResourceSpans
		for _, rs := range spans {
			if _, ok := keep[runIDOf(rs)]; ok {
				kept = append(kept, rs)
			}
		}
		if len(kept) > 0 {
			filtered[tid] = kept
		}
	}

	metrics, err := persistLoadAllMetrics(opts, progressFn)
	if err != nil {
		return Dataset{}, err
	}

	emit(ProgressEvent{Phase: "index", Detail: "grouping by encoded test name"})
	ds := Dataset{Roots: roots, TestSpans: filtered, Metrics: metrics}
	emit(ProgressEvent{Phase: "ready", Detail: "dashboard online"})
	return ds, nil
}
