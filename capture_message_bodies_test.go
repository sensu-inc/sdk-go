package sensu

import (
	"strings"
	"testing"
)

// sanitizeMessagesSnapshot is the Replay v1 wire contract: bodies are
// stripped unless CaptureMessageBodies was set on the client.

func newTestClient(capture bool) *SensuClient {
	return NewClient(ClientOptions{
		APIKey:               "test-key",
		BaseURL:              "http://localhost:9999",
		AgentID:              "agent-1",
		OrgID:                "org-1",
		BatchSize:            100,
		FlushIntervalMs:      999_999,
		DisableLivePricing:   true,
		CaptureMessageBodies: capture,
	})
}

func sample(body string) MessageSnapshotItem {
	return MessageSnapshotItem{
		Role:        "user",
		TokenCount:  5,
		ContentHash: "h1",
		Body:        body,
	}
}

func TestSanitize_StripsBodyWhenCaptureDisabled(t *testing.T) {
	c := newTestClient(false)
	out := c.sanitizeMessagesSnapshot([]MessageSnapshotItem{
		sample("hello"),
		sample("world"),
		sample(""),
	})
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	for i, m := range out {
		if m.Body != "" {
			t.Errorf("out[%d].Body = %q, want empty", i, m.Body)
		}
	}
}

func TestSanitize_PreservesBodyWhenCaptureEnabled(t *testing.T) {
	c := newTestClient(true)
	out := c.sanitizeMessagesSnapshot([]MessageSnapshotItem{
		sample("hello"),
		sample(""),
	})
	if out[0].Body != "hello" {
		t.Errorf("out[0].Body = %q, want %q", out[0].Body, "hello")
	}
	if out[1].Body != "" {
		t.Errorf("out[1].Body = %q, want empty", out[1].Body)
	}
}

func TestSanitize_CapsBodyAtServerSchemaLimit(t *testing.T) {
	c := newTestClient(true)
	giant := strings.Repeat("x", 80_000)
	out := c.sanitizeMessagesSnapshot([]MessageSnapshotItem{sample(giant)})
	if got := len(out[0].Body); got != maxBodyChars {
		t.Errorf("len(out[0].Body) = %d, want %d", got, maxBodyChars)
	}
}

func TestSanitize_DoesNotMutateCallerSlice(t *testing.T) {
	c := newTestClient(false)
	in := []MessageSnapshotItem{sample("secret")}
	_ = c.sanitizeMessagesSnapshot(in)
	if in[0].Body != "secret" {
		t.Errorf("sanitize mutated the caller's input: got %q", in[0].Body)
	}
}

func TestSanitize_PreservesNonBodyFields(t *testing.T) {
	m := MessageSnapshotItem{
		Role:        "assistant",
		ToolName:    "search",
		TokenCount:  42,
		ContentHash: "abc123",
		Body:        "sensitive",
	}
	off := newTestClient(false).sanitizeMessagesSnapshot([]MessageSnapshotItem{m})[0]
	if off.Role != "assistant" || off.ToolName != "search" || off.TokenCount != 42 || off.ContentHash != "abc123" {
		t.Errorf("non-body fields mutated: %+v", off)
	}
	if off.Body != "" {
		t.Errorf("Body should be stripped: got %q", off.Body)
	}
	on := newTestClient(true).sanitizeMessagesSnapshot([]MessageSnapshotItem{m})[0]
	if on.Body != "sensitive" {
		t.Errorf("on.Body = %q, want %q", on.Body, "sensitive")
	}
}
