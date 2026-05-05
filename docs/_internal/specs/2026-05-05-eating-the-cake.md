# Eating the cake — defrost vs LangSmith and Langfuse

**Date:** 2026-05-05
**Status:** Strategy & roadmap. Parent doc for the build specs in this directory dated 2026-05-05.
**Audience:** internal contributors. Public docs stay neutral — see §8.

## 1. TL;DR

defrost's wedge is six bullets long:

- **OTel-native.** The wire protocol is OTLP. Any language with an OTel SDK is already a defrost client.
- **Git-native.** No database to operate. `<repo>/.defrost/` is the storage; replication, auth, encryption, and backup are inherited from the user's git host.
- **$0.** No per-trace tax, no per-seat tax, no retention tax. Self-host is the default, not the upsell.
- **No SDK lock-in.** Migrate in or out by changing `OTEL_EXPORTER_OTLP_ENDPOINT`.
- **History travels with code.** Eval diffs are PR-diffable. `git diff main..feature -- .defrost/` shows exactly which traces, scores, and metrics moved.
- **We grow with you.** Git is the right answer up to ~GB-scale history. Beyond that, the architecture already pre-figures a swap-in (`query.Querier` in `internal/query/iface.go` was designed for it). When users outgrow git, we'll build the next tier *with them* — not abandon them at the limit.

That last bullet is a commitment, not a caveat. See §7.

## 2. Why the incumbents are vulnerable

### LangSmith — the SaaS pricing story

- **$39/seat/mo + $0.50 per 1k base traces.** Five engineers cost $195/mo before any traces ingest.
- **14-day default retention.** Extending to 400 days **doubles** the per-trace price.
- **Feedback / annotations silently auto-upgrade traces** to a more expensive tier — the more your team annotates, the more you pay.
- **No self-host below Enterprise.** No middle ground between SaaS and a sales call.
- **Closed source.** No code review of the storage layer, no air-gap option.
- Customers routinely sample down to **0.1%** of traffic to control cost — i.e., they're paying for an observability product that they then choose to be 99.9% blind for.

### Langfuse — the operational complexity story

Langfuse already won the "OSS LangSmith alternative" narrative. Where they're vulnerable is *operations*:

- **Self-host stack: ClickHouse + Postgres + Redis + S3/blob + ≥2 containers.** Co-founder publicly concedes ClickHouse adds ops anxiety.
- "Easy" path is Docker Compose on a VM, only fine for ≤1M traces/mo. Production scale needs Helm or Terraform.
- **EE features paywalled in self-host.** SSO, RBAC, scheduled exports, UI customization sit behind a $300/mo Teams add-on or $2,499/mo Enterprise tier.
- **Cloud Hobby:** 50k units/mo, 30-day retention, 2 users — and the data lives in their cloud.
- **Doesn't travel with code.** No PR diffs, no git history, no `git blame` on a regression in eval scores.

## 3. Feature map ranked by popularity

LangSmith and Langfuse have converged on the same feature surface. Rank applies to both unless noted.

### Tier S — the reason people log in

These are the features users encounter on their first session. Without them defrost doesn't get a demo.

1. **Trace tree visualization** — waterfall + nested spans + span detail panel.
2. **Datasets** — capture-from-trace → versioned test set → re-run.
3. **Evaluations** — LLM-as-judge, heuristic, custom evaluators.
4. **Prompt management + Playground** — version, edit, compare across models.

### Tier A — heavy use, but not the first-login feature

5. **Annotation queues / scores / user feedback** — structured human feedback workflow.
6. **Cost & token tracking** — auto-surfaced in UI.
7. **Monitoring / time-series analytics** — latency, error rate, cost over time.
8. **Pairwise comparison** — A/B prompts, A/B models.

### Tier B — enterprise / nice-to-have

9. **Deployments / hosted serving** — LangGraph Cloud territory. *Skip.* defrost is observability + eval, not orchestration.
10. **RBAC, orgs, SSO** — Langfuse paywalls these; LangSmith reserves them for Enterprise. defrost's answer: hosted-tier territory (§7).
11. **Shared / public datasets, prompt sharing across orgs** — hosted-tier territory.

### Tier B+ — Langfuse-specific

- **Server-side LLM-as-judge.** Langfuse runs judges as a managed service. defrost matches with a CLI / library that emits standard OTel — see spec [`2026-05-05-llm-as-judge.md`](./2026-05-05-llm-as-judge.md).
- **Sessions / users grouping.** Already representable in our OTel pipeline via `session.id` / `user.id` resource attributes. UI surfacing only — see spec [`2026-05-05-sessions-users.md`](./2026-05-05-sessions-users.md).

## 4. Gap matrix

