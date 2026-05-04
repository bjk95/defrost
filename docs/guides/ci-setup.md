---
title: 'CI setup'
---

defrost is designed for CI: every job that runs tests records to the
same data branch, history accumulates in the repo, no external service
required.

This guide covers GitHub Actions. Other CI systems work the same way —
the only requirements are an authenticated git remote and the
`defrost` binary on `PATH`.

## Minimal GitHub Actions job

```yaml
name: tests

on:
  pull_request:
  push:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    permissions:
      contents: write    # required: defrost pushes the _defrost branch
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: stable

      - name: Install defrost
        run: go install github.com/bjk95/defrost@latest

      - name: Run tests under defrost
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: defrost exec go test ./...
```

That's the whole setup. After the first run merges, `_defrost` exists
on `origin` and every subsequent CI run appends to it.

## What the env vars do

defrost reads four GitHub-related env vars (all set automatically by
GitHub Actions, no config needed):

| Variable | Effect |
|---|---|
| `GITHUB_TOKEN` | Used to push the data branch over HTTPS. The default `secrets.GITHUB_TOKEN` works as long as the workflow has `contents: write`. |
| `GITHUB_HEAD_REF` | Recovers the source branch name on PR runs (which check out in detached-HEAD mode). |
| `GITHUB_REF_NAME` | Secondary fallback for the branch name. |
| `GITHUB_PR_NUMBER` | Recorded as `vcs.repository.change.id` on the run. Set this from the workflow context if you want it on PR runs:<br>`GITHUB_PR_NUMBER: ${{ github.event.pull_request.number }}` |

See [Configuration](../reference/configuration.md#environment-variables)
for the full list.

## Parallel jobs

Multiple CI jobs writing concurrently is supported. Each run lands in
a different file path (`traces/<date>/<trace-id>.otlp.pb.zst`), and
defrost's `.gitattributes` declares those paths as `merge=union`, so
fan-out matrices like:

```yaml
strategy:
  matrix:
    py: ["3.11", "3.12", "3.13"]
```

…all push without colliding.

`suppressions.json` does **not** use `merge=union` (order matters).
Concurrent suppression mutations resolve via fetch-rebase-retry. In
practice this only matters if you're calling `defrost suppress` from
multiple CI jobs at the same time, which is unusual.

## What about pull requests from forks?

GitHub Actions does not give `secrets.GITHUB_TOKEN` write permission
on `pull_request` events triggered from forked repos — that's a
platform-level restriction, not a defrost one. Two options:

- **Use `pull_request_target`** with a checkout of the merge commit.
  Standard caveats apply (do not run untrusted code with elevated
  permissions).
- **Run with `--no-persist` on fork PRs** so defrost still reports
  results to the job log without trying to push:

  ```yaml
  - run: |
      if [[ "${{ github.event.pull_request.head.repo.fork }}" == "true" ]]; then
        defrost exec --no-persist go test ./...
      else
        defrost exec go test ./...
      fi
  ```

## Recording from any CI system

The pattern is the same anywhere:

1. Check out the repo, including write access to push to it.
2. Install the `defrost` binary.
3. Wrap your existing test/eval command with `defrost exec`.
4. (Optional) Set whichever env vars apply (`GITHUB_PR_NUMBER` if your
   CI exposes a PR number under a different name, etc.).

There is no defrost-specific service to deploy and no API key to
configure. If your test command runs and your job can `git push`, you
have a working defrost setup.
