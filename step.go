package sensu

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StepHandle represents a single step within an agent run.
// Created by RunHandle.StartStep(); ended by calling End().
type StepHandle struct {
	client    *SensuClient
	StepID    string
	RunID     string
	SessionID string
	AgentID   string
	OrgID     string
	TraceID   string
	SpanID    string
}

func (s *StepHandle) baseEvent() telemetryEvent {
	return telemetryEvent{
		"event_id":       uuid.New().String(),
		"timestamp":      utcNowISO(),
		"org_id":         s.OrgID,
		"agent_id":       s.AgentID,
		"session_id":     s.SessionID,
		"run_id":         s.RunID,
		"step_id":        s.StepID,
		"trace_id":       s.TraceID,
		"span_id":        uuid.New().String(),
		"parent_span_id": s.SpanID,
	}
}

// End emits agent.step.completed and returns.
func (s *StepHandle) End(_ context.Context) {
	s.client.batcher.enqueue(mergeEvent(s.baseEvent(), telemetryEvent{
		"event_type": EventStepCompleted,
	}))
}

// ---- Non-generic Record* methods -----------------------------------------

// RecordLLM emits an llm.request.completed event with pre-computed stats.
func (s *StepHandle) RecordLLM(opts RecordLLMOptions) {
	ev := mergeEvent(s.baseEvent(), telemetryEvent{
		"event_type": EventLLMRequestCompleted,
		"provider":   opts.Provider,
		"model":      opts.Model,
	})
	if opts.LLMCallID != "" {
		ev["llm_call_id"] = opts.LLMCallID
	}
	if opts.InputTokens != nil {
		ev["input_tokens"] = *opts.InputTokens
	}
	if opts.OutputTokens != nil {
		ev["output_tokens"] = *opts.OutputTokens
	}
	if opts.CachedInputTokens != nil {
		ev["cached_input_tokens"] = *opts.CachedInputTokens
	}
	if opts.TotalTokens != nil {
		ev["total_tokens"] = *opts.TotalTokens
	}
	if opts.MaxContextTokens != nil {
		ev["max_context_tokens"] = *opts.MaxContextTokens
	}
	if opts.ContextUsedTokens != nil {
		ev["context_used_tokens"] = *opts.ContextUsedTokens
	}
	if opts.LatencyMs != nil {
		ev["latency_ms"] = *opts.LatencyMs
	}
	if opts.TTFTMs != nil {
		ev["ttft_ms"] = *opts.TTFTMs
	}
	if opts.CostUSDEstimate != nil {
		ev["cost_usd_estimate"] = *opts.CostUSDEstimate
	}
	if opts.Status != "" {
		ev["status"] = opts.Status
	}
	if opts.ContextBreakdown != nil {
		ev["context_breakdown"] = opts.ContextBreakdown
	}
	if len(opts.MessagesSnapshot) > 0 {
		ev["messages_snapshot"] = opts.MessagesSnapshot
	}
	if len(opts.ReferencedChunkIDs) > 0 {
		ev["referenced_chunk_ids"] = opts.ReferencedChunkIDs
	}
	s.client.batcher.enqueue(ev)
}

// RecordRetrieval emits a retrieval.completed event with pre-computed stats.
func (s *StepHandle) RecordRetrieval(opts RecordRetrievalOptions) {
	ev := mergeEvent(s.baseEvent(), telemetryEvent{
		"event_type": EventRetrievalCompleted,
	})
	if opts.VectorStoreID != "" {
		ev["vector_store_id"] = opts.VectorStoreID
	}
	if opts.TopK != nil {
		ev["top_k"] = *opts.TopK
	}
	if opts.LatencyMs != nil {
		ev["latency_ms"] = *opts.LatencyMs
	}
	if opts.ChunksReturned != nil {
		ev["chunks_returned"] = *opts.ChunksReturned
	}
	if opts.TokensInjected != nil {
		ev["tokens_injected"] = *opts.TokensInjected
	}
	if opts.SimilarityScoreAvg != nil {
		ev["similarity_score_avg"] = *opts.SimilarityScoreAvg
	}
	if opts.Status != "" {
		ev["status"] = opts.Status
	}
	if len(opts.Chunks) > 0 {
		ev["chunks"] = opts.Chunks
	}
	s.client.batcher.enqueue(ev)
}

