---
title: '`defrost exec`'
---

Execute a test command, capture results as OpenTelemetry traces,
metrics, and logs, and commit them to the data branch.

## Synopsis

```text
defrost exec [flags] -- <command> [args...]
```

The `--` is optional but recommended when your command has flags of its
own; everything after `exec` (or after `--`) is passed to the child
verbatim.

## Examples

```sh
defrost exec go test ./...
defrost exec pytest tests/
defrost exec npm test
defrost exec -- pytest -k "evals" --maxfail=1
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--repo-dir` | string | `.` | Path to the git repo to persist into. |
| `--data-branch` | string | `_defrost` | Branch name where results are stored. |
| `--no-persist` | bool | `false` | Run tests without persisting results. The OTel receiver still runs (so child SDKs do not error) but nothing is committed. |
| `--dev`, `-d` | bool | `false` | Dev mode: write results locally only — files still land at `<repo-dir>/.defrost/data/` (same path as prod mode), but no push to origin. For developing defrost itself. |

## What it does

1. Detects the repository state at `--repo-dir`: commit SHA, branch, dirty
   working tree, host OS/arch, defrost version. These become OTel
   resource attributes on the run.
2. Starts the upstream
   [`otlpreceiver`](https://pkg.go.dev/go.opentelemetry.io/collector/receiver/otlpreceiver)
   in library mode on a random free port on `127.0.0.1`. The receiver
   accepts traces, metrics, AND logs — see [OTel ingestion](../../otel-ingestion/).
3. Sets `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_PROTOCOL`
   in the child environment so any OTel SDK in the child auto-points at
   defrost.
4. Runs the child command. Stdout and stderr stream through to the
   terminal unchanged.
5. Selects an adapter based on the command (`go test`, `pytest`, `jest`,
   `vitest`, Inspect AI, PromptFoo) and parses the structured output it
   produces.
6. Waits up to 2 seconds after the child exits for in-flight OTel
   exports to drain.
7. Writes one trace file (and, if any arrived during the run, one
   metrics file and one logs file) to the data branch — see
   [storage layout](../../storage-layout/).
8. Reads `<repo-dir>/.defrost/suppressions.json` (a local file in your
   working tree) and applies the [suppression rule](#exit-codes) to the
   exit code.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Child exited 0, **or** child exited non-zero but every failing test ID is in the suppression list. |
| Child's exit code | Child exited non-zero and at least one failing test is not suppressed, **or** any result is a file-level error (suffix `::<file-error>`, which is never suppressible). |
| `1` | Persistence failed. Persistence-failure-on-zero-child is rewritten to `1` because losing data is worse than losing a passing signal. Also returned for adapter-not-found, repo-detection failure, or commit/push failure. |
| `2` | No command provided. |

When suppression rewrites the exit code, defrost prints to stderr:

```text
defrost: all N failing test(s) suppressed; rewriting exit <code> → 0
```

## Network and authentication

When `--dev` is not set and `--repo-dir` has a remote configured, defrost
pushes the data branch after committing. If `GITHUB_TOKEN` is in the
environment, it is used for HTTPS authentication; otherwise the default
git credential flow applies. See [Configuration](../../configuration/) for
all environment variables read.
