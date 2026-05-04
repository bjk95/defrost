// Package serve hosts defrost's HTTP+SSE server. The dashboard is the
// only consumer; everything in this package is an implementation
// detail of "defrost serve".
//
// The seam between handlers and storage is `query.Querier`. Today only
// the embedded DuckDB impl exists; a hosted-defrost ClickHouse
// implementation will plug in here without changes.
package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/query"
)

// Deps is the dependency bundle for the dashboard handlers. The split
// between Querier (read) and persist.Options (suppress/drop, which need
// to write to the data branch) is the design's binary-split fault line.
type Deps struct {
	Querier query.Querier
	Persist persist.Options
	Assets  fs.FS
}

// suppressionsBackendFn is the seam for the suppressions endpoints.
// Tests override it to plug in an in-memory store; the default opens a
// fresh persist.Backend per call so suppress writes always hit the data
// branch.
var suppressionsBackendFn = func(opts persist.Options) suppressionsBackend {
	return persist.New(opts)
}

type suppressionsBackend interface {
	GetSuppressions() ([]string, error)
	UpdateSuppressions(mutate func([]string) []string, msg string) error
}

// dropBackendFn is the seam for the drop endpoints. Tests override it
// to plug in an in-memory implementation.
var dropBackendFn = func(opts persist.Options) dropBackend {
	return persist.New(opts)
}

type dropBackend interface {
	DropHistory(sel persist.DropSelector, confirm func(persist.DropPlan) bool) error
}

// New returns the http.Handler for `defrost serve`. The Querier is
// hydrated on demand from inside /api/tests; the boot screen subscribes
// to the same in-flight progress bus that hydration emits onto.
func New(deps Deps) http.Handler {
	mux := http.NewServeMux()
	bus := newProgressBus()

	// loadMu serializes /api/tests so concurrent callers don't
	// interleave events on the shared progress bus.
	var loadMu sync.Mutex

	mux.HandleFunc("/api/tests", func(w http.ResponseWriter, r *http.Request) {
		loadMu.Lock()
		bus.Reset()
		bus.Emit(ProgressEvent{Phase: "connect", Detail: "hydrating local cache"})
		err := deps.Querier.Hydrate()
		if err != nil {
			bus.Emit(ProgressEvent{Phase: "error", Detail: err.Error()})
			loadMu.Unlock()
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		bus.Emit(ProgressEvent{Phase: "ready", Detail: "dashboard online"})
		loadMu.Unlock()

		grid, err := deps.Querier.Grid(query.RunWindow{Limit: 0})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toGridResponse(grid))
	})

	mux.HandleFunc("/api/loading/progress", loadingProgressHandler(bus))

	mux.HandleFunc("/api/suppressions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetSuppressions(w, deps.Persist)
		case http.MethodPost:
			handleAddSuppression(w, r, deps.Persist)
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
		handleRemoveSuppression(w, r, deps.Persist)
	})

	mux.HandleFunc("/api/drop/plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleDropPlan(w, r, deps.Persist)
	})
	mux.HandleFunc("/api/drop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleDrop(w, r, deps.Persist)
	})

	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		if err := deps.Querier.Hydrate(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		grid, err := deps.Querier.Grid(query.RunWindow{Limit: 0})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		points, err := deps.Querier.MetricsAll()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Metrics []metricSeriesDTO `json:"metrics"`
		}{Metrics: buildMetricsResponse(points, grid.Runs)})
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
		if err := deps.Querier.Hydrate(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ql, ok := deps.Querier.(query.QuerierWithLookup)
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "lookup not supported by this querier")
			return
		}
		entry, run, found, err := ql.LookupEntry(tid, rid)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			writeJSONError(w, http.StatusNotFound, "unknown test or run")
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Test entryDTO     `json:"test"`
			Run  runDetailDTO `json:"run"`
		}{Test: toEntryDTO(entry), Run: toRunDetailDTO(run)})
	})

	mux.HandleFunc("/", spaHandler(deps.Assets))
	return mux
}

// --- DTOs (wire shapes the dashboard consumes) ---

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

func toGridResponse(g query.GridData) gridResponse {
	out := gridResponse{
		Runs:  make([]runDTO, 0, len(g.Runs)),
		Tests: make([]testDTO, 0, len(g.Tests)),
	}
	for _, r := range g.Runs {
		out.Runs = append(out.Runs, runDTO{
			RunID:       r.RunID,
			Timestamp:   timeRFC3339(r.Timestamp),
			Commit:      r.Commit,
			Parent:      r.Parent,
			Branch:      r.Branch,
			PR:          r.PR,
			AuthorEmail: r.AuthorEmail,
			AuthorName:  r.AuthorName,
			Cmd:         r.Cmd,
			OS:          r.OS,
			Arch:        r.Arch,
		})
	}
	for _, t := range g.Tests {
		row := testDTO{TestID: t.TestID, TestName: t.TestName, Cells: make([]cellDTO, 0, len(t.Cells))}
		for _, c := range t.Cells {
			row.Cells = append(row.Cells, cellDTO{RunID: c.RunID, Status: c.Status, DurationMs: c.DurationMs})
		}
		out.Tests = append(out.Tests, row)
	}
	return out
}

func toEntryDTO(e query.TestEntry) entryDTO {
	return entryDTO{
		TestID:     e.TestID,
		TestName:   e.TestName,
		RunID:      e.RunID,
		Timestamp:  timeRFC3339(e.Timestamp),
		Ran:        e.Ran,
		Passed:     e.Passed,
		Status:     e.Status,
		DurationMs: e.DurationMs,
		Output:     e.Output,
	}
}

func toRunDetailDTO(r query.Run) runDetailDTO {
	return runDetailDTO{
		RunID:       r.RunID,
		Commit:      r.Commit,
		Parent:      r.Parent,
		Branch:      r.Branch,
		PR:          r.PR,
		AuthorEmail: r.AuthorEmail,
		AuthorName:  r.AuthorName,
		Dirty:       r.Dirty,
		DirtyHash:   r.DirtyHash,
		Cmd:         r.Cmd,
		CmdHash:     r.CmdHash,
		GoVersion:   r.GoVersion,
		OS:          r.OS,
		Arch:        r.Arch,
		Timestamp:   timeRFC3339(r.Timestamp),
	}
}

func timeRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// --- suppressions ---

type suppressionsResponse struct {
	TestIDs []string `json:"test_ids"`
}

type addSuppressionRequest struct {
	TestID string `json:"test_id"`
}

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

// --- assets / SPA ---

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
	dist, err := fs.Sub(assets, "dist")
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
			// SPA fallback: any unmatched path returns index.html so React Router can resolve it.
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

// fmtSscanf is retained as a tiny no-import helper used by drop.go's
// time formatting. Kept for binary-size parity with the previous
// implementation.
var _ = fmt.Sprintf
