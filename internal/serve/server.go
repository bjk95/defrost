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

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/persist"
)

// loaderFn is a package-level seam so tests stub the data load.
var loaderFn = Load

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
			Test entryDTO `json:"test"`
			Run  runDetailDTO `json:"run"`
		}{Test: test, Run: run})
	})

	mux.HandleFunc("/", spaHandler(assets))
	return mux
}

type runDTO struct {
	RunID     string `json:"run_id"`
	Timestamp string `json:"ts"`
	Commit    string `json:"commit,omitempty"`
	Branch    string `json:"branch,omitempty"`
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
	for _, r := range ds.Roots {
		out.Runs = append(out.Runs, runDTO{
			RunID:     resourceString(r.Resource, "defrost.run_id"),
			Timestamp: nanosToRFC3339(r.StartTimeUnixNano),
			Commit:    resourceString(r.Resource, "vcs.repository.ref.revision"),
			Branch:    resourceString(r.Resource, "vcs.repository.ref.name"),
		})
	}
	for tid, spans := range ds.TestSpans {
		// Sort spans by StartTimeUnixNano ascending so cells flow
		// left→right as older→newer (matches the "← older · newer →"
		// header).
		sorted := append([]models.Span(nil), spans...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].StartTimeUnixNano < sorted[j].StartTimeUnixNano
		})
		t := testDTO{TestID: tid, TestName: sorted[0].Name}
		for _, s := range sorted {
			rid, _ := s.Attributes["defrost.run_id"].(string)
			durMs := (s.EndTimeUnixNano - s.StartTimeUnixNano) / 1_000_000
			t.Cells = append(t.Cells, cellDTO{
				RunID:      rid,
				Status:     spanResultStatus(s),
				DurationMs: durMs,
			})
		}
		out.Tests = append(out.Tests, t)
	}
	return out
}

func lookupEntry(ds Dataset, tid, rid string) (entryDTO, runDetailDTO, bool) {
	spans, ok := ds.TestSpans[tid]
	if !ok {
		return entryDTO{}, runDetailDTO{}, false
	}
	var hit models.Span
	found := false
	for _, s := range spans {
		if got, _ := s.Attributes["defrost.run_id"].(string); got == rid {
			hit = s
			found = true
			break
		}
	}
	if !found {
		return entryDTO{}, runDetailDTO{}, false
	}
	for _, r := range ds.Roots {
		if got, _ := r.Resource["defrost.run_id"].(string); got == rid {
			return spanToEntryDTO(hit), rootSpanToRunDTO(r), true
		}
	}
	return entryDTO{}, runDetailDTO{}, false
}

func spanToEntryDTO(s models.Span) entryDTO {
	rid, _ := s.Attributes["defrost.run_id"].(string)
	resultStatus, _ := s.Attributes["test.case.result.status"].(string)
	output := ""
	for _, ev := range s.Events {
		if ev.Name == "test.output" {
			if body, ok := ev.Attributes["body"].(string); ok {
				output = body
				break
			}
		}
	}
	durMs := (s.EndTimeUnixNano - s.StartTimeUnixNano) / 1_000_000
	return entryDTO{
		TestID:     persist.EncodeName(s.Name),
		TestName:   s.Name,
		RunID:      rid,
		Timestamp:  nanosToRFC3339(s.StartTimeUnixNano),
		Ran:        resultStatus != "skipped",
		Passed:     resultStatus == "passed",
		Status:     compactStatusFromSemconv(resultStatus),
		DurationMs: durMs,
		Output:     output,
	}
}

func rootSpanToRunDTO(r models.Span) runDetailDTO {
	prStr := resourceString(r.Resource, "vcs.repository.change.id")
	pr := 0
	if prStr != "" {
		// Best-effort parse; runDetailDTO field is int.
		var n int
		_, _ = fmtSscanf(prStr, &n)
		pr = n
	}
	cmd, _ := r.Resource["defrost.cmd"].([]string)
	if cmd == nil {
		// JSON-decoded slices come back as []any; widen.
		if anySlice, ok := r.Resource["defrost.cmd"].([]any); ok {
			for _, v := range anySlice {
				if s, ok := v.(string); ok {
					cmd = append(cmd, s)
				}
			}
		}
	}
	dirty, _ := r.Resource["defrost.dirty"].(bool)
	return runDetailDTO{
		RunID:       resourceString(r.Resource, "defrost.run_id"),
		Commit:      resourceString(r.Resource, "vcs.repository.ref.revision"),
		Parent:      resourceString(r.Resource, "defrost.parent_commit"),
		Branch:      resourceString(r.Resource, "vcs.repository.ref.name"),
		PR:          pr,
		AuthorEmail: resourceString(r.Resource, "defrost.author_email"),
		AuthorName:  resourceString(r.Resource, "defrost.author_name"),
		Dirty:       dirty,
		DirtyHash:   resourceString(r.Resource, "defrost.dirty_hash"),
		Cmd:         cmd,
		CmdHash:     resourceString(r.Resource, "defrost.cmd_hash"),
		GoVersion:   resourceString(r.Resource, "process.runtime.version"),
		OS:          resourceString(r.Resource, "host.os.type"),
		Arch:        resourceString(r.Resource, "host.arch"),
		Timestamp:   nanosToRFC3339(r.StartTimeUnixNano),
	}
}

// resourceString reads a Resource attribute as a string, returning ""
// when the key is missing or the value is not a string. JSON-decoded maps
// hand back interface{} which is type-asserted here.
func resourceString(res map[string]any, key string) string {
	v, _ := res[key].(string)
	return v
}

// nanosToRFC3339 formats a Unix-nano timestamp as RFC 3339, the same wire
// shape used by the schema-2 RunRecord.Timestamp/Entry.Timestamp fields.
func nanosToRFC3339(ns int64) string {
	if ns == 0 {
		return ""
	}
	return time.Unix(0, ns).UTC().Format(time.RFC3339Nano)
}

// spanResultStatus converts the OTel `test.case.result.status` attribute
// (passed/failed/skipped/aborted) into the compact status string the
// schema-2 frontend grid expects: "pass" / "fail" / "skip" / "panic".
func spanResultStatus(s models.Span) string {
	v, _ := s.Attributes["test.case.result.status"].(string)
	return compactStatusFromSemconv(v)
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

// fmtSscanf is a tiny wrapper to avoid importing fmt twice in this file.
// Returns (read, err) but we use it for a single decimal int parse only.
func fmtSscanf(s string, n *int) (int, error) {
	// Inline strconv for clarity; importing strconv adds a dep we already
	// touch elsewhere indirectly.
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