| Feature | defrost today | LangSmith | Langfuse | Verdict |
|---|---|---|---|---|
| OTel ingestion (any language) | ✅ embedded OTLP receiver (`internal/otlp/`) | ⚠️ SDK preferred | ✅ OTel + SDKs | parity |
| Storage backend | ✅ git, zstd OTLP protobuf | proprietary | ClickHouse + Postgres + Redis + S3 | **defrost wins on simplicity** |
| Ops burden (self-host) | **zero** (git only) | n/a (SaaS) | ≥4 services + ops | **defrost wins** |
| Trace tree UI | ⚠️ heatmap only (`web/src/`) | ✅ best-in-class | ✅ very good | **biggest UX gap** |
| Cost / token surfacing | ⚠️ data captured, not shown | ✅ first-class | ✅ first-class | UI gap only |
| Datasets | ❌ none | ✅ versioned | ✅ versioned | **gap** |
| Evals (test runners) | ✅ Go, pytest, Jest, Vitest, Inspect AI, Promptfoo | partial | partial | **defrost wins on breadth** |
| LLM-as-judge primitive | ❌ | ✅ | ✅ (server-side) | gap |
| Prompt management | ❌ | ✅ | ✅ + caching | gap |
| Playground | ❌ | ✅ | ✅ | gap |
| Annotations / feedback | ❌ | ✅ | ✅ (scores) | gap |
| Pairwise comparison | ❌ | ✅ | ✅ | gap |
| Sessions / users grouping | ⚠️ via OTel attrs, no UI | ✅ | ✅ | UI gap only |
| Monitoring dashboards | ⚠️ basic charts | ✅ rich | ✅ rich | partial |
| PR-diffable evals (history travels with code) | ✅ unique | ❌ | ❌ | **defrost wins, structural** |
| Air-gapped / offline CI | ✅ unique | ❌ | ⚠️ heavy stack | **defrost wins** |
| Slim CI binary (`defrost-ci`) | ✅ unique | ❌ | ❌ | **defrost wins** |
| Open source | ⚠️ license unset (see §8) | ❌ closed | ✅ MIT | **must license to match** |
| Per-trace pricing | $0 | $0.50/1k + extras | $0 self-host / $8 per 100k cloud | parity vs Langfuse self-host, win vs the rest |
| SSO / RBAC | ❌ | Enterprise only | EE add-on ($300/mo) | gap (hosted-tier) |
| Cross-project rollups | ❌ per-repo | ✅ | ✅ | hosted-tier territory |

## 5. Structural advantages

### vs LangSmith — they cannot match without rebuilding

1. **Pricing.** $0 forever for self-host. No per-trace tax, no retention tax, no surprise tier upgrades. Sample at 100%.
2. **OSS** (once licensed — see §8). No closed-source review. No vendor risk.
3. **No SDK lock-in.** OTel is the wire format.

### vs Langfuse — the unique wedge

4. **Zero-ops storage.** Git is the database. No ClickHouse, no Postgres, no Redis, no S3, no Helm chart. The "5-minute Docker Compose" path Langfuse markets is still 5 minutes more than `git init`.
5. **History travels with code.** Eval regressions are `git blame`-able. PR diffs include score diffs.
6. **Air-gapped CI.** No outbound network, no DB container. Critical for regulated environments.
7. **Slim CI variant.** `defrost-ci` is ~1/3 the size — no DuckDB, no web bundle.

### vs both

8. **Test-runner breadth.** Go, pytest, Jest, Vitest, Inspect AI, Promptfoo already shipped. Both incumbents leave most of these to the user.
9. **Single static binary install.** No package manager, no service mesh, no migration path.

## 6. Roadmap

Each phase delivers a credible demo against the incumbents. Specs are numbered #1–#10; each is self-contained and dispatchable to a sub-agent. Phase order is build order; specs within a phase can be parallelised.

| Phase | Specs | Demo deliverable |
|---|---|---|
| 1 — close the trace UX gap | [`#1 trace-tree-ui`](./2026-05-05-trace-tree-ui.md), [`#2 cost-token-surfacing`](./2026-05-05-cost-token-surfacing.md) | "I can debug a 500-span trace with token + cost on every row." |
| 2 — datasets primitive | [`#3 datasets`](./2026-05-05-datasets.md) | "I capture interesting prod traces into a versioned dataset and re-run." |
| 3 — evals as first-class | [`#4 llm-as-judge`](./2026-05-05-llm-as-judge.md), [`#5 pairwise-comparison`](./2026-05-05-pairwise-comparison.md) | "I score outputs with an LLM judge and compare runs side-by-side." |
| 4 — prompts + playground | [`#6 prompt-management`](./2026-05-05-prompt-management.md), [`#7 playground`](./2026-05-05-playground.md) | "I edit a prompt in the dashboard, run it across three models, and the run shows up in the trace tree." |
| 5 — feedback + observability polish | [`#8 annotations-feedback`](./2026-05-05-annotations-feedback.md), [`#9 sessions-users`](./2026-05-05-sessions-users.md), [`#10 monitoring-dashboards`](./2026-05-05-monitoring-dashboards.md) | "I review last week's bad runs in an annotation queue, and I have prod monitoring on cost / latency / error rate." |