// RecordEmbedding emits an embedding.created event with pre-computed stats.
func (s *StepHandle) RecordEmbedding(opts RecordEmbeddingOptions) {
	ev := mergeEvent(s.baseEvent(), telemetryEvent{
		"event_type": EventEmbeddingCreated,
		"model":      opts.Model,
	})
	if opts.InputTextLength != nil {
		ev["input_text_length"] = *opts.InputTextLength
	}
	if opts.TokenCount != nil {
		ev["token_count"] = *opts.TokenCount
	}
	if opts.LatencyMs != nil {
		ev["latency_ms"] = *opts.LatencyMs
	}
	if opts.CostUSDEstimate != nil {
		ev["cost_usd_estimate"] = *opts.CostUSDEstimate
	}
	if opts.BatchSize != nil {
		ev["batch_size"] = *opts.BatchSize
	}
	if opts.Status != "" {
		ev["status"] = opts.Status
	}
	s.client.batcher.enqueue(ev)
}

// RecordGuardrail emits guardrail events with pre-computed results.
func (s *StepHandle) RecordGuardrail(opts RecordGuardrailOptions) {
	ev := mergeEvent(s.baseEvent(), telemetryEvent{
		"event_type":     EventGuardrailCheckCompleted,
		"guardrail_id":   opts.GuardrailID,
		"guardrail_type": opts.GuardrailType,
	})
	if opts.InputHash != "" {
		ev["input_hash"] = opts.InputHash
	}
	if opts.Result != "" {
		ev["result"] = opts.Result
	}
	if opts.LatencyMs != nil {
		ev["latency_ms"] = *opts.LatencyMs
	}
	if opts.Blocked {
		ev["blocked"] = true
		if opts.BlockReason != "" {
			ev["block_reason"] = opts.BlockReason
		}
		if opts.Severity != "" {
			ev["severity"] = opts.Severity
		}
		s.client.batcher.enqueue(mergeEvent(s.baseEvent(), telemetryEvent{
			"event_type":     EventGuardrailBlocked,
			"guardrail_id":   opts.GuardrailID,
			"guardrail_type": opts.GuardrailType,
			"block_reason":   opts.BlockReason,
			"severity":       opts.Severity,
		}))
	}
	s.client.batcher.enqueue(ev)
}

// RecordPromptRender emits a prompt.rendered event.
func (s *StepHandle) RecordPromptRender(opts RecordPromptRenderOptions) {
	ev := mergeEvent(s.baseEvent(), telemetryEvent{
		"event_type":  EventPromptRendered,
		"template_id": opts.TemplateID,
	})
	if opts.TemplateVersion != "" {
		ev["template_version"] = opts.TemplateVersion
	}
	if opts.RenderedTokenCount != nil {
		ev["rendered_token_count"] = *opts.RenderedTokenCount
	}
	if opts.VariableCount != nil {
		ev["variable_count"] = *opts.VariableCount
	}
	if opts.LatencyMs != nil {
		ev["latency_ms"] = *opts.LatencyMs
	}
	s.client.batcher.enqueue(ev)
}

// ---- Generic Track* package-level functions ---------------------------------
// Go does not allow type parameters on method receivers, so these are
// package-level functions. This is the standard Go pattern (see slices.Map).

