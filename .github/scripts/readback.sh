#!/usr/bin/env bash
# Readback validation for defrost CI integration jobs.
#
# Reads `out.txt` for defrost's `persisted: trace_id=<hex>, ...` line,
# fetches the data branch from origin, and asserts the trace's
# per-signal files actually landed at the branch root. This is a
# stronger check than greping defrost's stderr summary for counts —
# it catches:
#
# - silent persist failures (the warn-not-fail path in `defrost exec`),
# - path-prefix bugs (writing under data/ instead of root),
# - corrupted/empty files (size > 0 minimum),
# - logs the receiver collected but the writer dropped (the third-
#   signal regression we just fixed).
#
# Usage:
#
#   readback.sh <out.txt> <branch> [traces|metrics|logs]...
#
# Example (the python job's call):
#
#   readback.sh out.txt _defrost traces metrics logs
#
# Asserts: <branch> on origin contains traces/<YYYY>/<MM>/<DD>/<id>.otlp.pb.zst
# (and similar for any other signals listed) where <id> is the trace_id
# defrost emitted, all non-empty.
set -euo pipefail

OUT="${1:?usage: readback.sh <out.txt> <branch> <signal>...}"
BRANCH="${2:?usage: readback.sh <out.txt> <branch> <signal>...}"
shift 2
SIGNALS=("$@")
if [ ${#SIGNALS[@]} -eq 0 ]; then
  echo "readback: no signals to check; pass at least one of: traces metrics logs" >&2
  exit 2
fi

# Pull the trace_id from defrost's 'persisted:' summary. There may be
# multiple persisted lines on a single CI run (if the test invokes
# defrost more than once); take the last one.
trace_id=$(grep -E '^defrost: persisted:' "$OUT" \
  | tail -1 \
  | sed -nE 's/.*trace_id=([a-f0-9]+).*/\1/p')

if [ -z "$trace_id" ]; then
  echo "readback FAIL: no 'defrost: persisted: trace_id=<hex>' line in $OUT" >&2
  echo "  (this means persist failed or the binary is too old to emit trace_id)" >&2
  echo "  --- last 30 lines of $OUT ---" >&2
  tail -30 "$OUT" >&2 || true
  exit 1
fi
echo "readback: trace_id=$trace_id, branch=$BRANCH"

git fetch --quiet origin "$BRANCH"

# Cache the tree listing once. `git ls-tree -r --name-only <ref>`
# returns just the paths.
all_paths=$(git ls-tree -r --name-only "origin/$BRANCH")

rc=0
for signal in "${SIGNALS[@]}"; do
  case "$signal" in
    traces|metrics|logs) ;;
    *) echo "readback: unknown signal '$signal' (want: traces|metrics|logs)" >&2; exit 2 ;;
  esac
  # Match anywhere under the signal directory (we don't assume the
  # exact YYYY/MM/DD partition).
  matches=$(echo "$all_paths" | grep -E "^${signal}/[0-9]{4}/[0-9]{2}/[0-9]{2}/${trace_id}\.otlp\.pb\.zst$" || true)
  if [ -z "$matches" ]; then
    echo "readback FAIL: ${signal}/<...>/${trace_id}.otlp.pb.zst NOT on origin/${BRANCH}" >&2
    rc=1
    continue
  fi
  # Size check: zstd-OTLP files for our test runs are reliably > 100B.
  # An empty/zero-byte file would slip past `git ls-tree` but fail
  # this guard.
  size=$(git cat-file -s "origin/${BRANCH}:${matches}")
  if [ "$size" -lt 50 ]; then
    echo "readback FAIL: $matches is suspiciously small ($size bytes)" >&2
    rc=1
    continue
  fi
  echo "readback OK:   $matches  ($size bytes)"
done

exit "$rc"
