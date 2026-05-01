package serve

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// loaderFn is a package-level seam so tests stub the data load.
var loaderFn = LoadWithProgress

// suppressionsBackendFn is the seam for the suppressions endpoints. Tests
// override it to plug in an in-memory store; the default opens a fresh
// persist.Backend per call so suppress writes always hit the data branch.
var suppressionsBackendFn = func(opts persist.Options) suppressionsBackend {
	return persist.New(opts)
}

// suppressionsBackend is the subset of persist.Backend the HTTP handlers
// need. Narrowed so tests don't have to stub the full Backend interface.
type suppressionsBackend interface {
	GetSuppressions() ([]string, error)
	UpdateSuppressions(mutate func([]string) []string, msg string) error
}

// dropBackendFn is the seam for the drop endpoints. Tests override it to
// plug in an in-memory implementation; the default opens a fresh
// persist.Backend per call.
var dropBackendFn = func(opts persist.Options) dropBackend {
	return persist.New(opts)
}

type dropBackend interface {
	DropHistory(sel persist.DropSelector, confirm func(persist.DropPlan) bool) error
}

// New returns the http.Handler for `defrost serve`. It does not retain
// any per-request state — each /api/* request loads the data branch via
// loaderFn(opts). A single progress bus is shared across all requests
// so the boot-screen SSE feed can stream events from the in-flight load.
func New(opts persist.Options, assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	bus := newProgressBus()

	// loadMu serializes /api/tests so concurrent callers (multiple
	// tabs, parallel CI hits) don't interleave events on the shared
	// progress bus. The bus's history would otherwise be cleared
	// mid-stream when a second loader called Reset, leaving the first
	// SSE subscriber with an inconsistent log feed. Loads already
	// dominate wall time with a cold git clone, so queueing has
	// negligible additional cost.
	var loadMu sync.Mutex

	mux.HandleFunc("/api/tests", func(w http.ResponseWriter, r *http.Request) {
		loadMu.Lock()
		bus.Reset()
		ds, err := loaderFn(opts, bus.Emit)
		// Publish the error event before releasing the lock so a queued
		// loader can't call bus.Reset() between our Unlock and Emit and
		// inherit our error in its history.
		if err != nil {
			bus.Emit(ProgressEvent{Phase: "error", Detail: err.Error()})
		}
		loadMu.Unlock()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildGridResponse(ds))
	})

	mux.HandleFunc("/api/loading/progress", loadingProgressHandler(bus))

	mux.HandleFunc("/api/suppressions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetSuppressions(w, opts)
		case http.MethodPost:
			handleAddSuppression(w, r, opts)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/api/suppressions/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleRemoveSuppression(w, r, opts)
	})

	mux.HandleFunc("/api/drop/plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleDropPlan(w, r, opts)
	})
	mux.HandleFunc("/api/drop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleDrop(w, r, opts)
	})

	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		ds, err := loaderFn(opts, nil)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Metrics []metricSeriesDTO `json:"metrics"`
		}{Metrics: buildMetricsResponse(ds.Metrics, ds.Roots)})
	})

	mux.HandleFunc("/api/test/", func(w http.ResponseWriter, r *http.Request) {
		tidEsc, ridEsc, ok := parseTestRunPath(r.URL.EscapedPath())
		if !ok {
			http.NotFound(w, r)
			return
		}
		tid, err := url.PathUnescape(tidEsc)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		rid, err := url.PathUnescape(ridEsc)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ds, err := loaderFn(opts, nil)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		test, run, ok := lookupEntry(ds, tid, rid)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "unknown test or run")
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Test entryDTO     `json:"test"`
			Run  runDetailDTO `json:"run"`
		}{Test: test, Run: run})
	})

	mux.HandleFunc("/", spaHandler(assets))
	return mux
}

type suppressionsResponse struct {
	TestIDs []string `json:"test_ids"`
}

type addSuppressionRequest struct {
	TestID string `json:"test_id"`
}

// On-disk and CLI use raw test names (e.g. `pkg/Path¬arg="x"`); the
// HTTP wire format mirrors TestRow.test_id (persist.EncodeName-applied)
// so the UI can use a single canonical id across /api/tests, routing,
// and /api/suppressions. Translation lives at this HTTP boundary only.
func encodeSuppressionIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		out = append(out, persist.EncodeName(id))
	}
	return out
}

func handleGetSuppressions(w http.ResponseWriter, opts persist.Options) {
	be := suppressionsBackendFn(opts)
	ids, err := be.GetSuppressions()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(suppressionsResponse{TestIDs: encodeSuppressionIDs(ids)})
}

