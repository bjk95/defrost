---
title: 'defrost'
---

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="../assets/logo-light-128.png">
    <img alt="defrost" src="../assets/logo-128.png" width="96" height="96">
  </picture>
</p>

Track AI evals, metrics, and tests with Git as the database.

defrost wraps your existing test or eval command, captures results as
[OpenTelemetry](https://opentelemetry.io/) traces, metrics, and logs,
and commits them to a `_defrost` branch in the same repo. The history
travels with the code — no database, no SaaS, no API keys.

## Install

```sh
# Full install: includes the dashboard and embedded DuckDB read engine.
go install github.com/bjk95/defrost/cmd/defrost@latest
```

For CI workflows that only need the write path (`exec`, `history`,
`suppress`, `drop`) — no dashboard, no DuckDB cgo, no embedded web
bundle — install the slim `defrost-ci` binary instead. About 1/3 the
size and faster to install:

```sh
go install github.com/bjk95/defrost/cmd/defrost-ci@latest
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

