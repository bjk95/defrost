package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb/v2"

	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/query"
)

// Querier is the local DuckDB-backed implementation of query.Querier.
// Hydrate clones (or stats) the data branch via persist.Backend and
// incrementally INSERTs any new files.
type Querier struct {
	pOpts persist.Options
	db    *sql.DB
}

// New opens (or creates) the cache DuckDB at
// $UserCacheDir/defrost/<repo-hash>/cache.duckdb and runs the schema
// bootstrap. Caller must call Close.
//
// repoHash is sha256(origin URL)[:16]; in --dev mode it's
// sha256("dev:"+repoDir)[:16] so the cache survives branch switches and
// is isolated per remote (or per dev directory).
func New(opts persist.Options) (*Querier, error) {
	cacheDir, err := cacheDirForOpts(opts)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir cache dir: %w", err)
	}
	dbPath := filepath.Join(cacheDir, "cache.duckdb")
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Querier{pOpts: opts, db: db}, nil
}

// cacheDirForOpts returns the same per-repo cache root that
// persist.CacheRoot uses, so cache.duckdb sits next to data/ under
// $UserCacheDir/defrost/<repo-hash>/.
func cacheDirForOpts(opts persist.Options) (string, error) {
	return persist.CacheRoot(opts)
}

// Close closes the underlying database connection.
func (q *Querier) Close() error {
	if q == nil || q.db == nil {
		return nil
	}
	return q.db.Close()
}

// Hydrate brings the cache up to date with the current data branch
// snapshot.
//
// Two-stage freshness check before any work happens:
//
//  1. Cheap probe: `git ls-remote origin <branch>` (one round-trip,
//     ~50ms typical). If the returned SHA matches cache_meta.last_sha,
//     return immediately — DuckDB is up to date by definition since
//     the data branch is append-only between drops.
//
//  2. CloneForRead against the persistent worktree at
//     $UserCacheDir/defrost/<repo-hash>/data — first call clones,
//     subsequent calls fetch+reset. If a force-reset is detected
//     (snap.Reset, e.g. after `defrost drop history` rewrote the
//     branch), the stale derived state is dropped and re-hydrated
//     from scratch.
//
// Idempotent — files already in hydration_state are skipped on the
// row-walk path.
func (q *Querier) Hydrate() error {
	be := persist.New(q.pOpts)
	ctx := context.Background()

	// (1) Cheap freshness probe. Skipped in dev mode (no remote).
	if !q.pOpts.Dev {
		remoteSHA, err := be.RemoteHeadSHA()
		if err != nil {
			return fmt.Errorf("remote head sha: %w", err)
		}
		if remoteSHA != "" {
			lastSHA, err := q.cacheMeta(ctx, "last_sha")
			if err != nil {
				return err
			}
			if lastSHA == remoteSHA {
				return nil
			}
		}
	}

	// (2) Materialise the working tree. Persistent in git mode; a
	// no-op in dev (just stat).
	snap, err := be.CloneForRead()
	if err != nil {
		return err
	}
	if snap.Dir == "" {
		// No data yet. Schema is in place; nothing to insert.
		return nil
	}

	// (3) Force-reset detected — drop stale derived state. The file
	// paths in hydration_state may now refer to deleted blobs and the
	// rows in materialised tables may correspond to dropped runs.
	if snap.Reset {
		if err := q.wipeDerivedState(ctx); err != nil {
			return err
		}
	}

	// (4) Walk every signal partition under the worktree, INSERTing
	// any file not already in hydration_state.
	for _, signal := range signalDirs {
		files, err := listFiles(snap.Dir, signal)
		if err != nil {
			return fmt.Errorf("list %s: %w", signal, err)
		}
		for _, path := range files {
			if err := q.hydrateFile(ctx, signal, path); err != nil {
				return fmt.Errorf("hydrate %s: %w", path, err)
			}
		}
	}

	// (5) Record the SHA we just hydrated against so the next call's
	// cheap probe can short-circuit.
	if snap.SHA != "" {
		if err := q.setCacheMeta(ctx, "last_sha", snap.SHA); err != nil {
			return err
		}
	}
	return nil
}

// cacheMeta reads a single key from cache_meta. Returns ("", nil) when
// the key is absent.
func (q *Querier) cacheMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := q.db.QueryRowContext(ctx, `SELECT value FROM cache_meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read cache_meta[%s]: %w", key, err)
	}
	return v, nil
}

func (q *Querier) setCacheMeta(ctx context.Context, key, value string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO cache_meta (key, value) VALUES (?, ?)
         ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("write cache_meta[%s]: %w", key, err)
	}
	return nil
}

// wipeDerivedState clears every row in the materialised tables and the
// hydration_state index, preparing the cache for a fresh full walk.
// Called when the persistent worktree was force-reset to a non-
// fast-forward remote tip — typically after `defrost drop history`.
//
// cache_meta is preserved (just last_sha overwritten on the next
// successful hydrate).
func (q *Querier) wipeDerivedState(ctx context.Context) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM traces`,
		`DELETE FROM metrics`,
		`DELETE FROM logs`,
		`DELETE FROM hydration_state`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("wipe: %s: %w", stmt, err)
		}
	}
	return tx.Commit()
}

