# `sdk-go` changelog

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
