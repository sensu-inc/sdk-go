package sensu

import (
	"context"
	"sync/atomic"

	"github.com/google/uuid"
)

// RunHandle represents a single agent run.
// Created by SensuClient.StartRun() or SensuClient.Run(); ended by End().
type RunHandle struct {
	client    *SensuClient
	RunID     string
	SessionID string
	AgentID   string
	OrgID     string
	TraceID   string
	SpanID    string

	stepCount atomic.Int32
	ended     atomic.Bool
}

func (r *RunHandle) baseEvent() telemetryEvent {
	return telemetryEvent{
		"event_id":       uuid.New().String(),
		"timestamp":      utcNowISO(),
		"org_id":         r.OrgID,
		"agent_id":       r.AgentID,
		"session_id":     r.SessionID,
		"run_id":         r.RunID,
		"trace_id":       r.TraceID,
		"span_id":        uuid.New().String(),
		"parent_span_id": r.SpanID,
	}
}

// StartStep creates a new StepHandle and emits agent.step.started.
// stepCount is incremented atomically so concurrent calls are safe.
func (r *RunHandle) StartStep(opts StartStepOptions) *StepHandle {
	stepID := opts.StepID
	if stepID == "" {
		stepID = uuid.New().String()
	}
	seq := int(r.stepCount.Add(1)) - 1

	stepType := opts.StepType
	if stepType == "" {
		stepType = "generic"
	}

	step := &StepHandle{
		client:    r.client,
		StepID:    stepID,
		RunID:     r.RunID,
		SessionID: r.SessionID,
		AgentID:   r.AgentID,
		OrgID:     r.OrgID,
		TraceID:   r.TraceID,
		SpanID:    uuid.New().String(),
	}

	ev := mergeEvent(step.baseEvent(), telemetryEvent{
		"event_type": EventStepStarted,
		"step_type":  stepType,
		"sequence":   seq,
	})
	if opts.Name != "" {
		ev["step_name"] = opts.Name
	}
	r.client.batcher.enqueue(ev)

	return step
}

// RecordFeedback emits a feedback.received event on this run.
func (r *RunHandle) RecordFeedback(opts RecordFeedbackOptions) {
	ev := mergeEvent(r.baseEvent(), telemetryEvent{
		"event_type": EventFeedbackReceived,
		"type":       opts.Type,
	})
	if opts.Score != nil {
		ev["score"] = *opts.Score
	}
	if opts.Comment != "" {
		ev["comment"] = opts.Comment
	}
	if opts.EndUserID != "" {
		ev["end_user_id"] = opts.EndUserID
	}
	r.client.batcher.enqueue(ev)
}

// RecordEvalScore emits an eval.score.recorded event on this run.
func (r *RunHandle) RecordEvalScore(opts RecordEvalScoreOptions) {
	ev := mergeEvent(r.baseEvent(), telemetryEvent{
		"event_type": EventEvalScoreRecorded,
		"metric":     opts.Metric,
		"score":      opts.Score,
	})
	if opts.EvaluatorID != "" {
		ev["evaluator_id"] = opts.EvaluatorID
	}
	if opts.ModelUsedForEval != "" {
		ev["model_used_for_eval"] = opts.ModelUsedForEval
	}
	if opts.StepID != "" {
		ev["step_id"] = opts.StepID
	}
	if opts.LLMCallID != "" {
		ev["llm_call_id"] = opts.LLMCallID
	}
	r.client.batcher.enqueue(ev)
}

// Handoff emits an agent.handoff event, recording context transfer to another agent.
func (r *RunHandle) Handoff(opts HandoffOptions) {
	ev := mergeEvent(r.baseEvent(), telemetryEvent{
		"event_type":  EventAgentHandoff,
		"to_agent_id": opts.ToAgentID,
	})
	if opts.Reason != "" {
		ev["reason"] = opts.Reason
	}
	if opts.ContextTokensTransferred != nil {
		ev["context_tokens_transferred"] = *opts.ContextTokensTransferred
	}
	r.client.batcher.enqueue(ev)
}

// End emits agent.run.completed or agent.run.failed and flushes buffered events.
// Idempotent: subsequent calls are no-ops.
func (r *RunHandle) End(ctx context.Context, status string) error {
	if !r.ended.CompareAndSwap(false, true) {
		return nil
	}
	if status == "" {
		status = "completed"
	}
	eventType := EventRunCompleted
	if status == "failed" {
		eventType = EventRunFailed
	}
	r.client.batcher.enqueue(mergeEvent(r.baseEvent(), telemetryEvent{
		"event_type": eventType,
		"status":     status,
	}))
	r.client.clearRunLoopState(r.RunID)
	return r.client.batcher.flush(ctx)
}
