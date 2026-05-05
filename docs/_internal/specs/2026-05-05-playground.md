# Spec #7 — Interactive playground

**Date:** 2026-05-05
**Parent:** [`2026-05-05-eating-the-cake.md`](./2026-05-05-eating-the-cake.md)
**Status:** Build spec, ready for implementation. Depends on [`#6 prompt-management`](./2026-05-05-prompt-management.md), [`#1 trace-tree-ui`](./2026-05-05-trace-tree-ui.md).
**Phase:** 4 — prompts + playground.

## 1. Goal

An interactive UI inside `defrost serve` for editing prompts, swapping models, and comparing outputs side-by-side. Critical positioning constraint: **API keys are never persisted server-side.** Keys live only in browser localStorage and travel with each request to the server-side proxy. Each playground run is captured by the same OTel pipeline as everything else, so it appears in the heatmap and the trace tree without any extra step.

## 2. Why this matters competitively

Both LangSmith and Langfuse ship a playground. It's table stakes for the four-feature Tier-S surface. defrost's differentiator is positioning, not novelty: **we never store your API keys.** Self-host LLM observability shouldn't ask users to hand secrets to yet-another-service. Every key in the wire is the user's; defrost is a thin proxy that emits telemetry and forgets the key as soon as the response returns.

This is also a marketing surface: "defrost playground — your keys never touch our disk" is a one-line pitch even non-technical reviewers grok.

## 3. Public docs to write first

- **New page:** `docs/guides/playground.md` — "Try a prompt without writing code." Sections: "Open the playground", "Bring your API key", "Multi-model comparison", "Pin a result to a dataset". Make the keys-stay-in-the-browser story explicit.
- **Edit:** `docs/concepts/git-as-database.md` or a new `docs/concepts/api-keys.md` — short page on the no-server-side-key-storage policy.
- **Edit:** `docs/reference/serve-api.md` — document `/api/playground/run`.

## 4. Storage layout

**No new persistent storage on the data branch.** Playground runs flow through the standard OTel pipeline (`internal/otlp/`) and land as normal trace files. The user can identify playground runs by a resource attribute `defrost.run.source = "playground"` on the root span.

Server-side, the key handling rule is strict:

- The HTTP request body carries the key.
- Server holds the key in memory only for the duration of the LLM call.
- **Logs never contain the key.** Verified by test (see §10).
- The key is **not** written to disk. **Not** passed to subprocesses.

## 5. CLI surface

**No new CLI commands.** The playground is a dashboard concern. CLI users have `defrost exec` for the same outcome.

## 6. HTTP API surface

In `internal/serve/playground.go`:

### `POST /api/playground/run`

Request body:

```json
{
  "prompt_name": "support/welcome",        // or use prompt_text directly
  "prompt_ref":  "production",             // optional, defaults to HEAD
  "prompt_text": "...",                    // alternative to prompt_name+ref
  "model":       "anthropic/claude-opus-4-7",
  "temperature": 0.2,
  "max_tokens":  512,
  "input":       { "user_name": "Alice", "intent": "billing" },
  "api_key":     "sk-...",                 // sent per-request, never stored
  "stream":      true
}
```

Response: streaming Server-Sent Events.

```
event: token
data: {"text": "Hello"}

event: token
data: {"text": ", Alice"}

event: done
data: {"trace_id": "...", "tokens_in": 23, "tokens_out": 12, "cost_usd": 0.0006}
```

### `POST /api/playground/compare`

Same as `/run` but accepts `models: [...]` and emits parallel streams (one event per model). Useful for the side-by-side view.

### `GET /api/playground/providers`

Returns the static list of supported providers + models that defrost knows how to proxy to.

```json
{
  "providers": [
    { "id": "anthropic", "models": ["claude-opus-4-7", "claude-sonnet-4-6", "claude-haiku-4-5"] },
    { "id": "openai", "models": ["gpt-5", "gpt-5-mini"] },
    { "id": "google", "models": ["gemini-2.5-pro", "gemini-2.5-flash"] },
    { "id": "openrouter", "models": ["*"] }
  ]
}
```

