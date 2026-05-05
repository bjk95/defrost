# Spec #8 — Annotations & feedback

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation. Depends on [`#1 trace-tree-ui`](./2026-05-05-trace-tree-ui.md).
**Phase:** 5 — feedback + observability polish.

## 1. Goal

Human-in-the-loop scoring and feedback on captured runs. A reviewer can score a trace (or a specific span), leave a label and free-text comment, and have it persist on the data branch. An "annotation queue" view lists unscored runs ordered by need-review-most.

A reviewer can:

1. Click a span / trace, open an annotation panel, score and comment in one action.
2. See all prior annotations on the same span.
3. Open `/queue` to triage a backlog of unscored runs.

## 2. Why this matters competitively

Both LangSmith and Langfuse ship "annotation queues" and "user feedback" as explicit features. LangSmith's pricing model penalizes feedback (it auto-upgrades traces to a more expensive tier — see parent spec). defrost has no such penalty: annotations are JSON files on the data branch, free to add, free to query, and `git diff`-able like everything else.

This is also the natural pair with spec [`#3 datasets`](./2026-05-05-datasets.md): unscored rows captured from production become the queue. Scoring them turns the queue into a labeled dataset — closing the human-feedback loop.

## 3. Public docs to write first

- **New page:** `docs/guides/reviewing-runs.md` — "Score traces and build a labeled dataset." Sections: "Open a trace", "Annotate a span", "Use the annotation queue", "From annotations to datasets".
- **New page:** `docs/reference/cli/annotate.md` — full CLI reference.
- **New concept page:** `docs/concepts/annotations.md` — the relationship between annotations, feedback, datasets, and judges.
- **Edit:** `docs/reference/storage-layout.md` — add `feedback/` to the directory tree.
- **Edit:** `docs/reference/serve-api.md` — document new endpoints.

## 4. Storage layout

On the data branch:

```text
<repo>/.defrost/feedback/
└── <trace-id>.jsonl       # append-only feedback events for this trace
```

Each line is one feedback event:

```json
{
  "id": "<uuid>",
  "trace_id": "<hex>",
  "span_id": "<hex|null>",
  "score": 0.7,
  "label": "off-topic",
  "comment": "The reply ignored the user's specific question.",
  "by": "alice@example.com",
  "at": "2026-05-05T12:00:00Z",
  "kind": "human"
}
```

- `score` is a float `[0, 1]`. Empty if the reviewer chose label-only.
- `label` is a string. Free-form in v1; a future spec can add per-project label vocabularies.
- `kind` is `"human"` for manual annotations, `"feedback"` for end-user thumbs-up/down (reserved for future), `"judge"` is **NOT** used here — judge scores live on the trace span itself per spec #4.
- Append-only. Editing an annotation is a new event with `id` referencing the prior. Deleting is a new event with `kind = "retract"` referencing the prior — never erasing the line. (`git revert` on the file is the escape hatch for true mistakes.)

`<trace-id>.jsonl` files are written atomically (write `.tmp`, fsync, rename, fsync parent — same pattern as trace files).

## 5. CLI surface

`defrost annotate` parent command. Subcommands:

### `defrost annotate <trace-id> [--span <span-id>] --score <0-1> [--label <s>] [--comment <s>]`

Append a feedback event. Reviewer identity defaults to `git config user.email` (with a fallback to `$USER@<hostname>`). Override with `--by <email>`.

### `defrost feedback list [--trace <id>] [--by <email>] [--label <s>] [--json]`

Print feedback events. Default: NDJSON to stdout. Filters compose with AND.

### `defrost feedback queue [--limit <N>] [--unscored-only] [--json]`

Print runs that need review. Sorted by oldest-first within the cap. `--unscored-only` excludes traces with any human feedback.

## 6. HTTP API surface

In `internal/serve/feedback.go`:

| Method + path | Returns |
|---|---|
| `POST /api/trace/{trace-id}/feedback` | Body: `{span_id?, score?, label?, comment?}`. Appends an event. Reviewer identity from request body or session-set "Reviewer" header. Returns 201 + the new event. |
| `GET /api/trace/{trace-id}/feedback` | `{events: [...]}` — all feedback events for this trace. |
| `GET /api/feedback?trace_id=&by=&label=` | Filtered feed. |
| `GET /api/feedback/queue?limit=50&unscored_only=true` | Returns runs ordered by triage priority. |
| `DELETE /api/trace/{trace-id}/feedback/{event-id}` | Appends a `kind=retract` event referencing `<event-id>`. Returns 200 + the retract event. Does NOT delete the original line — uses the retract pattern. |

Cache-Control:

- `GET /api/trace/{trace-id}/feedback` → `public, max-age=10` (mutable on annotation).
- `GET /api/feedback/queue` → `public, max-age=10`.
- POSTs / DELETEs → no cache.

## 7. Web UI surface

