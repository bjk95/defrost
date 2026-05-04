# Internal docs

Anything under `docs/_internal/` is **not** part of the user-facing
documentation site. The static-site builder is configured to skip
underscore-prefixed directories.

## What lives here

- `specs/` — design notes, RFCs, and architecture decisions. Dated one file
  per feature (e.g. `2026-04-30-defrost-suppress-design.md`).

## Workflow

User-facing behaviour is defined in the public docs (`docs/guides/`,
`docs/reference/`, `docs/concepts/`). Those pages **are** the spec — they
land before or alongside the implementation.

`docs/_internal/specs/` is for design notes that don't belong on the public
site: things like data-model trade-offs, migration plans, or comparisons
between options that were considered. If a spec is purely describing
user-visible behaviour, it should be a reference page instead.
