// Package query is the seam between the dashboard handlers and the
// data engine. The Querier interface insulates `internal/serve` from
// whether the data lives in an embedded local DuckDB (the default)
// or a remote ClickHouse cluster (future hosted-defrost). Today only
// the DuckDB impl exists.
package query

import "time"

// Querier is the read-side abstraction. Implementations may load data
// from a local DuckDB hydrated by `defrost serve` or, eventually, a
// remote ClickHouse cluster speaking the same column schema.
type Querier interface {
	// Hydrate brings the engine up to date with the data branch.
	// Idempotent. Cheap on warm runs.
	Hydrate() error

	// Grid returns one row per (test, run) cell for the dashboard's
	// heat-map. Newest runs first, capped by RunWindow.
	Grid(w RunWindow) (GridData, error)

	// TestHistory returns one row per persisted run for testID,
	// oldest-first.
	TestHistory(testID string) ([]TestPoint, error)

	// MetricsAll returns every metric data point persisted across all
	// runs, with attributes flattened to JSON. The serve layer
	// resolves data points to runs via exemplar trace_id or
	// time-window fallback (mirroring the pre-DuckDB behaviour).
	MetricsAll() ([]MetricPoint, error)

	// Close releases engine resources.
	Close() error
}

// RunWindow scopes a Grid query to a time range or run-count cap.
// Today only Limit is honoured (Newest, then capped). Time ranges are
// reserved for future server-side pagination.
type RunWindow struct {
	Limit int
	Since time.Time
	Until time.Time
}

// GridData is the dashboard's heat-map view: one Run per column,
// one Test per row, plus the per-cell rendering data.
type GridData struct {
	Runs  []Run
	Tests []TestRow
}

// Run carries every field the dashboard renders for a single defrost
// exec invocation. Populated from the run's root span resource.
type Run struct {
	RunID       string
	Timestamp   time.Time
	Commit      string
	Parent      string
	Branch      string
	PR          int
	AuthorEmail string
	AuthorName  string
	Cmd         []string
	CmdHash     string
	GoVersion   string
	OS          string
	Arch        string
	Dirty       bool
	DirtyHash   string
}

// TestRow is one row of the dashboard heat-map: a test ID + its
// per-run cells.
type TestRow struct {
	TestID   string // url.PathEscape'd encoding of TestName
	TestName string
	Cells    []Cell
}

// Cell is one render-ready entry in a TestRow.Cells: status + duration.
type Cell struct {
	RunID      string
	Status     string // "pass" | "fail" | "skip" | "panic" | other (raw semconv value)
	DurationMs int64
}

// TestPoint is one entry in a single test's history sparkline.
type TestPoint struct {
	RunID      string
	Timestamp  time.Time
	Status     string
	DurationMs int64
	Output     string
}

// MetricPoint is one persisted metric data point. Attrs holds every
// attribute (resource and data-point) flattened into a single
// string→any map for the serve layer's existing aggregation logic.
type MetricPoint struct {
	Name       string
	Unit       string
	Value      float64
	Timestamp  time.Time
	StartTime  time.Time
	TraceID    string // hex; empty if no exemplar
	Attrs      map[string]any
	ResAttrs   map[string]any
}

// TestEntry is the per-(test, run) detail page payload.
type TestEntry struct {
	TestID     string
	TestName   string
	RunID      string
	Timestamp  time.Time
	Ran        bool
	Passed     bool
	Status     string
	DurationMs int64
	Output     string
}

// LookupTestRun returns the (TestEntry, Run) pair for one cell,
// or ok=false when the test or the run is unknown.
type LookupTestRun = func(testID, runID string) (TestEntry, Run, bool)