func (q *Querier) hydrateFile(ctx context.Context, signal, path string) error {
	stat, err := statFile(path)
	if err != nil {
		return err
	}
	row := q.db.QueryRowContext(ctx, `SELECT file_size, file_mtime FROM hydration_state WHERE file_path = ?`, path)
	var prevSize, prevMtime int64
	switch err := row.Scan(&prevSize, &prevMtime); err {
	case nil:
		if prevSize == stat.size && prevMtime == stat.mtime {
			return nil
		}
	case sql.ErrNoRows:
		// fall through to ingestion
	default:
		return err
	}
	raw, err := persist.ReadSignalBytes(path)
	if err != nil {
		return err
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	switch signal {
	case "traces":
		td, err := decodeTraces(raw)
		if err != nil {
			return err
		}
		if err := insertTraces(ctx, tx, td); err != nil {
			return err
		}
	case "metrics":
		md, err := decodeMetrics(raw)
		if err != nil {
			return err
		}
		if err := insertMetrics(ctx, tx, md); err != nil {
			return err
		}
	case "logs":
		ld, err := decodeLogs(raw)
		if err != nil {
			return err
		}
		if err := insertLogs(ctx, tx, ld); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown signal %q", signal)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO hydration_state (file_path, file_size, file_mtime)
                                       VALUES (?, ?, ?)
                                       ON CONFLICT (file_path) DO UPDATE SET file_size = excluded.file_size, file_mtime = excluded.file_mtime`,
		path, stat.size, stat.mtime); err != nil {
		return fmt.Errorf("upsert hydration_state: %w", err)
	}
	return tx.Commit()
}

// runIDOfRow extracts a run id from a JSON resource attributes blob.
// Used by row mappers below — DuckDB returns resource as a JSON string.
func runIDOfRow(resourceJSON string) string {
	if resourceJSON == "" {
		return ""
	}
	var attrs map[string]any
	if err := json.Unmarshal([]byte(resourceJSON), &attrs); err != nil {
		return ""
	}
	if v, ok := attrs["cicd.pipeline.run.id"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func attrString(attrs map[string]any, key string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func attrStringArray(attrs map[string]any, key string) []string {
	v, ok := attrs[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func attrInt(attrs map[string]any, key string) int {
	if v, ok := attrs[key]; ok {
		switch x := v.(type) {
		case float64:
			return int(x)
		case int:
			return x
		case int64:
			return int(x)
		case string:
			n := 0
			fmt.Sscanf(x, "%d", &n)
			return n
		}
	}
	return 0
}

func attrBool(attrs map[string]any, key string) bool {
	if v, ok := attrs[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// Grid implements query.Querier by joining root spans (as runs) with
// test-case spans (as cells), capped at w.Limit runs newest-first.
func (q *Querier) Grid(w query.RunWindow) (query.GridData, error) {
	limit := w.Limit
	if limit <= 0 {
		limit = 50
	}
	ctx := context.Background()
	rows, err := q.db.QueryContext(ctx, `SELECT trace_id, start_time, status_code, status_msg, resource
        FROM traces WHERE span_name = ? ORDER BY start_time DESC LIMIT ?`,
		"defrost.run", limit)
	if err != nil {
		return query.GridData{}, fmt.Errorf("query roots: %w", err)
	}
	defer rows.Close()
	type runRow struct {
		traceID string
		run     query.Run
	}
	var runs []runRow
	traceIDs := make([]string, 0, limit)
	for rows.Next() {
		var traceID string
		var startTime time.Time
		var statusCode int32
		var statusMsg string
		var resJSON string
		if err := rows.Scan(&traceID, &startTime, &statusCode, &statusMsg, &resJSON); err != nil {
			return query.GridData{}, err
		}
		var attrs map[string]any
		_ = json.Unmarshal([]byte(resJSON), &attrs)
		runID, _ := attrs["cicd.pipeline.run.id"].(string)
		r := query.Run{
			RunID:       runID,
			TraceID:     traceID,
			Timestamp:   startTime,
			Commit:      attrString(attrs, "vcs.repository.ref.revision"),
			Parent:      attrString(attrs, "defrost.parent_commit"),
			Branch:      attrString(attrs, "vcs.repository.ref.name"),
			PR:          attrInt(attrs, "vcs.repository.change.id"),
			AuthorEmail: attrString(attrs, "defrost.author_email"),
			AuthorName:  attrString(attrs, "defrost.author_name"),
			Cmd:         attrStringArray(attrs, "defrost.cmd"),
			CmdHash:     attrString(attrs, "defrost.cmd_hash"),
			GoVersion:   attrString(attrs, "process.runtime.version"),
			OS:          attrString(attrs, "host.os.type"),
			Arch:        attrString(attrs, "host.arch"),
			Dirty:       attrBool(attrs, "defrost.dirty"),
			DirtyHash:   attrString(attrs, "defrost.dirty_hash"),
		}
		runs = append(runs, runRow{traceID: traceID, run: r})
		traceIDs = append(traceIDs, traceID)
	}
	if err := rows.Err(); err != nil {
		return query.GridData{}, err
	}
	out := query.GridData{Runs: make([]query.Run, 0, len(runs))}
	for _, rr := range runs {
		out.Runs = append(out.Runs, rr.run)
	}
	if len(traceIDs) == 0 {
		return out, nil
	}
	traceIDtoRunID := make(map[string]string, len(runs))
	for _, rr := range runs {
		traceIDtoRunID[rr.traceID] = rr.run.RunID
	}
	cellRows, err := q.db.QueryContext(ctx, fmt.Sprintf(`SELECT trace_id, span_name, start_time, end_time, attrs FROM traces
        WHERE span_name <> 'defrost.run' AND trace_id IN (%s)`, placeholders(len(traceIDs))),
		anyArgs(traceIDs)...)
	if err != nil {
		return query.GridData{}, fmt.Errorf("query test spans: %w", err)
	}
	defer cellRows.Close()
	type cellKey struct {
		testID   string
		testName string
	}
	type cellEntry struct {
		ts   time.Time
		cell query.Cell
	}
	cellsByTest := make(map[cellKey][]cellEntry)
	for cellRows.Next() {
		var traceID, spanName string
		var start, end time.Time
		var attrsJSON string
		if err := cellRows.Scan(&traceID, &spanName, &start, &end, &attrsJSON); err != nil {
			return query.GridData{}, err
		}
		var attrs map[string]any
		_ = json.Unmarshal([]byte(attrsJSON), &attrs)
		status := compactStatus(attrString(attrs, "test.case.result.status"))
		runID := traceIDtoRunID[traceID]
		dur := int64(0)
		if end.After(start) {
			dur = end.Sub(start).Milliseconds()
		}
		key := cellKey{testID: persist.EncodeName(spanName), testName: spanName}
		cellsByTest[key] = append(cellsByTest[key], cellEntry{ts: start, cell: query.Cell{
			RunID: runID, Status: status, DurationMs: dur,
		}})
	}
	if err := cellRows.Err(); err != nil {
		return query.GridData{}, err
	}
	tests := make([]query.TestRow, 0, len(cellsByTest))
	for key, entries := range cellsByTest {
		sort.Slice(entries, func(i, j int) bool { return entries[i].ts.Before(entries[j].ts) })
		row := query.TestRow{TestID: key.testID, TestName: key.testName, Cells: make([]query.Cell, 0, len(entries))}
		for _, e := range entries {
			row.Cells = append(row.Cells, e.cell)
		}
		tests = append(tests, row)
	}
	sort.Slice(tests, func(i, j int) bool { return tests[i].TestName < tests[j].TestName })
	out.Tests = tests
	return out, nil
}

// TestHistory returns oldest-first history for a single test ID.
func (q *Querier) TestHistory(testID string) ([]query.TestPoint, error) {
	testName, err := url.PathUnescape(testID)
	if err != nil {
		testName = testID
	}
	ctx := context.Background()
	rows, err := q.db.QueryContext(ctx, `SELECT t.trace_id, t.start_time, t.end_time, t.attrs, t.output, t.resource
        FROM traces t WHERE t.span_name = ? ORDER BY t.start_time ASC`, testName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []query.TestPoint
	for rows.Next() {
		var traceID, attrsJSON, output, resJSON string
		var start, end time.Time
		if err := rows.Scan(&traceID, &start, &end, &attrsJSON, &output, &resJSON); err != nil {
			return nil, err
		}
		var attrs map[string]any
		_ = json.Unmarshal([]byte(attrsJSON), &attrs)
		var resAttrs map[string]any
		_ = json.Unmarshal([]byte(resJSON), &resAttrs)
		runID, _ := resAttrs["cicd.pipeline.run.id"].(string)
		dur := int64(0)
		if end.After(start) {
			dur = end.Sub(start).Milliseconds()
		}
		out = append(out, query.TestPoint{
			RunID:      runID,
			Timestamp:  start,
			Status:     compactStatus(attrString(attrs, "test.case.result.status")),
			DurationMs: dur,
			Output:     output,
		})
	}
	return out, rows.Err()
}

// LookupEntry returns the (TestEntry, Run) pair for one cell, or
// ok=false when the test or run is unknown.
func (q *Querier) LookupEntry(testID, runID string) (query.TestEntry, query.Run, bool, error) {
	testName, err := url.PathUnescape(testID)
	if err != nil {
		testName = testID
	}
	ctx := context.Background()
	row := q.db.QueryRowContext(ctx, `SELECT t.trace_id, t.start_time, t.end_time, t.attrs, t.output, t.resource
        FROM traces t WHERE t.span_name = ? AND json_extract_string(t.resource, '$."cicd.pipeline.run.id"') = ?
        LIMIT 1`, testName, runID)
	var traceID, attrsJSON, output, resJSON string
	var start, end time.Time
	if err := row.Scan(&traceID, &start, &end, &attrsJSON, &output, &resJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return query.TestEntry{}, query.Run{}, false, nil
		}
		return query.TestEntry{}, query.Run{}, false, err
	}
	var tAttrs map[string]any
	_ = json.Unmarshal([]byte(attrsJSON), &tAttrs)
	resultStatus := attrString(tAttrs, "test.case.result.status")
	dur := int64(0)
	if end.After(start) {
		dur = end.Sub(start).Milliseconds()
	}
	entry := query.TestEntry{
		TestID:     persist.EncodeName(testName),
		TestName:   testName,
		RunID:      runID,
		Timestamp:  start,
		Ran:        resultStatus != "skipped",
		Passed:     resultStatus == "passed",
		Status:     compactStatus(resultStatus),
		DurationMs: dur,
		Output:     output,
	}

	// Look up the root span for the run.
	rootRow := q.db.QueryRowContext(ctx, `SELECT t.start_time, t.resource FROM traces t
        WHERE t.span_name = 'defrost.run' AND t.trace_id = ? LIMIT 1`, traceID)
	var rootStart time.Time
	var rootResJSON string
	if err := rootRow.Scan(&rootStart, &rootResJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return entry, query.Run{}, false, nil
		}
		return entry, query.Run{}, false, err
	}
	var rAttrs map[string]any
	_ = json.Unmarshal([]byte(rootResJSON), &rAttrs)
	r := query.Run{
		RunID:       runID,
		Timestamp:   rootStart,
		Commit:      attrString(rAttrs, "vcs.repository.ref.revision"),
		Parent:      attrString(rAttrs, "defrost.parent_commit"),
		Branch:      attrString(rAttrs, "vcs.repository.ref.name"),
		PR:          attrInt(rAttrs, "vcs.repository.change.id"),
		AuthorEmail: attrString(rAttrs, "defrost.author_email"),
		AuthorName:  attrString(rAttrs, "defrost.author_name"),
		Cmd:         attrStringArray(rAttrs, "defrost.cmd"),
		CmdHash:     attrString(rAttrs, "defrost.cmd_hash"),
		GoVersion:   attrString(rAttrs, "process.runtime.version"),
		OS:          attrString(rAttrs, "host.os.type"),
		Arch:        attrString(rAttrs, "host.arch"),
		Dirty:       attrBool(rAttrs, "defrost.dirty"),
		DirtyHash:   attrString(rAttrs, "defrost.dirty_hash"),
	}
	return entry, r, true, nil
}

// MetricsAll returns every persisted metric data point with its
// attributes flattened to JSON (for the serve layer's existing
// time-window resolver).
func (q *Querier) MetricsAll() ([]query.MetricPoint, error) {
	ctx := context.Background()
	rows, err := q.db.QueryContext(ctx, `SELECT metric_name, metric_unit, value, ts, start_ts, trace_id, attrs, resource FROM metrics`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []query.MetricPoint
	for rows.Next() {
		var name, unit, traceID, attrsJSON, resJSON string
		var value float64
		var ts, start time.Time
		if err := rows.Scan(&name, &unit, &value, &ts, &start, &traceID, &attrsJSON, &resJSON); err != nil {
			return nil, err
		}
		var attrs, resAttrs map[string]any
		_ = json.Unmarshal([]byte(attrsJSON), &attrs)
		_ = json.Unmarshal([]byte(resJSON), &resAttrs)
		out = append(out, query.MetricPoint{
			Name:      name,
			Unit:      unit,
			Value:     value,
			Timestamp: ts,
			StartTime: start,
			TraceID:   traceID,
			Attrs:     attrs,
			ResAttrs:  resAttrs,
		})
	}
	return out, rows.Err()
}

func compactStatus(v string) string {
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

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func anyArgs(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// Compile-time check.
var _ query.Querier = (*Querier)(nil)
var _ query.QuerierWithLookup = (*Querier)(nil)
