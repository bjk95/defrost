# `defrost serve` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `defrost serve` subcommand that exposes a local web UI for inspecting the test history persisted by `defrost exec`. v1 is a heatmap timeline grid (rows = tests, columns = recent runs) with a click-to-inspect side panel.

**Architecture:** A Go HTTP server reads the `_defrost` data branch via the existing `persist` package on each API request and serves an embedded React SPA. Browser-tab reload is the v1 refresh mechanism, throttled by HTTP `Cache-Control` headers. SPA built once with Vite into `web/dist/`, committed, and embedded into the binary via `go:embed`.

**Tech Stack:** Go 1.24 + kong CLI + `go:embed`. Web: Vite + React 18 + TypeScript + React Router v6 + Tailwind + shadcn/ui + recharts (via shadcn `<Chart>`) + TanStack Query v5 + Vitest + @testing-library/react + jsdom.

**Reference spec:** [`docs/superpowers/specs/2026-04-30-defrost-serve-design.md`](../specs/2026-04-30-defrost-serve-design.md).

---

## File Structure

**Go (new files unless noted):**

- `internal/persist/persist.go` — modify: add `LoadAll`.
- `internal/persist/persist_test.go` — modify: add `LoadAll` test.
- `internal/serve/data.go` — `Dataset`, `Load`.
- `internal/serve/data_test.go` — `Load` shape tests.
- `internal/serve/server.go` — `New(opts, assets)` HTTP handler with `loaderFn` seam.
- `internal/serve/server_test.go` — handler tests.
- `assets.go` — top-level `//go:embed all:web/dist` declaration.
- `serve.go` — top-level `HandleServe`.
- `cli.go` — modify: add `Serve` subcommand.
- `main.go` — modify: dispatch to `HandleServe`.

**Web (all new):**

- `web/package.json`, `web/vite.config.ts`, `web/tailwind.config.ts`, `web/postcss.config.js`, `web/tsconfig.json`, `web/tsconfig.node.json`, `web/index.html`, `web/components.json`, `web/vitest.setup.ts`.
- `web/src/main.tsx`, `web/src/App.tsx`, `web/src/index.css`.
- `web/src/queryClient.ts`, `web/src/test-utils.tsx`.
- `web/src/lib/utils.ts` (shadcn `cn` helper).
- `web/src/api.ts`, `web/src/api.test.ts`, `web/src/types.ts`.
- `web/src/components/StatusBadge.tsx`, `RunCell.tsx`, `RunCell.test.tsx`, `TestRow.tsx`, `Grid.tsx`, `Grid.test.tsx`, `DurationSparkline.tsx`, `DurationSparkline.test.tsx`, `RunDetailSheet.tsx`, `RunDetailSheet.test.tsx`.
- `web/src/components/ui/*.tsx` (shadcn-generated: button, badge, sheet, tooltip, sonner, chart).
- `web/dist/**` (committed after the first production build).

**Build / CI:**

- `Makefile` — `build`, `test` targets.
- `.gitignore` — add `web/node_modules/` and `.superpowers/`.
- `.github/workflows/web-dist.yml` — auto-rebuild dist.

---

## Task 1: `persist.LoadAll`

**Files:**

- Modify: `internal/persist/persist.go`
- Modify: `internal/persist/persist_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/persist/persist_test.go`:

```go
func TestLoadAll_ReturnsAllRunsAndEntriesGroupedByTest(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	resultsRun1 := []models.TestResult{
		{Id: "github.com/x/p/TestA", Ran: true, Passed: true, Duration: 5 * time.Millisecond, StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Id: "github.com/x/p/TestB", Ran: true, Passed: false, Duration: 9 * time.Millisecond, StartTime: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), Output: "fail one"},
	}
	run1 := newTestRun("run-1", "1111111111111111", "main")
	if err := Persist(Options{RepoDir: repoDir}, run1, resultsRun1); err != nil {
		t.Fatalf("persist run1: %v", err)
	}

	resultsRun2 := []models.TestResult{
		{Id: "github.com/x/p/TestA", Ran: true, Passed: true, Duration: 4 * time.Millisecond, StartTime: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	run2 := newTestRun("run-2", "2222222222222222", "main")
	if err := Persist(Options{RepoDir: repoDir}, run2, resultsRun2); err != nil {
		t.Fatalf("persist run2: %v", err)
	}

	runs, byTest, err := LoadAll(Options{RepoDir: repoDir})
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	gotRunIDs := map[string]bool{}
	for _, r := range runs {
		gotRunIDs[r.RunID] = true
	}
	if !gotRunIDs["run-1"] || !gotRunIDs["run-2"] {
		t.Errorf("missing run IDs in %v", gotRunIDs)
	}

	idA := EncodeTestID("github.com/x/p/TestA")
	idB := EncodeTestID("github.com/x/p/TestB")
	if len(byTest[idA]) != 2 {
		t.Errorf("TestA: want 2 entries, got %d", len(byTest[idA]))
	}
	if len(byTest[idB]) != 1 {
		t.Errorf("TestB: want 1 entry, got %d", len(byTest[idB]))
	}
}

func TestLoadAll_NoBranch_ReturnsEmpty(t *testing.T) {
	requireGit(t)
	repoDir, _ := makeFixture(t)

	runs, byTest, err := LoadAll(Options{RepoDir: repoDir})
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(runs))
	}
	if len(byTest) != 0 {
		t.Errorf("expected 0 test groups, got %d", len(byTest))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/persist/ -run TestLoadAll -v`

Expected: FAIL with `undefined: LoadAll`.

- [ ] **Step 3: Implement `LoadAll`**

Append to `internal/persist/persist.go` (after the `History` function):

