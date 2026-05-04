package serve

import (
	"sort"
	"time"

	"github.com/bjk95/defrost/internal/query"
)

// metricSeriesDTO is the wire shape of /api/metrics. One element per
// distinct metric name; data points are tagged with the run id they
// belong to (resolved from the exemplar trace_id, or by the
// time-window fallback when no exemplar is present).
type metricSeriesDTO struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Unit        string             `json:"unit,omitempty"`
	Points      []metricPointDTO   `json:"points"`
	Resource    map[string]string  `json:"resource,omitempty"`
	Attrs       []metricAttrPairDTO `json:"attrs,omitempty"`
}

type metricPointDTO struct {
	RunID     string  `json:"run_id,omitempty"`
	Timestamp string  `json:"ts"`
	Value     float64 `json:"value"`
	TraceID   string  `json:"trace_id,omitempty"`
}

type metricAttrPairDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// buildMetricsResponse groups the flat list of metric points into one
// metricSeriesDTO per distinct metric name. Each point is tagged with
// the run id it belongs to (resolved via exemplar trace_id when
// present, otherwise via a time-window fallback that picks the run
// whose start_time is the largest one ≤ the point's timestamp).
func buildMetricsResponse(points []query.MetricPoint, runs []query.Run) []metricSeriesDTO {
	if len(points) == 0 {
		return []metricSeriesDTO{}
	}
	type runIdx struct {
		runID string
		ts    time.Time
	}
	traceToRun := make(map[string]string, len(runs))
	tsRuns := make([]runIdx, 0, len(runs))
	for _, r := range runs {
		tsRuns = append(tsRuns, runIdx{runID: r.RunID, ts: r.Timestamp})
	}
	sort.Slice(tsRuns, func(i, j int) bool { return tsRuns[i].ts.Before(tsRuns[j].ts) })

	resolveRun := func(p query.MetricPoint) string {
		if p.TraceID != "" {
			if id, ok := traceToRun[p.TraceID]; ok {
				return id
			}
		}
		// Time-window fallback: return the latest run whose start_time
		// is ≤ the point's timestamp.
		var best string
		for _, r := range tsRuns {
			if r.ts.After(p.Timestamp) {
				break
			}
			best = r.runID
		}
		return best
	}

	bySeries := make(map[string]*metricSeriesDTO)
	order := make([]string, 0)
	for _, p := range points {
		s, ok := bySeries[p.Name]
		if !ok {
			s = &metricSeriesDTO{Name: p.Name, Unit: p.Unit, Resource: map[string]string{}}
			for k, v := range p.ResAttrs {
				if str, ok := v.(string); ok {
					s.Resource[k] = str
				}
			}
			bySeries[p.Name] = s
			order = append(order, p.Name)
		}
		s.Points = append(s.Points, metricPointDTO{
			RunID:     resolveRun(p),
			Timestamp: p.Timestamp.UTC().Format(time.RFC3339Nano),
			Value:     p.Value,
			TraceID:   p.TraceID,
		})
	}
	out := make([]metricSeriesDTO, 0, len(order))
	for _, name := range order {
		s := bySeries[name]
		sort.Slice(s.Points, func(i, j int) bool { return s.Points[i].Timestamp < s.Points[j].Timestamp })
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
