---
title: 'Configuration'
---

defrost resolves each setting through a four-level precedence chain:

```
command-line flag  >  environment variable  >  .defrost.toml  >  built-in default
```

## `.defrost.toml`

Place `.defrost.toml` at the repo root (the directory containing `.git`).
The file is optional — a missing file is silently ignored. Unknown keys
produce a warning. Type errors (e.g. `port = "abc"`) are fatal.

```toml
# .defrost.toml
repo-dir    = "."          # path to the git repo (default: current dir)
data-branch = "_defrost"   # branch where results live
dev         = false        # local-only mode (no push to origin)

[serve]
port = 6969                # port for `defrost serve`
```

## Global flags

These flags are **global** — they must appear before the subcommand:

```sh
defrost --repo-dir=. exec go test ./...    # correct
defrost exec --repo-dir=. go test ./...    # wrong — --repo-dir ignored
```

| Flag | Short | Default | Env var | Description |
|---|---|---|---|---|
| `--repo-dir <path>` | `-C` | `.` | `DEFROST_REPO_DIR` | Path to the git repo defrost reads from / writes to. The local `.defrost/` tree is rooted here. |
| `--data-branch <name>` | | `_defrost` | `DEFROST_DATA_BRANCH` | Branch name where results live. Override only if you have a naming collision; the dashboard, history, suppress, and drop commands all share this default. |
| `--dev` | `-d` | `false` | `DEFROST_DEV` | Local-only mode: write OTLP files to `<repo-dir>/.defrost/` (same path as prod mode) but skip the push to origin's data branch. Reads also come from the local tree. |
| `--no-color` | | `false` | `NO_COLOR` | Disable colour output regardless of TTY. |
| `--verbose` | `-v` | `0` | | Increase log verbosity. Repeat (`-vv`) for more detail. |
| `--quiet` | `-q` | `false` | | Suppress informational output. |
| `--version` | `-V` | | | Print version and exit. |

`--no-persist` is `defrost exec`-only and runs the OTel receiver without
recording anything.

## Environment variables

defrost **reads**:

| Variable | When | Effect |
|---|---|---|
| `DEFROST_REPO_DIR` | All commands | Sets `--repo-dir` when the flag is not provided. |
| `DEFROST_DATA_BRANCH` | All commands | Sets `--data-branch` when the flag is not provided. |
| `DEFROST_DEV` | All commands | Enables `--dev` when the flag is not provided. |
| `DEFROST_SERVE_PORT` | `defrost serve` | Sets the default port when `--port` is not provided. |
| `GITHUB_TOKEN` | All commands | Used for HTTPS authentication when pushing/fetching the data branch. Falls back to git's default credential flow when unset. |
| `GITHUB_HEAD_REF` | `defrost exec` | Used to recover the source branch name when `git rev-parse --abbrev-ref HEAD` returns `HEAD` (detached-HEAD checkouts in CI). Recorded as `vcs.repository.ref.name` on the run. |
| `GITHUB_REF_NAME` | `defrost exec` | Same as above, used as a secondary fallback. |
| `GITHUB_PR_NUMBER` | `defrost exec` | Recorded as `vcs.repository.change.id` on the run if set. |
| `NO_COLOR` | All commands | Disables colour output regardless of TTY. |
| `FORCE_COLOR` | All commands | Enables colour output even when stdout is not a TTY. Takes effect only when `NO_COLOR` is unset. |

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