```go
// LoadAll returns every persisted RunRecord and every Entry across all
// tests on the data branch. Entries are grouped by encoded test ID
// (the on-disk filename). Used by `defrost serve`. Returns empty (nil)
// slices/maps if the data branch does not yet exist.
func LoadAll(opts Options) ([]RunRecord, map[string][]Entry, error) {
	branch := opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}

	remoteURL, err := resolveTargetURL(opts)
	if err != nil {
		return nil, nil, err
	}

	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, nil
	}

	workDir, err := os.MkdirTemp("", "defrost-read-")
	if err != nil {
		return nil, nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)
	_ = os.Remove(workDir)

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, nil, fmt.Errorf("clone data branch: %w", err)
	}

	runs, err := readAllRunRecords(workDir)
	if err != nil {
		return nil, nil, err
	}
	byTest, err := readAllEntries(workDir)
	if err != nil {
		return nil, nil, err
	}
	return runs, byTest, nil
}

func readAllRunRecords(workDir string) ([]RunRecord, error) {
	dir := filepath.Join(workDir, "runs")
	files, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]RunRecord, 0, len(files))
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		runID := strings.TrimSuffix(f.Name(), ".json")
		rec, err := readRunRecord(workDir, runID)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func readAllEntries(workDir string) (map[string][]Entry, error) {
	dir := filepath.Join(workDir, "tests")
	files, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string][]Entry{}, nil
		}
		return nil, err
	}
	out := map[string][]Entry{}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".ndjson") {
			continue
		}
		path := filepath.Join(dir, f.Name())
		rf, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		entries, err := parseNDJSON(rf)
		rf.Close()
		if err != nil {
			return nil, err
		}
		testID := strings.TrimSuffix(f.Name(), ".ndjson")
		out[testID] = entries
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/persist/ -run TestLoadAll -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/persist/persist.go internal/persist/persist_test.go
git commit -m "feat(persist): add LoadAll for serve"
```

---

## Task 2: `internal/serve/data.go`

**Files:**

- Create: `internal/serve/data.go`
- Create: `internal/serve/data_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/serve/data_test.go`:

```go
package serve

import (
	"testing"

	"github.com/bjk95/defrost/internal/persist"
)

func TestLoad_SortsRunsNewestFirstAndCapsAtFifty(t *testing.T) {
	runs := []persist.RunRecord{}
	for i := 0; i < 60; i++ {
		runs = append(runs, persist.RunRecord{
			RunID:     idFor(i),
			Timestamp: timestampFor(i),
		})
	}
	byTest := map[string][]persist.Entry{
		"tid-A": {
			{TestID: "tid-A", TestName: "pkg.TestA", RunID: idFor(0), Status: "pass"},
			{TestID: "tid-A", TestName: "pkg.TestA", RunID: idFor(59), Status: "fail"},
		},
	}

	prevLoadAll := persistLoadAll
	persistLoadAll = func(_ persist.Options) ([]persist.RunRecord, map[string][]persist.Entry, error) {
		return runs, byTest, nil
	}
	defer func() { persistLoadAll = prevLoadAll }()

	ds, err := Load(persist.Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ds.Runs) != 50 {
		t.Errorf("want 50 runs after cap, got %d", len(ds.Runs))
	}
	if ds.Runs[0].RunID != idFor(59) {
		t.Errorf("want newest run first, got %q", ds.Runs[0].RunID)
	}
	// Entry whose RunID is no longer in the capped set must be dropped.
	if len(ds.TestEntries["tid-A"]) != 1 {
		t.Errorf("want 1 entry for tid-A after run-cap filter, got %d", len(ds.TestEntries["tid-A"]))
	}
	if ds.TestEntries["tid-A"][0].RunID != idFor(59) {
		t.Errorf("want surviving entry to reference run %q, got %q", idFor(59), ds.TestEntries["tid-A"][0].RunID)
	}
}

func idFor(i int) string {
	return "run-" + leftPad(i)
}

func timestampFor(i int) string {
	// Lex-sortable timestamps. RFC3339 ascending with i.
	return "2026-01-01T00:00:" + leftPad(i) + "Z"
}

func leftPad(i int) string {
	if i < 10 {
		return "0" + itoa(i)
	}
	return itoa(i)
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(digits[i%10]) + out
		i /= 10
	}
	return out
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/serve/ -v`

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement `data.go`**

Create `internal/serve/data.go`:

```go
package serve

import (
	"sort"

	"github.com/bjk95/defrost/internal/persist"
)

// MaxRuns is the cap on grid columns. Runs older than the latest 50 are
// dropped from the in-memory Dataset.
const MaxRuns = 50

// Dataset is a request-scoped view of the data branch sized for the grid
// UI. Runs is the column axis (newest first); TestEntries are filtered to
// only those whose RunID still appears in Runs after capping.
type Dataset struct {
	Runs        []persist.RunRecord
	TestEntries map[string][]persist.Entry
}

// persistLoadAll is a package-level seam so tests can stub the data
// source without going through git.
var persistLoadAll = persist.LoadAll

// Load reads the data branch and returns a sorted, capped Dataset suitable
// for serving over HTTP.
func Load(opts persist.Options) (Dataset, error) {
	runs, byTest, err := persistLoadAll(opts)
	if err != nil {
		return Dataset{}, err
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].Timestamp > runs[j].Timestamp
	})
	if len(runs) > MaxRuns {
		runs = runs[:MaxRuns]
	}

	keep := make(map[string]struct{}, len(runs))
	for _, r := range runs {
		keep[r.RunID] = struct{}{}
	}

	filtered := make(map[string][]persist.Entry, len(byTest))
	for tid, entries := range byTest {
		var kept []persist.Entry
		for _, e := range entries {
			if _, ok := keep[e.RunID]; ok {
				kept = append(kept, e)
			}
		}
		if len(kept) > 0 {
			filtered[tid] = kept
		}
	}

	return Dataset{Runs: runs, TestEntries: filtered}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/serve/ -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/serve/data.go internal/serve/data_test.go
git commit -m "feat(serve): add Dataset + Load with run cap + entry filter"
```

---

## Task 3: `internal/serve/server.go` (API handlers)

**Files:**

- Create: `internal/serve/server.go`
- Create: `internal/serve/server_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/serve/server_test.go`:

```go
package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/bjk95/defrost/internal/persist"
)

func stubDataset() Dataset {
	return Dataset{
		Runs: []persist.RunRecord{
			{RunID: "run-2", Timestamp: "2026-01-02T00:00:00Z", Commit: "deadbee", Branch: "main"},
			{RunID: "run-1", Timestamp: "2026-01-01T00:00:00Z", Commit: "cafebab", Branch: "main"},
		},
		TestEntries: map[string][]persist.Entry{
			"tid-A": {
				{TestID: "tid-A", TestName: "pkg.TestA", RunID: "run-1", Status: "pass", DurationMs: 5},
				{TestID: "tid-A", TestName: "pkg.TestA", RunID: "run-2", Status: "fail", DurationMs: 9, Output: "BOOM"},
			},
		},
	}
}

func newTestServer(t *testing.T, ds Dataset, assets fstest.MapFS) *httptest.Server {
	t.Helper()
	prev := loaderFn
	loaderFn = func(_ persist.Options) (Dataset, error) { return ds, nil }
	t.Cleanup(func() { loaderFn = prev })

	h := New(persist.Options{}, assets)
	return httptest.NewServer(h)
}

func TestServer_GetTests_ShapeAndCacheControl(t *testing.T) {
	srv := newTestServer(t, stubDataset(), fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tests")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control: want %q, got %q", "public, max-age=60", got)
	}

	var body struct {
		Runs  []map[string]any `json:"runs"`
		Tests []struct {
			TestID   string           `json:"test_id"`
			TestName string           `json:"test_name"`
			Cells    []map[string]any `json:"cells"`
		} `json:"tests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Runs) != 2 {
		t.Errorf("want 2 runs, got %d", len(body.Runs))
	}
	if len(body.Tests) != 1 || body.Tests[0].TestName != "pkg.TestA" {
		t.Errorf("unexpected tests: %+v", body.Tests)
	}
	if len(body.Tests[0].Cells) != 2 {
		t.Errorf("want 2 cells, got %d", len(body.Tests[0].Cells))
	}
	if _, hasOutput := body.Tests[0].Cells[0]["output"]; hasOutput {
		t.Errorf("Output must be omitted from /api/tests cells")
	}
}

