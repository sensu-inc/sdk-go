package sensu_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sensu-inc/sdk-go"
)

// ---------------------------------------------------------------------------
// TrackToolCtx
// ---------------------------------------------------------------------------

func TestTrackToolCtx_EmitsStepAndToolCallEvents(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	err := c.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		result, err := sensu.TrackToolCtx(ctx, func() (string, error) {
			return "found 3 results", nil
		}, sensu.TrackToolOptions{ToolName: "web_search"})
		if err != nil {
			return err
		}
		if result != "found 3 results" {
			t.Errorf("expected result from fn, got %q", result)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	c.Flush(ctx)

	types := eventTypes(allEvents(*batches))
	mustContain(t, types, sensu.EventStepStarted)
	mustContain(t, types, sensu.EventToolCallCompleted)
	mustContain(t, types, sensu.EventStepCompleted)

	for _, e := range allEvents(*batches) {
		if e["event_type"] == sensu.EventToolCallCompleted {
			if e["tool_name"] != "web_search" {
				t.Errorf("expected tool_name=web_search, got %v", e["tool_name"])
			}
			return
		}
	}
	t.Fatal("tool.call.completed event not found")
}

func TestTrackToolCtx_PropagatesFnError(t *testing.T) {
	c, _, ts := makeClientWithServer(t)
	defer ts.Close()

	want := errors.New("tool blew up")
	err := c.Run(context.Background(), sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		_, gotErr := sensu.TrackToolCtx(ctx, func() (string, error) {
			return "", want
		}, sensu.TrackToolOptions{ToolName: "broken"})
		if !errors.Is(gotErr, want) {
			t.Errorf("expected propagated error, got %v", gotErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}

func TestTrackToolCtx_NoRunInContext_FallsThroughWithoutTelemetry(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	// Plain context.Background() — no run injected. The wrapper should
	// just call fn and emit no events.
	result, err := sensu.TrackToolCtx(context.Background(), func() (string, error) {
		return "ok", nil
	}, sensu.TrackToolOptions{ToolName: "search"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected fn return, got %q", result)
	}
	c.Flush(context.Background())

	for _, e := range allEvents(*batches) {
		if strings.HasPrefix(e["event_type"].(string), "tool.") || strings.HasPrefix(e["event_type"].(string), "agent.step.") {
			t.Fatalf("expected no telemetry without an active run, saw: %v", e["event_type"])
		}
	}
}

// ---------------------------------------------------------------------------
// TrackRetrievalCtx
// ---------------------------------------------------------------------------

func TestTrackRetrievalCtx_EmitsStepAndRetrievalEvents(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	err := c.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		_, err := sensu.TrackRetrievalCtx(ctx, func() ([]string, error) {
			return []string{"chunk-1", "chunk-2"}, nil
		}, sensu.TrackRetrievalOptions{VectorStoreID: "vs-1"})
		return err
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	c.Flush(ctx)

	types := eventTypes(allEvents(*batches))
	mustContain(t, types, sensu.EventStepStarted)
	mustContain(t, types, sensu.EventRetrievalCompleted)
}

// ---------------------------------------------------------------------------
// TrackEmbeddingCtx
// ---------------------------------------------------------------------------

func TestTrackEmbeddingCtx_EmitsStepAndEmbeddingEvents(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	err := c.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		_, err := sensu.TrackEmbeddingCtx(ctx, func() ([]float32, error) {
			return []float32{0.1, 0.2}, nil
		}, sensu.TrackEmbeddingOptions{Model: "text-embedding-3-small"})
		return err
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	c.Flush(ctx)

	types := eventTypes(allEvents(*batches))
	mustContain(t, types, sensu.EventStepStarted)
	mustContain(t, types, sensu.EventEmbeddingCreated)
}

// ---------------------------------------------------------------------------
// TrackGuardrailCtx
// ---------------------------------------------------------------------------

func TestTrackGuardrailCtx_EmitsGuardrailEvents(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	err := c.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
		result, err := sensu.TrackGuardrailCtx(ctx, func() (string, error) {
			return "pass", nil
		}, sensu.TrackGuardrailOptions{GuardrailID: "pii-check", GuardrailType: "pii"})
		if err != nil {
			return err
		}
		if result != "pass" {
			t.Errorf("expected pass, got %q", result)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	c.Flush(ctx)

	types := eventTypes(allEvents(*batches))
	mustContain(t, types, sensu.EventStepStarted)
	mustContain(t, types, sensu.EventGuardrailCheckCompleted)
}

func TestTrackGuardrailCtx_NoRunFallsThroughWithoutTelemetry(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	result, err := sensu.TrackGuardrailCtx(context.Background(), func() (string, error) {
		return "pass", nil
	}, sensu.TrackGuardrailOptions{GuardrailID: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "pass" {
		t.Errorf("expected pass, got %q", result)
	}
	c.Flush(context.Background())

	for _, e := range allEvents(*batches) {
		if strings.HasPrefix(e["event_type"].(string), "guardrail.") {
			t.Fatalf("expected no telemetry without an active run, saw: %v", e["event_type"])
		}
	}
}

// mustContain fails the test if want is not in slice.
func mustContain(t *testing.T, slice []string, want string) {
	t.Helper()
	for _, s := range slice {
		if s == want {
			return
		}
	}
	t.Errorf("expected event type %q in %v", want, slice)
}
