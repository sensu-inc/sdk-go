package sensu

// ClientOptions configures a SensuClient. All fields are optional;
// zero values fall back to defaults or environment variables when FromEnv is true.
type ClientOptions struct {
	APIKey             string
	BaseURL            string
	AgentID            string
	OrgID              string
	FromEnv            bool
	BatchSize          int  // default 10
	FlushIntervalMs    int  // default 2000
	Disabled           bool
	DisableLivePricing bool
	// PricingCacheTTLMs — how long the SDK caches resolved per-(provider,
	// model) pricing before refetching from the Sensu API. After expiry
	// the next ResolvePricing-driven call hits the live endpoint and
	// replaces the cached entry.
	//
	// Defaults: zero-value (field left unset) → 1 hour (3_600_000). To
	// disable caching entirely, set this to NoPricingCache (-1) — this
	// sentinel exists because Go can't distinguish "not set" from
	// "explicitly 0" on a primitive int. Set to a positive value to
	// override the default.
	//
	// Long-running services should keep this short enough that pricing
	// changes propagate within the freshness window dashboards depend
	// on; short-lived processes (Lambda, CLI) effectively get fresh
	// pricing per invocation regardless.
	//
	// Parity with sdk-ts (pricingCacheTtlMs, 0 = disable) and sdk-python
	// (pricing_cache_ttl_ms, 0 = disable). Go uses the NoPricingCache
	// sentinel instead of 0 due to the zero-value caveat above.
	PricingCacheTTLMs int
	DebugMode         bool
	LoopThreshold     int // default 5; fires OnLoopDetected when a tool is called this many times
	OnLoopDetected    func(toolName string, callCount int)
	// CaptureMessageBodies — when true, raw message Bodies on
	// MessagesSnapshot are forwarded to the API. The API masks PII via its
	// shared pipeline at ingest; the raw form stays tenant-side and
	// requires an audited unmask to read. Default false (privacy + back-compat).
	// See planning/REPLAY_V1_PLAN.md §7.
	CaptureMessageBodies bool
}

// StartRunOptions controls a new agent run.
type StartRunOptions struct {
	RunID     string
	SessionID string
	RunType   string
	EndUserID string
	Tags      map[string]string
}

// StartStepOptions controls a new step within a run.
type StartStepOptions struct {
	StepID   string
	Name     string
	StepType string // "llm" | "tool" | "retrieval" | "embedding" | "guardrail" | "generic"
	Sequence *int
}

// ContextBreakdown records how context tokens are distributed by role.
type ContextBreakdown struct {
	SystemTokens    *int `json:"system_tokens,omitempty"`
	UserTokens      *int `json:"user_tokens,omitempty"`
	AssistantTokens *int `json:"assistant_tokens,omitempty"`
	ToolTokens      *int `json:"tool_tokens,omitempty"`
	RetrievalTokens *int `json:"retrieval_tokens,omitempty"`
	CachedTokens    *int `json:"cached_tokens,omitempty"`
}

// MessageSnapshotItem is one message entry in a context-window snapshot.
type MessageSnapshotItem struct {
	Role        string `json:"role"`
	ToolName    string `json:"tool_name,omitempty"`
	TokenCount  int    `json:"token_count"`
	ContentHash string `json:"content_hash,omitempty"`
	// Body is the raw message text. Only forwarded to the API when the
	// SensuClient was constructed with CaptureMessageBodies=true; the
	// sanitizer in client.go strips it otherwise. Capped at 65,536 chars
	// client-side to match the server schema. See REPLAY_V1_PLAN.md §7.
	Body string `json:"body,omitempty"`
}

// TrackLLMOptions wraps an LLM call. Fn is provided at the call site via TrackLLM[T].
type TrackLLMOptions struct {
	Provider                string
	Model                   string
	MaxContextTokens        *int
	LLMCallID               string
	MessagesSnapshot        []MessageSnapshotItem
	ReferencedChunkIDs      []string
	ExtractContextBreakdown func(result any) *ContextBreakdown
}