// TrackLLM wraps an LLM call fn, measures latency, extracts token usage via
// reflection, resolves live pricing, and emits llm.request.started/completed.
// T is the return type of fn (e.g., *anthropic.Message, *openai.ChatCompletion).
func TrackLLM[T any](ctx context.Context, step *StepHandle, fn func() (T, error), opts TrackLLMOptions) (T, error) {
	start := time.Now()
	llmCallID := opts.LLMCallID
	if llmCallID == "" {
		llmCallID = uuid.New().String()
	}
	spanID := uuid.New().String()

	started := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type": EventLLMRequestStarted,
		"span_id":    spanID,
		"provider":   opts.Provider,
		"model":      opts.Model,
		"llm_call_id": llmCallID,
	})
	if opts.MaxContextTokens != nil {
		started["max_context_tokens"] = *opts.MaxContextTokens
	}
	step.client.batcher.enqueue(started)

	result, err := fn()

	latencyMs := float64(time.Since(start).Milliseconds())
	status := "success"
	if err != nil {
		status = "error"
	}

	usage := extractUsage(result)
	pricing := resolvePricing(ctx, step.client.httpClient,
		step.client.baseURL, step.client.apiKey,
		opts.Provider, opts.Model,
		step.client.pricing,
		step.client.disableLivePricing, step.client.disabled)

	completed := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type":  EventLLMRequestCompleted,
		"span_id":     spanID,
		"llm_call_id": llmCallID,
		"provider":    opts.Provider,
		"model":       opts.Model,
		"latency_ms":  latencyMs,
		"status":      status,
	})
	if usage.inputTokens > 0 {
		completed["input_tokens"] = usage.inputTokens
	}
	if usage.outputTokens > 0 {
		completed["output_tokens"] = usage.outputTokens
	}
	if usage.cachedInputTokens != nil {
		completed["cached_input_tokens"] = *usage.cachedInputTokens
	}
	if usage.totalTokens > 0 {
		completed["total_tokens"] = usage.totalTokens
	}
	if usage.inputTokens > 0 || usage.outputTokens > 0 {
		cost := estimateCost(pricing, usage.inputTokens, usage.outputTokens)
		completed["cost_usd_estimate"] = cost
	}
	if opts.MaxContextTokens != nil {
		completed["max_context_tokens"] = *opts.MaxContextTokens
	}
	if opts.ExtractContextBreakdown != nil {
		if bd := opts.ExtractContextBreakdown(result); bd != nil {
			completed["context_breakdown"] = bd
		}
	}
	if len(opts.MessagesSnapshot) > 0 {
		completed["messages_snapshot"] = opts.MessagesSnapshot
	}
	if len(opts.ReferencedChunkIDs) > 0 {
		completed["referenced_chunk_ids"] = opts.ReferencedChunkIDs
	}
	step.client.batcher.enqueue(completed)

	return result, err
}

// TrackTool wraps a tool call fn, measures latency, and emits
// tool.call.started/completed. Also notifies the client for loop detection.
func TrackTool[T any](ctx context.Context, step *StepHandle, fn func() (T, error), opts TrackToolOptions) (T, error) {
	start := time.Now()
	toolCallID := uuid.New().String()

	started := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type":   EventToolCallStarted,
		"tool_name":    opts.ToolName,
		"tool_call_id": toolCallID,
	})
	if opts.RetryOf != "" {
		started["retry_of"] = opts.RetryOf
	}
	step.client.batcher.enqueue(started)

	result, err := fn()

	latencyMs := float64(time.Since(start).Milliseconds())
	status := "success"
	if err != nil {
		status = "error"
	}

	step.client.batcher.enqueue(mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type":        EventToolCallCompleted,
		"tool_name":         opts.ToolName,
		"tool_call_id":      toolCallID,
		"latency_ms":        latencyMs,
		"status":            status,
		"output_size_bytes": estimateBytes(result),
	}))

	step.client.notifyToolCall(step.RunID, opts.ToolName)

	return result, err
}

// TrackRetrieval wraps a vector-store retrieval call and emits
// retrieval.started/completed.
func TrackRetrieval[T any](ctx context.Context, step *StepHandle, fn func() (T, error), opts TrackRetrievalOptions) (T, error) {
	start := time.Now()
	spanID := uuid.New().String()

	started := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type": EventRetrievalStarted,
		"span_id":    spanID,
	})
	if opts.VectorStoreID != "" {
		started["vector_store_id"] = opts.VectorStoreID
	}
	if opts.TopK != nil {
		started["top_k"] = *opts.TopK
	}
	step.client.batcher.enqueue(started)

	result, err := fn()

	latencyMs := float64(time.Since(start).Milliseconds())
	status := "success"
	if err != nil {
		status = "error"
	}

	completed := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type": EventRetrievalCompleted,
		"span_id":    spanID,
		"latency_ms": latencyMs,
		"status":     status,
	})
	if opts.VectorStoreID != "" {
		completed["vector_store_id"] = opts.VectorStoreID
	}
	if opts.TopK != nil {
		completed["top_k"] = *opts.TopK
	}
	step.client.batcher.enqueue(completed)

	return result, err
}

