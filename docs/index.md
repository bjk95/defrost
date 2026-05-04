# defrost

Track AI evals, metrics, and tests with Git as the database.

defrost records every test run, eval, and metric as commits on a `_defrost`
branch in the same repo, so the history travels with the code — no database,
no SaaS, no API keys.

## Where to go next

- **[Guides](./guides/)** — task-oriented walkthroughs (Quickstart, CI setup,
  suppressing known-failing tests).
- **[Reference](./reference/)** — the behavioural contract for every CLI
  command, config key, and OpenTelemetry signal defrost ingests.
- **[Concepts](./concepts/)** — how defrost works: the Git-as-database model,
  the `_defrost` branch lifecycle, and the OTel ingestion path.

## Docs as spec

These docs are the specification. New behaviour lands here first (in a PR
that updates the relevant guide and reference page) and the implementation
follows. If something on this site doesn't match the binary, that's a bug.
