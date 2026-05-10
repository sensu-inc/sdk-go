package sensu_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sensu-inc/sdk-go"
)

// makeFeedbackServer spins up an httptest server that records every request
// it receives and replies with `{"id": "..."}`. Used to assert wire format
// of the run-less Feedback() / Score() helpers.
func makeFeedbackServer(t *testing.T) (*httptest.Server, *[]recordedReq) {
	t.Helper()
	var seen []recordedReq

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, recordedReq{
			path:   r.URL.Path,
			apiKey: r.Header.Get("X-API-Key"),
			body:   body,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "id_test_123"})
	}))
	return ts, &seen
}

type recordedReq struct {
	path   string
	apiKey string
	body   map[string]any
}

func TestFeedback_PostsCamelCasePayloadToCorrectEndpoint(t *testing.T) {
	ts, seen := makeFeedbackServer(t)
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: ts.URL,
		DisableLivePricing: true, BatchSize: 100, FlushIntervalMs: 999_999,
	})

	score := 0.2
	id, err := c.Feedback(context.Background(), sensu.FeedbackOptions{
		RunID: "run-abc", Type: "thumbs_down",
		Score: &score, Comment: "missed the point", EndUserID: "user-77",
	})
	if err != nil {
		t.Fatalf("Feedback returned error: %v", err)
	}
	if id != "id_test_123" {
		t.Errorf("expected id 'id_test_123', got %q", id)
	}

	if len(*seen) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*seen))
	}
	r := (*seen)[0]
	if r.path != "/api/v1/feedback" {
		t.Errorf("expected path /api/v1/feedback, got %s", r.path)
	}
	if r.apiKey != "test-key" {
		t.Errorf("expected X-API-Key 'test-key', got %q", r.apiKey)
	}

	expect := map[string]any{
		"runId": "run-abc", "type": "thumbs_down",
		"score": 0.2, "comment": "missed the point", "endUserId": "user-77",
	}
	for k, v := range expect {
		if r.body[k] != v {
			t.Errorf("body[%q]: expected %v, got %v", k, v, r.body[k])
		}
	}
}

func TestFeedback_OmitsUnsetOptionalFields(t *testing.T) {
	ts, seen := makeFeedbackServer(t)
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: ts.URL,
		DisableLivePricing: true, BatchSize: 100, FlushIntervalMs: 999_999,
	})
	_, err := c.Feedback(context.Background(), sensu.FeedbackOptions{
		RunID: "run-abc", Type: "thumbs_up",
	})
	if err != nil {
		t.Fatalf("Feedback returned error: %v", err)
	}

	r := (*seen)[0]
	for _, k := range []string{"score", "comment", "endUserId"} {
		if _, ok := r.body[k]; ok {
			t.Errorf("expected %q to be absent from body, got %v", k, r.body[k])
		}
	}
}

func TestFeedback_RejectsMissingRunIDOrType(t *testing.T) {
	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: "http://unused", DisableLivePricing: true,
	})

	if _, err := c.Feedback(context.Background(), sensu.FeedbackOptions{Type: "thumbs_up"}); err == nil {
		t.Error("expected error for missing RunID")
	}
	if _, err := c.Feedback(context.Background(), sensu.FeedbackOptions{RunID: "r"}); err == nil {
		t.Error("expected error for missing Type")
	}
}

func TestFeedback_DisabledClientShortCircuits(t *testing.T) {
	c := sensu.NewClient(sensu.ClientOptions{Disabled: true})
	id, err := c.Feedback(context.Background(), sensu.FeedbackOptions{
		RunID: "run", Type: "thumbs_up",
	})
	if err != nil || id != "" {
		t.Errorf("expected disabled client to return ('', nil), got (%q, %v)", id, err)
	}
}

func TestFeedback_ReportsNon2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Run not found", http.StatusNotFound)
	}))
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: ts.URL, DisableLivePricing: true,
	})
	_, err := c.Feedback(context.Background(), sensu.FeedbackOptions{
		RunID: "missing", Type: "thumbs_up",
	})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got %v", err)
	}
}

func TestScore_PostsCamelCasePayloadToCorrectEndpoint(t *testing.T) {
	ts, seen := makeFeedbackServer(t)
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: ts.URL,
		DisableLivePricing: true, BatchSize: 100, FlushIntervalMs: 999_999,
	})

	id, err := c.Score(context.Background(), sensu.ScoreOptions{
		RunID: "run-abc", Metric: "helpfulness", Score: 0.83,
		EvaluatorID:      "human-v1",
		ModelUsedForEval: "claude-haiku-4-5",
		StepID:           "step-1",
		LLMCallID:        "call-1",
	})
	if err != nil {
		t.Fatalf("Score returned error: %v", err)
	}
	if id != "id_test_123" {
		t.Errorf("expected id 'id_test_123', got %q", id)
	}

	r := (*seen)[0]
	if r.path != "/api/v1/eval-scores" {
		t.Errorf("expected path /api/v1/eval-scores, got %s", r.path)
	}
	expect := map[string]any{
		"runId": "run-abc", "metric": "helpfulness", "score": 0.83,
		"evaluatorId": "human-v1", "modelUsedForEval": "claude-haiku-4-5",
		"stepId": "step-1", "llmCallId": "call-1",
	}
	for k, v := range expect {
		if r.body[k] != v {
			t.Errorf("body[%q]: expected %v, got %v", k, v, r.body[k])
		}
	}
}

func TestScore_RejectsMissingRunIDOrMetric(t *testing.T) {
	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: "http://unused", DisableLivePricing: true,
	})
	if _, err := c.Score(context.Background(), sensu.ScoreOptions{Metric: "m", Score: 0.5}); err == nil {
		t.Error("expected error for missing RunID")
	}
	if _, err := c.Score(context.Background(), sensu.ScoreOptions{RunID: "r", Score: 0.5}); err == nil {
		t.Error("expected error for missing Metric")
	}
}
