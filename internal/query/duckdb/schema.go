package duckdb

// schemaSQL is the DuckDB schema the hydrator populates from canonical
// OTLP/protobuf bytes on disk.
//
// Table and column shapes are an exact match for the duckdb-otlp
// extension (https://github.com/smithclay/duckdb-otlp): same names,
// same types, same field order. That extension exposes table-valued
// functions like read_otlp_traces(<file>) producing rows in this
// schema; matching it means a defrost user can run the same SQL
// against their cache.duckdb and against ad-hoc OTLP files via the
// extension, with no rewrites.
//
// Identifiers (trace_id, span_id, parent_span_id) are VARCHAR hex.
// Attribute fields (resource_attributes, scope_attributes,
// span_attributes, log_attributes, metric_attributes,
// exemplars_json, events_json, links_json, bucket_counts,
// explicit_bounds, positive_bucket_counts, negative_bucket_counts)
// are VARCHAR holding JSON — query with json_extract / json_extract_string.
//
// Primary `timestamp` is TIMESTAMP_MS (ms since epoch). Companion
// timestamps (`start_timestamp`, `end_timestamp`, `observed_timestamp`)
// are BIGINT nanoseconds since epoch — same as the OTLP wire encoding.
//
// hydration_state and cache_meta are defrost-internal bookkeeping
// tables (not part of duckdb-otlp); the hydrator uses them to track
// per-file ingest state and the last-hydrated commit SHA.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS otel_traces (
    timestamp                TIMESTAMP_MS,
    end_timestamp            BIGINT,
    duration                 BIGINT,
    trace_id                 VARCHAR,
    span_id                  VARCHAR,
    parent_span_id           VARCHAR,
    trace_state              VARCHAR,
    service_name             VARCHAR,
    service_namespace        VARCHAR,
    service_instance_id      VARCHAR,
    span_name                VARCHAR,
    span_kind                INTEGER,
    status_code              INTEGER,
    status_message           VARCHAR,
    resource_attributes      VARCHAR,
    scope_name               VARCHAR,
    scope_version            VARCHAR,
    scope_attributes         VARCHAR,
    span_attributes          VARCHAR,
    events_json              VARCHAR,
    links_json               VARCHAR,
    dropped_attributes_count INTEGER,
    dropped_events_count     INTEGER,
    dropped_links_count      INTEGER,
    flags                    INTEGER
);

CREATE TABLE IF NOT EXISTS otel_logs (
    timestamp           TIMESTAMP_MS,
    observed_timestamp  BIGINT,
    trace_id            VARCHAR,
    span_id             VARCHAR,
    service_name        VARCHAR,
    service_namespace   VARCHAR,
    service_instance_id VARCHAR,
    severity_number     INTEGER,
    severity_text       VARCHAR,
    body                VARCHAR,
    resource_attributes VARCHAR,
    scope_name          VARCHAR,
    scope_version       VARCHAR,
    scope_attributes    VARCHAR,
    log_attributes      VARCHAR
);

CREATE TABLE IF NOT EXISTS otel_metrics_gauge (
    timestamp           TIMESTAMP_MS,
    start_timestamp     BIGINT,
    metric_name         VARCHAR,
    metric_description  VARCHAR,
    metric_unit         VARCHAR,
    value               DOUBLE,
    service_name        VARCHAR,
    service_namespace   VARCHAR,
    service_instance_id VARCHAR,
    resource_attributes VARCHAR,
    scope_name          VARCHAR,
    scope_version       VARCHAR,
    scope_attributes    VARCHAR,
    metric_attributes   VARCHAR,
    flags               INTEGER,
    exemplars_json      VARCHAR
);

CREATE TABLE IF NOT EXISTS otel_metrics_sum (
    timestamp               TIMESTAMP_MS,
    start_timestamp         BIGINT,
    metric_name             VARCHAR,
    metric_description      VARCHAR,
    metric_unit             VARCHAR,
    value                   DOUBLE,
    service_name            VARCHAR,
    service_namespace       VARCHAR,
    service_instance_id     VARCHAR,
    resource_attributes     VARCHAR,
    scope_name              VARCHAR,
    scope_version           VARCHAR,
    scope_attributes        VARCHAR,
    metric_attributes       VARCHAR,
    flags                   INTEGER,
    exemplars_json          VARCHAR,
    aggregation_temporality INTEGER,
    is_monotonic            BOOLEAN
);

CREATE TABLE IF NOT EXISTS otel_metrics_histogram (
    timestamp               TIMESTAMP_MS,
    start_timestamp         BIGINT,
    metric_name             VARCHAR,
    metric_description      VARCHAR,
    metric_unit             VARCHAR,
    count                   BIGINT,
    sum                     DOUBLE,
    min                     DOUBLE,
    max                     DOUBLE,
    bucket_counts           VARCHAR,
    explicit_bounds         VARCHAR,
    service_name            VARCHAR,
    service_namespace       VARCHAR,
    service_instance_id     VARCHAR,
    resource_attributes     VARCHAR,
    scope_name              VARCHAR,
    scope_version           VARCHAR,
    scope_attributes        VARCHAR,
    metric_attributes       VARCHAR,
    flags                   INTEGER,
    exemplars_json          VARCHAR,
    aggregation_temporality INTEGER
);

CREATE TABLE IF NOT EXISTS otel_metrics_exp_histogram (
    timestamp               TIMESTAMP_MS,
    start_timestamp         BIGINT,
    metric_name             VARCHAR,
    metric_description      VARCHAR,
    metric_unit             VARCHAR,
    count                   BIGINT,
    sum                     DOUBLE,
    min                     DOUBLE,
    max                     DOUBLE,
    scale                   INTEGER,
    zero_count              BIGINT,
    zero_threshold          DOUBLE,
    positive_offset         INTEGER,
    positive_bucket_counts  VARCHAR,
    negative_offset         INTEGER,
    negative_bucket_counts  VARCHAR,
    service_name            VARCHAR,
    service_namespace       VARCHAR,
    service_instance_id     VARCHAR,
    resource_attributes     VARCHAR,
    scope_name              VARCHAR,
    scope_version           VARCHAR,
    scope_attributes        VARCHAR,
    metric_attributes       VARCHAR,
    flags                   INTEGER,
    exemplars_json          VARCHAR,
    aggregation_temporality INTEGER
);

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
