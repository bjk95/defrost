---
title: 'Configuration'
---

defrost has no config file. Behaviour is controlled by per-command flags
(see each [CLI page](../#cli)) and a small number of environment
variables.

## Shared flags

Every defrost command accepts the same set of repo-targeting flags:

| Flag | Default | Description |
|---|---|---|
| `--repo-dir` | `.` | Path to the git repo defrost reads from / writes to. The local `.defrost/` tree is rooted here. |
| `--data-branch` | `_defrost` | Branch name where results live. Override only if you have a naming collision; the dashboard, history, suppress, and drop commands all share this default. |
| `--dev`, `-d` | `false` | Local-only mode: write OTLP files to `<repo-dir>/.defrost/` (same path as prod mode) but skip the push to origin's data branch. Reads also come from the local tree. Intended for developing defrost itself, not for production use. |

`--no-persist` is `defrost exec`-only and runs the OTel receiver without
recording anything.

## Environment variables

defrost **reads**:

| Variable | When | Effect |
|---|---|---|
| `GITHUB_TOKEN` | All commands | Used for HTTPS authentication when pushing/fetching the data branch. Falls back to git's default credential flow when unset. |
| `GITHUB_HEAD_REF` | `defrost exec` | Used to recover the source branch name when `git rev-parse --abbrev-ref HEAD` returns `HEAD` (detached-HEAD checkouts in CI). Recorded as `vcs.repository.ref.name` on the run. |
| `GITHUB_REF_NAME` | `defrost exec` | Same as above, used as a secondary fallback. |
| `GITHUB_PR_NUMBER` | `defrost exec` | Recorded as `vcs.repository.change.id` on the run if set. |

defrost **sets** in the child process started by `defrost exec`:

| Variable | Value | Purpose |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://127.0.0.1:<random-port>` | Points OTel SDKs in the child at the embedded receiver — captures traces, metrics, AND logs. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | Forces OTLP/HTTP protobuf (the only protocol the embedded receiver speaks). |

The embedded receiver is the upstream
`go.opentelemetry.io/collector/receiver/otlpreceiver` running in
library mode, so any default-configured OTel SDK (Python, Node, Go,
Rust, …) exports to it without extra wiring.

Any value the user already had for `OTEL_EXPORTER_OTLP_ENDPOINT` /
`OTEL_EXPORTER_OTLP_PROTOCOL` is overridden inside `defrost exec`. If you
need to forward to a separate collector, do it from defrost's storage
(e.g. by reading from the data branch), not by trying to set those vars.

## What defrost does **not** read

There is no `defrost.yml`, `defrost.toml`, `.defrostrc`, or any other
config file *yet*. The `<repo>/.defrost/` directory is reserved for
future committed config (e.g. `.defrost/config.toml`); right now only
`suppressions.json` lives there alongside the auto-generated
`.gitignore`.
