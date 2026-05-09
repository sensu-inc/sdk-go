package sensu_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sensu-inc/sdk-go"
)

func TestFlushPostsEventsToServer(t *testing.T) {
	var received []map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("X-API-Key") == "" {
			t.Error("missing X-API-Key header")
		}
		var body struct {
			Events []map[string]any `json:"events"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		received = body.Events
		json.NewEncoder(w).Encode(map[string]any{"processed": len(body.Events)})
	}))
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey:             "snz_test",
		BaseURL:            ts.URL,
		DisableLivePricing: true,
		FlushIntervalMs:    999_999,
		BatchSize:          100,
	})
	defer c.Close(context.Background())

	c.Enqueue(map[string]any{"event_type": "test.one"})
	c.Enqueue(map[string]any{"event_type": "test.two"})

	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
}

func TestBatchSizeTriggerFlushes(t *testing.T) {
	var calls atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"processed": 1})
	}))
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey:          "snz_test",
		BaseURL:         ts.URL,
		BatchSize:       3,
		FlushIntervalMs: 999_999,
	})
	defer c.Close(context.Background())

	// Enqueue exactly batchSize events — should auto-flush
	for i := 0; i < 3; i++ {
		c.Enqueue(map[string]any{"event_type": "test"})
	}

	// Allow runLoop to process
	time.Sleep(50 * time.Millisecond)

	if calls.Load() < 1 {
		t.Fatal("expected at least one auto-flush when batchSize is reached")
	}
}

func TestFlushReturnsErrorOnServerFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey:          "snz_test",
		BaseURL:         ts.URL,
		FlushIntervalMs: 999_999,
		BatchSize:       100,
	})

	c.Enqueue(map[string]any{"event_type": "test"})

	err := c.Flush(context.Background())
	if err == nil {
		t.Fatal("expected error when server returns 500")
	}
}

func TestDisabledClientSkipsHTTP(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"processed": 1})
	}))
	defer ts.Close()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey:    "snz_test",
		BaseURL:   ts.URL,
		Disabled:  true,
		BatchSize: 1,
	})

	c.Enqueue(map[string]any{"event_type": "test"})
	c.Flush(context.Background())
	time.Sleep(20 * time.Millisecond)

	if calls.Load() != 0 {
		t.Fatalf("disabled client should make no HTTP calls, got %d", calls.Load())
	}
}

func TestFlushContextCancellation(t *testing.T) {
	// Server that never responds within the test window.
	hangCh := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hangCh: // unblocked by test cleanup
		case <-r.Context().Done(): // unblocked when the client cancels
		}
	}))
	defer func() {
		close(hangCh)
		ts.CloseClientConnections() // abort in-flight connections so ts.Close() doesn't block
		ts.Close()
	}()

	c := sensu.NewClient(sensu.ClientOptions{
		APIKey:          "snz_test",
		BaseURL:         ts.URL,
		FlushIntervalMs: 999_999,
		BatchSize:       100,
	})

	c.Enqueue(map[string]any{"event_type": "test"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := c.Flush(ctx)
	if err == nil {
		t.Fatal("expected context deadline error from Flush")
	}
}
