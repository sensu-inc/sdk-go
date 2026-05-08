package senzu_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/senzu-ai/sdk-go"
)

func TestTrackLLMEmitsStartedAndCompleted(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(senzu.StartRunOptions{})
	step := run.StartStep(senzu.StartStepOptions{StepType: "llm"})
	*batches = nil

	type fakeResponse struct {
		Usage struct {
			InputTokens  int64
			OutputTokens int64
		}
	}
	fake := &fakeResponse{}
	fake.Usage.InputTokens = 100
	fake.Usage.OutputTokens = 50

	result, err := senzu.TrackLLM(ctx, step, func() (*fakeResponse, error) {
		return fake, nil
	}, senzu.TrackLLMOptions{Provider: "anthropic", Model: "claude-sonnet-4-6"})

	if err != nil {
		t.Fatal(err)
	}
	if result != fake {
		t.Fatal("expected the same pointer back")
	}

	c.Flush(ctx)
	events := allEvents(*batches)

	if !containsType(events, senzu.EventLLMRequestStarted) {
		t.Fatalf("missing llm.request.started in %v", eventTypes(events))
	}
	if !containsType(events, senzu.EventLLMRequestCompleted) {
		t.Fatalf("missing llm.request.completed in %v", eventTypes(events))
	}

	for _, e := range events {
		if e["event_type"] == senzu.EventLLMRequestCompleted {
			if e["status"] != "success" {
				t.Fatalf("expected status=success, got %v", e["status"])
			}
			// JSON decode produces float64 for all numbers.
			if int(e["input_tokens"].(float64)) != 100 {
				t.Fatalf("expected input_tokens=100, got %v", e["input_tokens"])
			}
			if int(e["output_tokens"].(float64)) != 50 {
				t.Fatalf("expected output_tokens=50, got %v", e["output_tokens"])
			}
			return
		}
	}
	t.Fatal("llm.request.completed not found")
}

func TestTrackLLMErrorSetsStatusError(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(senzu.StartRunOptions{})
	step := run.StartStep(senzu.StartStepOptions{})
	*batches = nil

	_, err := senzu.TrackLLM(ctx, step, func() (any, error) {
		return nil, fmt.Errorf("rate limited")
	}, senzu.TrackLLMOptions{Provider: "openai", Model: "gpt-4o"})

	if err == nil {
		t.Fatal("expected error")
	}

	c.Flush(ctx)
	for _, e := range allEvents(*batches) {
		if e["event_type"] == senzu.EventLLMRequestCompleted {
			if e["status"] != "error" {
				t.Fatalf("expected status=error, got %v", e["status"])
			}
			return
		}
	}
	t.Fatal("llm.request.completed not found")
}

func TestTrackLLMCostEstimation(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(senzu.StartRunOptions{})
	step := run.StartStep(senzu.StartStepOptions{})
	*batches = nil

	type fakeResponse struct {
		Usage struct {
			InputTokens  int64
			OutputTokens int64
		}
	}
	fake := &fakeResponse{}
	fake.Usage.InputTokens = 1_000_000
	fake.Usage.OutputTokens = 1_000_000

	senzu.TrackLLM(ctx, step, func() (*fakeResponse, error) {
		return fake, nil
	}, senzu.TrackLLMOptions{Provider: "anthropic", Model: "claude-sonnet-4-6"})

	c.Flush(ctx)

	// claude-sonnet-4-6: $3/1M input + $15/1M output = $18 total
	for _, e := range allEvents(*batches) {
		if e["event_type"] == senzu.EventLLMRequestCompleted {
			cost, ok := e["cost_usd_estimate"].(float64)
			if !ok {
				t.Fatalf("expected cost_usd_estimate float64, got %T", e["cost_usd_estimate"])
			}
			if cost < 17.9 || cost > 18.1 {
				t.Fatalf("expected cost ~$18, got $%.4f", cost)
			}
			return
		}
	}
	t.Fatal("llm.request.completed not found")
}

func TestTrackGuardrailEmitsEvents(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(senzu.StartRunOptions{})
	step := run.StartStep(senzu.StartStepOptions{})
	*batches = nil

	result, err := senzu.TrackGuardrail(ctx, step, func() (string, error) {
		return "pass", nil
	}, senzu.TrackGuardrailOptions{GuardrailID: "g1", GuardrailType: "content"})

	if err != nil {
		t.Fatal(err)
	}
	if result != "pass" {
		t.Fatalf("expected 'pass', got %q", result)
	}

	c.Flush(ctx)
	events := allEvents(*batches)

	if !containsType(events, senzu.EventGuardrailCheckStarted) {
		t.Fatalf("missing guardrail.check.started in %v", eventTypes(events))
	}
	if !containsType(events, senzu.EventGuardrailCheckCompleted) {
		t.Fatalf("missing guardrail.check.completed in %v", eventTypes(events))
	}
}

func TestTrackGuardrailEmitsBlockedOnFail(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(senzu.StartRunOptions{})
	step := run.StartStep(senzu.StartStepOptions{})
	*batches = nil

	senzu.TrackGuardrail(ctx, step, func() (string, error) {
		return "fail", nil
	}, senzu.TrackGuardrailOptions{GuardrailID: "g1", GuardrailType: "pii"})

	c.Flush(ctx)

	if !containsType(allEvents(*batches), senzu.EventGuardrailBlocked) {
		t.Fatal("expected guardrail.blocked event when result is 'fail'")
	}
}

func TestTrackStreamingLLMMeasuresTTFT(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(senzu.StartRunOptions{})
	step := run.StartStep(senzu.StartStepOptions{})
	*batches = nil

	streamCh := make(chan string, 5)
	go func() {
		time.Sleep(10 * time.Millisecond) // simulate network delay
		for _, chunk := range []string{"Hello", " world", "!"} {
			streamCh <- chunk
		}
		close(streamCh)
	}()

	var gotTTFT bool
	text, err := senzu.TrackStreamingLLM(ctx, step, streamCh, senzu.TrackStreamingLLMOptions{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-6",
		OnComplete: func(t string, ttftMs *float64) {
			if ttftMs != nil && *ttftMs >= 0 {
				gotTTFT = true
			}
		},
	})

	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello world!" {
		t.Fatalf("expected 'Hello world!', got %q", text)
	}
	if !gotTTFT {
		t.Fatal("expected OnComplete to be called with a non-nil ttftMs")
	}

	c.Flush(ctx)
	events := allEvents(*batches)
	if !containsType(events, senzu.EventLLMRequestCompleted) {
		t.Fatalf("missing llm.request.completed after streaming in %v", eventTypes(events))
	}
}

func TestRecordRetrievalEmitsEvent(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(senzu.StartRunOptions{})
	step := run.StartStep(senzu.StartStepOptions{})
	*batches = nil

	topK := 5
	latency := 42.0
	step.RecordRetrieval(senzu.RecordRetrievalOptions{
		VectorStoreID: "store-1",
		TopK:          &topK,
		LatencyMs:     &latency,
		Status:        "success",
	})

	c.Flush(ctx)

	if !containsType(allEvents(*batches), senzu.EventRetrievalCompleted) {
		t.Fatal("missing retrieval.completed event")
	}
}

func TestRecordEvalScore(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(senzu.StartRunOptions{})
	run.RecordEvalScore(senzu.RecordEvalScoreOptions{
		Metric: "faithfulness",
		Score:  0.95,
	})
	c.Flush(ctx)

	if !containsType(allEvents(*batches), senzu.EventEvalScoreRecorded) {
		t.Fatal("missing eval.score.recorded event")
	}
}
