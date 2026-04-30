# defrost

> Track AI evals, metrics, and tests with Git as the database.

Every team wants a record of how their evals, benchmarks, and tests have changed
over time. Almost nobody wants to host a database for it. **defrost** records
each run as a commit on a `_defrost` branch in the same repo, so the history
travels with the code — no database, no SaaS, no API keys.

## What you get

- **Persisted history** — every run is appended to a `_defrost` branch in your repo and pushed alongside your code.
- **Suppression** — mark known-failing tests as suppressed so red CI goes green without skipping them in source.
- **Local web UI** — `defrost serve` opens a heatmap of recent runs at `http://localhost:6969`.

## Install

```sh
go install github.com/bjk95/defrost@latest
```

## Quickstart

From inside any Git repo with an `origin`:

```sh
# Run your tests through defrost. Results are persisted on the _defrost branch.
defrost exec go test ./...

# Inspect the history of a single test.
defrost history github.com/you/pkg.TestThing

# Open the heatmap UI.
defrost serve
```

## Supported runners

| Language | Runner | Invocation |
|---|---|---|
| Go | `go test` | `defrost exec go test ./...` |
| Python | pytest (JUnit XML) | `defrost exec pytest path/` |
| JavaScript / TypeScript | jest | `defrost exec npm test` |

Eval and metric ingestion are under active development.

## How storage works

defrost stores its data on a side branch named `_defrost` (configurable with
`--data-branch`). The branch contains:

- `runs/<run_id>.json` — one file per `defrost exec` invocation (timestamp, commit, branch).
- `tests/<test_id>.ndjson` — one line per recorded result for that test, append-only.
- `suppressions.json` — the current suppression list.

Cloning the repo gives you the full history. Pushing the repo shares it. Two
writers committing simultaneously rebase against each other automatically.

To experiment without touching git, pass `--no-persist` (don't store anything)
or `--dev` (write to `.defrost-dev/` instead of the data branch).

## Commands

### `defrost exec <cmd...>`

Runs the test command, parses results, and persists them. Exits with the
child's exit code — unless every failing test is on the suppression list, in
which case the exit is rewritten to 0.

### `defrost history <test_id>`

Prints the recorded history for a single test as NDJSON, oldest first. The
test ID is the same string the runner emits (e.g.
`github.com/x/p/TestFoo`, `tests/test_x.py::TestY::test_z`,
`src/foo.test.ts > Foo > does the thing`).

### `defrost suppress add | remove | list`

Manage the suppression list. When every failing test in a run is suppressed,
defrost rewrites the exit code to 0 — anything outside the list (a new
failure, a build error, a panic) still exits non-zero.

### `defrost serve`

Serves a local heatmap UI at `127.0.0.1:6969` (override with `--port`). One
row per test, one cell per recent run; click a cell to see that run's failure
output, duration, and commit.

## Status

Test ingestion (Go, pytest, jest) is the first use case to ship. Eval and
performance-metric ingestion are next on the roadmap and reuse the same
storage model.
