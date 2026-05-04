---
title: 'Persist failed'
---

You saw something like this on your terminal:

```text
⚠️  defrost: persist failed — this run was NOT pushed to origin.
   The test command's exit code is preserved.
   Cause: <some git error>
   What to do: https://bjk95.github.io/defrost/guides/troubleshooting/persist-failed/
```

This means: your tests ran, results were captured, but defrost couldn't
push the trace/metric/log files to the `_defrost` branch on origin.
The test command's exit code is unchanged — defrost will not turn a
green build red just because a push failed. The data for *this* run
is gone (it lived in a temp directory that's been cleaned up). Future
runs will keep working.

This page walks through the most common causes and what to do.

## What just happened

For each `defrost exec` run, defrost:

1. Captures test results, metrics, and logs locally.
2. Clones the `_defrost` branch into a temp directory.
3. Writes the captured data as canonical OTLP files there.
4. Commits and pushes.

The error above means step 3 or step 4 failed. The temp directory has
already been cleaned up. The run isn't queued anywhere — it's gone.
That's OK: the next `defrost exec` will create new files, and your
existing history on the data branch is untouched.

If losing this one run is unacceptable for your case, you can re-run
the same test command — defrost is idempotent at the framework level
(test results don't depend on the previous run's persistence).

## Common causes

### 1. No `origin` remote

```text
Cause: no 'origin' remote configured
```

Defrost pushes to `origin/_defrost`. If your repo has no `origin`
remote, persistence has nowhere to go.

**Fix:** add an `origin` remote, or pass `--dev` to write only to
`<repo>/.defrost/` without pushing:

```sh
git remote add origin git@github.com:you/your-repo.git
# OR
defrost exec --dev go test ./...
```

### 2. Authentication failure

```text
Cause: ... fatal: Authentication failed for 'https://github.com/...'
Cause: ... Permission denied (publickey)
```

The git credentials your shell has don't allow pushing to `origin`.

**Fix:** ensure `git push origin HEAD` works in the same shell. For
HTTPS remotes, set `GITHUB_TOKEN` or configure git's credential
helper. For SSH remotes, ensure `ssh -T git@github.com` succeeds.

In CI:

```yaml
- run: defrost-ci exec go test ./...
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

…and the workflow needs `permissions: contents: write` (or higher).
See [CI setup](../ci-setup/) for the full template.

### 3. Branch protection rejects the push

```text
Cause: ... protected branch hook declined
Cause: ... GH013: Repository rule violations found
```

GitHub / GitLab branch-protection rules can be configured to require
PRs, signed commits, status checks, or review approvals before
accepting any push to a branch — including `_defrost`.

**Fix:** exempt `_defrost` from your branch protection rules, OR
create a rule that allows defrost's bot identity
(`defrost[bot] <defrost[bot]@users.noreply.github.com>`) to push
directly to it.

For GitHub:

- Settings → Branches → Add rule → Branch name pattern: `_defrost`
- *Don't* enable "Require a pull request before merging"
- *Don't* enable "Require signed commits" (defrost commits are
  unsigned bot commits)
- *Don't* enable "Require status checks"

### 4. Push race exhausted retries

```text
Cause: push failed after 5 retries: ...
```

Two `defrost exec` invocations both finished at the same time and
raced to push. Defrost retries up to 5 times with a fetch+rebase
between attempts; if all 5 fail, the run is dropped.

**Fix:** this should be very rare in practice — collisions on
`refs/heads/_defrost` only matter if multiple jobs push within a few
hundred milliseconds. If you see this often, you have an unusual
workload (e.g. CI matrix with hundreds of parallel jobs all pushing
their results within a single second). Open an issue with details.

### 5. Network failure

```text
Cause: ... could not resolve host
Cause: ... Connection timed out
Cause: ... TLS handshake failed
```

Transient network issues between you and `origin`.

**Fix:** retry the test command. If the issue persists, check your
network / VPN / proxy. Defrost uses `git` directly, so anything that
makes `git fetch origin` work will make defrost work.

### 6. Disk full / permission denied

```text
Cause: ... no space left on device
Cause: ... permission denied
```

Defrost uses `os.MkdirTemp` for the staging clone. If `$TMPDIR` (or
`/tmp` on most systems) is full or unwritable, the clone fails before
files can be written.

**Fix:** free space in `/tmp` (or whichever directory `$TMPDIR`
points at), or set `TMPDIR=<dir-with-space>` before running defrost.

## How to recover the missing run

You can't, directly — the captured data was in a temp directory that's
been cleaned up. But:

- **Subsequent runs are unaffected.** Your `_defrost` branch still
  has every previously-pushed run; the next successful `defrost exec`
  appends to it.
- **You can re-run the same command** to capture fresh results. They
  won't be byte-identical to the lost run (timestamps differ) but
  they capture the same code state.
- **For CI builds where every run matters**, fix the underlying push
  failure cause first, then re-run the workflow. Most CI systems
  expose a "re-run failed jobs" button.

## When this might be intentional

If you're running `defrost exec` against a fork's PR that doesn't
have write access to the upstream repo, persist failure on every CI
run is expected. The standard mitigation is to use `--no-persist` on
fork runs:

```yaml
- run: |
    if [[ "${{ github.event.pull_request.head.repo.fork }}" == "true" ]]; then
      defrost-ci exec --no-persist go test ./...
    else
      defrost-ci exec go test ./...
    fi
```

`--no-persist` skips the OTLP receiver and the push entirely — no
data is captured, no warning is emitted. Use it when you know the
push will fail and don't care.

## Why defrost doesn't fail the run

A persist failure is a defrost failure, not a test failure. The user
ran tests; the tests passed (or failed) on their merits. Failing the
build because we couldn't push the test history would mask the actual
test result and create a CI flake that has nothing to do with the
code under test.

If your workflow needs every run to land on the data branch, treat
persist failures as a follow-up to investigate after the build —
don't conflate "did my tests pass" with "did defrost record this
correctly."