After Phase 5, defrost has feature parity on the Tier S + Tier A surface from §3. Tier B (RBAC, cross-project rollups) is hosted-tier territory.

## 7. Scaling story — grow with you

Git as a database has a ceiling: practically, ~1–2 GB of accumulated history per repo before push frequency, fetch cost, and dashboard load times start hitting protocol limits (this is documented in [`docs/reference/storage-layout.md`](../../reference/storage-layout.md)). That ceiling is real.

The framing is **not** "this is the cap, take it or leave it." The framing is:

> Git is what we focus on now. Git is the right answer for almost every team that doesn't already operate ClickHouse and isn't planning to. **When you outgrow git, we will build the next tier with you** — clustered storage, server-side query, multi-tenant data branches, auth, RBAC, billing. The architecture is already pre-figured for it.

Concrete pre-figuring already in the codebase:

- `internal/query/iface.go` defines a `query.Querier` interface — `internal/query/duckdb/` is one impl. The package comment in `internal/serve/server.go` states explicitly: *"a hosted-defrost ClickHouse implementation will plug in here without changes."*
- All read paths go through that interface. None of the dashboard code knows about git or DuckDB.
- All write paths go through `internal/persist/`. A hosted impl plugs in there.
- OTLP ingestion (`internal/otlp/`) is already wire-compatible with any storage backend.

**Sketches of the hosted tier (design only, NOT in scope here):**

- **Multi-tenant data branches.** One git remote per org/project. Or a hosted-only ClickHouse-backed `query.Querier` with the same wire shape. Either way, no breaking change for self-host users.
- **Server-side query API.** Hosted clusters expose `/api/query` for cross-project rollups, retention windowing, etc. Self-host stays local-DuckDB.
- **Auth/RBAC.** Layered above the existing handlers (middleware, not a rewrite). Self-host: passthrough (single-user, loopback).
- **Billing.** Per-org, usage-based on traces stored × retention days. Mirror Langfuse Pro's $199/mo for an unlimited-seats baseline so we don't get out-priced.

These bullets are scaffolding for the conversation when users hit the git ceiling — not a v1 work item. Spec future hosted-tier work in its own dated spec when the conversation becomes real.

## 8. Blockers

### License is unset

defrost has no `LICENSE` file at the repo root. Until that's fixed, we can't credibly market as the "OSS alternative" — Langfuse will out-position us on day one with their MIT license. **Recommendation: Apache 2.0** (preferred for the patent grant). MIT is acceptable if matching Langfuse exactly is the priority.

This blocks marketing, not engineering. Specs #1–#10 can be built before the license lands.

### Public-docs guardrails

The user-facing docs at `docs/index.md`, `docs/concepts/`, `docs/guides/`, `docs/reference/` must stay neutral. **Never name LangSmith or Langfuse** in public docs. Public docs lead with:

- OTel-native ingestion (works with anything that emits OTLP)
- Git-native storage (no DB to operate)
- $0, self-host by default
- History travels with code

Migration / comparison pages may be added later as `docs/guides/coming-from-langsmith.md` and `docs/guides/coming-from-langfuse.md`. Those are out of scope for the current spec batch.

### Heatmap deprecation path

The current `Grid` view in `web/src/components/Grid.tsx` is the test-run heatmap. The trace tree from spec #1 lives alongside it (per-run drill-down), not as a replacement. Both stay. If usage data later shows the heatmap is dead, deprecate it in a follow-up spec.

## Appendix — index of build specs in this batch

All under `docs/_internal/specs/`, all dated 2026-05-05.

| # | File | Phase |
|---|---|---|
| 1 | [`2026-05-05-trace-tree-ui.md`](./2026-05-05-trace-tree-ui.md) | 1 |
| 2 | [`2026-05-05-cost-token-surfacing.md`](./2026-05-05-cost-token-surfacing.md) | 1 |
| 3 | [`2026-05-05-datasets.md`](./2026-05-05-datasets.md) | 2 |
| 4 | [`2026-05-05-llm-as-judge.md`](./2026-05-05-llm-as-judge.md) | 3 |
| 5 | [`2026-05-05-pairwise-comparison.md`](./2026-05-05-pairwise-comparison.md) | 3 |
| 6 | [`2026-05-05-prompt-management.md`](./2026-05-05-prompt-management.md) | 4 |
| 7 | [`2026-05-05-playground.md`](./2026-05-05-playground.md) | 4 |
| 8 | [`2026-05-05-annotations-feedback.md`](./2026-05-05-annotations-feedback.md) | 5 |
| 9 | [`2026-05-05-sessions-users.md`](./2026-05-05-sessions-users.md) | 5 |
| 10 | [`2026-05-05-monitoring-dashboards.md`](./2026-05-05-monitoring-dashboards.md) | 5 |
