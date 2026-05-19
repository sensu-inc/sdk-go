# `sdk-go` changelog

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
