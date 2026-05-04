# Reference

The behavioural contract for defrost. Every command, flag, config key, and
ingestion schema is documented here. If the binary disagrees with this
section, that is a bug.

## Sections

_To be written._ Planned reference pages:

- **CLI** — one page per command (`defrost run`, `defrost serve`,
  `defrost suppress`, `defrost drop`, `defrost history`).
- **Configuration** — config file schema and environment variables.
- **OTel ingestion** — accepted spans, attributes, and how they map to
  defrost test/eval/metric records.
- **`_defrost` branch layout** — the on-disk format of recorded runs.

Reference pages are the spec. New behaviour starts as a change here.
