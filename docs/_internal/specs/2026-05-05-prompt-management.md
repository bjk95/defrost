# Spec #6 — Prompt management

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation.
**Phase:** 4 — prompts + playground.

## 1. Goal

Versioned prompt registry. Prompts are Markdown files with YAML frontmatter, stored on the data branch under `prompts/`. Versioning is git history of the file — no separate registry service. Apps fetch prompts via a Go library or HTTP API; pinning to a label (e.g. `production`) is a git tag.

A user can:

1. Create / edit a prompt in the dashboard or via CLI.
2. Read the prompt at HEAD or at any past ref.
3. Move a label (e.g., `production`) to a new version atomically.
4. Diff two prompt versions visually.
5. From their app, call `defrost.GetPrompt("name", "production")` and get atomic, cached updates.

## 2. Why this matters competitively

Prompt management is half of LangSmith's "Prompt Hub" pitch (the other half being the playground — spec [`#7`](./2026-05-05-playground.md)). Langfuse ships server-side caching for low-latency prompt fetches. Without prompt management, defrost is missing one of the four Tier-S features.

defrost's angle: **prompts are files in git.** Versioning, blame, history, branching, code review all come for free. Apps consuming prompts get atomic updates by moving a label — the same mechanism teams already understand. Public-cloud incumbents reinvent this with a custom registry; we just use git.

## 3. Public docs to write first

- **New page:** `docs/guides/managing-prompts.md` — "Version your prompts in git." Sections: "Create a prompt", "Reference from your app", "Pin a label", "Diff two versions", "Roll back".
- **New page:** `docs/reference/cli/prompt.md` — full CLI reference.
- **New concept page:** `docs/concepts/prompts.md` — short page on why prompts live on the data branch alongside everything else. Cite `docs/concepts/git-as-database.md`.
- **Edit:** `docs/reference/storage-layout.md` — add `prompts/` to the directory tree.
- **Edit:** `docs/reference/serve-api.md` — document new endpoints.

## 4. Storage layout

On the data branch:

```text
<repo>/.defrost/prompts/
└── <name>.md
```

`<name>` matches the dataset/judge charset: `[a-z0-9_/-]+`. Slashes are allowed for namespacing (e.g. `support/welcome.md`); the path is taken literally relative to `prompts/`.

### File format

```markdown
---
schema: 1
description: Customer-support welcome message
model: anthropic/claude-opus-4-7
temperature: 0.2
max_tokens: 512
input_schema:
  type: object
  properties:
    user_name: { type: string }
    intent:    { type: string }
labels:
  production: 8a3f2b1c   # short SHA on data branch — the pinned ref
  staging:    9d4e8f7a
---

You are a friendly customer support agent for Acme Corp.

User: {{ user_name }}
Detected intent: {{ intent }}

Greet the user, acknowledge their intent, and offer next steps...
```

The body is the prompt text. Templating uses `{{ var }}` (mustache-compatible — pick a tiny Go templating library; `text/template` works but breaks on `{{`/`}}` collisions in the prompt itself, so prefer `cbroglie/mustache` or similar).

`labels:` is a frontmatter table mapping label name → git short SHA (or full SHA) on the data branch. Moving a label is a re-write of this section + a commit. Discussed in §12.

### Versioning model

Versions are git commits on the data branch. There's no `versions/v1.md` directory — just file history. `defrost prompt show <name>` reads HEAD by default; `--ref <sha-or-label>` reads any past version.

### Atomic write & conflict handling

Same model as suppressions / datasets / judges. Use `internal/persist/persist.go` clone-mutate-push helpers; retry on non-FF.

## 5. CLI surface

`defrost prompt` parent command. Subcommands:

### `defrost prompt create <name> [--from <file>]`

Write a new `prompts/<name>.md` with default frontmatter and (if provided) body from `<file>`. Open `$EDITOR` if `--from` is omitted.

### `defrost prompt edit <name>`

