package sensu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// RegisterAgentVersion registers a candidate config (system prompt +
// optional model) used at a given commit so eval-gate checks (§5.2) can
// reference it by versionId instead of inlining the full config in every
// request. Run-less helper; does not require an active run context.
//
// Hits POST /api/v1/agents/:id/versions directly (not the event buffer).
// Returns the parsed AgentVersion. AgentID is URL-encoded so labels
// containing reserved characters round-trip safely.
//
// Customers typically call this from their deploy step, then pass the
// returned ID to the Sensu eval-gate Action:
//
//	v, err := client.RegisterAgentVersion(ctx, sensu.RegisterAgentVersionOptions{
//	    AgentID: "cust-support-v3",
//	    SHA:     os.Getenv("GITHUB_SHA"),
//	    Config:  sensu.CandidateConfig{SystemPrompt: prompt, Model: "claude-sonnet-4-6"},
//	})
func (c *SensuClient) RegisterAgentVersion(
	ctx context.Context, opts RegisterAgentVersionOptions,
) (*AgentVersion, error) {
	if c.disabled {
		return nil, nil
	}
	if c.apiKey == "" {
		return nil, fmt.Errorf("sensu: RegisterAgentVersion requires an API key")
	}
	if opts.AgentID == "" {
		return nil, fmt.Errorf("sensu: RegisterAgentVersion requires AgentID")
	}
	if opts.SHA == "" {
		return nil, fmt.Errorf("sensu: RegisterAgentVersion requires SHA")
	}
	if opts.Config.SystemPrompt == "" {
		return nil, fmt.Errorf("sensu: RegisterAgentVersion requires Config.SystemPrompt")
	}

	body := map[string]any{
		"sha":    opts.SHA,
		"config": opts.Config,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("sensu: marshal body: %w", err)
	}

	path := "/api/v1/agents/" + url.PathEscape(opts.AgentID) + "/versions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("sensu: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sensu: post %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sensu: %s returned %d: %s", path, resp.StatusCode, string(respBody))
	}

	var version AgentVersion
	if err := json.Unmarshal(respBody, &version); err != nil {
		return nil, fmt.Errorf("sensu: parse response: %w", err)
	}
	return &version, nil
}