func handleAddSuppression(w http.ResponseWriter, r *http.Request, opts persist.Options) {
	var req addSuppressionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.TestID == "" {
		writeJSONError(w, http.StatusBadRequest, "test_id is required")
		return
	}
	rawID, err := persist.DecodeName(req.TestID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid test_id encoding: "+err.Error())
		return
	}
	be := suppressionsBackendFn(opts)
	mutate := func(cur []string) []string { return append(cur, rawID) }
	if err := be.UpdateSuppressions(mutate, "suppress: add "+rawID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids, err := be.GetSuppressions()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(suppressionsResponse{TestIDs: encodeSuppressionIDs(ids)})
}

func handleRemoveSuppression(w http.ResponseWriter, r *http.Request, opts persist.Options) {
	const prefix = "/api/suppressions/"
	rest := strings.TrimPrefix(r.URL.EscapedPath(), prefix)
	if rest == "" || strings.Contains(rest, "/") {
		http.NotFound(w, r)
		return
	}
	encodedTID, err := url.PathUnescape(rest)
	if err != nil || encodedTID == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid test id")
		return
	}
	rawTID, err := persist.DecodeName(encodedTID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid test_id encoding: "+err.Error())
		return
	}
	be := suppressionsBackendFn(opts)
	mutate := func(cur []string) []string {
		out := make([]string, 0, len(cur))
		for _, id := range cur {
			if id != rawTID {
				out = append(out, id)
			}
		}
		return out
	}
	if err := be.UpdateSuppressions(mutate, "suppress: remove "+rawTID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids, err := be.GetSuppressions()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(suppressionsResponse{TestIDs: encodeSuppressionIDs(ids)})
}

type runDTO struct {
	RunID       string   `json:"run_id"`
	Timestamp   string   `json:"ts"`
	Commit      string   `json:"commit,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Branch      string   `json:"branch,omitempty"`
	PR          int      `json:"pr,omitempty"`
	AuthorEmail string   `json:"author_email,omitempty"`
	AuthorName  string   `json:"author_name,omitempty"`
	Cmd         []string `json:"cmd,omitempty"`
	OS          string   `json:"os,omitempty"`
	Arch        string   `json:"arch,omitempty"`
}

type cellDTO struct {
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
}

type testDTO struct {
	TestID   string    `json:"test_id"`
	TestName string    `json:"test_name"`
	Cells    []cellDTO `json:"cells"`
}

type gridResponse struct {
	Runs  []runDTO  `json:"runs"`
	Tests []testDTO `json:"tests"`
}

// entryDTO mirrors the wire shape of the schema-2 persist.Entry struct
// the frontend was built against. Populated from a span at request time.
type entryDTO struct {
	TestID     string `json:"test_id"`
	TestName   string `json:"test_name"`
	RunID      string `json:"run_id"`
	Timestamp  string `json:"ts"`
	Ran        bool   `json:"ran"`
	Passed     bool   `json:"passed"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
}

// runDetailDTO mirrors the wire shape of the schema-2 persist.RunRecord
// struct the frontend was built against. Populated from a root run span
// at request time, pulling fields from Resource attributes.
type runDetailDTO struct {
	RunID       string   `json:"run_id"`
	Commit      string   `json:"commit,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Branch      string   `json:"branch,omitempty"`
	PR          int      `json:"pr,omitempty"`
	AuthorEmail string   `json:"author_email,omitempty"`
	AuthorName  string   `json:"author_name,omitempty"`
	Dirty       bool     `json:"dirty"`
	DirtyHash   string   `json:"dirty_hash,omitempty"`
	Cmd         []string `json:"cmd,omitempty"`
	CmdHash     string   `json:"cmd_hash,omitempty"`
	GoVersion   string   `json:"go_version,omitempty"`
	OS          string   `json:"os,omitempty"`
	Arch        string   `json:"arch,omitempty"`
	Timestamp   string   `json:"ts"`
}

func buildGridResponse(ds Dataset) gridResponse {
	out := gridResponse{
		Runs:  make([]runDTO, 0, len(ds.Roots)),
		Tests: make([]testDTO, 0, len(ds.TestSpans)),
	}
	for _, rs := range ds.Roots {
		span := persist.SpanFromResourceSpans(rs)
		if span == nil {
			continue
		}
		prStr := models.ResourceString(rs.Resource, "vcs.repository.change.id")
		pr := 0
		if prStr != "" {
			var n int
			_, _ = fmtSscanf(prStr, &n)
			pr = n
		}
		out.Runs = append(out.Runs, runDTO{
			RunID:       runIDOf(rs),
			Timestamp:   nanosToRFC3339(int64(span.StartTimeUnixNano)),
			Commit:      models.ResourceString(rs.Resource, "vcs.repository.ref.revision"),
			Parent:      models.ResourceString(rs.Resource, "defrost.parent_commit"),
			Branch:      models.ResourceString(rs.Resource, "vcs.repository.ref.name"),
			PR:          pr,
			AuthorEmail: models.ResourceString(rs.Resource, "defrost.author_email"),
			AuthorName:  models.ResourceString(rs.Resource, "defrost.author_name"),
			Cmd:         readStringArrayAttr(rs.Resource.GetAttributes(), "defrost.cmd"),
			OS:          models.ResourceString(rs.Resource, "host.os.type"),
			Arch:        models.ResourceString(rs.Resource, "host.arch"),
		})
	}
	for tid, traces := range ds.TestSpans {
		// Sort spans by StartTimeUnixNano ascending so cells flow
		// left→right as older→newer (matches the "← older · newer →"
		// header).
		sorted := append([]*tracepb.ResourceSpans(nil), traces...)
		sort.Slice(sorted, func(i, j int) bool {
			a := persist.SpanFromResourceSpans(sorted[i])
			b := persist.SpanFromResourceSpans(sorted[j])
			if a == nil || b == nil {
				return false
			}
			return a.StartTimeUnixNano < b.StartTimeUnixNano
		})
		first := persist.SpanFromResourceSpans(sorted[0])
		if first == nil {
			continue
		}
		t := testDTO{TestID: tid, TestName: first.Name}
		for _, rs := range sorted {
			s := persist.SpanFromResourceSpans(rs)
			if s == nil {
				continue
			}
			durMs := int64(0)
			if s.EndTimeUnixNano > s.StartTimeUnixNano {
				durMs = int64((s.EndTimeUnixNano - s.StartTimeUnixNano) / 1_000_000)
			}
			t.Cells = append(t.Cells, cellDTO{
				RunID:      runIDOf(rs),
				Status:     spanResultStatus(s),
				DurationMs: durMs,
			})
		}
		out.Tests = append(out.Tests, t)
	}
	return out
}

func lookupEntry(ds Dataset, tid, rid string) (entryDTO, runDetailDTO, bool) {
	traces, ok := ds.TestSpans[tid]
	if !ok {
		return entryDTO{}, runDetailDTO{}, false
	}
	var hit *tracepb.ResourceSpans
	for _, rs := range traces {
		if runIDOf(rs) == rid {
			hit = rs
			break
		}
	}
	if hit == nil {
		return entryDTO{}, runDetailDTO{}, false
	}
	for _, root := range ds.Roots {
		if runIDOf(root) == rid {
			return spanToEntryDTO(hit), rootSpanToRunDTO(root), true
		}
	}
	return entryDTO{}, runDetailDTO{}, false
}

// runIDOf reads the run id from a ResourceSpans's Resource, using OTel's
// `cicd.pipeline.run.id` semconv key. Schema 4 stores this only on the
// Resource — never on individual spans — so there is no span-attribute
// fallback.
func runIDOf(rs *tracepb.ResourceSpans) string {
	if rs == nil {
		return ""
	}
	return models.ResourceString(rs.Resource, "cicd.pipeline.run.id")
}

func spanToEntryDTO(rs *tracepb.ResourceSpans) entryDTO {
	s := persist.SpanFromResourceSpans(rs)
	if s == nil {
		return entryDTO{}
	}
	resultStatus := models.AttrString(s.Attributes, "test.case.result.status")
	output := ""
	for _, ev := range s.Events {
		if ev.Name == "test.output" {
			output = models.AttrString(ev.Attributes, "body")
			if output != "" {
				break
			}
		}
	}
	durMs := int64(0)
	if s.EndTimeUnixNano > s.StartTimeUnixNano {
		durMs = int64((s.EndTimeUnixNano - s.StartTimeUnixNano) / 1_000_000)
	}
	return entryDTO{
		TestID:     persist.EncodeName(s.Name),
		TestName:   s.Name,
		RunID:      runIDOf(rs),
		Timestamp:  nanosToRFC3339(int64(s.StartTimeUnixNano)),
		Ran:        resultStatus != "skipped",
		Passed:     resultStatus == "passed",
		Status:     compactStatusFromSemconv(resultStatus),
		DurationMs: durMs,
		Output:     output,
	}
}

func rootSpanToRunDTO(rs *tracepb.ResourceSpans) runDetailDTO {
	span := persist.SpanFromResourceSpans(rs)
	if span == nil {
		return runDetailDTO{}
	}
	prStr := models.ResourceString(rs.Resource, "vcs.repository.change.id")
	pr := 0
	if prStr != "" {
		var n int
		_, _ = fmtSscanf(prStr, &n)
		pr = n
	}
	cmd := readStringArrayAttr(rs.Resource.GetAttributes(), "defrost.cmd")
	dirty := false
	for _, kv := range rs.Resource.GetAttributes() {
		if kv.Key == "defrost.dirty" {
			if v, ok := kv.Value.GetValue().(*commonpb.AnyValue_BoolValue); ok {
				dirty = v.BoolValue
			}
			break
		}
	}
	return runDetailDTO{
		RunID:       models.ResourceString(rs.Resource, "cicd.pipeline.run.id"),
		Commit:      models.ResourceString(rs.Resource, "vcs.repository.ref.revision"),
		Parent:      models.ResourceString(rs.Resource, "defrost.parent_commit"),
		Branch:      models.ResourceString(rs.Resource, "vcs.repository.ref.name"),
		PR:          pr,
		AuthorEmail: models.ResourceString(rs.Resource, "defrost.author_email"),
		AuthorName:  models.ResourceString(rs.Resource, "defrost.author_name"),
		Dirty:       dirty,
		DirtyHash:   models.ResourceString(rs.Resource, "defrost.dirty_hash"),
		Cmd:         cmd,
		CmdHash:     models.ResourceString(rs.Resource, "defrost.cmd_hash"),
		GoVersion:   models.ResourceString(rs.Resource, "process.runtime.version"),
		OS:          models.ResourceString(rs.Resource, "host.os.type"),
		Arch:        models.ResourceString(rs.Resource, "host.arch"),
		Timestamp:   nanosToRFC3339(int64(span.StartTimeUnixNano)),
	}
}

func readStringArrayAttr(attrs []*commonpb.KeyValue, key string) []string {
	for _, kv := range attrs {
		if kv.Key != key {
			continue
		}
		arr, ok := kv.Value.GetValue().(*commonpb.AnyValue_ArrayValue)
		if !ok {
			return nil
		}
		out := make([]string, 0, len(arr.ArrayValue.Values))
		for _, v := range arr.ArrayValue.Values {
			if s, ok := v.GetValue().(*commonpb.AnyValue_StringValue); ok {
				out = append(out, s.StringValue)
			}
		}
		return out
	}
	return nil
}

// nanosToRFC3339 formats a Unix-nano timestamp as RFC 3339, the same wire
// shape the schema-2 frontend was built against.
func nanosToRFC3339(ns int64) string {
	if ns == 0 {
		return ""
	}
	return time.Unix(0, ns).UTC().Format(time.RFC3339Nano)
}

// spanResultStatus reads the OTel `test.case.result.status` attribute
// (passed/failed/skipped/aborted) from a span and converts to the
// frontend's compact status string.
func spanResultStatus(s *tracepb.Span) string {
	if s == nil {
		return ""
	}
	return compactStatusFromSemconv(models.AttrString(s.Attributes, "test.case.result.status"))
}

func compactStatusFromSemconv(v string) string {
	switch v {
	case "passed":
		return "pass"
	case "failed":
		return "fail"
	case "skipped":
		return "skip"
	case "aborted":
		return "panic"
	}
	return v
}

// fmtSscanf is a tiny helper for parsing one decimal int from a string
// without pulling strconv in.
func fmtSscanf(s string, n *int) (int, error) {
	parsed := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return i, nil
		}
		parsed = parsed*10 + int(c-'0')
	}
	*n = parsed
	return len(s), nil
}

// parseTestRunPath parses /api/test/{tid}/run/{rid}. Returns ok=false
// for any other shape.
func parseTestRunPath(p string) (tid, rid string, ok bool) {
	const prefix = "/api/test/"
	if !strings.HasPrefix(p, prefix) {
		return "", "", false
	}
	rest := p[len(prefix):]
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[1] != "run" || parts[0] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

func spaHandler(assets fs.FS) http.HandlerFunc {
	dist, err := fs.Sub(assets, "web/dist")
	useFS := err == nil
	return func(w http.ResponseWriter, r *http.Request) {
		if useFS {
			cleaned := strings.TrimPrefix(r.URL.Path, "/")
			if cleaned == "" {
				cleaned = "index.html"
			}
			if f, err := dist.Open(cleaned); err == nil {
				defer f.Close()
				if stat, err := f.Stat(); err == nil && !stat.IsDir() {
					http.ServeContent(w, r, cleaned, stat.ModTime(), f.(io.ReadSeeker))
					return
				}
			}
			// SPA fallback: any unmatched path returns index.html so the
			// browser can let React Router resolve it.
			if f, err := dist.Open("index.html"); err == nil {
				defer f.Close()
				if stat, err := f.Stat(); err == nil {
					http.ServeContent(w, r, "index.html", stat.ModTime(), f.(io.ReadSeeker))
					return
				}
			}
		}
		http.NotFound(w, r)
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