func TestServer_GetTestRun_HappyPath(t *testing.T) {
	srv := newTestServer(t, stubDataset(), fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/test/tid-A/run/run-2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=86400" {
		t.Errorf("Cache-Control: want %q, got %q", "public, max-age=86400", got)
	}

	var body struct {
		Test persist.Entry     `json:"test"`
		Run  persist.RunRecord `json:"run"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Test.Output != "BOOM" {
		t.Errorf("want full Output, got %q", body.Test.Output)
	}
	if body.Run.RunID != "run-2" {
		t.Errorf("want run-2, got %q", body.Run.RunID)
	}
}

func TestServer_GetTestRun_404OnUnknown(t *testing.T) {
	srv := newTestServer(t, stubDataset(), fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/test/unknown/run/run-2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestServer_SPAFallback(t *testing.T) {
	srv := newTestServer(t, stubDataset(), fstest.MapFS{
		"web/dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html>spa</html>")},
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/some/spa/path")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != "<!doctype html><html>spa</html>" {
		t.Errorf("fallback body wrong: %q", buf[:n])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/serve/ -v`

Expected: FAIL — `New`, `loaderFn` not defined.

- [ ] **Step 3: Implement `server.go`**

Create `internal/serve/server.go`:

```go
package serve

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
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
		tid, rid, ok := parseTestRunPath(r.URL.Path)
		if !ok {
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
		t := testDTO{TestID: tid, TestName: entries[0].TestName}
		for _, e := range entries {
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/serve/ -v`

Expected: PASS for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/serve/server.go internal/serve/server_test.go
git commit -m "feat(serve): add HTTP handlers + SPA fallback"
```

---

## Task 4: Top-level wiring (`assets.go`, `serve.go`, `cli.go`, `main.go`)

The Go binary needs an `embed.FS` for the SPA, a `HandleServe` entrypoint, and a kong subcommand.

**Files:**

- Create: `assets.go`
- Create: `serve.go`
- Modify: `cli.go`
- Modify: `main.go`
- Create: `web/dist/.gitkeep` (placeholder so `go:embed all:web/dist` resolves before the first SPA build)

- [ ] **Step 1: Create the embed FS placeholder**

Create `web/dist/.gitkeep` (empty file). Then `web/dist/index.html` (placeholder so the embed has at least one file once we run a real build):

```bash
mkdir -p web/dist
touch web/dist/.gitkeep
cat > web/dist/index.html <<'EOF'
<!doctype html>
<html><body>defrost serve placeholder — run `make build` to populate.</body></html>
EOF
```

- [ ] **Step 2: Create `assets.go`**

Create `assets.go`:

```go
package main

import "embed"

// Assets is the SPA, embedded at build time. The directory is committed
// (rebuilt by CI on web/ changes) so `go install` works without Node.
//
//go:embed all:web/dist
var Assets embed.FS
```

- [ ] **Step 3: Create `serve.go`**

Create `serve.go`:

```go
package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/serve"
)

type ServeOpts struct {
	Port       int
	RepoDir    string
	DataBranch string
	NoRemote   bool
}

func HandleServe(opts ServeOpts) int {
	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
	}

	addr := "127.0.0.1:" + strconv.Itoa(opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			fmt.Fprintf(os.Stderr, "port %d already in use; pass --port to override\n", opts.Port)
			return 1
		}
		fmt.Fprintln(os.Stderr, "serve:", err)
		return 1
	}

	handler := serve.New(pOpts, Assets)
	srv := &http.Server{Handler: handler}

	fmt.Printf("→ http://localhost:%d\n", opts.Port)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "serve:", err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Add the `Serve` subcommand to `cli.go`**

In `cli.go`, append a `Serve` field to the `CLI` struct:

```go
Serve struct {
	Port       int    `name:"port" default:"6969" help:"Port to bind on 127.0.0.1."`
	RepoDir    string `name:"repo-dir" default:"." help:"Path to the git repo."`
	DataBranch string `name:"data-branch" default:"_defrost" help:"Branch name to read from."`
	NoRemote   bool   `name:"no-remote" help:"Read from the local repo only — do not consult origin."`
} `cmd:"" help:"Serve a local UI for inspecting test history."`
```

- [ ] **Step 5: Wire dispatch in `main.go`**

In `main.go`, add a case after `history`:

```go
case strings.HasPrefix(cmd, "serve"):
	os.Exit(HandleServe(ServeOpts{
		Port:       CLI.Serve.Port,
		RepoDir:    CLI.Serve.RepoDir,
		DataBranch: CLI.Serve.DataBranch,
		NoRemote:   CLI.Serve.NoRemote,
	}))
```

- [ ] **Step 6: Verify it builds and runs**

Run: `go build ./...`

Expected: success.

Run: `./defrost serve --no-remote --port 6969 &` then `curl -s http://localhost:6969/` and kill the server.

Expected: returns the placeholder HTML; binary exits 0 on Ctrl-C.

- [ ] **Step 7: Commit**

```bash
git add assets.go serve.go cli.go main.go web/dist/.gitkeep web/dist/index.html
git commit -m "feat: add defrost serve subcommand wiring"
```

---

## Task 5: Web scaffold (Vite + React + TS + Tailwind)

**Files:**

