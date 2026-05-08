package senzu

// ClientOptions configures a SenzuClient. All fields are optional;
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
	DebugMode          bool
	LoopThreshold      int // default 5; fires OnLoopDetected when a tool is called this many times
	OnLoopDetected     func(toolName string, callCount int)
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
	Role         string `json:"role"`
	ToolName     string `json:"tool_name,omitempty"`
	TokenCount   int    `json:"token_count"`
	ContentHash  string `json:"content_hash,omitempty"`
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