Open `$EDITOR` on `prompts/<name>.md`. Validate frontmatter on save; reject commit if invalid.

### `defrost prompt show <name> [--ref <sha-or-label>] [--body-only] [--json]`

Print the prompt at HEAD or specified ref. `--body-only` prints just the prompt text. `--json` returns `{frontmatter, body}` as JSON.

### `defrost prompt list [--json]`

Print all prompts: `<name>\t<labels>\t<updated-at>`.

### `defrost prompt diff <name> [<ref-a>] [<ref-b>]`

Sugar for `git diff <ref-a> <ref-b> -- prompts/<name>.md` on the data branch. With one arg, diffs HEAD against `<ref-a>`. With no args, diffs HEAD against `HEAD~1`.

### `defrost prompt label <name> <label> [<ref>]`

Move a label to a ref (default: HEAD). Re-writes the prompt's `labels:` frontmatter and commits with message `prompt(<name>): label <label> -> <ref>`. The label re-write is itself a new commit, which means a label history exists naturally in the file's git log. Use a label name `production` consistently to keep app code stable.

### `defrost prompt unlabel <name> <label>`

Remove a label from the file's frontmatter.

## 6. HTTP API surface

In `internal/serve/prompts.go`:

| Method + path | Returns |
|---|---|
| `GET /api/prompts` | `{prompts: [{name, labels, updated_at}]}` |
| `GET /api/prompts/{name}?ref=<sha-or-label>` | `{name, frontmatter: {...}, body: "..."}` |
| `GET /api/prompts/{name}/history` | `[{sha, ts, author, message, label?}]` — git log of this file |
| `GET /api/prompts/{name}/diff?from=<ref>&to=<ref>` | Unified diff text or structured per-line diff |
| `PUT /api/prompts/{name}` | Body: `{frontmatter, body}`. Writes a new commit. |
| `POST /api/prompts/{name}/labels/{label}` | Body: `{ref}`. Moves the label. |
| `DELETE /api/prompts/{name}/labels/{label}` | Removes the label. |

Cache-Control:

- `GET /api/prompts` → `public, max-age=60`.
- `GET /api/prompts/{name}?ref=<full-sha>` → `public, max-age=86400` (immutable when `ref` is a full SHA).
- `GET /api/prompts/{name}?ref=<label>` → `public, max-age=10` (label can move).
- `GET /api/prompts/{name}` (no ref, defaulting to HEAD) → `public, max-age=10`.
- All writes → no cache.

## 7. Web UI surface

| File | Role |
|---|---|
| `web/src/pages/Prompts.tsx` (new) | List view at `/prompts`. Table of prompts with active labels and last-updated. New / search. |
| `web/src/pages/PromptDetail.tsx` (new) | Detail view at `/prompts/:name`. Tabs: "Edit", "History", "Diff". |
| `web/src/components/PromptEditor.tsx` (new) | Monaco-based editor split into frontmatter (YAML) and body (Markdown). Validates on save. Cmd+S writes via PUT. |
| `web/src/components/PromptDiffView.tsx` (new) | Side-by-side diff of two refs. Reuses `react-diff-view` or similar. |
| `web/src/components/LabelChip.tsx` (new) | Small chip showing a label name + ref. Click to open a "move label" modal. |

App.tsx routes: `/prompts`, `/prompts/:name`.

