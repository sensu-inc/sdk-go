// Package openai provides a Sensu wrapper for the OpenAI Go SDK.
//
// Usage:
//
//	import (
//	    openaiSDK "github.com/openai/openai-go"
//	    sopenai "github.com/sensu-inc/sdk-go/integrations/openai"
//	    "github.com/sensu-inc/sdk-go"
//	)
//
//	sensuClient := sensu.New(sensu.ClientOptions{FromEnv: true})
//	openaiClient := openaiSDK.NewClient()
//	wrapped := sopenai.Wrap(&openaiClient, sopenai.WrapOptions{Client: sensuClient})
//
//	// Inside a sensu.Run() callback, Chat.Completions.New is auto-tracked:
//	resp, err := wrapped.Chat.Completions.New(ctx, params)
//
// Mirrors integrations/anthropic — same wrapping pattern, same event
// shapes on the wire. The only differences from the Anthropic wrapper
// are: (a) the underlying SDK's call path is Chat.Completions.New
// instead of Messages.New, (b) the usage field names are different
// (prompt_tokens / completion_tokens / cached_tokens via
// PromptTokensDetails), and (c) provider defaults to "openai".
package openai

import (
	"context"
	"time"

	openaiSDK "github.com/openai/openai-go"

	"github.com/google/uuid"
	"github.com/sensu-inc/sdk-go"
)

// WrapOptions configures the OpenAI wrapper.
type WrapOptions struct {
	// Client is the Sensu client to emit events to. Required.
	Client *sensu.SensuClient
	// RunHandle pins this wrapper to an explicit run instead of resolving
	// the run from context. Useful in environments without context propagation.
	RunHandle *sensu.RunHandle
	// DefaultProvider is used in telemetry events (default: "openai").
	DefaultProvider string
}

// WrappedOpenAI is a thin proxy around *openaiSDK.Client that
// auto-tracks all Chat.Completions.New calls via the Sensu SDK.
//
// Because Go does not allow monkey-patching method pointers, WrappedOpenAI
// is a separate struct. It exposes a Chat field that mirrors the OpenAI
// SDK's Chat field, making it a near-drop-in replacement in typed code.
type WrappedOpenAI struct {
	Inner *openaiSDK.Client
	opts  WrapOptions
	Chat  WrappedChat
}

// WrappedChat mirrors openaiSDK.Client.Chat.
type WrappedChat struct {
	Completions WrappedChatCompletions
}

// WrappedChatCompletions mirrors openaiSDK.ChatService.Completions.
type WrappedChatCompletions struct {
	wo *WrappedOpenAI
}

// Wrap wraps an *openaiSDK.Client so that every Chat.Completions.New call is
// automatically tracked as a Sensu LLM telemetry event.
func Wrap(client *openaiSDK.Client, opts WrapOptions) *WrappedOpenAI {
	if opts.DefaultProvider == "" {
		opts.DefaultProvider = "openai"
	}
	wo := &WrappedOpenAI{Inner: client, opts: opts}
	wo.Chat = WrappedChat{Completions: WrappedChatCompletions{wo: wo}}
	return wo
}

// New calls the underlying OpenAI Chat.Completions.New and emits
// llm.request.started + llm.request.completed Sensu events.
//
// Run resolution order (same as the Anthropic integration + TS/Python):
//  1. opts.RunHandle — explicit; takes priority
//  2. ctx RunHandle  — set by client.Run()
//  3. Standalone event — emitted without a run so no data is silently dropped
func (m *WrappedChatCompletions) New(
	ctx context.Context, params openaiSDK.ChatCompletionNewParams,
) (*openaiSDK.ChatCompletion, error) {
	wo := m.wo
	c := wo.opts.Client
	provider := wo.opts.DefaultProvider
	model := string(params.Model)

	run := wo.opts.RunHandle
	if run == nil {
		run = c.GetActiveRun(ctx)
	}

	llmCallID := uuid.New().String()
	spanID := uuid.New().String()

	if run != nil {
		startedEv := map[string]any{
			"event_id":    uuid.New().String(),
			"event_type":  sensu.EventLLMRequestStarted,
			"timestamp":   utcNow(),
			"org_id":      c.OrgID(),
			"agent_id":    c.AgentID(),
			"session_id":  run.SessionID,
			"run_id":      run.RunID,
			"trace_id":    run.TraceID,
			"span_id":     spanID,
			"llm_call_id": llmCallID,
			"provider":    provider,
			"model":       model,
		}
		c.Enqueue(startedEv)
	}

	start := time.Now()
	resp, err := wo.Inner.Chat.Completions.New(ctx, params)
	latencyMs := float64(time.Since(start).Milliseconds())

	status := "success"
	if err != nil {
		status = "error"
	}

	var (
		inputTokens       int
		outputTokens      int
		cachedInputTokens *int
		actualModel       = model
	)
	if resp != nil {
		// OpenAI: prompt_tokens = total inputs; cached_tokens = cache hits
		// within that. We report INPUT as total prompt tokens (matches the
		// platform's convention) and CACHED separately for cost-saving
		// visibility.
		inputTokens = int(resp.Usage.PromptTokens)
		outputTokens = int(resp.Usage.CompletionTokens)
		if v := int(resp.Usage.PromptTokensDetails.CachedTokens); v > 0 {
			cachedInputTokens = &v
		}
		if resp.Model != "" {
			actualModel = resp.Model
		}
	}

	completedEv := map[string]any{
		"event_id":    uuid.New().String(),
		"event_type":  sensu.EventLLMRequestCompleted,
		"timestamp":   utcNow(),
		"org_id":      c.OrgID(),
		"agent_id":    c.AgentID(),
		"trace_id":    uuid.New().String(),
		"span_id":     uuid.New().String(),
		"llm_call_id": llmCallID,
		"provider":    provider,
		"model":       actualModel,
		"latency_ms":  latencyMs,
		"status":      status,
	}
	if run != nil {
		completedEv["session_id"] = run.SessionID
		completedEv["run_id"] = run.RunID
		completedEv["trace_id"] = run.TraceID
		completedEv["parent_span_id"] = spanID
	} else {
		completedEv["session_id"] = uuid.New().String()
		completedEv["run_id"] = uuid.New().String()
	}
	if inputTokens > 0 {
		completedEv["input_tokens"] = inputTokens
	}
	if outputTokens > 0 {
		completedEv["output_tokens"] = outputTokens
	}
	if cachedInputTokens != nil {
		completedEv["cached_input_tokens"] = *cachedInputTokens
	}

	c.Enqueue(completedEv)

	return resp, err
}

func utcNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