- Create: `web/package.json`, `web/index.html`, `web/vite.config.ts`, `web/tsconfig.json`, `web/tsconfig.node.json`, `web/tailwind.config.ts`, `web/postcss.config.js`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/index.css`, `web/src/lib/utils.ts`.

- [ ] **Step 1: Initialise Vite + React + TS**

Run:

```bash
mkdir -p web && cd web
npm create vite@latest . -- --template react-ts
```

When prompted to overwrite the empty `web/` directory, accept. Vite generates `package.json`, `index.html`, `vite.config.ts`, `tsconfig*.json`, `src/main.tsx`, `src/App.tsx`, `src/index.css`.

- [ ] **Step 2: Install Tailwind**

Inside `web/`:

```bash
npm install -D tailwindcss@^3 postcss autoprefixer
npx tailwindcss init -p
```

Replace `web/tailwind.config.js` with `web/tailwind.config.ts`:

```ts
import type { Config } from "tailwindcss";

export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
} satisfies Config;
```

```bash
rm -f web/tailwind.config.js
npm install -D tailwindcss-animate
```

Overwrite `web/src/index.css`:

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    --background: 0 0% 100%;
    --foreground: 240 10% 3.9%;
    --card: 0 0% 100%;
    --card-foreground: 240 10% 3.9%;
    --popover: 0 0% 100%;
    --popover-foreground: 240 10% 3.9%;
    --primary: 240 5.9% 10%;
    --primary-foreground: 0 0% 98%;
    --secondary: 240 4.8% 95.9%;
    --secondary-foreground: 240 5.9% 10%;
    --muted: 240 4.8% 95.9%;
    --muted-foreground: 240 3.8% 46.1%;
    --accent: 240 4.8% 95.9%;
    --accent-foreground: 240 5.9% 10%;
    --destructive: 0 84.2% 60.2%;
    --destructive-foreground: 0 0% 98%;
    --border: 240 5.9% 90%;
    --input: 240 5.9% 90%;
    --ring: 240 5.9% 10%;
    --radius: 0.5rem;
  }
}

@layer base {
  * { @apply border-border; }
  body { @apply bg-background text-foreground; }
}
```

- [ ] **Step 3: Configure Vite proxy + path alias**

Overwrite `web/vite.config.ts`:

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:6969",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
```

- [ ] **Step 4: Add path alias to TypeScript config**

Recent Vite React+TS scaffolds use project references — the `compilerOptions` live in `tsconfig.app.json`, not `tsconfig.json`. Add the alias to **whichever file contains `compilerOptions`** (typically `tsconfig.app.json`); duplicate it in `tsconfig.json` if both have a `compilerOptions` block:

```json
"baseUrl": ".",
"paths": { "@/*": ["./src/*"] }
```

- [ ] **Step 5: Install `@types/node` for the path alias**

```bash
npm install -D @types/node
```

- [ ] **Step 6: Add the shadcn `cn` helper**

Create `web/src/lib/utils.ts`:

```ts
import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
```

```bash
npm install clsx tailwind-merge
```

- [ ] **Step 7: Replace the default App with a placeholder**

Overwrite `web/src/App.tsx`:

```tsx
export default function App() {
  return <div className="p-6">defrost serve — scaffold ready</div>;
}
```

Overwrite `web/src/main.tsx`:

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
```

- [ ] **Step 8: Verify dev server runs**

```bash
cd web && npm run dev
```

Expected: Vite reports `Local: http://localhost:5173/`. Open it; placeholder text renders. Stop with Ctrl-C.

- [ ] **Step 9: Commit**

```bash
git add web/package.json web/package-lock.json web/index.html web/vite.config.ts web/tsconfig.json web/tsconfig.node.json web/tailwind.config.ts web/postcss.config.js web/src/main.tsx web/src/App.tsx web/src/index.css web/src/lib/utils.ts
git commit -m "feat(web): scaffold Vite + React + TS + Tailwind"
```

---

## Task 6: shadcn init + add components

**Files:**

- Create: `web/components.json`, `web/src/components/ui/*.tsx`.

- [ ] **Step 1: Initialise shadcn**

In `web/`:

```bash
npx shadcn@latest init -d
```

`-d` accepts the defaults (style: default, base color: slate, CSS variables: yes). This writes `web/components.json` and ensures the alias `@/components/ui` works. If the CLI prompts about an existing `index.css`, accept the overwrite — shadcn merges its variables into the file we wrote in Task 5.

- [ ] **Step 2: Add the shadcn components we use**

```bash
npx shadcn@latest add button badge sheet tooltip sonner chart
```

