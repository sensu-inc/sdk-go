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

// makeAgentVersionsServer records every request and replies with an
// AgentVersion-shaped JSON body so tests can assert wire format of
// RegisterAgentVersion().
func makeAgentVersionsServer(t *testing.T, returnedID string) (*httptest.Server, *[]recordedReq) {
	t.Helper()
	var seen []recordedReq

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		// EscapedPath preserves percent-encoded segments so URL-encoding
		// assertions stay honest — r.URL.Path is always decoded.
		seen = append(seen, recordedReq{
			path:   r.URL.EscapedPath(),
			apiKey: r.Header.Get("X-API-Key"),
			body:   body,
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        returnedID,
			"agentId":   "org-1:cust-support-v3",
			"sha":       "a1b2c3d4",
			"config":    map[string]any{"systemPrompt": "tighter rules", "model": "claude-sonnet-4-6"},
			"createdAt": "2026-05-19T12:00:00.000Z",
		})
	}))
	return ts, &seen
}

func TestRegisterAgentVersion_PostsCorrectBody(t *testing.T) {
	ts, seen := makeAgentVersionsServer(t, "ver_xyz123")
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: ts.URL,
		DisableLivePricing: true, BatchSize: 100, FlushIntervalMs: 999_999,
	})

	v, err := c.RegisterAgentVersion(context.Background(), sensu.RegisterAgentVersionOptions{
		AgentID: "cust-support-v3",
		SHA:     "a1b2c3d4",
		Config:  sensu.CandidateConfig{SystemPrompt: "tighter rules", Model: "claude-sonnet-4-6"},
	})
	if err != nil {
		t.Fatalf("RegisterAgentVersion returned error: %v", err)
	}
	if v == nil {
		t.Fatal("expected non-nil AgentVersion")
	}
	if v.ID != "ver_xyz123" {
		t.Errorf("expected id 'ver_xyz123', got %q", v.ID)
	}
	if v.AgentID != "org-1:cust-support-v3" {
		t.Errorf("expected agentId 'org-1:cust-support-v3', got %q", v.AgentID)
	}
	if v.Config.Model != "claude-sonnet-4-6" {
		t.Errorf("expected model 'claude-sonnet-4-6', got %q", v.Config.Model)
	}

	if len(*seen) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*seen))
	}
	r := (*seen)[0]
	if r.path != "/api/v1/agents/cust-support-v3/versions" {
		t.Errorf("expected path /api/v1/agents/cust-support-v3/versions, got %s", r.path)
	}
	if r.apiKey != "test-key" {
		t.Errorf("expected X-API-Key 'test-key', got %q", r.apiKey)
	}
	if r.body["sha"] != "a1b2c3d4" {
		t.Errorf("expected sha 'a1b2c3d4', got %v", r.body["sha"])
	}
	cfg, ok := r.body["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config object, got %T", r.body["config"])
	}
	if cfg["systemPrompt"] != "tighter rules" {
		t.Errorf("config.systemPrompt: expected 'tighter rules', got %v", cfg["systemPrompt"])
	}
	if cfg["model"] != "claude-sonnet-4-6" {
		t.Errorf("config.model: expected 'claude-sonnet-4-6', got %v", cfg["model"])
	}
}

func TestRegisterAgentVersion_OmitsModelWhenEmpty(t *testing.T) {
	ts, seen := makeAgentVersionsServer(t, "ver_no_model")
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: ts.URL, DisableLivePricing: true,
	})

	_, err := c.RegisterAgentVersion(context.Background(), sensu.RegisterAgentVersionOptions{
		AgentID: "cust-support-v3",
		SHA:     "sha",
		Config:  sensu.CandidateConfig{SystemPrompt: "prompt only"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := (*seen)[0].body["config"].(map[string]any)
	if _, hasModel := cfg["model"]; hasModel {
		t.Errorf("expected config.model to be omitted when empty, got %v", cfg["model"])
	}
}

func TestRegisterAgentVersion_URLEncodesAgentID(t *testing.T) {
	ts, seen := makeAgentVersionsServer(t, "ver_encoded")
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: ts.URL, DisableLivePricing: true,
	})

	_, err := c.RegisterAgentVersion(context.Background(), sensu.RegisterAgentVersionOptions{
		AgentID: "agent/with/slashes",
		SHA:     "s",
		Config:  sensu.CandidateConfig{SystemPrompt: "p"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := (*seen)[0].path
	want := "/api/v1/agents/agent%2Fwith%2Fslashes/versions"
	if got != want {
		t.Errorf("expected path %q, got %q", want, got)
	}
}

func TestRegisterAgentVersion_RejectsMissingFields(t *testing.T) {
	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: "http://unused", DisableLivePricing: true,
	})
	good := sensu.RegisterAgentVersionOptions{
		AgentID: "a", SHA: "s",
		Config: sensu.CandidateConfig{SystemPrompt: "p"},
	}

	missing := []struct {
		name string
		opts sensu.RegisterAgentVersionOptions
	}{
		{"no AgentID", sensu.RegisterAgentVersionOptions{SHA: good.SHA, Config: good.Config}},
		{"no SHA", sensu.RegisterAgentVersionOptions{AgentID: good.AgentID, Config: good.Config}},
		{"no Config.SystemPrompt", sensu.RegisterAgentVersionOptions{AgentID: good.AgentID, SHA: good.SHA}},
	}
	for _, m := range missing {
		if _, err := c.RegisterAgentVersion(context.Background(), m.opts); err == nil {
			t.Errorf("%s: expected validation error, got nil", m.name)
		}
	}
}

func TestRegisterAgentVersion_DisabledClientShortCircuits(t *testing.T) {
	c := sensu.NewClient(sensu.ClientOptions{Disabled: true})
	v, err := c.RegisterAgentVersion(context.Background(), sensu.RegisterAgentVersionOptions{
		AgentID: "a", SHA: "s",
		Config: sensu.CandidateConfig{SystemPrompt: "p"},
	})
	if err != nil {
		t.Errorf("expected disabled client to return nil error, got %v", err)
	}
	if v != nil {
		t.Errorf("expected disabled client to return nil version, got %+v", v)
	}
}

func TestRegisterAgentVersion_ReportsNon2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Agent not found", http.StatusNotFound)
	}))
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey: "test-key", BaseURL: ts.URL, DisableLivePricing: true,
	})
	_, err := c.RegisterAgentVersion(context.Background(), sensu.RegisterAgentVersionOptions{
		AgentID: "missing", SHA: "s",
		Config: sensu.CandidateConfig{SystemPrompt: "p"},
	})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got %v", err)
	}
}