Monaco may not already be in the bundle. If not, this spec introduces the dependency. Coordinate with spec [`#4 llm-as-judge`](./2026-05-05-llm-as-judge.md) which also wants a YAML editor — pick a single editor library and reuse.

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/persist/persist.go` | Clone-mutate-push pattern. Add `WritePrompt(name string, frontmatter PromptMeta, body []byte, msg string) error` and `ReadPrompt(name, ref string) (PromptMeta, []byte, error)`. |
| `internal/cli/cli.go`, `wire.go` | Add `Prompt` command. |
| `internal/serve/server.go` | Wire new endpoints; impl in `internal/serve/prompts.go`. |
| `internal/runner/runtime.go` | If we want `defrost exec` to inject `DEFROST_PROMPT_<NAME>` env vars, hook here. **Defer to a follow-up spec.** v1 keeps prompt access via the library / API only. |

**New files only:**

- `internal/prompt/` — `prompt.go`, `frontmatter.go`, `template.go`, `client.go`, plus tests. The `client.go` exposes `GetPrompt(name, label) (Prompt, error)` for app-side consumption with an in-memory TTL cache.
- `internal/persist/prompt.go` (+ test)
- `internal/serve/prompts.go` (+ test)
- `internal/cli/prompt.go` (+ test)
- `web/src/pages/Prompts.tsx`, `PromptDetail.tsx` plus tests.
- `web/src/components/PromptEditor.tsx`, `PromptDiffView.tsx`, `LabelChip.tsx` plus tests.

## 9. OTel emission

Library callers (`defrost.GetPrompt(...)`) emit a span:

```text
span.name        = "defrost.prompt.get"
span.attributes  = {
  "defrost.prompt.name":  <name>,
  "defrost.prompt.label": <label-or-empty>,
  "defrost.prompt.ref":   <resolved-sha>,
  "defrost.prompt.cache_hit": <bool>,
}
```

This makes prompt-fetch observability automatic for any app using the library; cache hit-rate becomes visible in the trace tree.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/prompt/frontmatter_test.go` | YAML parse round-trip; missing required fields error; templating substitution with various inputs. |
| `internal/prompt/client_test.go` | `GetPrompt` cache TTL behaviour: hits cache within TTL, misses after. Concurrent callers don't double-fetch (singleflight). |
| `internal/persist/prompt_test.go` | Write/read round-trip; label move produces the expected commit; concurrent writers retry. |
| `internal/serve/prompts_test.go` | All endpoints. `Cache-Control` correct per ref type (full SHA vs label vs HEAD). |
| `internal/cli/prompt_test.go` | `create` / `show --ref` / `label` / `diff` end-to-end against a temp repo. |
| `web/src/pages/Prompts.test.tsx`, `PromptDetail.test.tsx` and component tests. | Render + edit + label-move flows. |

## 11. Acceptance criteria

- A user can create, edit, and version a prompt entirely from the dashboard.
- A user can move a `production` label and have all running apps pick up the new version within TTL (default 60s).
- An app calling `defrost.GetPrompt("welcome", "production")` gets the body and frontmatter, with templating support.
- The library emits OTel spans for each fetch; cache hit-rate is visible in the trace tree.
- `git diff main..feature -- .defrost/prompts/` shows exactly which prompts changed in a PR.
- Public docs at `docs/guides/managing-prompts.md`, `docs/concepts/prompts.md`, `docs/reference/cli/prompt.md` exist and match the implementation.
- All test cases in §10 pass.

## 12. Open questions / decisions

- **Where do labels live — frontmatter or git tags?** Recommendation: **frontmatter.** Git tags are repo-scoped and would collide across N prompts; frontmatter labels are file-scoped naturally. Document the trade-off: tag pushes happen separately from commit pushes, so label updates are atomic with file changes.
- **Templating library.** `text/template` collides with prompt syntax. Recommendation: `cbroglie/mustache` — small, well-maintained, no collision. Document the dependency clearly.
- **Caching strategy.** Default TTL 60s, configurable via `defrost.NewPromptClient(opts)`. Recommendation: in-memory TTL + singleflight. Skip Redis-style externally-shared cache; that's hosted-tier territory.
- **Prompt deletion.** Recommendation: support `defrost prompt delete <name>` only with a `--force` flag. The data is recoverable from git, so deletion is an operational nicety, not a destructive op.
- **Multiple prompts per file (e.g., system + user prompts).** Recommendation: keep it one prompt per file. If users need a system+user pairing, name them with a shared prefix (`support/welcome.system.md`, `support/welcome.user.md`).
- **Sharing prompts across repos.** Hosted-tier territory. Don't try to solve.
