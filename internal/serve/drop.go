package serve

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/bjk95/defrost/internal/persist"
)

// dropPlanResponse mirrors persist.DropPlan as a wire-friendly DTO. All
// timestamps are RFC 3339 strings; bytes are int64 so the UI can render
// "8.4 KiB"-style sizes without losing precision on multi-GiB branches.
type dropPlanResponse struct {
	Branch        string `json:"branch"`
	OriginURL     string `json:"origin_url,omitempty"`
	Dev           bool   `json:"dev"`
	TraceFiles    int    `json:"trace_files"`
	MetricFiles   int    `json:"metric_files"`
	TraceBytes    int64  `json:"trace_bytes"`
	MetricBytes   int64  `json:"metric_bytes"`
	OldestRunUTC  string `json:"oldest_run_utc,omitempty"`
	NewestRunUTC  string `json:"newest_run_utc,omitempty"`
	SuppressionsN int    `json:"suppressions_n"`
	BranchMissing bool   `json:"branch_missing"`
	DropTraces    bool   `json:"drop_traces"`
	DropMetrics   bool   `json:"drop_metrics"`
	Nothing       bool   `json:"nothing"`
}

type dropRequest struct {
	DropTraces  bool `json:"drop_traces"`
	DropMetrics bool `json:"drop_metrics"`
}

func parseDropSelector(q url.Values) (persist.DropSelector, error) {
	traces := q.Get("drop_traces") != "false"
	metrics := q.Get("drop_metrics") != "false"
	if !traces && !metrics {
		return persist.DropSelector{}, errSelectorEmpty
	}
	return persist.DropSelector{DropTraces: traces, DropMetrics: metrics}, nil
}

var errSelectorEmpty = httpErr{status: http.StatusBadRequest, msg: "select at least one of drop_traces or drop_metrics"}

type httpErr struct {
	status int
	msg    string
}

func (e httpErr) Error() string { return e.msg }

func toDropPlanResponse(plan persist.DropPlan) dropPlanResponse {
	out := dropPlanResponse{
		Branch:        plan.Branch,
		OriginURL:     sanitizeOriginURLForResponse(plan.OriginURL),
		Dev:           plan.Dev,
		TraceFiles:    plan.TraceFiles,
		MetricFiles:   plan.MetricFiles,
		TraceBytes:    plan.TraceBytes,
		MetricBytes:   plan.MetricBytes,
		SuppressionsN: plan.SuppressionsN,
		BranchMissing: plan.BranchMissing,
		DropTraces:    plan.Sel.DropTraces,
		DropMetrics:   plan.Sel.DropMetrics,
		Nothing:       plan.Nothing(),
	}
	if !plan.OldestRunUTC.IsZero() {
		out.OldestRunUTC = plan.OldestRunUTC.UTC().Format(time.RFC3339)
	}
	if !plan.NewestRunUTC.IsZero() {
		out.NewestRunUTC = plan.NewestRunUTC.UTC().Format(time.RFC3339)
	}
	return out
}

// sanitizeOriginURLForResponse strips the userinfo segment so any
// embedded token in the remote URL doesn't leak into the JSON response.
// Matches the CLI prompt's sanitizer.
func sanitizeOriginURLForResponse(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func handleDropPlan(w http.ResponseWriter, r *http.Request, opts persist.Options) {
	sel, err := parseDropSelector(r.URL.Query())
	if err != nil {
		if e, ok := err.(httpErr); ok {
			writeJSONError(w, e.status, e.msg)
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	be := dropBackendFn(opts)
	var captured persist.DropPlan
	captured.Sel = sel
	confirm := func(plan persist.DropPlan) bool {
		captured = plan
		return false // never execute on plan-only path
	}
	if err := be.DropHistory(sel, confirm); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(toDropPlanResponse(captured))
}

func handleDrop(w http.ResponseWriter, r *http.Request, opts persist.Options) {
	var req dropRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if !req.DropTraces && !req.DropMetrics {
		writeJSONError(w, http.StatusBadRequest, "select at least one of drop_traces or drop_metrics")
		return
	}
	sel := persist.DropSelector{DropTraces: req.DropTraces, DropMetrics: req.DropMetrics}
	be := dropBackendFn(opts)
	var executed persist.DropPlan
	executed.Sel = sel
	confirm := func(plan persist.DropPlan) bool {
		executed = plan
		return !plan.Nothing() // skip work for nothing-to-drop branches
	}
	if err := be.DropHistory(sel, confirm); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(toDropPlanResponse(executed))
}
