---
title: '`defrost history`'
---

Print recorded history for a single test as newline-delimited JSON. Each
line is one OTel `ResourceSpans` (canonical OTel JSON encoding) — the
spans for a single run that contain at least one span matching the test
ID.

## Synopsis

```text
defrost history [flags] <test-id>
```

## Examples

```sh
# Go test
defrost history github.com/bjk95/defrost/internal/persist.TestRoundTrip

# pytest
defrost history "tests/test_basics.py::test_pass"

# Jest / Vitest
defrost history "src/foo.test.ts > Foo > squares input"
```

Pipe through `jq` to inspect:

```sh
defrost history <test-id> | jq -r '.scopeSpans[].spans[] | "\(.startTimeUnixNano) \(.status.code)"'
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--repo-dir` | string | `.` | Path to the git repo. |
| `--data-branch` | string | `_defrost` | Branch name to read from. |
| `--dev`, `-d` | bool | `false` | Dev mode: read from `<repo-dir>/.defrost-dev/` instead of the data branch. |

## Test ID format

Test IDs are exactly what the adapter records — there is no separate
naming layer. By language:

- **Go:** `<import-path>.<TestName>`, e.g.
  `github.com/bjk95/defrost.TestExec`.
- **pytest:** `<file>::<TestClass>::<test_name>` or `<file>::<test_name>`,
  e.g. `tests/test_basics.py::test_pass`. Parametrize variants get the
  pytest suffix: `test_squared[2-4]`.
- **Jest / Vitest:** `<file> > <describe...> > <test name>`, e.g.
  `src/foo.test.ts > Foo > squares input`.

The exact test ID for any recorded run is visible in the dashboard or in
the JSON output of a previous `defrost history` call.

## Output

NDJSON. One line per matching run, oldest first by run start time. Each
line is the OTel `ResourceSpans` for that run filtered to spans whose
name matches the requested test ID. The root `defrost.run` span is
included so resource attributes (commit SHA, branch, PR number, etc.) are
available per line.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success (including "no history found" — output is empty). |
| `1` | Data branch missing, fetch failed, or read failed. |