// TrackStreamingLLMOptions wraps a streaming LLM call fed via a channel of text chunks.
type TrackStreamingLLMOptions struct {
	Provider         string
	Model            string
	MaxContextTokens *int
	LLMCallID        string
	EmitEveryNTokens int // default 10
	OnComplete       func(text string, ttftMs *float64)
}

// RecordLLMOptions records a pre-computed LLM event (when stats are already known).
type RecordLLMOptions struct {
	Provider           string
	Model              string
	InputTokens        *int
	OutputTokens       *int
	CachedInputTokens  *int
	TotalTokens        *int
	MaxContextTokens   *int
	ContextUsedTokens  *int
	LatencyMs          *float64
	TTFTMs             *float64
	CostUSDEstimate    *float64
	Status             string // "success" | "error" | "timeout"
	ContextBreakdown   *ContextBreakdown
	MessagesSnapshot   []MessageSnapshotItem
	ReferencedChunkIDs []string
	LLMCallID          string
}

// TrackToolOptions wraps a tool call.
type TrackToolOptions struct {
	ToolName string
	RetryOf  string // tool_call_id of a prior failed attempt (links retry chain)

	// Args is the tool-call input. When CaptureBodies is true, Args is
	// JSON-marshaled and shipped on tool.call.completed as input_body;
	// the awaited result of fn becomes output_body. Server runs the PII
	// pipeline on each at ingest, so raw bodies never leave the tenant
	// boundary unmasked. Default behavior (CaptureBodies false) keeps
	// the v1 shape — neither body field is emitted.
	Args any

	// CaptureBodies, when true, opts the call into Tool I/O body
	// capture (TOOL_IO_CAPTURE_PLAN.md §5.3). Default OFF (the Go bool
	// zero value); opt-in per call so storage and PII exposure are
	// explicit decisions per §11.2. Same posture as sdk-ts and
	// sdk-python.
	//
	// Serialization is best-effort: if encoding/json.Marshal returns
	// an error for either side (channels, complex numbers, function
	// values, cyclic structures via MarshalJSON, etc.) both bodies are
	// skipped so the inspector's "snapshotMissing" affordance stays
	// coherent (§11.4). Never half-capture.
	CaptureBodies bool
}

// TrackRetrievalOptions wraps a vector-store retrieval call.
type TrackRetrievalOptions struct {
	VectorStoreID string
	TopK          *int
}

// RetrievalChunkInput describes a single retrieved chunk for noise analysis.
type RetrievalChunkInput struct {
	ChunkID         string   `json:"chunk_id"`
	Source          string   `json:"source,omitempty"`
	TokenCount      int      `json:"token_count"`
	SimilarityScore *float64 `json:"similarity_score,omitempty"`
	ContentPreview  string   `json:"content_preview,omitempty"`
}

// RecordRetrievalOptions records a pre-computed retrieval event.
type RecordRetrievalOptions struct {
	VectorStoreID      string
	TopK               *int
	LatencyMs          *float64
	ChunksReturned     *int
	TokensInjected     *int
	SimilarityScoreAvg *float64
	Status             string // "success" | "error"
	Chunks             []RetrievalChunkInput
}

// TrackEmbeddingOptions wraps an embedding generation call.
type TrackEmbeddingOptions struct {
	Model           string
	InputTextLength *int
	BatchSize       *int
}

// RecordEmbeddingOptions records a pre-computed embedding event.
type RecordEmbeddingOptions struct {
	Model           string
	InputTextLength *int
	TokenCount      *int
	LatencyMs       *float64
	CostUSDEstimate *float64
	BatchSize       *int
	Status          string
}

// TrackGuardrailOptions wraps a guardrail check.
type TrackGuardrailOptions struct {
	GuardrailID   string
	GuardrailType string // "content" | "pii" | "jailbreak" | "custom"
	InputHash     string
}

