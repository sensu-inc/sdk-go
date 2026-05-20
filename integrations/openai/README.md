# `sensu-inc/sdk-go/integrations/openai`

Sensu wrapper for the [OpenAI Go SDK](https://github.com/openai/openai-go). Auto-tracks every `Chat.Completions.New` call as a Sensu LLM telemetry event — `llm.request.started` on dispatch, `llm.request.completed` on return with token usage + latency + provider/model.

Sibling of [`integrations/anthropic`](../anthropic/README.md) with the same wrapping pattern and identical wire shapes on the Sensu side.

## Install

```bash
go get github.com/sensu-inc/sdk-go/integrations/openai
```

This is a separate Go module under the `sdk-go` repo so the OpenAI SDK dependency is opt-in — the root `sdk-go` module stays dependency-light for customers who don't use OpenAI.

## Quick start

```go
package main

import (
	"context"
	"log"

	openaiSDK "github.com/openai/openai-go"

	"github.com/sensu-inc/sdk-go"
	sopenai "github.com/sensu-inc/sdk-go/integrations/openai"
)

func main() {
	sensuClient := sensu.New(sensu.ClientOptions{FromEnv: true})
	defer sensuClient.Close(context.Background())

	openaiClient := openaiSDK.NewClient() // reads OPENAI_API_KEY from env
	wrapped := sopenai.Wrap(&openaiClient, sopenai.WrapOptions{Client: sensuClient})

	ctx := context.Background()
	err := sensuClient.Run(ctx, sensu.StartRunOptions{RunType: "chat"}, func(ctx context.Context, _ *sensu.RunHandle) error {
		resp, err := wrapped.Chat.Completions.New(ctx, openaiSDK.ChatCompletionNewParams{
			Model: "gpt-4o",
			Messages: []openaiSDK.ChatCompletionMessageParamUnion{
				openaiSDK.UserMessage("Why is the sky blue?"),
			},
		})
		if err != nil {
			return err
		}
		log.Println(resp.Choices[0].Message.Content)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

## What gets tracked

For each `Chat.Completions.New` call inside a `sensu.Run(...)` callback:

| Event | Fields |
|---|---|
| `llm.request.started` | `provider`, `model` (as requested), `llm_call_id`, `span_id`, `run_id`, `session_id`, `trace_id` |
| `llm.request.completed` | Above + `model` (actual, from response), `status` (success/error), `latency_ms`, `input_tokens` (prompt_tokens), `output_tokens` (completion_tokens), `cached_input_tokens` (from `prompt_tokens_details.cached_tokens`) |

Both events share the same `llm_call_id`; `completed.parent_span_id == started.span_id` keeps the trace chain intact.

## `WrapOptions`

| Field | Type | Default | Notes |
|---|---|---|---|
| `Client` | `*sensu.SensuClient` | **required** | The Sensu client to emit events to. |
| `RunHandle` | `*sensu.RunHandle` | `nil` | Pin the wrapper to an explicit run instead of resolving via `ctx`. Use when context propagation isn't available (serverless, legacy code). |
| `DefaultProvider` | `string` | `"openai"` | Provider label on telemetry events. Override when routing OpenAI-compatible traffic through Azure OpenAI, Fireworks, Groq, etc. — `"azure-openai"` is a common value. |

## Standalone events (no active run)

If `WrapOptions.RunHandle` is nil **and** there's no run in `ctx` (caller forgot to wrap in `sensu.Run(...)`), the wrapper still emits a `llm.request.completed` event with orphan session/run IDs so data isn't silently dropped. No `llm.request.started` in standalone mode (no parent span to link to).

This matches the no-run behavior of [`integrations/anthropic`](../anthropic/README.md) and the TS/Python SDK integrations.

## Error tracking

On a failed OpenAI call (non-2xx, network error, parse failure), the wrapper:
- Returns the underlying SDK error to the caller (no swallowing)
- Emits `llm.request.completed` with `status: "error"`, no `input_tokens`/`output_tokens`/`cached_input_tokens` (the response was nil)
- `latency_ms` reflects the failed-call duration so latency dashboards still show the request

## OpenAI-compatible providers

Many vendors expose OpenAI-compatible APIs. Configure the underlying OpenAI client to point at the provider's base URL + use `DefaultProvider` to label telemetry correctly:

```go
openaiClient := openaiSDK.NewClient(
    option.WithAPIKey(os.Getenv("AZURE_API_KEY")),
    option.WithBaseURL("https://my-resource.openai.azure.com/openai/deployments/gpt-4o"),
)
wrapped := sopenai.Wrap(&openaiClient, sopenai.WrapOptions{
    Client:          sensuClient,
    DefaultProvider: "azure-openai",
})
```

## License

MIT, same as the parent module.
