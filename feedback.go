package sensu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// feedbackResponse is the parsed JSON body returned by POST /api/v1/feedback
// and POST /api/v1/eval-scores on success.
type feedbackResponse struct {
	ID string `json:"id"`
}

// Feedback posts end-user feedback for a run. Run-less helper — does not
// require an active run context. Hits POST /api/v1/feedback directly
// (not the event buffer). Returns the created feedback id.
func (c *SensuClient) Feedback(ctx context.Context, opts FeedbackOptions) (string, error) {
	if c.disabled {
		return "", nil
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("sensu: Feedback requires an API key")
	}
	if opts.RunID == "" || opts.Type == "" {
		return "", fmt.Errorf("sensu: Feedback requires RunID and Type")
	}

	body := map[string]any{
		"runId": opts.RunID,
		"type":  opts.Type,
	}
	if opts.Score != nil {
		body["score"] = *opts.Score
	}
	if opts.Comment != "" {
		body["comment"] = opts.Comment
	}
	if opts.EndUserID != "" {
		body["endUserId"] = opts.EndUserID
	}

	return c.postIngestJSON(ctx, "/api/v1/feedback", body)
}

// Score posts an automated eval score for a run. Run-less helper. Hits
// POST /api/v1/eval-scores directly (not the event buffer). Returns the
// created eval score id.
func (c *SensuClient) Score(ctx context.Context, opts ScoreOptions) (string, error) {
	if c.disabled {
		return "", nil
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("sensu: Score requires an API key")
	}
	if opts.RunID == "" || opts.Metric == "" {
		return "", fmt.Errorf("sensu: Score requires RunID and Metric")
	}

	body := map[string]any{
		"runId":  opts.RunID,
		"metric": opts.Metric,
		"score":  opts.Score,
	}
	if opts.EvaluatorID != "" {
		body["evaluatorId"] = opts.EvaluatorID
	}
	if opts.ModelUsedForEval != "" {
		body["modelUsedForEval"] = opts.ModelUsedForEval
	}
	if opts.StepID != "" {
		body["stepId"] = opts.StepID
	}
	if opts.LLMCallID != "" {
		body["llmCallId"] = opts.LLMCallID
	}

	return c.postIngestJSON(ctx, "/api/v1/eval-scores", body)
}

func (c *SensuClient) postIngestJSON(ctx context.Context, path string, body map[string]any) (string, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("sensu: marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("sensu: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sensu: post %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("sensu: %s returned %d: %s", path, resp.StatusCode, string(respBody))
	}

	var parsed feedbackResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("sensu: parse response: %w", err)
	}
	return parsed.ID, nil
}
