# defrost

Track AI evals, metrics, and tests with Git as the database.

defrost wraps your existing test or eval command, captures results as
[OpenTelemetry](https://opentelemetry.io/) traces and metrics, and commits
them to a `_defrost` branch in the same repo. The history travels with the
code — no database, no SaaS, no API keys.

## Install

```sh
go install github.com/bjk95/defrost@latest
```

## Quickstart

Wrap any test command with `defrost exec`:

```sh
defrost exec go test ./...
defrost exec pytest tests/
defrost exec npm test
```

The first run creates a `_defrost` branch with one commit per result file.
Browse the history with:

```sh
defrost serve              # opens a dashboard at http://127.0.0.1:6969
defrost history <test-id>  # NDJSON for a single test
```

## Where to go next

- **[Guides](./guides/)** — task-oriented walkthroughs (Quickstart,
  recording tests, recording evals, suppressing failures, running the
  dashboard, CI setup).
- **[Reference](./reference/)** — the behavioural contract for every CLI
  command, flag, configuration option, storage path, and HTTP endpoint.
- **[Concepts](./concepts/)** — how defrost works: Git as the database,
  the `_defrost` branch lifecycle, OpenTelemetry as the ingestion API,
  the suppression model.

## Docs as spec

These docs are the specification. New behaviour lands here first (in a PR
that updates the relevant guide and reference page) and the implementation
follows. If the binary disagrees with this site, that is a bug — please
[open an issue](https://github.com/bjk95/defrost/issues).
