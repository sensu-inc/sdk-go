package senzu

// Event type constants — mirror EventTypeSchema in packages/shared/src/schemas.ts.
const (
	EventRunStarted  = "agent.run.started"
	EventRunCompleted = "agent.run.completed"
	EventRunFailed   = "agent.run.failed"

	EventStepStarted   = "agent.step.started"
	EventStepCompleted = "agent.step.completed"

	EventLLMRequestStarted   = "llm.request.started"
	EventLLMRequestCompleted = "llm.request.completed"
	EventStreamTokenReceived = "stream.token.received"

	EventToolCallStarted   = "tool.call.started"
	EventToolCallCompleted = "tool.call.completed"

	EventRetrievalStarted   = "retrieval.started"
	EventRetrievalCompleted = "retrieval.completed"

	EventEmbeddingCreated = "embedding.created"

	EventFeedbackReceived   = "feedback.received"
	EventEvalScoreRecorded  = "eval.score.recorded"

	EventAgentSpawned = "agent.spawned"
	EventAgentHandoff = "agent.handoff"

	EventSessionStarted = "session.started"
	EventSessionEnded   = "session.ended"
	EventSessionResumed = "session.resumed"

	EventGuardrailCheckStarted   = "guardrail.check.started"
	EventGuardrailCheckCompleted = "guardrail.check.completed"
	EventGuardrailBlocked        = "guardrail.blocked"

	EventPromptRendered        = "prompt.rendered"
	EventPromptVersionDeployed = "prompt.version.deployed"

	EventToolLoopDetected = "tool.loop.detected"
)

// telemetryEvent is a loosely-typed event map, matching the TS/Python
// object-spread composition style. json.Marshal + nil checks handle omission.
type telemetryEvent map[string]any
