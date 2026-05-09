package sensu

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SensuClient is the main entry point for Sensu AI observability telemetry.
type SensuClient struct {
	apiKey             string
	baseURL            string
	agentID            string
	orgID              string
	disabled           bool
	disableLivePricing bool
	debugMode          bool
	loopThreshold      int
	onLoopDetected     func(toolName string, callCount int)

	batcher    *batcher
	pricing    *pricingCache
	httpClient *http.Client

	// runToolCallCounts: runID → toolName → count; protected by mu.
	mu                sync.Mutex
	runToolCallCounts map[string]map[string]int
}

// NewClient creates a SensuClient. All fields in opts are optional.
// When FromEnv is true, SENZU_API_KEY, SENZU_BASE_URL, SENZU_AGENT_ID, and
// SENZU_ORG_ID are read from the environment.
func NewClient(opts ClientOptions) *SensuClient {
	apiKey := opts.APIKey
	baseURL := opts.BaseURL
	agentID := opts.AgentID
	orgID := opts.OrgID

	if opts.FromEnv {
		if v := os.Getenv("SENZU_API_KEY"); v != "" && apiKey == "" {
			apiKey = v
		}
		if v := os.Getenv("SENZU_BASE_URL"); v != "" && baseURL == "" {
			baseURL = v
		}
		if v := os.Getenv("SENZU_AGENT_ID"); v != "" && agentID == "" {
			agentID = v
		}
		if v := os.Getenv("SENZU_ORG_ID"); v != "" && orgID == "" {
			orgID = v
		}
	}

	if baseURL == "" {
		baseURL = "http://localhost:3001"
	}
	if agentID == "" {
		agentID = "unknown-agent"
	}

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}
	flushInterval := opts.FlushIntervalMs
	if flushInterval <= 0 {
		flushInterval = 2000
	}
	loopThreshold := opts.LoopThreshold
	if loopThreshold <= 0 {
		loopThreshold = 5
	}

	c := &SensuClient{
		apiKey:             apiKey,
		baseURL:            baseURL,
		agentID:            agentID,
		orgID:              orgID,
		disabled:           opts.Disabled,
		disableLivePricing: opts.DisableLivePricing,
		debugMode:          opts.DebugMode,
		loopThreshold:      loopThreshold,
		onLoopDetected:     opts.OnLoopDetected,
		pricing:            newPricingCache(),
		httpClient:         &http.Client{Timeout: 15 * time.Second},
		runToolCallCounts:  make(map[string]map[string]int),
	}
	c.batcher = newBatcher(apiKey, baseURL, batchSize, flushInterval,
		opts.DebugMode, opts.Disabled)
	return c
}

// AgentID returns the configured agent ID.
func (c *SensuClient) AgentID() string { return c.agentID }

// OrgID returns the configured org ID.
func (c *SensuClient) OrgID() string { return c.orgID }

// Run is the high-level context-propagating wrapper. It:
//  1. Creates a RunHandle and emits agent.run.started
//  2. Injects the RunHandle into ctx via contextWithRun
//  3. Calls fn(ctx, run) — any code that receives ctx can call GetActiveRun(ctx)
//  4. Emits agent.run.completed or agent.run.failed, then flushes
//
// This is the Go equivalent of sensu.run() with AsyncLocalStorage (TS) and
// the `async with client.run()` context manager (Python).
func (c *SensuClient) Run(ctx context.Context, opts StartRunOptions, fn func(ctx context.Context, run *RunHandle) error) error {
	run := c.StartRun(opts)
	ctx = contextWithRun(ctx, run)

	fnErr := fn(ctx, run)

	status := "completed"
	if fnErr != nil {
		status = "failed"
	}
	endErr := run.End(ctx, status)
	if fnErr != nil {
		return fnErr
	}
	return endErr
}

// StartRun creates a RunHandle and emits agent.run.started.
// Use this instead of Run() when you need to manage the run lifetime manually
// (e.g., in serverless environments without context propagation).
func (c *SensuClient) StartRun(opts StartRunOptions) *RunHandle {
	runID := opts.RunID
	if runID == "" {
		runID = uuid.New().String()
	}
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	traceID := uuid.New().String()
	spanID := uuid.New().String()

	run := &RunHandle{
		client:    c,
		RunID:     runID,
		SessionID: sessionID,
		AgentID:   c.agentID,
		OrgID:     c.orgID,
		TraceID:   traceID,
		SpanID:    spanID,
	}

	ev := telemetryEvent{
		"event_id":   uuid.New().String(),
		"event_type": EventRunStarted,
		"timestamp":  utcNowISO(),
		"org_id":     c.orgID,
		"agent_id":   c.agentID,
		"session_id": sessionID,
		"run_id":     runID,
		"trace_id":   traceID,
		"span_id":    spanID,
	}
	if opts.RunType != "" {
		ev["run_type"] = opts.RunType
	}
	if opts.EndUserID != "" {
		ev["end_user_id"] = opts.EndUserID
	}
	if len(opts.Tags) > 0 {
		ev["tags"] = opts.Tags
	}
	c.batcher.enqueue(ev)

	return run
}

