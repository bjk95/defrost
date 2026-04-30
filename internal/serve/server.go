package serve

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strings"

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
		entry, run, ok := lookupEntry(ds, tid, rid)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "unknown test or run")
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Test persist.Entry     `json:"test"`
			Run  persist.RunRecord `json:"run"`
		}{Test: entry, Run: run})
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

func buildGridResponse(ds Dataset) gridResponse {
	out := gridResponse{
		Runs:  make([]runDTO, 0, len(ds.Runs)),
		Tests: make([]testDTO, 0, len(ds.TestEntries)),
	}
	for _, r := range ds.Runs {
		out.Runs = append(out.Runs, runDTO{
			RunID: r.RunID, Timestamp: r.Timestamp, Commit: r.Commit, Branch: r.Branch,
		})
	}
	for tid, entries := range ds.TestEntries {
		// Sort entries by Timestamp ascending so cells flow left→right as
		// older→newer (matches the "← older · newer →" header).
		sortedEntries := append([]persist.Entry(nil), entries...)
		sort.Slice(sortedEntries, func(i, j int) bool {
			return sortedEntries[i].Timestamp < sortedEntries[j].Timestamp
		})
		t := testDTO{TestID: tid, TestName: sortedEntries[0].TestName}
		for _, e := range sortedEntries {
			t.Cells = append(t.Cells, cellDTO{RunID: e.RunID, Status: e.Status, DurationMs: e.DurationMs})
		}
		out.Tests = append(out.Tests, t)
	}
	return out
}

func lookupEntry(ds Dataset, tid, rid string) (persist.Entry, persist.RunRecord, bool) {
	entries, ok := ds.TestEntries[tid]
	if !ok {
		return persist.Entry{}, persist.RunRecord{}, false
	}
	var entry persist.Entry
	found := false
	for _, e := range entries {
		if e.RunID == rid {
			entry = e
			found = true
			break
		}
	}
	if !found {
		return persist.Entry{}, persist.RunRecord{}, false
	}
	for _, r := range ds.Runs {
		if r.RunID == rid {
			return entry, r, true
		}
	}
	return persist.Entry{}, persist.RunRecord{}, false
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
