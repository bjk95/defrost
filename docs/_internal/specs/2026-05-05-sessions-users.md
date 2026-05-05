# Spec #9 — Sessions & users grouping

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation. Depends on [`#1 trace-tree-ui`](./2026-05-05-trace-tree-ui.md).
**Phase:** 5 — feedback + observability polish.

## 1. Goal

Group traces by `session.id` and `user.id`. Both are existing OTel resource attributes that defrost already captures (no schema change needed). This spec is pure UI surfacing: list sessions, view a session timeline, view a user's trace history, filter the heatmap by session or user.

## 2. Why this matters competitively

LangSmith and Langfuse both ship "Sessions" and "Users" as named features. They're surface UI on top of OTel attributes that any well-instrumented app already emits. Without these views, defrost users have no way to ask "what was Alice doing yesterday?" or "what's the conversation history of session X?" — even though the data is right there.

This is one of the cheapest wins in the spec batch: no new ingestion, no new storage, just new query patterns and new pages.

## 3. Public docs to write first

- **New page:** `docs/guides/sessions-and-users.md` — "Group traces by session or user." Sections: "Set `session.id` and `user.id` in your app", "Browse sessions", "User history", "PII considerations".
- **Edit:** `docs/reference/otel-ingestion.md` — document the resource attributes we read.
- **Edit:** `docs/reference/serve-api.md` — document new endpoints.

## 4. Storage layout

**No change.** Reads `session.id` and `user.id` from OTel resource attributes already persisted. The DuckDB cache projects resource attributes; this spec adds query patterns over already-indexed columns.

## 5. CLI surface

Lightweight read-only commands:

- `defrost sessions list [--limit <N>] [--user <id>] [--json]`
- `defrost session show <session-id> [--json]` — print all traces in the session, ordered.
- `defrost users list [--limit <N>] [--json]`
- `defrost user show <user-id> [--limit <N>] [--json]` — print recent traces by this user.

## 6. HTTP API surface

In `internal/serve/sessions.go`:

| Method + path | Returns |
|---|---|
| `GET /api/sessions?limit=&user=` | `{sessions: [{session_id, user_id?, run_count, first_at, last_at, total_cost_usd?}]}` |
| `GET /api/sessions/{session-id}` | `{session_id, user_id?, runs: [{run_id, trace_id, ts, status, duration_ms, cost_usd?}]}` |
| `GET /api/users?limit=` | `{users: [{user_id, run_count, first_at, last_at}]}` |
| `GET /api/users/{user-id}?limit=` | `{user_id, sessions: [...], runs: [...]}` |

Cache-Control: `public, max-age=60` for all (data updates with new runs).

### Querier extension

In `internal/query/iface.go`:

```go
type QuerierWithSessions interface {
    Querier
    SessionsList(filter SessionFilter) ([]SessionSummary, error)
    SessionRuns(sessionID string) ([]Run, error)
    UsersList(filter UserFilter) ([]UserSummary, error)
    UserRuns(userID string, limit int) ([]Run, error)
}
```

Same opt-in pattern as `QuerierWithLookup` and `QuerierWithTraceTree` — DuckDB impl satisfies; future hosted impls plug in or 500 with a clear message.

Implementation in `internal/query/duckdb/sessions.go`: SQL aggregations over the existing span resource-attribute projection.

## 7. Web UI surface

| File | Role |
|---|---|
| `web/src/pages/Sessions.tsx` (new) | `/sessions`. Table of sessions (most recent first). Click → session detail. |
| `web/src/pages/SessionDetail.tsx` (new) | `/sessions/:id`. Timeline of traces in this session, oldest-first. Each row is a small trace summary; click jumps to `/trace/<trace-id>`. |
| `web/src/pages/Users.tsx` (new) | `/users`. Table of users. |
| `web/src/pages/UserProfile.tsx` (new) | `/users/:id`. Recent sessions + recent traces. |
| `web/src/components/HeatmapFilter.tsx` (extended) | Add session / user filter chips to the existing heatmap. |

Empty-state message when no `session.id` resource attribute is found, with a link to `docs/guides/sessions-and-users.md` showing how to set it via OTel SDK.

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/query/duckdb/` | Add `sessions.go` next to existing query files. SQL over the existing `resource_attrs` projection. |
| `internal/serve/server.go` | Wire endpoints; impl in `internal/serve/sessions.go`. |
| `internal/cli/cli.go`, `wire.go` | Add `Sessions` / `Users` commands. |
| `web/src/components/Grid.tsx` | Existing heatmap to add filter chips to. |
| `web/src/components/RunCell.tsx` | When a session filter is active, dim cells that don't belong to the session. |

**New files only:**

- `internal/query/duckdb/sessions.go` (+ test)
- `internal/serve/sessions.go` (+ test)
- `internal/cli/sessions.go` (+ test)
- `web/src/pages/Sessions.tsx`, `SessionDetail.tsx`, `Users.tsx`, `UserProfile.tsx` plus tests.
- `docs/guides/sessions-and-users.md`

## 9. OTel emission

This feature is read-only. It does **not** emit telemetry. It surfaces existing OTel resource attributes:

- `session.id` (OTel SemConv reserved; `https://opentelemetry.io/docs/specs/semconv/general/session/`)
- `user.id` (OTel SemConv reserved)

If a producer also sets `enduser.id` (an older SemConv), treat it as a fallback for `user.id`.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/query/duckdb/sessions_test.go` | Given a fixture with multiple sessions and users, `SessionsList` / `SessionRuns` / `UsersList` / `UserRuns` return the expected aggregations. Filter by `user_id` works. Sort order is stable. |
| `internal/serve/sessions_test.go` | All endpoints. Pagination via `limit`. |
| `internal/cli/sessions_test.go` | `defrost sessions list --user alice` returns expected rows. |
| `web/src/pages/Sessions.test.tsx`, `SessionDetail.test.tsx`, `Users.test.tsx`, `UserProfile.test.tsx` | Render + fetch flows. Empty-state when no session attrs are present. |

## 11. Acceptance criteria

- The dashboard `/sessions` page lists sessions ordered most-recent first, with run counts and time bounds.
- Clicking a session opens a chronological timeline of its traces.
- The dashboard `/users` page lists users; clicking opens a profile with recent sessions and traces.
- The heatmap can be filtered by session or user.
- An empty-state message guides users on how to set `session.id` / `user.id` via OTel SDK when none are present.
- `defrost sessions list` / `defrost user show` from the CLI return the same data as the dashboard.
- Public docs at `docs/guides/sessions-and-users.md` exist and include OTel SDK examples.
- All test cases in §10 pass.

## 12. Open questions / decisions

- **PII concerns for `user.id` in self-host.** Many apps emit user emails directly. Recommendation: surface a config flag `DEFROST_HASH_USER_ID=true` (consumed by `internal/otlp/sink.go` at ingestion time) that hashes `user.id` to a stable but opaque value. Document the trade-off. Default off — opt in.
- **How wide a session?** Some apps treat a "session" as a single conversation; others as a multi-day persistent context. defrost has no opinion. The producer sets `session.id`; we group by it. Document this in the guide.
- **Cross-repo sessions.** A session that spans multiple deployments (e.g., a microservice fanout) won't be cross-repo-queryable in OSS. Hosted-tier territory. Acknowledge in the guide.
- **`enduser.id` deprecation.** OTel SemConv has shifted from `enduser.id` to `user.id`. Recommendation: read both, prefer `user.id`. Surface a deprecation hint in the dashboard if a producer is still emitting `enduser.id`.
