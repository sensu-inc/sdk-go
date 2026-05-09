// Package anthropic provides a Sensu wrapper for the Anthropic Go SDK.
//
// Usage:
//
//	import (
//	    anthropicSDK "github.com/anthropics/anthropic-sdk-go"
//	    santhropicSDK "github.com/anthropics/anthropic-sdk-go/option"
//	    santhropic "github.com/sensu-inc/sdk-go/integrations/anthropic"
//	    "github.com/sensu-inc/sdk-go"
//	)
//
//	sensuClient := sensu.New(sensu.ClientOptions{FromEnv: true})
//	anthropicClient := anthropicSDK.NewClient()
//	wrapped := santhropic.Wrap(anthropicClient, santhropic.WrapOptions{Client: sensuClient})
//
//	// Inside a sensu.Run() callback, Messages.New is auto-tracked:
//	resp, err := wrapped.Messages.New(ctx, params)
package anthropic

import (
	"context"
	"time"

	anthropicSDK "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/sensu-inc/sdk-go"
)

// WrapOptions configures the Anthropic wrapper.
type WrapOptions struct {
	// Client is the Sensu client to emit events to. Required.
	Client *sensu.SensuClient
	// RunHandle pins this wrapper to an explicit run instead of resolving
	// the run from context. Useful in environments without context propagation.
	RunHandle *sensu.RunHandle
	// DefaultProvider is used in telemetry events (default: "anthropic").
	DefaultProvider string
}

// WrappedAnthropic is a thin proxy around *anthropicSDK.Client that
// auto-tracks all Messages.New calls via the Sensu SDK.
//
// Because Go does not allow monkey-patching method pointers, WrappedAnthropic
// is a separate struct. It exposes a Messages field that mirrors the Anthropic
// SDK's Messages field, making it a near-drop-in replacement in typed code.
type WrappedAnthropic struct {
	Inner    *anthropicSDK.Client
	opts     WrapOptions
	Messages WrappedMessages
}

// WrappedMessages mirrors anthropicSDK.Client.Messages.
type WrappedMessages struct {
	wa *WrappedAnthropic
}

// Wrap wraps an *anthropicSDK.Client so that every Messages.New call is
// automatically tracked as a Sensu LLM telemetry event.
func Wrap(client *anthropicSDK.Client, opts WrapOptions) *WrappedAnthropic {
	if opts.DefaultProvider == "" {
		opts.DefaultProvider = "anthropic"
	}
	wa := &WrappedAnthropic{Inner: client, opts: opts}
	wa.Messages = WrappedMessages{wa: wa}
	return wa
}

// New calls the underlying Anthropic Messages.New and emits
// llm.request.started + llm.request.completed Sensu events.
//
// Run resolution order (same as TS/Python integrations):
//  1. opts.RunHandle — explicit; takes priority
//  2. ctx RunHandle  — set by client.Run()
//  3. Standalone event — emitted without a run so no data is silently dropped
func (m *WrappedMessages) New(ctx context.Context, params anthropicSDK.MessageNewParams) (*anthropicSDK.Message, error) {
	wa := m.wa
	c := wa.opts.Client
	provider := wa.opts.DefaultProvider
	model := string(params.Model)

	// Resolve active run
	run := wa.opts.RunHandle
	if run == nil {
		run = c.GetActiveRun(ctx)
	}

	llmCallID := uuid.New().String()
	spanID := uuid.New().String()

	// Emit llm.request.started (either via a step or standalone)
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
	resp, err := wa.Inner.Messages.New(ctx, params)
	latencyMs := float64(time.Since(start).Milliseconds())

	status := "success"
	if err != nil {
		status = "error"
	}

	// Extract token usage
	var (
		inputTokens       int
		outputTokens      int
		cachedInputTokens *int
		actualModel       = model
	)
	if resp != nil {
		inputTokens = int(resp.Usage.InputTokens) + int(resp.Usage.CacheCreationInputTokens)
		outputTokens = int(resp.Usage.OutputTokens)
		if v := int(resp.Usage.CacheReadInputTokens); v > 0 {
			cachedInputTokens = &v
		}
		actualModel = string(resp.Model)
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
		// Standalone event — generate orphan IDs so it still ingests
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
