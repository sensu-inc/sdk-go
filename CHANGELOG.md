# `sdk-go` changelog

## Unreleased

### Docs — add README; document the framework-integration gap

Adds a top-level `README.md` (the repo had none before). Sections:

- Quick start
- Tracking primitives table (`Run` / `Track*` / `Track*Ctx`)
- **Framework integrations** — what ships (Anthropic, OpenAI) + an
  honest "Not available — and why" section for LangChain, LangGraph,
  and CrewAI: those frameworks have **no Go ports**, so there's
  nothing to wrap. Documents the manual-instrumentation pattern via
  the existing `Track*` primitives for customers using `langchaingo`
  or rolling their own agent loop.
- Other features index (multi-agent, sessions, prompts, body
  capture, run-less helpers, pricing, loop detection)

Phase 2 PR 4 of the platform `SDK_CONSOLIDATION_PLAN.md`. Closes
the "Go has only Anthropic, sdk-ts/sdk-python have 4 framework
handlers" gap via documentation rather than building wrappers
around frameworks that don't exist in Go.

No code changes; no version bump.

## 0.6.0 — 2026-05-20

### Added — OpenAI integration (`integrations/openai`)

New sibling of `integrations/anthropic`. Auto-tracks every
`Chat.Completions.New` call as a Sensu LLM telemetry event with the
same wire shape — `llm.request.started` on dispatch, `llm.request.completed`
on return (or failure) with token usage + latency + provider + model.

```go
import (
    openaiSDK "github.com/openai/openai-go"
    sopenai  "github.com/sensu-inc/sdk-go/integrations/openai"
)

sensuClient := sensu.New(sensu.ClientOptions{FromEnv: true})
openaiClient := openaiSDK.NewClient()
wrapped := sopenai.Wrap(&openaiClient, sopenai.WrapOptions{Client: sensuClient})

err := sensuClient.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
    resp, err := wrapped.Chat.Completions.New(ctx, openaiSDK.ChatCompletionNewParams{...})
    // ...
})
```

Mirrors the Anthropic wrapper: same `WrapOptions` shape (`Client`,
`RunHandle`, `DefaultProvider`), same standalone-mode behavior when
no run is active, same error-path emission. The differences are:

- Underlying call path is `Chat.Completions.New` instead of
  `Messages.New`.
- Token usage maps from `PromptTokens` / `CompletionTokens` /
  `PromptTokensDetails.CachedTokens`.
- Default provider label is `"openai"`. Override to `"azure-openai"`
  or similar when routing through OpenAI-compatible vendors.

`integrations/openai` is a separate Go module under the `sdk-go` repo
so the OpenAI SDK dependency stays opt-in — the root module is
dependency-light for customers who don't use OpenAI.

5 new tests covering: happy-path (started + completed + token usage
+ cached_tokens + parent_span_id chain), error-path (status="error"
+ no usage fields), standalone (no run) emits orphan IDs without
started, default `DefaultProvider="openai"`, custom DefaultProvider
override (Azure / Groq / Fireworks use case).

Release workflow updated to run `go test ./...` inside
`integrations/openai` alongside the existing Anthropic integration
test step.

## 0.5.0 — 2026-05-20

### Added — context-bound tracking shortcuts (Phase 2 PR 1)

Four new free-function helpers that auto-manage step lifecycle when an
active run is in context. Removes the boilerplate of `StartStep` →
`defer step.End()` → call the underlying `Track*` for the common
"track one tool/retrieval/embedding/guardrail call" case.

```go
err := client.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
    result, err := sensu.TrackToolCtx(ctx, func() (string, error) {
        return mySearchTool(query)
    }, sensu.TrackToolOptions{ToolName: "search"})
    _ = result
    return err
})
```

New helpers (all in the top-level `sensu` package):

- `sensu.TrackToolCtx[T](ctx, fn, opts)` — wraps `TrackTool[T]`
- `sensu.TrackRetrievalCtx[T](ctx, fn, opts)` — wraps `TrackRetrieval[T]`
- `sensu.TrackEmbeddingCtx[T](ctx, fn, opts)` — wraps `TrackEmbedding[T]`
- `sensu.TrackGuardrailCtx(ctx, fn, opts)` — wraps `TrackGuardrail`

