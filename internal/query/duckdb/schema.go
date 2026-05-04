package duckdb

// schemaSQL is the DuckDB schema the hydrator populates from canonical
// OTLP/protobuf bytes on disk. Column shape mirrors the OTel Collector's
// ClickHouse exporter conventions so a future hosted-defrost ClickHouse
// implementation can serve the same dashboard with negligible
// translation work.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS traces (
    trace_id      VARCHAR,
    span_id       VARCHAR,
    parent_span   VARCHAR,
    span_name     VARCHAR,
    service_name  VARCHAR,
    start_time    TIMESTAMP,
    end_time      TIMESTAMP,
    duration_ns   BIGINT,
    status_code   INTEGER,
    status_msg    VARCHAR,
    attrs         JSON,
    resource      JSON,
    output        VARCHAR
);
CREATE INDEX IF NOT EXISTS idx_traces_name_start ON traces(span_name, start_time);
CREATE INDEX IF NOT EXISTS idx_traces_trace_id ON traces(trace_id);

CREATE TABLE IF NOT EXISTS metrics (
    metric_name   VARCHAR,
    metric_unit   VARCHAR,
    metric_type   VARCHAR,            -- gauge | sum | histogram | exp_histogram
    value         DOUBLE,             -- scalar for gauge/sum, mean for hist (lossy summary)
    ts            TIMESTAMP,
    start_ts      TIMESTAMP,
    trace_id      VARCHAR,
    attrs         JSON,
    resource      JSON,
    histogram     JSON                -- full payload for histogram/exp_histogram, NULL otherwise
);
CREATE INDEX IF NOT EXISTS idx_metrics_name_ts ON metrics(metric_name, ts);
CREATE INDEX IF NOT EXISTS idx_metrics_trace_id ON metrics(trace_id);
-- Idempotent upgrade for caches created before metric_type / histogram
-- columns existed. CREATE TABLE IF NOT EXISTS won't ALTER an existing
-- table, so we add columns conditionally.
ALTER TABLE metrics ADD COLUMN IF NOT EXISTS metric_type VARCHAR;
ALTER TABLE metrics ADD COLUMN IF NOT EXISTS histogram JSON;

CREATE TABLE IF NOT EXISTS logs (
    trace_id     VARCHAR,
    span_id      VARCHAR,
    ts           TIMESTAMP,
    severity     VARCHAR,
    body         VARCHAR,
    attrs        JSON,
    resource     JSON
);
CREATE INDEX IF NOT EXISTS idx_logs_trace_id_ts ON logs(trace_id, ts);

CREATE TABLE IF NOT EXISTS hydration_state (
    file_path  VARCHAR PRIMARY KEY,
    file_size  BIGINT,
    file_mtime BIGINT
);

-- cache_meta is a single-row k/v table tracking the SHA of the data
-- branch tip that the materialised tables were last hydrated against.
-- Hydrate() compares 'last_sha' to git ls-remote's output and short-
-- circuits the entire fetch+walk when they match.
CREATE TABLE IF NOT EXISTS cache_meta (
    key   VARCHAR PRIMARY KEY,
    value VARCHAR
);
`
