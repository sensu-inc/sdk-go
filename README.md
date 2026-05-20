# `sensu-inc/sdk-go`

Go SDK for the [Sensu](https://sensu.ai) AI agent observability platform. Track LLM calls, tools, retrievals, embeddings, guardrails, and multi-agent flows from any Go service.

```bash
go get github.com/sensu-inc/sdk-go
```

```go
import "github.com/sensu-inc/sdk-go"

client := sensu.New(sensu.ClientOptions{FromEnv: true})  // reads SENSU_API_KEY etc.
defer client.Close(ctx)

err := client.Run(ctx, sensu.StartRunOptions{RunType: "chat"}, func(ctx context.Context, run *sensu.RunHandle) error {
    // Auto-step shortcut (v0.5.0+): no need to manage StartStep / step.End manually
    result, err := sensu.TrackToolCtx(ctx, func() (string, error) {
        return mySearch(query)
    }, sensu.TrackToolOptions{ToolName: "search"})
    _ = result
    return err
})
```

Sibling SDKs: [`sdk-ts`](https://github.com/sensu-inc/sdk-ts) (npm: `@sensu-ai/sdk`) and [`sdk-python`](https://github.com/sensu-inc/sdk-python) (PyPI: `sensu-sdk`). The wire format is identical across all three; events flow into the same backend.

---

## Tracking primitives

| Function | Use when |
|---|---|
| `client.Run(ctx, opts, fn)` | Top-level entry — emits `agent.run.started` / `completed`, injects the run into `ctx` |
| `sensu.TrackLLM[T](ctx, step, fn, opts)` | Wrap an LLM call — emits `llm.request.started` + `llm.request.completed` with usage/cost |
| `sensu.TrackTool[T](ctx, step, fn, opts)` | Wrap a tool call — emits `tool.call.started` + `tool.call.completed` |
| `sensu.TrackRetrieval[T](ctx, step, fn, opts)` | Wrap a retrieval call — emits `retrieval.started` + `retrieval.completed` |
| `sensu.TrackEmbedding[T](ctx, step, fn, opts)` | Wrap an embedding call — emits `embedding.created` |
| `sensu.TrackGuardrail(ctx, step, fn, opts)` | Wrap a guardrail check — emits `guardrail.check.completed` |

### Context-bound shortcuts (v0.5.0+)

Each of the four `Track*` functions above (except `TrackLLM`) has a `*Ctx` sibling that auto-manages step lifecycle when an active run is in `ctx` (i.e. inside a `client.Run(...)` callback):

```go
// Without Ctx — manual step lifecycle
step := run.StartStep(sensu.StartStepOptions{Name: "search", StepType: "tool"})
defer step.End(ctx)
result, err := sensu.TrackTool(ctx, step, fn, opts)

// With Ctx — auto step lifecycle
result, err := sensu.TrackToolCtx(ctx, fn, opts)
```

Available: `TrackToolCtx[T]`, `TrackRetrievalCtx[T]`, `TrackEmbeddingCtx[T]`, `TrackGuardrailCtx`.

If no run is in `ctx`, the wrapper falls through to calling `fn` directly with no telemetry — matches the no-run behavior of the trinity SDKs.

> Note: `TrackLLM` doesn't have a `*Ctx` sibling because LLM tracking requires explicit `Provider` + `Model` options that are intentionally part of the type signature. The boilerplate cost is one-time per step; the win from auto-step would be marginal.

---

## Framework integrations

### Available

Both ship as separate Go modules under this repo so their third-party deps stay opt-in for customers who don't use them.

| Provider | Module | Wraps |
|---|---|---|
| Anthropic | [`integrations/anthropic`](./integrations/anthropic/) | `*anthropicSDK.Client` — auto-tracks `Messages.New` |
| OpenAI | [`integrations/openai`](./integrations/openai/) | `*openaiSDK.Client` — auto-tracks `Chat.Completions.New`. Also works for Azure OpenAI / Fireworks / Groq via `DefaultProvider` override |

Both follow the same `Wrap(client, WrapOptions) *Wrapped*` pattern and emit identical event shapes; switching providers requires no other changes.

### Not available — framework wrappers (and why)

The TypeScript and Python SDKs ship handlers for LangChain, LangGraph, and CrewAI. **The Go SDK does not — those frameworks have no Go ports.**

- LangChain has [`langchaingo`](https://github.com/tmc/langchaingo), a community port, but it's not feature-parity with the official LangChain libraries the TS/Python handlers wrap; we'd be wrapping a different code surface.
- LangGraph has no Go port.
- CrewAI has no Go port.

We track this and will revisit if first-party Go ports of any of the three ever ship. Until then:

#### How to instrument an agent framework without a wrapper

The `Track*` primitives are framework-agnostic. If you're using `langchaingo` or rolling your own agent loop, instrument the seams manually:

```go
// 1. LLM calls — wrap whatever LLM the framework calls under the hood
result, err := sensu.TrackToolCtx(ctx, func() (langchaingo.LLMResult, error) {
    return chain.Call(ctx, input)
}, sensu.TrackToolOptions{ToolName: "qa-chain"})

// 2. Multi-agent handoffs — emit when the framework's agent A passes control to agent B
run.Handoff(sensu.HandoffOptions{
    ToAgentID: "specialist-agent",
    Reason:    "user asked a domain-specific question",
})

// 3. Spawned sub-runs — when an agent launches a child
childRun, _ := client.SpawnRun(ctx, sensu.SpawnRunOptions{
    ChildAgentID: "researcher",
    SpawnReason:  "deep-dive sub-task",
})
```

This is more code than the TS/Python handler pattern, but produces the same backend events and shows up identically in the Sensu UI.

If you're shipping a Go agent framework and want Sensu to consider a first-party integration, [open an issue](https://github.com/sensu-inc/sdk-go/issues/new) describing the framework's lifecycle hooks — we'd rather wrap a real Go-native framework than ask customers to glue our primitives into one we don't ship.

---

## Other features

| Feature | Where |
|---|---|
| Multi-agent (`SpawnRun`, `Handoff`) | `run.go` |
| Sessions (`StartSession`, `ResumeSession`) | `client.go` |
| Prompt versioning (`DeployPromptVersion`) | `client.go` |
| Tool I/O body capture (PII-safe replay) | `TrackToolOptions.CaptureBodies` — see [`step.go`](./step.go) |
| Run-less helpers (`Feedback`, `Score`, `RegisterAgentVersion`) | `feedback.go`, `agent_versions.go` |
| Live pricing resolution | `pricing.go` — hits `/api/v1/pricing/models/:p/:m`; returns `[0, 0]` + logs warning on failure (no bundled fallback as of v0.4.0; see [SDK_CONSOLIDATION_PLAN.md §3c](https://github.com/sensu-inc/sensu/blob/main/planning/SDK_CONSOLIDATION_PLAN.md) for rationale) |
| Loop detection callback | `ClientOptions.OnLoopDetected` |

---

## License

MIT — see [LICENSE](./LICENSE) (if absent, MIT applies per the parent repo conventions).