This creates `web/src/components/ui/button.tsx`, `badge.tsx`, `sheet.tsx`, `tooltip.tsx`, `sonner.tsx`, `chart.tsx` and pulls in their dependencies (lucide-react, @radix-ui/* peers, sonner, recharts).

- [ ] **Step 3: Verify build**

```bash
npm run build
```

Expected: succeeds, populates `web/dist/`.

- [ ] **Step 4: Commit**

```bash
git add web/components.json web/src/components/ui web/package.json web/package-lock.json
git commit -m "feat(web): init shadcn + add ui components"
```

---

## Task 7: Add TanStack Query + React Router + Vitest

**Files:**

- Modify: `web/package.json` (via `npm install`)
- Create: `web/vitest.setup.ts`
- Modify: `web/vite.config.ts` (add Vitest config)

- [ ] **Step 1: Install runtime deps**

```bash
cd web
npm install @tanstack/react-query react-router-dom
```

- [ ] **Step 2: Install test deps**

```bash
npm install -D vitest @testing-library/react @testing-library/jest-dom @testing-library/user-event jsdom
```

- [ ] **Step 3: Create `web/vitest.setup.ts`**

```ts
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 4: Add Vitest config to `web/vite.config.ts`**

Replace the file with:

```ts
/// <reference types="vitest" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:6969",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    css: true,
  },
});
```

- [ ] **Step 5: Add a `test` script to `web/package.json`**

In the `"scripts"` block, add `"test": "vitest run"` and `"test:watch": "vitest"`.

- [ ] **Step 6: Smoke-test Vitest**

Create a temporary `web/src/smoke.test.ts`:

```ts
import { describe, it, expect } from "vitest";
describe("smoke", () => {
  it("runs", () => { expect(1 + 1).toBe(2); });
});
```

Run:

```bash
npm test
```

Expected: 1 passing test.

Delete the smoke file.

```bash
rm web/src/smoke.test.ts
```

- [ ] **Step 7: Commit**

```bash
git add web/package.json web/package-lock.json web/vite.config.ts web/vitest.setup.ts
git commit -m "feat(web): add TanStack Query, React Router, Vitest"
```

---

## Task 8: `queryClient` + `test-utils`

**Files:**

- Create: `web/src/queryClient.ts`
- Create: `web/src/test-utils.tsx`

- [ ] **Step 1: Create the query client**

Create `web/src/queryClient.ts`:

```ts
import { QueryClient } from "@tanstack/react-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      gcTime: 5 * 60_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});
```

- [ ] **Step 2: Create the test render helper**

Create `web/src/test-utils.tsx`:

```tsx
import { render, type RenderOptions } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, type MemoryRouterProps } from "react-router-dom";
import type { ReactElement, ReactNode } from "react";

type Options = Omit<RenderOptions, "queries"> & {
  router?: MemoryRouterProps;
};

export function renderWithProviders(ui: ReactElement, options: Options = {}) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity, staleTime: 0 },
    },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter {...options.router}>{children}</MemoryRouter>
      </QueryClientProvider>
    );
  }
  return render(ui, { wrapper: Wrapper, ...options });
}
```

- [ ] **Step 3: Commit**

```bash
git add web/src/queryClient.ts web/src/test-utils.tsx
git commit -m "feat(web): add QueryClient config + test render helper"
```

---

## Task 9: API types + `api.ts` (TDD)

**Files:**

- Create: `web/src/types.ts`
- Create: `web/src/api.ts`
- Create: `web/src/api.test.ts`

- [ ] **Step 1: Define types**

Create `web/src/types.ts`:

```ts
export type Status = "pass" | "fail" | "skip" | string;

export interface RunSummary {
  run_id: string;
  ts: string;
  commit?: string;
  branch?: string;
}

export interface Cell {
  run_id: string;
  status: Status;
  duration_ms: number;
}

export interface TestRow {
  test_id: string;
  test_name: string;
  cells: Cell[];
}

export interface GridResponse {
  runs: RunSummary[];
  tests: TestRow[];
}

export interface Entry {
  schema: number;
  test_id: string;
  test_name: string;
  run_id: string;
  ts: string;
  ran: boolean;
  passed: boolean;
  status: string;
  duration_ms: number;
  output?: string;
}

export interface RunRecord {
  schema: number;
  run_id: string;
  commit?: string;
  parent?: string;
  branch?: string;
  pr?: number;
  author_email?: string;
  author_name?: string;
  dirty: boolean;
  dirty_hash?: string;
  cmd?: string[];
  cmd_hash?: string;
  go_version?: string;
  os?: string;
  arch?: string;
  ts: string;
}

export interface TestRunDetail {
  test: Entry;
  run: RunRecord;
}
```

- [ ] **Step 2: Write the failing test**

Create `web/src/api.test.ts`:

```ts
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { getTests, getTestRun } from "./api";

describe("api.getTests", () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; });

  it("parses GridResponse from /api/tests", async () => {
    global.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ runs: [], tests: [] }), { status: 200 })
    );
    const r = await getTests();
    expect(r).toEqual({ runs: [], tests: [] });
    expect((global.fetch as any).mock.calls[0][0]).toBe("/api/tests");
  });

  it("throws on non-2xx", async () => {
    global.fetch = vi.fn().mockResolvedValue(new Response("nope", { status: 500 }));
    await expect(getTests()).rejects.toThrow();
  });
});

describe("api.getTestRun", () => {
  beforeEach(() => {
    global.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ test: {}, run: {} }), { status: 200 })
    );
  });
  it("hits the right URL", async () => {
    await getTestRun("tid-A", "run-1");
    expect((global.fetch as any).mock.calls[0][0]).toBe("/api/test/tid-A/run/run-1");
  });
  it("URL-encodes its arguments", async () => {
    await getTestRun("tid/with slash", "run/1");
    expect((global.fetch as any).mock.calls[0][0]).toBe("/api/test/tid%2Fwith%20slash/run/run%2F1");
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd web && npm test
```

Expected: FAIL — `./api` does not resolve.

- [ ] **Step 4: Implement `api.ts`**

Create `web/src/api.ts`:

```ts
import type { GridResponse, TestRunDetail } from "./types";

async function fetchJSON<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`${url}: ${r.status} ${r.statusText}`);
  return (await r.json()) as T;
}

export function getTests(): Promise<GridResponse> {
  return fetchJSON<GridResponse>("/api/tests");
}

export function getTestRun(testID: string, runID: string): Promise<TestRunDetail> {
  return fetchJSON<TestRunDetail>(
    `/api/test/${encodeURIComponent(testID)}/run/${encodeURIComponent(runID)}`
  );
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
npm test
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/types.ts web/src/api.ts web/src/api.test.ts
git commit -m "feat(web): add typed API client + tests"
```

---

## Task 10: Wire `main.tsx` providers (App stays a stub for now)

We wire QueryClientProvider, BrowserRouter, and the Toaster in `main.tsx`. `App.tsx` stays a placeholder — we'll wire real components in Task 14 once they all exist, so every commit between here and there leaves `main` building cleanly.

**Files:**

- Modify: `web/src/main.tsx`
- Modify: `web/src/App.tsx` (keep as a stub)

- [ ] **Step 1: Wire the providers**

Overwrite `web/src/main.tsx`:

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { queryClient } from "./queryClient";
import { Toaster } from "@/components/ui/sonner";
import "./index.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
        <Toaster />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>
);
```

- [ ] **Step 2: Keep `App.tsx` as a placeholder**

Overwrite `web/src/App.tsx` with a stub that compiles standalone:

```tsx
export default function App() {
  return (
    <div className="min-h-screen bg-background text-foreground p-6">
      <h1 className="text-lg font-semibold">defrost</h1>
      <p className="text-sm text-muted-foreground">scaffold ready — components landing in next tasks</p>
    </div>
  );
}
```

- [ ] **Step 3: Verify it still builds**

```bash
cd web && npm run build
```

Expected: succeeds.

- [ ] **Step 4: Commit**

```bash
git add web/src/main.tsx web/src/App.tsx
git commit -m "feat(web): wire QueryClient + Router providers"
```

---

## Task 11: `StatusBadge` + `RunCell` (TDD)

**Files:**

- Create: `web/src/components/StatusBadge.tsx`
- Create: `web/src/components/RunCell.tsx`
- Create: `web/src/components/RunCell.test.tsx`

- [ ] **Step 1: Implement `StatusBadge`**

Create `web/src/components/StatusBadge.tsx`:

```tsx
import { Badge } from "@/components/ui/badge";

export function StatusBadge({ status }: { status: string }) {
  const className =
    status === "pass" ? "bg-green-600 text-white" :
    status === "fail" ? "bg-red-600 text-white" :
    "bg-neutral-300 text-neutral-900";
  return <Badge className={className}>{status}</Badge>;
}
```

(No test — pure presentation, exercised via `Grid.test.tsx` indirectly.)

- [ ] **Step 2: Write the failing `RunCell` test**

Create `web/src/components/RunCell.test.tsx`:

```tsx
import { describe, it, expect } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { RunCell } from "./RunCell";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

describe("RunCell", () => {
  it("colors by status", () => {
    renderWithProviders(<RunCell testId="t1" runId="r1" status="fail" />);
    const cell = screen.getByTestId("run-cell-t1-r1");
    expect(cell.className).toContain("bg-red-500");
  });

  it("uses neutral when no status (missing run)", () => {
    renderWithProviders(<RunCell testId="t1" runId="r1" status={null} />);
    const cell = screen.getByTestId("run-cell-t1-r1");
    expect(cell.className).toContain("bg-neutral-100");
  });

  it("click updates ?run=&test=", async () => {
    const user = userEvent.setup();
    renderWithProviders(
      <RunCell testId="tid-A" runId="run-2" status="pass" />,
      { router: { initialEntries: ["/"] } }
    );
    await user.click(screen.getByTestId("run-cell-tid-A-run-2"));
    expect(window.location.search).toContain("run=run-2");
    expect(window.location.search).toContain("test=tid-A");
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

```bash
npm test -- RunCell
```

Expected: FAIL — `./RunCell` does not resolve.

- [ ] **Step 4: Implement `RunCell`**

Create `web/src/components/RunCell.tsx`:

```tsx
import { useSearchParams } from "react-router-dom";
import { cn } from "@/lib/utils";

const colorByStatus: Record<string, string> = {
  pass: "bg-green-500 hover:ring-2 ring-green-700",
  fail: "bg-red-500 hover:ring-2 ring-red-700",
  skip: "bg-neutral-300 hover:ring-2 ring-neutral-500",
};

export function RunCell({
  testId,
  runId,
  status,
}: {
  testId: string;
  runId: string;
  status: string | null;
}) {
  const [, setParams] = useSearchParams();
  const color = status ? colorByStatus[status] ?? "bg-yellow-500" : "bg-neutral-100 border border-neutral-200";
  return (
    <button
      type="button"
      data-testid={`run-cell-${testId}-${runId}`}
      className={cn("h-3.5 w-3.5 rounded-sm shrink-0", color)}
      onClick={() => {
        setParams({ test: testId, run: runId });
      }}
      aria-label={`run ${runId} test ${testId} status ${status ?? "missing"}`}
    />
  );
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
npm test -- RunCell
```

Expected: PASS for all three cases.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/StatusBadge.tsx web/src/components/RunCell.tsx web/src/components/RunCell.test.tsx
git commit -m "feat(web): add StatusBadge + RunCell with click → URL"
```

---

## Task 12: `TestRow` + `Grid` (TDD)

**Files:**

- Create: `web/src/components/TestRow.tsx`
- Create: `web/src/components/Grid.tsx`
- Create: `web/src/components/Grid.test.tsx`

- [ ] **Step 1: Implement `TestRow`**

Create `web/src/components/TestRow.tsx`:

```tsx
import type { RunSummary, TestRow as TestRowData } from "@/types";
import { RunCell } from "./RunCell";

export function TestRow({
  row,
  runs,
}: {
  row: TestRowData;
  runs: RunSummary[];
}) {
  const cellByRun = new Map(row.cells.map((c) => [c.run_id, c]));
  return (
    <div className="flex items-center gap-2 py-1 font-mono text-xs">
      <div className="flex-1 truncate" title={row.test_name}>
        {row.test_name}
      </div>
      <div className="flex gap-0.5">
        {runs.map((r) => {
          const cell = cellByRun.get(r.run_id);
          return (
            <RunCell
              key={r.run_id}
              testId={row.test_id}
              runId={r.run_id}
              status={cell?.status ?? null}
            />
          );
        })}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Write the failing `Grid` test**

Create `web/src/components/Grid.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { Grid } from "./Grid";
import { screen, waitFor } from "@testing-library/react";
import * as api from "@/api";

vi.mock("@/api");

describe("Grid", () => {
  beforeEach(() => {
    vi.mocked(api.getTests).mockResolvedValue({
      runs: [
        { run_id: "run-2", ts: "2026-01-02T00:00:00Z" },
        { run_id: "run-1", ts: "2026-01-01T00:00:00Z" },
      ],
      tests: [
        {
          test_id: "tid-A",
          test_name: "pkg.TestA",
          cells: [
            { run_id: "run-1", status: "pass", duration_ms: 5 },
            { run_id: "run-2", status: "fail", duration_ms: 9 },
          ],
        },
      ],
    });
  });

  it("renders one row per test with cells in run order", async () => {
    renderWithProviders(<Grid />);
    await waitFor(() => screen.getByText("pkg.TestA"));
    expect(screen.getByTestId("run-cell-tid-A-run-1")).toBeInTheDocument();
    expect(screen.getByTestId("run-cell-tid-A-run-2")).toBeInTheDocument();
  });

  it("shows empty state when no runs", async () => {
    vi.mocked(api.getTests).mockResolvedValue({ runs: [], tests: [] });
    renderWithProviders(<Grid />);
    await waitFor(() => screen.getByText(/no test runs yet/i));
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

```bash
npm test -- Grid
```

Expected: FAIL — `./Grid` does not resolve.

- [ ] **Step 4: Implement `Grid`**

Create `web/src/components/Grid.tsx`:

```tsx
import { useQuery } from "@tanstack/react-query";
import { getTests } from "@/api";
import { TestRow } from "./TestRow";

export function Grid() {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["tests"],
    queryFn: getTests,
  });

  if (isLoading) return <p className="text-sm text-muted-foreground">loading…</p>;
  if (isError) return <p className="text-sm text-red-600">failed: {(error as Error).message}</p>;
  if (!data) return null;
  if (data.runs.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No test runs yet — run <code>defrost exec go test ./...</code> to record some.
      </p>
    );
  }

  return (
    <div className="space-y-0">
      <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
        <span className="flex-1">test</span>
        <span>← older · newer →</span>
      </div>
      {data.tests.map((row) => (
        <TestRow key={row.test_id} row={row} runs={[...data.runs].reverse()} />
      ))}
    </div>
  );
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
npm test
```

Expected: PASS for all tests so far.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/TestRow.tsx web/src/components/Grid.tsx web/src/components/Grid.test.tsx
git commit -m "feat(web): add TestRow + Grid with TanStack Query"
```

---

## Task 13: `DurationSparkline` (smoke test)

**Files:**

- Create: `web/src/components/DurationSparkline.tsx`
- Create: `web/src/components/DurationSparkline.test.tsx`

- [ ] **Step 1: Write the failing smoke test**

Create `web/src/components/DurationSparkline.test.tsx`:

```tsx
import { describe, it } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { DurationSparkline } from "./DurationSparkline";

describe("DurationSparkline", () => {
  it("renders without throwing on sample data", () => {
    renderWithProviders(
      <DurationSparkline
        cells={[
          { run_id: "run-1", status: "pass", duration_ms: 5 },
          { run_id: "run-2", status: "fail", duration_ms: 9 },
          { run_id: "run-3", status: "pass", duration_ms: 7 },
        ]}
        runs={[
          { run_id: "run-3", ts: "2026-01-03T00:00:00Z" },
          { run_id: "run-2", ts: "2026-01-02T00:00:00Z" },
          { run_id: "run-1", ts: "2026-01-01T00:00:00Z" },
        ]}
        selectedRunId="run-2"
      />
    );
  });

  it("handles an empty cell array", () => {
    renderWithProviders(
      <DurationSparkline cells={[]} runs={[]} selectedRunId="" />
    );
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
npm test -- DurationSparkline
```

Expected: FAIL — module missing.

- [ ] **Step 3: Implement `DurationSparkline`**

Create `web/src/components/DurationSparkline.tsx`:

```tsx
import { LineChart, Line, ReferenceDot, XAxis, YAxis, Tooltip as RTooltip, ResponsiveContainer } from "recharts";
import type { Cell, RunSummary } from "@/types";

export function DurationSparkline({
  cells,
  runs,
  selectedRunId,
}: {
  cells: Cell[];
  runs: RunSummary[];
  selectedRunId: string;
}) {
  // Reverse runs so x-axis goes oldest → newest.
  const ordered = [...runs].reverse();
  const cellByRun = new Map(cells.map((c) => [c.run_id, c]));
  const data = ordered
    .map((r) => ({ run_id: r.run_id, ts: r.ts, duration_ms: cellByRun.get(r.run_id)?.duration_ms ?? null }))
    .filter((p) => p.duration_ms !== null) as Array<{ run_id: string; ts: string; duration_ms: number }>;

  if (data.length === 0) {
    return <p className="text-xs text-muted-foreground">no duration data</p>;
  }

  const selected = data.find((p) => p.run_id === selectedRunId);

  return (
    <div className="h-24 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 8, bottom: 8, left: 0, right: 0 }}>
          <XAxis dataKey="ts" hide />
          <YAxis hide domain={["dataMin", "dataMax"]} />
          <RTooltip
            formatter={(v: number) => [`${v} ms`, "duration"]}
            labelFormatter={(_, payload) => payload?.[0]?.payload?.run_id ?? ""}
          />
          <Line type="monotone" dataKey="duration_ms" stroke="hsl(var(--primary))" strokeWidth={1.5} dot={false} isAnimationActive={false} />
          {selected && (
            <ReferenceDot x={selected.ts} y={selected.duration_ms} r={4} fill="hsl(var(--primary))" />
          )}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
npm test -- DurationSparkline
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/DurationSparkline.tsx web/src/components/DurationSparkline.test.tsx
git commit -m "feat(web): add DurationSparkline (recharts)"
```

---

## Task 14: `RunDetailSheet` (TDD)

**Files:**

- Create: `web/src/components/RunDetailSheet.tsx`
- Create: `web/src/components/RunDetailSheet.test.tsx`

- [ ] **Step 1: Write the failing test**

Create `web/src/components/RunDetailSheet.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { RunDetailSheet } from "./RunDetailSheet";
import { screen, waitFor } from "@testing-library/react";
import * as api from "@/api";

vi.mock("@/api");

describe("RunDetailSheet", () => {
  beforeEach(() => {
    vi.mocked(api.getTestRun).mockResolvedValue({
      test: {
        schema: 2,
        test_id: "tid-A",
        test_name: "pkg.TestA",
        run_id: "run-2",
        ts: "2026-01-02T00:00:00Z",
        ran: true,
        passed: false,
        status: "fail",
        duration_ms: 12,
        output: "FAIL\n  expected 1, got 2\n",
      },
      run: {
        schema: 2,
        run_id: "run-2",
        commit: "deadbee",
        branch: "main",
        ts: "2026-01-02T00:00:00Z",
        dirty: false,
      },
    });
    vi.mocked(api.getTests).mockResolvedValue({
      runs: [{ run_id: "run-2", ts: "2026-01-02T00:00:00Z" }],
      tests: [
        {
          test_id: "tid-A",
          test_name: "pkg.TestA",
          cells: [{ run_id: "run-2", status: "fail", duration_ms: 12 }],
        },
      ],
    });
  });

  it("opens when ?run=&test= is set and renders output + commit", async () => {
    renderWithProviders(<RunDetailSheet />, {
      router: { initialEntries: ["/?run=run-2&test=tid-A"] },
    });
    await waitFor(() => screen.getByText(/expected 1, got 2/));
    expect(screen.getByText(/deadbee/)).toBeInTheDocument();
    expect(screen.getByText(/pkg\.TestA/)).toBeInTheDocument();
  });

  it("renders nothing when no params are set", () => {
    const { container } = renderWithProviders(<RunDetailSheet />, {
      router: { initialEntries: ["/"] },
    });
    expect(container.querySelector("[data-state='open']")).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
npm test -- RunDetailSheet
```

Expected: FAIL — module missing.

- [ ] **Step 3: Implement `RunDetailSheet`**

Create `web/src/components/RunDetailSheet.tsx`:

```tsx
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription,
} from "@/components/ui/sheet";
import { StatusBadge } from "./StatusBadge";
import { DurationSparkline } from "./DurationSparkline";
import { getTestRun, getTests } from "@/api";

export function RunDetailSheet() {
  const [params, setParams] = useSearchParams();
  const tid = params.get("test") ?? "";
  const rid = params.get("run") ?? "";
  const open = Boolean(tid && rid);

  const detail = useQuery({
    queryKey: ["test", tid, "run", rid],
    queryFn: () => getTestRun(tid, rid),
    enabled: open,
    staleTime: Infinity,
  });

  const grid = useQuery({
    queryKey: ["tests"],
    queryFn: getTests,
    enabled: open,
  });

  const close = () => {
    const next = new URLSearchParams(params);
    next.delete("test");
    next.delete("run");
    setParams(next);
  };

  return (
    <Sheet open={open} onOpenChange={(v) => { if (!v) close(); }}>
      <SheetContent className="w-[640px] sm:max-w-[640px] overflow-y-auto">
        {detail.isError && (
          <p className="text-red-600">failed to load: {(detail.error as Error).message}</p>
        )}
        {detail.data && (
          <>
            <SheetHeader>
              <SheetTitle className="font-mono text-base">{detail.data.test.test_name}</SheetTitle>
              <SheetDescription>
                <span className="flex items-center gap-2">
                  <StatusBadge status={detail.data.test.status} />
                  <span>{detail.data.test.duration_ms}ms</span>
                  {detail.data.run.commit && (
                    <span className="font-mono text-xs">{detail.data.run.commit.slice(0, 7)}</span>
                  )}
                  {detail.data.run.branch && (
                    <span className="text-xs text-muted-foreground">{detail.data.run.branch}</span>
                  )}
                </span>
              </SheetDescription>
            </SheetHeader>

            <section className="mt-6">
              <h3 className="mb-2 text-sm font-medium">duration over recent runs</h3>
              {grid.data && (() => {
                const row = grid.data.tests.find((t) => t.test_id === tid);
                return row ? (
                  <DurationSparkline cells={row.cells} runs={grid.data.runs} selectedRunId={rid} />
                ) : null;
              })()}
            </section>

            <section className="mt-6">
              <h3 className="mb-2 text-sm font-medium">output</h3>
              <pre className="rounded bg-muted p-3 text-xs whitespace-pre-wrap font-mono">
                {detail.data.test.output ?? "(no output)"}
              </pre>
            </section>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
npm test
```

Expected: PASS for all suites.

- [ ] **Step 5: Wire the real `App.tsx`**

Now that all components exist, replace the `App.tsx` stub with the real one. Overwrite `web/src/App.tsx`:

```tsx
import { Routes, Route } from "react-router-dom";
import { Grid } from "@/components/Grid";
import { RunDetailSheet } from "@/components/RunDetailSheet";

export default function App() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b px-6 py-4">
        <h1 className="text-lg font-semibold">defrost</h1>
        <p className="text-sm text-muted-foreground">test history</p>
      </header>
      <main className="p-6">
        <Routes>
          <Route path="/" element={<Grid />} />
        </Routes>
        <RunDetailSheet />
      </main>
    </div>
  );
}
```

- [ ] **Step 6: Verify production build succeeds**

```bash
npm run build
```

Expected: TypeScript and Vite both succeed; `web/dist/` is repopulated.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/RunDetailSheet.tsx web/src/components/RunDetailSheet.test.tsx web/src/App.tsx
git commit -m "feat(web): add RunDetailSheet + wire App routes"
```

---

## Task 15: Makefile + `.gitignore`

**Files:**

- Create: `Makefile`
- Create: `.gitignore`

- [ ] **Step 1: Create the Makefile**

Create `Makefile` at repo root:

```makefile
.PHONY: build test web-build web-test go-build go-test

build: web-build go-build

web-build:
	cd web && npm ci && npm run build

go-build:
	go build ./...

test: web-test go-test

web-test:
	cd web && npm test

go-test:
	go test ./...
```

- [ ] **Step 2: Create the `.gitignore`**

Create `.gitignore` at repo root:

```
# Build artefacts
defrost
defrost.exe

# Web tooling
web/node_modules/

# Brainstorm session content (Visual Companion)
.superpowers/

# OS noise
.DS_Store
```

(Note: `web/dist/` is intentionally NOT ignored — it's committed.)

- [ ] **Step 3: Verify the full build works**

```bash
make build
make test
```

Expected: both succeed.

- [ ] **Step 4: Commit**

```bash
git add Makefile .gitignore
git commit -m "chore: add Makefile + .gitignore"
```

---

## Task 16: First production build → commit `web/dist/`

**Files:**

- Modify: `web/dist/**` (overwrite the placeholder).

- [ ] **Step 1: Build**

```bash
cd web && npm run build && cd ..
```

Expected: `web/dist/index.html`, `web/dist/assets/*` populated.

- [ ] **Step 2: Verify the binary serves the real SPA**

```bash
go build -o defrost . && ./defrost serve --no-remote --port 6969 &
PID=$!
sleep 1
curl -s http://localhost:6969/ | grep -q "<div id=\"root\"" && echo OK
kill $PID
```

Expected: prints `OK`.

- [ ] **Step 3: Remove the placeholder**

The first real build overwrites `web/dist/index.html`. Delete the `.gitkeep` (the directory now has real content):

```bash
rm -f web/dist/.gitkeep
```

- [ ] **Step 4: Commit the built dist**

```bash
git add web/dist
git commit -m "build(web): initial production dist"
```

---

## Task 17: GitHub Action — auto-rebuild `web/dist/`

**Files:**

- Create: `.github/workflows/web-dist.yml`

- [ ] **Step 1: Create the workflow**

Create `.github/workflows/web-dist.yml`:

```yaml
name: Rebuild web/dist

on:
  push:
    paths:
      - 'web/src/**'
      - 'web/index.html'
      - 'web/package.json'
      - 'web/package-lock.json'
      - 'web/vite.config.ts'
      - 'web/tailwind.config.ts'
      - 'web/tsconfig.json'
      - 'web/components.json'

permissions:
  contents: write

jobs:
  rebuild:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.GITHUB_TOKEN }}

      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: web/package-lock.json

      - name: Install dependencies
        working-directory: web
        run: npm ci

      - name: Build
        working-directory: web
        run: npm run build

      - name: Commit rebuilt dist if changed
        run: |
          if git diff --quiet web/dist; then
            echo "No dist changes; nothing to commit."
            exit 0
          fi
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          git add web/dist
          git commit -m "chore: rebuild web/dist [skip ci]"
          git push
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/web-dist.yml
git commit -m "ci: auto-rebuild web/dist on web/ source changes"
```

---

## Self-Review Checklist (do these before declaring done)

After running all tasks, verify against the spec:

- [ ] `defrost serve` starts on `127.0.0.1:6969` and prints the URL.
- [ ] Visiting `/` renders the heatmap; rows = tests, columns = runs (newest right).
- [ ] Clicking a cell pushes `?run=&test=` and opens the side panel.
- [ ] Side panel shows status badge, duration, commit prefix, branch, sparkline, output.
- [ ] Reloading the tab fetches new data subject to `Cache-Control: max-age=60`.
- [ ] `defrost exec ...` then reload after 60s shows the new run.
- [ ] `go test ./...` passes.
- [ ] `cd web && npm test` passes.
- [ ] `make build` succeeds and `defrost` runs from the resulting binary.
- [ ] GitHub Action workflow file is valid YAML (`gh workflow view web-dist.yml --repo <slug>` if pushed).
- [ ] `.gitignore` excludes `web/node_modules/` and `.superpowers/` but not `web/dist/`.

If any item fails, return to the relevant task and fix; do not mark the plan complete until all checkboxes pass.