// RecordGuardrailOptions records a pre-computed guardrail event.
type RecordGuardrailOptions struct {
	GuardrailID   string
	GuardrailType string
	InputHash     string
	Result        string   // "pass" | "fail" | "modified"
	BlockReason   string
	Severity      string   // "low" | "medium" | "high"
	LatencyMs     *float64
	Blocked       bool
}

// RecordPromptRenderOptions records a prompt template render event.
type RecordPromptRenderOptions struct {
	TemplateID         string
	TemplateVersion    string
	RenderedTokenCount *int
	VariableCount      *int
	LatencyMs          *float64
}

// RecordFeedbackOptions records end-user feedback on a run.
type RecordFeedbackOptions struct {
	Type      string   // "thumbs_up" | "thumbs_down" | "score" | "correction"
	Score     *float64
	Comment   string
	EndUserID string
}

// RecordEvalScoreOptions records an automated evaluation metric.
type RecordEvalScoreOptions struct {
	Metric           string
	Score            float64
	EvaluatorID      string
	ModelUsedForEval string
	StepID           string
	LLMCallID        string
}

// FeedbackOptions are the options for the run-less, top-level
// SensuClient.Feedback() helper. RunID is required because there is no
// active run handle.
type FeedbackOptions struct {
	RunID     string   // required
	Type      string   // required: "thumbs_up" | "thumbs_down" | "score" | "correction"
	Score     *float64 // optional
	Comment   string   // optional
	EndUserID string   // optional
}

// ScoreOptions are the options for the run-less, top-level
// SensuClient.Score() helper. RunID is required because there is no
// active run handle.
type ScoreOptions struct {
	RunID            string  // required
	Metric           string  // required
	Score            float64 // required
	EvaluatorID      string  // optional
	ModelUsedForEval string  // optional
	StepID           string  // optional
	LLMCallID        string  // optional
}

// CandidateConfig is the candidate config registered under an agent
// version, used by the eval-gated CI/CD flow (§5.2). Mirrors the API
// shape: SystemPrompt required, Model optional (defaults to the sampled
// run's source model at gate time).
type CandidateConfig struct {
	SystemPrompt string `json:"systemPrompt"`
	Model        string `json:"model,omitempty"`
}

// RegisterAgentVersionOptions are the options for the run-less, top-level
// SensuClient.RegisterAgentVersion() helper.
type RegisterAgentVersionOptions struct {
	AgentID string          // required — customer-facing agent name
	SHA     string          // required — opaque identifier, usually a git commit SHA
	Config  CandidateConfig // required
}

// AgentVersion is the server response shape for a registered agent
// version.
type AgentVersion struct {
	ID        string          `json:"id"`
	AgentID   string          `json:"agentId"`
	SHA       string          `json:"sha"`
	Config    CandidateConfig `json:"config"`
	CreatedAt string          `json:"createdAt"`
}

// SpawnRunOptions creates a child run from a parent.
type SpawnRunOptions struct {
	ChildAgentID string
	ChildRunID   string
	SpawnReason  string
	RunType      string
	SessionID    string
}

// HandoffOptions records an agent-to-agent handoff.
type HandoffOptions struct {
	ToAgentID                string
	Reason                   string
	ContextTokensTransferred *int
}

// StartSessionOptions starts an explicit user session.
type StartSessionOptions struct {
	SessionID string
	Channel   string // "web" | "api" | "mobile"
	EndUserID string
}

// ResumeSessionOptions resumes a prior session.
type ResumeSessionOptions struct {
	SessionID            string
	ResumedFromSessionID string
	Channel              string
	EndUserID            string
}

// DeployPromptVersionOptions records a prompt template version deployment.
type DeployPromptVersionOptions struct {
	TemplateID string
	NewVersion string
	OldVersion string
	DeployedBy string
}