### Provider proxy implementation

In `internal/playground/`:

- `provider.go` — `type Provider interface { Stream(ctx, req) (<-chan Token, error) }`
- `anthropic.go`, `openai.go`, `google.go`, `openrouter.go` — one impl per provider. Direct HTTP calls to the provider's API; do not pull in heavy SDKs.
- `proxy.go` — request-scoped struct that owns the key for the duration of one call.

Each provider impl:

1. Receives `(req, key)`.
2. Builds the upstream request with the user's key.
3. Streams the response back as `Token` channel events.
4. Emits OTel spans with full `gen_ai.*` attributes.
5. Drops the key from memory on return.

**Logging discipline:** `internal/playground/proxy.go` uses a custom logger wrapper that scrubs any field whose key contains `key`, `token`, `secret`, or `authorization`. Every test in this package asserts no upstream-key value appears in captured log output.

## 7. Web UI surface

| File | Role |
|---|---|
| `web/src/pages/Playground.tsx` (new) | Route `/playground`. Three-pane layout. |
| `web/src/components/PromptPane.tsx` (new) | Left pane. Prompt editor (reuses `PromptEditor` from spec #6 if present, or a minimal Monaco-backed editor). Optional: pick an existing prompt from the registry. |
| `web/src/components/ModelControls.tsx` (new) | Middle pane. Model picker (from `/api/playground/providers`), temperature, max-tokens, and an input form derived from the prompt's `input_schema`. |
| `web/src/components/OutputPane.tsx` (new) | Right pane. Streams output from `POST /api/playground/run`. Multi-model mode shows N output panels side-by-side. |
| `web/src/components/ApiKeyInput.tsx` (new) | Modal / drawer. Lists configured keys (one per provider), stored in `localStorage`. Toggle "remember key on this device" — if off, keys clear on tab close (`sessionStorage`). |
| `web/src/lib/secrets.ts` (new) | Storage abstraction. Never logs values. |

App.tsx route: `/playground`.

### Capture-to-dataset

A "Pin to dataset" button on the output pane opens a modal (reuses `AddToDatasetButton` from spec #3). Pins the `(input, output)` pair as a row in the chosen dataset.

### "Open in playground" affordance

On any LLM span in the trace tree (spec #1), an "Open in playground" button pre-fills the playground with that span's prompt and input. Reproduces and modifies a prior call in two clicks.

## 8. Reuse map

| Existing | Use as |
|---|---|
| `internal/otlp/sink.go` | Existing OTel span emission. Playground spans flow through here unchanged. |
| `internal/runner/runtime.go` | Existing OTLP env var setup. Playground runs **don't** spawn a subprocess, so this isn't needed; the proxy emits spans directly via the in-process OTel SDK. |
| `internal/serve/server.go` | Wire new endpoints; impls in `internal/serve/playground.go` and `internal/playground/`. |
| `internal/prompt/client.go` (spec #6) | Resolve `prompt_name + prompt_ref` to body + frontmatter. |
| `internal/cost/compute.go` (spec #2) | Compute cost from token counts. |
| `web/src/components/AddToDatasetButton.tsx` (spec #3) | Reuse for the "Pin to dataset" button. |
| `web/src/components/PromptEditor.tsx` (spec #6, if shipped) | Reuse for the prompt pane. |

**New files only:**

- `internal/playground/` — `playground.go`, `provider.go`, `anthropic.go`, `openai.go`, `google.go`, `openrouter.go`, `proxy.go`, plus `*_test.go`.
- `internal/serve/playground.go` (+ test)
- `web/src/pages/Playground.tsx` (+ test)
- `web/src/components/PromptPane.tsx`, `ModelControls.tsx`, `OutputPane.tsx`, `ApiKeyInput.tsx` plus tests.
- `web/src/lib/secrets.ts` (+ test)
- `docs/guides/playground.md`

## 9. OTel emission

Each playground call emits one parent span and provider-specific child spans:

```text
parent span:
  name        = "defrost.playground.run"
  attributes  = {
    "defrost.run.source":     "playground",
    "defrost.prompt.name":    <name-or-empty>,
    "defrost.prompt.ref":     <resolved-sha-or-empty>,
    "gen_ai.request.model":   <model>,
    "gen_ai.request.temperature": <float>,
    "gen_ai.request.max_tokens":  <int>,
  }

child span:
  name        = "gen_ai.client.chat"
  attributes  = standard gen_ai.* + provider-specific
```

This makes playground runs first-class in the trace tree, the cost chart, and any judge that scores them.

## 10. Test plan

| Test file | Asserts |
|---|---|
| `internal/playground/anthropic_test.go` (and analogues) | Stub the upstream HTTP transport (`httptest.Server`); assert request shape, response streaming, error handling (429, 5xx). The user's API key never appears in captured server logs. |
| `internal/playground/proxy_test.go` | The key in the request never reaches the OTel span attribute. Logger scrubbing is correct (test with strings containing "key", "token", etc.). |
| `internal/serve/playground_test.go` | `/api/playground/run` end-to-end with a stub provider; emits SSE in the expected order; the response-final event includes `trace_id`, `tokens_in`, `tokens_out`, `cost_usd`. The persisted trace file (verify against the test fixture data branch) contains a span with `defrost.run.source = "playground"`. |
| `web/src/pages/Playground.test.tsx` | Three-pane layout renders. Picking a prompt + model + filling input + clicking "Run" issues the expected POST. SSE chunks render incrementally. |
| `web/src/lib/secrets.test.ts` | localStorage / sessionStorage round-trip; no value ever logged via console. |
| `web/src/components/ApiKeyInput.test.tsx` | "Remember key" toggle controls localStorage vs sessionStorage. |

## 11. Acceptance criteria

- A user can paste an API key, edit a prompt, and run it against any supported model from `/playground`.
- Multi-model comparison shows N outputs streaming in parallel.
- Each run shows up in the heatmap and at `/trace/<trace-id>` automatically.
- The user's API key is **never** written to defrost's logs, never persisted to disk on the server, and never passed to a subprocess.
- "Pin to dataset" captures the run as a dataset row.
- "Open in playground" from a span in the trace tree pre-fills with the right inputs.
- Public docs at `docs/guides/playground.md` exist and explicitly state the no-server-side-key policy.
- All test cases in §10 pass.

## 12. Open questions / decisions

- **Streaming proxy buffering.** Large outputs need flow control to avoid head-of-line blocking. Recommendation: stream in 64-byte chunks (or whatever the provider sends). Don't buffer.
- **Rate limit / 429 handling.** Recommendation: surface the upstream 429 status to the UI verbatim; don't retry in v1. Document that the user's account is the rate-limit subject, not defrost.
- **Provider auth other than bearer tokens.** Some providers (e.g., AWS Bedrock) use signed requests. Recommendation: defer Bedrock-style auth to a follow-up. v1 covers bearer-token providers (Anthropic, OpenAI, Google, OpenRouter).
- **Local model providers (ollama, llama.cpp, vLLM).** Recommendation: add as a v1 provider — it's a simple HTTP proxy and the "no API key needed" path is even simpler. Add `localhost` to the providers list. This is a cheap additional differentiator vs Langfuse, which assumes cloud LLMs.
- **Where does the key actually live in memory?** Recommendation: pass through function args, never assign to a struct field with a long lifetime. Add a unit test that uses Go's `runtime.GC` + `runtime.SetFinalizer` to assert keys are GC'd promptly. (May be over-engineered; flag and revisit.)
- **CSRF on `/api/playground/run`.** `defrost serve` binds to `127.0.0.1` only, so CSRF is moot. Document the assumption. If a future hosted tier exposes the same endpoint over the open Internet, CSRF protection becomes mandatory.