Each:
1. Pulls the active `RunHandle` from `ctx` via `RunFromContext`
2. Creates a transient step (`StartStep` with sensible default name +
   type from the options)
3. Calls the existing underlying generic
4. Auto-ends the step on return (via `defer step.End(ctx)`)

**Fall-through behavior:** if no active run is in `ctx`, the wrapper
just calls `fn` directly with no telemetry — matches the no-run
behavior in the existing trinity SDKs. No errors raised; cost of
forgetting `client.Run()` is just missing observability for that call.

**Why "Ctx" suffix?** Go can't put type parameters on methods, so
TypeScript-style `client.trackTool(...)` isn't possible. The free-
function pattern with a `Ctx` suffix follows the established Go
convention (`http.NewRequest` vs `http.NewRequestWithContext`); the
existing step-bound generics (`TrackTool[T](ctx, step, fn, opts)`) are
unchanged.

7 new tests covering all four shortcuts, error propagation, and the
no-run fall-through. Full suite: 76/76 (was 69).

## 0.4.0 — 2026-05-20

### Changed — pricing fallback removed; live API is the only path

The SDK no longer ships a bundled `bundledPricing` map. `resolvePricing`
calls the platform endpoint `GET /api/v1/pricing/models/:p/:m` and
caches the result per `(provider, model)` for the client lifetime
(unchanged). On failure (API unreachable, 4xx/5xx, null rates,
`disableLivePricing: true`, client `disabled`, or no API key), it
returns `[2]float64{0, 0}` and logs a warning via the standard
`log.Printf` at most once per `(provider, model)` per client lifetime
so logs don't spam.

**Why:** the bundled table drifted from the platform catalog and
required a manual sync step on every release. With this change the
platform is the single source of truth — including for custom models
customers register via the new `POST /api/v1/pricing/org-models`
endpoint. The server's ingest pipeline reconciles cost from
`llm_calls` + the catalog regardless of what the SDK sent, so cost
dashboards stay correct even when an SDK call sends 0.

**Breaking-ish:** prior versions returned a (potentially stale)
fallback price on API failure. This version returns 0 + warns. If you
relied on the fallback in an air-gapped environment, please open an
issue — we can revisit. See
[`SDK_CONSOLIDATION_PLAN.md`](https://github.com/sensu-inc/sensu/blob/main/planning/SDK_CONSOLIDATION_PLAN.md)
§3c for the design rationale.

**Removed:**
- The package-level `bundledPricing` map (was an unexported var; not
  part of the public API).
- The `lookupBundled(model)` helper.

**Internal:**
- `pricingCache` now also tracks a `warned` set so the failure-path
  warning fires at most once per (provider, model). Concurrent access
  is mutex-protected.
- `estimateCost(pricing, in, out)` unchanged — pure helper used by
  `TrackLLM`.

10 new tests in `pricing_test.go` (white-box `package sensu`)
covering success cache + reuse, 4xx, 5xx, network error, null-rates,
three short-circuit paths, per-(provider, model) warning isolation,
and `estimateCost` pure math.

## 0.3.0 — 2026-05-19

### Added — agent version registry for eval-gated CI/CD (§5.2)

- **`client.RegisterAgentVersion(ctx, opts)`** — new run-less helper
  that wraps `POST /api/v1/agents/:id/versions`. Lets customers
  register the candidate config (system prompt + optional model) used
  at a given commit, then reference the returned versionId from the
  Sensu eval-gate Action instead of inlining the full config in
  every PR check.
- New exported types: `CandidateConfig`, `RegisterAgentVersionOptions`,
  `AgentVersion`.
- AgentID is URL-encoded via `url.PathEscape` so labels containing
  reserved characters round-trip safely.
- Owner/admin role required server-side (the registration represents
  a deploy fact); an API key with `full` scope works as expected. See
  the platform repo's `planning/EVAL_GATED_CI_PLAN.md` PR 5 for the
  matching backend.

Sibling SDK releases:
- `@sensu-ai/sdk` 0.11.0 (TS) — `client.registerAgentVersion`
- `sensu-sdk` 0.12.0 (Python) — `client.register_agent_version`

## 0.2.0

Earlier releases (no CHANGELOG kept). Notable additions before this
file existed:
- `Tool I/O capture` — per-call `CaptureBodies` opt-in on `TrackTool`
- Phase 0a run-less helpers: `Feedback`, `Score`
- RCA reporting
