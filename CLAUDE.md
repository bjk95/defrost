# defrost — instructions for Claude

## Documentation layout

This repo uses a **docs-as-spec** workflow. The user-facing documentation
under `docs/` *is* the specification — desired behaviour is defined there
before or alongside implementation.

### Where things go

- `docs/index.md`, `docs/guides/`, `docs/reference/`, `docs/concepts/` —
  public, user-facing documentation. Built into the static site. This is
  the spec for user-visible behaviour.
- `docs/_internal/` — **not** published. Internal design notes and RFCs
  live here. The static-site builder skips underscore-prefixed
  directories.
- `docs/_internal/specs/` — design notes / RFCs, one Markdown file per
  feature, dated `YYYY-MM-DD-<short-name>.md`.

### When writing a new feature

1. **First**, edit the relevant page(s) under `docs/guides/` and/or
   `docs/reference/` to describe the desired user-facing behaviour. This
   is the spec.
2. **Optionally**, add a design note at
   `docs/_internal/specs/YYYY-MM-DD-<feature>.md` for trade-offs,
   migration plans, or implementation notes that do not belong in the
   public docs. Use today's date.
3. **Then** implement to match the public docs.

### Where new specs MUST land

All new internal specs / design docs go in `docs/_internal/specs/`.
Never create them at `docs/specs/`, `specs/`, or any other path. Never
put internal design notes in the public `docs/guides/`, `docs/reference/`,
or `docs/concepts/` directories.

If a spec is purely describing user-visible behaviour, skip the internal
spec entirely and write the reference / guide page instead.
