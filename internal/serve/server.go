package serve

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// loaderFn is a package-level seam so tests stub the data load.
var loaderFn = Load

// metricsLoaderFn is the metrics-handler seam, parallel to loaderFn.
// Kept separate so /api/tests and /api/test/* never run metrics I/O.
var metricsLoaderFn = LoadMetricsView

// New returns the http.Handler for `defrost serve`. It does not retain
// any per-request state — each /api/* request loads the data branch via
// loaderFn(opts).
func New(opts persist.Options, assets fs.FS) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/tests", func(w http.ResponseWriter, r *http.Request) {
		ds, err := loaderFn(opts)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildGridResponse(ds))
	})

	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		mv, err := metricsLoaderFn(opts)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Metrics []metricSeriesDTO `json:"metrics"`
		}{Metrics: buildMetricsResponse(mv.Metrics, mv.Roots)})
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
		ds, err := loaderFn(opts)
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

// runIDOf reads the defrost.run_id from either the ResourceSpans's
// Resource (preferred for root spans) or the contained Span's Attributes
// (preferred for test spans).
func runIDOf(rs *tracepb.ResourceSpans) string {
	if rs == nil {
		return ""
	}
	if id := models.ResourceString(rs.Resource, "defrost.run_id"); id != "" {
		return id
	}
	span := persist.SpanFromResourceSpans(rs)
	if span == nil {
		return ""
	}
	return models.AttrString(span.Attributes, "defrost.run_id")
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
		RunID:       models.ResourceString(rs.Resource, "defrost.run_id"),
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
