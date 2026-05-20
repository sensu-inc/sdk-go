package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	openaiSDK "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/sensu-inc/sdk-go"
	sopenai "github.com/sensu-inc/sdk-go/integrations/openai"
)

// recordingSensu spins up a Sensu /api/v1/events recorder + returns the
// configured Sensu client + a thunk to read every event the client has
// flushed. Matches the pattern used in the sdk-go root test suite.
func recordingSensu(t *testing.T) (*sensu.SensuClient, func() []map[string]any) {
	t.Helper()
	var (
		mu    sync.Mutex
		seen  [][]map[string]any
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Events []map[string]any `json:"events"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seen = append(seen, body.Events)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"processed": len(body.Events)})
	}))
	t.Cleanup(ts.Close)

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey:             "sensu-test-key",
		BaseURL:            ts.URL,
		AgentID:            "test-agent",
		OrgID:              "test-org",
		DisableLivePricing: true, // tests don't exercise the pricing path
		BatchSize:          100,
		FlushIntervalMs:    999_999,
	})

	flatten := func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		var out []map[string]any
		for _, b := range seen {
			out = append(out, b...)
		}
		return out
	}
	return c, flatten
}

// fakeOpenAIServer responds to POST /chat/completions with a fixed
// ChatCompletion body. The returned URL feeds option.WithBaseURL on the
// OpenAI client so calls hit the fake instead of the real API.
func fakeOpenAIServer(t *testing.T, status int, body any) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// OpenAI's Go SDK strictly requires application/json — without
		// this header the response parser errors out with "expected
		// destination type of 'string' or '[]byte'…".
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func openaiClient(baseURL string) *openaiSDK.Client {
	c := openaiSDK.NewClient(
		option.WithAPIKey("openai-test-key"),
		option.WithBaseURL(baseURL),
	)
	return &c
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestWrappedChatCompletions_EmitsStartedAndCompleted(t *testing.T) {
	sensuClient, events := recordingSensu(t)

	openaiURL := fakeOpenAIServer(t, 200, map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"created": 1700000000,
		"model":   "gpt-4o-2024-08-06", // server "actual model" differs from requested
		"choices": []map[string]any{
			{"index": 0, "message": map[string]any{"role": "assistant", "content": "hi"}, "finish_reason": "stop"},
		},
		"usage": map[string]any{
			"prompt_tokens":     120,
			"completion_tokens": 30,
			"total_tokens":      150,
			"prompt_tokens_details": map[string]any{
				"cached_tokens": 25,
			},
		},
	})

	wrapped := sopenai.Wrap(openaiClient(openaiURL), sopenai.WrapOptions{Client: sensuClient})

	ctx := context.Background()
	err := sensuClient.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		_, err := wrapped.Chat.Completions.New(ctx, openaiSDK.ChatCompletionNewParams{
			Model: "gpt-4o",
			Messages: []openaiSDK.ChatCompletionMessageParamUnion{
				openaiSDK.UserMessage("hello"),
			},
		})
		return err
	})
	if err != nil {
		t.Fatalf("OpenAI New returned error: %v", err)
	}
	sensuClient.Flush(ctx)

	var started, completed map[string]any
	for _, e := range events() {
		switch e["event_type"] {
		case sensu.EventLLMRequestStarted:
			started = e
		case sensu.EventLLMRequestCompleted:
			completed = e
		}
	}
	if started == nil {
		t.Fatal("expected llm.request.started event, got none")
	}
	if completed == nil {
		t.Fatal("expected llm.request.completed event, got none")
	}

	// Started: requested model + provider + same llm_call_id
	if started["model"] != "gpt-4o" {
		t.Errorf("started.model: expected gpt-4o, got %v", started["model"])
	}
	if started["provider"] != "openai" {
		t.Errorf("started.provider: expected openai, got %v", started["provider"])
	}

	// Completed: actual model from response + usage fields
	if completed["model"] != "gpt-4o-2024-08-06" {
		t.Errorf("completed.model: expected gpt-4o-2024-08-06 (from response), got %v", completed["model"])
	}
	if completed["status"] != "success" {
		t.Errorf("completed.status: expected success, got %v", completed["status"])
	}
	if got := completed["input_tokens"]; got != float64(120) && got != 120 {
		t.Errorf("input_tokens: expected 120, got %v (%T)", got, got)
	}
	if got := completed["output_tokens"]; got != float64(30) && got != 30 {
		t.Errorf("output_tokens: expected 30, got %v (%T)", got, got)
	}
	if got := completed["cached_input_tokens"]; got != float64(25) && got != 25 {
		t.Errorf("cached_input_tokens: expected 25, got %v (%T)", got, got)
	}

	// Both events share the same llm_call_id
	if started["llm_call_id"] != completed["llm_call_id"] {
		t.Errorf("llm_call_id mismatch: started=%v completed=%v",
			started["llm_call_id"], completed["llm_call_id"])
	}
	// Completed's parent_span_id == started's span_id (parent chain intact)
	if completed["parent_span_id"] != started["span_id"] {
		t.Errorf("parent_span_id != started.span_id: parent=%v started=%v",
			completed["parent_span_id"], started["span_id"])
	}
}

// ---------------------------------------------------------------------------
// Error path
// ---------------------------------------------------------------------------

func TestWrappedChatCompletions_EmitsErrorStatusOnFailure(t *testing.T) {
	sensuClient, events := recordingSensu(t)
	openaiURL := fakeOpenAIServer(t, 500, map[string]any{
		"error": map[string]any{"message": "model overloaded"},
	})

	wrapped := sopenai.Wrap(openaiClient(openaiURL), sopenai.WrapOptions{Client: sensuClient})

	ctx := context.Background()
	_ = sensuClient.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		_, err := wrapped.Chat.Completions.New(ctx, openaiSDK.ChatCompletionNewParams{
			Model:    "gpt-4o",
			Messages: []openaiSDK.ChatCompletionMessageParamUnion{openaiSDK.UserMessage("hi")},
		})
		// We want the SDK error returned to the caller, but the test
		// shouldn't fail on it — the point is observing the telemetry.
		_ = err
		return nil
	})
	sensuClient.Flush(ctx)

	for _, e := range events() {
		if e["event_type"] == sensu.EventLLMRequestCompleted {
			if e["status"] != "error" {
				t.Errorf("expected status=error, got %v", e["status"])
			}
			// On failure the response is nil so input/output tokens shouldn't be set.
			if _, ok := e["input_tokens"]; ok {
				t.Errorf("expected no input_tokens on failure, got %v", e["input_tokens"])
			}
			return
		}
	}
	t.Fatal("expected llm.request.completed event with status=error, got none")
}

// ---------------------------------------------------------------------------
// Standalone (no run)
// ---------------------------------------------------------------------------

func TestWrappedChatCompletions_StandaloneEventsWithoutRun(t *testing.T) {
	sensuClient, events := recordingSensu(t)
	openaiURL := fakeOpenAIServer(t, 200, map[string]any{
		"id": "x", "object": "chat.completion", "created": 1, "model": "gpt-4o",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": ""}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
	wrapped := sopenai.Wrap(openaiClient(openaiURL), sopenai.WrapOptions{Client: sensuClient})

	// NB: no Run() wrapper — no run in ctx + no opts.RunHandle.
	_, err := wrapped.Chat.Completions.New(context.Background(), openaiSDK.ChatCompletionNewParams{
		Model:    "gpt-4o",
		Messages: []openaiSDK.ChatCompletionMessageParamUnion{openaiSDK.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("OpenAI call: %v", err)
	}
	sensuClient.Flush(context.Background())

	// In standalone mode, no started event is emitted, but a completed
	// event is — with orphan session/run IDs.
	startedCount, completedCount := 0, 0
	for _, e := range events() {
		switch e["event_type"] {
		case sensu.EventLLMRequestStarted:
			startedCount++
		case sensu.EventLLMRequestCompleted:
			completedCount++
			if e["session_id"] == "" || e["session_id"] == nil {
				t.Error("standalone completed event missing session_id")
			}
			if e["run_id"] == "" || e["run_id"] == nil {
				t.Error("standalone completed event missing run_id")
			}
			// No parent_span_id in standalone mode (nothing to link to)
			if _, ok := e["parent_span_id"]; ok {
				t.Errorf("standalone completed should not have parent_span_id, got %v", e["parent_span_id"])
			}
		}
	}
	if startedCount != 0 {
		t.Errorf("expected 0 started events in standalone mode, got %d", startedCount)
	}
	if completedCount != 1 {
		t.Errorf("expected 1 completed event in standalone mode, got %d", completedCount)
	}
}

// ---------------------------------------------------------------------------
// WrapOptions plumbing
// ---------------------------------------------------------------------------

func TestWrap_DefaultProviderIsOpenAI(t *testing.T) {
	sensuClient, events := recordingSensu(t)
	openaiURL := fakeOpenAIServer(t, 200, map[string]any{
		"id": "x", "object": "chat.completion", "created": 1, "model": "gpt-4o",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": ""}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
	// Default WrapOptions — no DefaultProvider set.
	wrapped := sopenai.Wrap(openaiClient(openaiURL), sopenai.WrapOptions{Client: sensuClient})

	_ = sensuClient.Run(context.Background(), sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		_, err := wrapped.Chat.Completions.New(ctx, openaiSDK.ChatCompletionNewParams{
			Model:    "gpt-4o",
			Messages: []openaiSDK.ChatCompletionMessageParamUnion{openaiSDK.UserMessage("hi")},
		})
		return err
	})
	sensuClient.Flush(context.Background())

	for _, e := range events() {
		if e["event_type"] == sensu.EventLLMRequestCompleted {
			if e["provider"] != "openai" {
				t.Errorf("default provider: expected openai, got %v", e["provider"])
			}
			return
		}
	}
	t.Fatal("no completed event")
}

func TestWrap_CustomDefaultProviderOverride(t *testing.T) {
	sensuClient, events := recordingSensu(t)
	openaiURL := fakeOpenAIServer(t, 200, map[string]any{
		"id": "x", "object": "chat.completion", "created": 1, "model": "gpt-4o",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": ""}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
	// Customer routes traffic through an OpenAI-compatible vendor (Azure
	// OpenAI, Fireworks, Groq, etc.); they want the provider label to
	// reflect that, not the literal "openai".
	wrapped := sopenai.Wrap(openaiClient(openaiURL), sopenai.WrapOptions{
		Client:          sensuClient,
		DefaultProvider: "azure-openai",
	})

	_ = sensuClient.Run(context.Background(), sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		_, err := wrapped.Chat.Completions.New(ctx, openaiSDK.ChatCompletionNewParams{
			Model:    "gpt-4o",
			Messages: []openaiSDK.ChatCompletionMessageParamUnion{openaiSDK.UserMessage("hi")},
		})
		return err
	})
	sensuClient.Flush(context.Background())

	for _, e := range events() {
		if e["event_type"] == sensu.EventLLMRequestCompleted {
			if e["provider"] != "azure-openai" {
				t.Errorf("provider override: expected azure-openai, got %v", e["provider"])
			}
			return
		}
	}
	t.Fatal("no completed event")
}
