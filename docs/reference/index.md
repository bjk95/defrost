---
title: 'Reference'
---

The behavioural contract for defrost. Every command, flag, default,
configuration option, on-disk path, and HTTP endpoint is documented
here. If the binary disagrees with this section, that is a bug.

## CLI

- **[`defrost exec`](./cli/exec/)** — run a test command, capture
  results as OTel, commit to the data branch.
- **[`defrost history`](./cli/history/)** — print recorded history for
  a single test as NDJSON.
- **[`defrost suppress`](./cli/suppress/)** — manage the suppression
  list (`add`, `remove`, `list`).
- **[`defrost drop`](./cli/drop/)** — destructively drop persisted
  traces and/or metrics.
- **[`defrost serve`](./cli/serve/)** — serve the local dashboard.

## Configuration

- **[Configuration](./configuration/)** — flags shared across all
  commands and the environment variables defrost reads.

## Storage and ingestion

- **[Storage layout](./storage-layout/)** — what defrost writes to the
  data branch, file paths, formats, the `suppressions.json` schema.
- **[OTel ingestion](./otel-ingestion/)** — the OTLP receiver embedded
  in `defrost exec`, accepted signals, span and attribute conventions.
- **[Serve HTTP API](./serve-api/)** — endpoints exposed by
  `defrost serve` for the dashboard.