// SpawnRun creates a child RunHandle, emits agent.spawned on the parent, and
// agent.run.started for the child. The child shares trace_id and session_id.
func (c *SensuClient) SpawnRun(ctx context.Context, parent *RunHandle, opts SpawnRunOptions) *RunHandle {
	childRunID := opts.ChildRunID
	if childRunID == "" {
		childRunID = uuid.New().String()
	}
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = parent.SessionID
	}
	childAgentID := opts.ChildAgentID
	if childAgentID == "" {
		childAgentID = c.agentID
	}
	childSpanID := uuid.New().String()

	// Emit agent.spawned on the parent run
	spawnEv := mergeEvent(parent.baseEvent(), telemetryEvent{
		"event_type":     EventAgentSpawned,
		"child_run_id":   childRunID,
		"child_agent_id": childAgentID,
	})
	if opts.SpawnReason != "" {
		spawnEv["spawn_reason"] = opts.SpawnReason
	}
	c.batcher.enqueue(spawnEv)

	child := &RunHandle{
		client:    c,
		RunID:     childRunID,
		SessionID: sessionID,
		AgentID:   childAgentID,
		OrgID:     c.orgID,
		TraceID:   parent.TraceID,
		SpanID:    childSpanID,
	}

	startEv := telemetryEvent{
		"event_id":       uuid.New().String(),
		"event_type":     EventRunStarted,
		"timestamp":      utcNowISO(),
		"org_id":         c.orgID,
		"agent_id":       childAgentID,
		"session_id":     sessionID,
		"run_id":         childRunID,
		"trace_id":       parent.TraceID,
		"span_id":        childSpanID,
		"parent_run_id":  parent.RunID,
		"parent_span_id": parent.SpanID,
	}
	if opts.RunType != "" {
		startEv["run_type"] = opts.RunType
	}
	c.batcher.enqueue(startEv)

	return child
}

// GetActiveRun returns the RunHandle stored in ctx by Run(), or nil.
func (c *SensuClient) GetActiveRun(ctx context.Context) *RunHandle {
	return RunFromContext(ctx)
}

// StartSession emits a session.started event and returns the session ID.
func (c *SensuClient) StartSession(opts StartSessionOptions) string {
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	ev := telemetryEvent{
		"event_id":   uuid.New().String(),
		"event_type": EventSessionStarted,
		"timestamp":  utcNowISO(),
		"org_id":     c.orgID,
		"agent_id":   c.agentID,
		"session_id": sessionID,
		"trace_id":   uuid.New().String(),
		"span_id":    uuid.New().String(),
	}
	if opts.Channel != "" {
		ev["channel"] = opts.Channel
	}
	if opts.EndUserID != "" {
		ev["end_user_id"] = opts.EndUserID
	}
	c.batcher.enqueue(ev)
	return sessionID
}

// ResumeSession emits a session.resumed event and returns the session ID.
func (c *SensuClient) ResumeSession(opts ResumeSessionOptions) string {
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	ev := telemetryEvent{
		"event_id":               uuid.New().String(),
		"event_type":             EventSessionResumed,
		"timestamp":              utcNowISO(),
		"org_id":                 c.orgID,
		"agent_id":               c.agentID,
		"session_id":             sessionID,
		"resumed_from_session_id": opts.ResumedFromSessionID,
		"trace_id":               uuid.New().String(),
		"span_id":                uuid.New().String(),
	}
	if opts.Channel != "" {
		ev["channel"] = opts.Channel
	}
	if opts.EndUserID != "" {
		ev["end_user_id"] = opts.EndUserID
	}
	c.batcher.enqueue(ev)
	return sessionID
}

// DeployPromptVersion emits a prompt.version.deployed event.
func (c *SensuClient) DeployPromptVersion(opts DeployPromptVersionOptions) {
	ev := telemetryEvent{
		"event_id":    uuid.New().String(),
		"event_type":  EventPromptVersionDeployed,
		"timestamp":   utcNowISO(),
		"org_id":      c.orgID,
		"agent_id":    c.agentID,
		"session_id":  uuid.New().String(),
		"run_id":      uuid.New().String(),
		"trace_id":    uuid.New().String(),
		"span_id":     uuid.New().String(),
		"template_id": opts.TemplateID,
		"new_version": opts.NewVersion,
	}
	if opts.OldVersion != "" {
		ev["old_version"] = opts.OldVersion
	}
	if opts.DeployedBy != "" {
		ev["deployed_by"] = opts.DeployedBy
	}
	c.batcher.enqueue(ev)
}

// Enqueue inserts a raw event map directly into the buffer.
// Intended for integration layers that build events themselves.
func (c *SensuClient) Enqueue(ev map[string]any) {
	c.batcher.enqueue(telemetryEvent(ev))
}

// Flush immediately delivers all buffered events to the API.
// Blocks until the POST completes or ctx is cancelled.
func (c *SensuClient) Flush(ctx context.Context) error {
	return c.batcher.flush(ctx)
}

// Close flushes remaining events and stops the background goroutine.
// Call via defer after creating the client.
func (c *SensuClient) Close(ctx context.Context) error {
	if err := c.Flush(ctx); err != nil {
		return err
	}
	c.batcher.stop()
	return nil
}

// notifyToolCall increments the loop-detection counter for toolName within runID.
// Fires OnLoopDetected when the count reaches LoopThreshold.
func (c *SensuClient) notifyToolCall(runID, toolName string) {
	if c.onLoopDetected == nil {
		return
	}
	c.mu.Lock()
	m, ok := c.runToolCallCounts[runID]
	if !ok {
		m = make(map[string]int)
		c.runToolCallCounts[runID] = m
	}
	m[toolName]++
	count := m[toolName]
	c.mu.Unlock()

	if count >= c.loopThreshold {
		c.onLoopDetected(toolName, count)
	}
}

func (c *SensuClient) clearRunLoopState(runID string) {
	c.mu.Lock()
	delete(c.runToolCallCounts, runID)
	c.mu.Unlock()
}