| File | Role |
|---|---|
| `web/src/components/AnnotationPanel.tsx` (new) | Right-side panel attached to `SpanDetail` (spec #1). Shows existing annotations + an inline "Add annotation" form (score slider, label input, comment textarea). |
| `web/src/components/FeedbackChip.tsx` (new) | Small chip on a span row (in `TraceTree`) when one or more feedback events exist. Hover: see latest. |
| `web/src/pages/Queue.tsx` (new) | `/queue`. Table of unscored runs with quick-score buttons. Keyboard-driven (j/k to move, 0–9 to score, l for label, c for comment). |
| `web/src/components/ReviewerIdentity.tsx` (new) | Top-nav widget. On first use, prompts the reviewer for an email; persists to localStorage; sends as `Reviewer` header on every request from then on. |

App.tsx route: `/queue`.

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/persist/persist.go` | Same clone-mutate-push pattern. Add `AppendFeedback(traceID string, event FeedbackEvent, msg string) error`. |
| `internal/serve/server.go` | Wire endpoints; impl in `internal/serve/feedback.go`. |
| `internal/cli/cli.go`, `wire.go` | Add `Annotate` and `Feedback` commands. |
| `web/src/components/SpanDetail.tsx` (spec #1) | Mount `AnnotationPanel`. |
| `web/src/components/TraceTree.tsx` (spec #1) | Render `FeedbackChip` on annotated spans. |
| `web/src/components/AddToDatasetButton.tsx` (spec #3) | Reuse pattern for the queue's "promote to dataset" button. |

**New files only:**

- `internal/feedback/` — `event.go`, `event_test.go`, `queue.go`, `queue_test.go`. Pure logic.
- `internal/persist/feedback.go` (+ test)
- `internal/serve/feedback.go` (+ test)
- `internal/cli/annotate.go` (+ test); `internal/cli/feedback.go` (+ test)
- `web/src/components/AnnotationPanel.tsx`, `FeedbackChip.tsx`, `ReviewerIdentity.tsx` plus tests.
- `web/src/pages/Queue.tsx` (+ test)
- Public docs as listed in §3.

## 9. OTel emission

`POST /api/trace/{trace-id}/feedback` emits one span:

```text
span.name       = "defrost.feedback.append"
span.attributes = {
  "defrost.feedback.trace_id":  <hex>,
  "defrost.feedback.event_id":  <uuid>,
  "defrost.feedback.kind":      "human",
  "defrost.feedback.has_score": <bool>,
  "defrost.feedback.has_label": <bool>,
  "defrost.feedback.by":        <reviewer email>,
}
```

This makes annotation activity itself observable. Useful for tracking reviewer throughput.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/feedback/event_test.go` | JSON round-trip; UUID generation; retract pattern preserves history. |
| `internal/feedback/queue_test.go` | Queue ordering: oldest-first, unscored-only filter, `limit` honored. |
| `internal/persist/feedback_test.go` | Append produces a new commit; concurrent appends to the same file converge under retry. |
| `internal/serve/feedback_test.go` | All endpoints. POST with malformed body → 400 with clear error. DELETE produces a retract event, not a line removal. |
| `internal/cli/annotate_test.go` | `defrost annotate <trace-id> --score 0.5 --comment "..."` end-to-end. |
| `web/src/components/AnnotationPanel.test.tsx`, `FeedbackChip.test.tsx`, `ReviewerIdentity.test.tsx` | Render + form submission flows. |
| `web/src/pages/Queue.test.tsx` | Keyboard navigation. Quick-score writes via POST and updates the row. |

## 11. Acceptance criteria

- A reviewer can score a trace or a span from the dashboard, with the result persisting on the data branch.
- Existing annotations show in `AnnotationPanel` ordered newest-first.
- A `FeedbackChip` appears on annotated span rows in the trace tree.
- `/queue` shows unscored runs and supports keyboard-driven triage.
- `defrost annotate` from the CLI produces the same on-disk event as the dashboard would.
- Retract is a new event, never a line deletion. Original annotations remain in git history.
- `git diff main..feature -- .defrost/feedback/` shows annotation evolution per PR.
- Public docs at `docs/guides/reviewing-runs.md`, `docs/concepts/annotations.md`, `docs/reference/cli/annotate.md` exist.
- All test cases in §10 pass.

## 12. Open questions / decisions

- **Reviewer identity in OSS self-host.** Recommendation: read `git config user.email` for CLI; for the dashboard, `ReviewerIdentity.tsx` prompts on first use and persists to localStorage. Document explicitly that this is honor-system in the OSS tier; auth is hosted-tier territory.
- **Per-project label vocabularies.** Out of scope for v1. Add later if free-text labels prove unwieldy.
- **End-user "thumbs up/down" feedback.** Important for production apps but requires a programmatic POST endpoint with rate limiting and probably auth. Defer; spec separately when demand surfaces.
- **Retract vs delete.** Recommendation: retract pattern as specified. Annotation provenance matters for audit and for ML training data quality. Don't allow erasure.
- **Promote queue items to datasets.** Mentioned as a button in §7 (`AddToDatasetButton`). Recommendation: ship in v1 — it's the workflow-completing affordance.
- **Pairwise human eval.** "Which output is better, A or B?" UI. Recommendation: defer — implementable atop `/compare` (spec #5) + this spec's POST endpoint, but the UI is its own design problem.
