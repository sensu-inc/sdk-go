package sensu_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sensu-inc/sdk-go"
)

// makeClient returns a disabled test client that never POSTs events.
func makeClient() *sensu.SensuClient {
	return sensu.NewClient(sensu.ClientOptions{Disabled: true})
}

// makeClientWithServer returns a client wired to a test HTTP server.
// The server records every batch received and responds 200 OK.
func makeClientWithServer(t *testing.T) (*sensu.SensuClient, *[][]map[string]any, *httptest.Server) {
	t.Helper()
	var mu sync.Mutex
	var batches [][]map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Events []map[string]any `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		batches = append(batches, body.Events)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"processed": len(body.Events)})
	}))

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey:             "test-key",
		BaseURL:            ts.URL,
		AgentID:            "test-agent",
		DisableLivePricing: true,
		BatchSize:          100,     // large batch so we control flush
		FlushIntervalMs:    999_999, // disable auto-flush
	})
	return c, &batches, ts
}

// allEvents flattens all batches into a single slice.
func allEvents(batches [][]map[string]any) []map[string]any {
	var out []map[string]any
	for _, b := range batches {
		out = append(out, b...)
	}
	return out
}

func eventTypes(events []map[string]any) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i], _ = e["event_type"].(string)
	}
	return out
}

func containsType(events []map[string]any, t string) bool {
	for _, e := range events {
		if e["event_type"] == t {
			return true
		}
	}
	return false
}

// ---- Tests ------------------------------------------------------------------

func TestDisabledClientDropsAllEvents(t *testing.T) {
	c := makeClient()
	c.Enqueue(map[string]any{"event_type": "test.event"})
	// No panic, no HTTP calls. Just verify the client doesn't crash.
}

func TestNewClientDefaults(t *testing.T) {
	c := sensu.NewClient(sensu.ClientOptions{})
	if c.AgentID() != "unknown-agent" {
		t.Fatalf("expected default agentID 'unknown-agent', got %q", c.AgentID())
	}
}

func TestStartRunEmitsRunStarted(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()
	defer c.Close(context.Background())

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{RunID: "run-1", SessionID: "sess-1"})
	if run.RunID != "run-1" {
		t.Fatalf("expected run-1, got %s", run.RunID)
	}

	c.Flush(ctx)

	events := allEvents(*batches)
	if !containsType(events, sensu.EventRunStarted) {
		t.Fatalf("expected agent.run.started in %v", eventTypes(events))
	}
}

func TestRunEmitsStartedAndCompleted(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	err := c.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, run *sensu.RunHandle) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	events := allEvents(*batches)
	types := eventTypes(events)
	if !containsType(events, sensu.EventRunStarted) {
		t.Fatalf("missing agent.run.started in %v", types)
	}
	if !containsType(events, sensu.EventRunCompleted) {
		t.Fatalf("missing agent.run.completed in %v", types)
	}
}

func TestRunEmitsFailedOnError(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	err := c.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, run *sensu.RunHandle) error {
		return fmt.Errorf("something failed")
	})
	if err == nil {
		t.Fatal("expected error from Run, got nil")
	}

	events := allEvents(*batches)
	if !containsType(events, sensu.EventRunFailed) {
		t.Fatalf("missing agent.run.failed in %v", eventTypes(events))
	}
}

func TestContextPropagation(t *testing.T) {
	c := makeClient()

	var capturedRun *sensu.RunHandle
	c.Run(context.Background(), sensu.StartRunOptions{}, func(ctx context.Context, run *sensu.RunHandle) error {
		capturedRun = c.GetActiveRun(ctx)
		return nil
	})

	if capturedRun == nil {
		t.Fatal("expected GetActiveRun to return the active run, got nil")
	}
}

func TestContextIsolationAcrossConcurrentRuns(t *testing.T) {
	c := makeClient()

	const n = 10
	results := make([]string, n)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runID := fmt.Sprintf("run-%d", i)
			c.Run(context.Background(), sensu.StartRunOptions{RunID: runID},
				func(ctx context.Context, run *sensu.RunHandle) error {
					// Simulate async work with random sleep
					time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
					active := c.GetActiveRun(ctx)
					if active != nil {
						results[i] = active.RunID
					}
					return nil
				})
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		expected := fmt.Sprintf("run-%d", i)
		if r != expected {
			t.Errorf("goroutine %d: expected run %s, got %s", i, expected, r)
		}
	}
}

func TestGetActiveRunReturnsNilOutsideRun(t *testing.T) {
	c := makeClient()
	if got := c.GetActiveRun(context.Background()); got != nil {
		t.Fatalf("expected nil outside run, got %+v", got)
	}
}

func TestRunIdempotentEnd(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	run.End(ctx, "completed")
	run.End(ctx, "completed") // second call should be a no-op

	c.Flush(ctx)

	var completedCount int
	for _, e := range allEvents(*batches) {
		if e["event_type"] == sensu.EventRunCompleted {
			completedCount++
		}
	}
	if completedCount != 1 {
		t.Fatalf("expected exactly 1 agent.run.completed, got %d", completedCount)
	}
}

func TestLoopDetection(t *testing.T) {
	var loopTool string
	var loopCount int
	c := sensu.NewClient(sensu.ClientOptions{
		Disabled:      true,
		LoopThreshold: 3,
		OnLoopDetected: func(toolName string, callCount int) {
			loopTool = toolName
			loopCount = callCount
		},
	})

	run := c.StartRun(sensu.StartRunOptions{})
	step := run.StartStep(sensu.StartStepOptions{})

	for i := 0; i < 5; i++ {
		sensu.TrackTool(context.Background(), step, func() (string, error) {
			return "ok", nil
		}, sensu.TrackToolOptions{ToolName: "search"})
	}

	if loopTool != "search" {
		t.Fatalf("expected loop detected for 'search', got %q", loopTool)
	}
	if loopCount < 3 {
		t.Fatalf("expected loopCount >= 3, got %d", loopCount)
	}
}

func TestStartStepEmitsStepStarted(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	step := run.StartStep(sensu.StartStepOptions{Name: "my-step", StepType: "llm"})
	step.End(ctx)
	c.Flush(ctx)

	events := allEvents(*batches)
	if !containsType(events, sensu.EventStepStarted) {
		t.Fatalf("missing agent.step.started in %v", eventTypes(events))
	}
	if !containsType(events, sensu.EventStepCompleted) {
		t.Fatalf("missing agent.step.completed in %v", eventTypes(events))
	}
}

func TestTrackToolEmitsEvents(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	step := run.StartStep(sensu.StartStepOptions{})
	c.Flush(ctx)
	*batches = nil // clear setup events

	result, err := sensu.TrackTool(ctx, step, func() (string, error) {
		return "found", nil
	}, sensu.TrackToolOptions{ToolName: "web_search"})

	if err != nil {
		t.Fatal(err)
	}
	if result != "found" {
		t.Fatalf("expected 'found', got %q", result)
	}

	c.Flush(ctx)
	events := allEvents(*batches)
	if !containsType(events, sensu.EventToolCallStarted) {
		t.Fatalf("missing tool.call.started in %v", eventTypes(events))
	}
	if !containsType(events, sensu.EventToolCallCompleted) {
		t.Fatalf("missing tool.call.completed in %v", eventTypes(events))
	}
}

func TestTrackToolErrorSetsStatusError(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	step := run.StartStep(sensu.StartStepOptions{})

	_, err := sensu.TrackTool(ctx, step, func() (string, error) {
		return "", fmt.Errorf("tool blew up")
	}, sensu.TrackToolOptions{ToolName: "failing_tool"})

	if err == nil {
		t.Fatal("expected error from TrackTool")
	}

	c.Flush(ctx)
	for _, e := range allEvents(*batches) {
		if e["event_type"] == sensu.EventToolCallCompleted {
			if e["status"] != "error" {
				t.Fatalf("expected status=error, got %v", e["status"])
			}
			return
		}
	}
	t.Fatal("tool.call.completed event not found")
}

func TestSpawnRunEmitsAgentSpawned(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	parent := c.StartRun(sensu.StartRunOptions{RunID: "parent-run"})
	child := c.SpawnRun(ctx, parent, sensu.SpawnRunOptions{
		ChildAgentID: "child-agent",
		SpawnReason:  "subtask",
	})

	if child.TraceID != parent.TraceID {
		t.Fatal("child and parent should share trace_id")
	}
	if child.SessionID != parent.SessionID {
		t.Fatal("child and parent should share session_id")
	}

	c.Flush(ctx)
	events := allEvents(*batches)
	if !containsType(events, sensu.EventAgentSpawned) {
		t.Fatalf("missing agent.spawned in %v", eventTypes(events))
	}
}

func TestRecordFeedback(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	score := 0.9
	run.RecordFeedback(sensu.RecordFeedbackOptions{
		Type:  "score",
		Score: &score,
	})
	c.Flush(ctx)

	events := allEvents(*batches)
	if !containsType(events, sensu.EventFeedbackReceived) {
		t.Fatalf("missing feedback.received in %v", eventTypes(events))
	}
}