// TrackEmbedding wraps an embedding generation call.
func TrackEmbedding[T any](ctx context.Context, step *StepHandle, fn func() (T, error), opts TrackEmbeddingOptions) (T, error) {
	start := time.Now()
	result, err := fn()
	latencyMs := float64(time.Since(start).Milliseconds())

	ev := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type": EventEmbeddingCreated,
		"model":      opts.Model,
		"latency_ms": latencyMs,
	})
	if opts.InputTextLength != nil {
		ev["input_text_length"] = *opts.InputTextLength
	}
	if opts.BatchSize != nil {
		ev["batch_size"] = *opts.BatchSize
	}
	if err != nil {
		ev["status"] = "error"
	} else {
		ev["status"] = "success"
	}
	step.client.batcher.enqueue(ev)

	return result, err
}

// TrackGuardrail wraps a guardrail check fn that returns "pass", "fail", or "modified".
func TrackGuardrail(ctx context.Context, step *StepHandle, fn func() (string, error), opts TrackGuardrailOptions) (string, error) {
	start := time.Now()

	step.client.batcher.enqueue(mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type":     EventGuardrailCheckStarted,
		"guardrail_id":   opts.GuardrailID,
		"guardrail_type": opts.GuardrailType,
		"input_hash":     opts.InputHash,
	}))

	result, err := fn()
	latencyMs := float64(time.Since(start).Milliseconds())

	ev := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type":     EventGuardrailCheckCompleted,
		"guardrail_id":   opts.GuardrailID,
		"guardrail_type": opts.GuardrailType,
		"result":         result,
		"latency_ms":     latencyMs,
	})
	if err != nil {
		ev["status"] = "error"
	}
	step.client.batcher.enqueue(ev)

	if result == "fail" {
		step.client.batcher.enqueue(mergeEvent(step.baseEvent(), telemetryEvent{
			"event_type":     EventGuardrailBlocked,
			"guardrail_id":   opts.GuardrailID,
			"guardrail_type": opts.GuardrailType,
		}))
	}

	return result, err
}

// TrackStreamingLLM consumes a channel of text chunks, measuring TTFT and total
// latency. The caller is responsible for feeding chunks into streamCh and closing
// it when the stream ends.
func TrackStreamingLLM(ctx context.Context, step *StepHandle, streamCh <-chan string, opts TrackStreamingLLMOptions) (string, error) {
	start := time.Now()
	llmCallID := opts.LLMCallID
	if llmCallID == "" {
		llmCallID = uuid.New().String()
	}
	emitEvery := opts.EmitEveryNTokens
	if emitEvery <= 0 {
		emitEvery = 10
	}

	step.client.batcher.enqueue(mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type":  EventLLMRequestStarted,
		"provider":    opts.Provider,
		"model":       opts.Model,
		"llm_call_id": llmCallID,
		"stream":      true,
	}))

	var (
		ttftMs      *float64
		tokenCount  int
		accumulated string
	)

	for chunk := range streamCh {
		if ttftMs == nil {
			ms := float64(time.Since(start).Milliseconds())
			ttftMs = &ms
		}
		accumulated += chunk
		tokenCount++
		if tokenCount%emitEvery == 0 {
			step.client.batcher.enqueue(mergeEvent(step.baseEvent(), telemetryEvent{
				"event_type":    EventStreamTokenReceived,
				"llm_call_id":   llmCallID,
				"tokens_so_far": tokenCount,
				"ttft_ms":       ttftMs,
			}))
		}
	}

	latencyMs := float64(time.Since(start).Milliseconds())

	completed := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type":  EventLLMRequestCompleted,
		"provider":    opts.Provider,
		"model":       opts.Model,
		"llm_call_id": llmCallID,
		"latency_ms":  latencyMs,
		"streamed":    true,
		"status":      "success",
	})
	if ttftMs != nil {
		completed["ttft_ms"] = *ttftMs
	}
	step.client.batcher.enqueue(completed)

	if opts.OnComplete != nil {
		opts.OnComplete(accumulated, ttftMs)
	}

	return accumulated, nil
}
