// White-box tests for the pricing resolver (v0.4.0 — bundled fallback
// removed per SDK_CONSOLIDATION_PLAN.md §3c). Lives in `package sensu`
// rather than `package sensu_test` so it can call the unexported
// resolvePricing + pricingCache directly.
package sensu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// captureLogs redirects the standard logger to a buffer for the duration
// of the test, returning a thunk to read its accumulated output.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	prev := log.Writer()
	prevFlags := log.Flags()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0) // strip timestamps for stable substring asserts
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(prevFlags)
	})
	var mu sync.Mutex
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

// makePricingServer responds with a fixed JSON body + status. callsHit
// records how many times the server was hit so cache tests can assert
// "second call did not refetch."
func makePricingServer(t *testing.T, status int, body any) (string, *http.Client, *int) {
	t.Helper()
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
	t.Cleanup(ts.Close)
	return ts.URL, ts.Client(), &calls
}

// ---------------------------------------------------------------------------
// Success path
// ---------------------------------------------------------------------------

func TestResolvePricing_LiveSuccessCachesAndReuses(t *testing.T) {
	baseURL, client, calls := makePricingServer(t, http.StatusOK, map[string]any{
		"provider":               "anthropic",
		"model":                  "claude-opus-4-7",
		"source":                 "estimated",
		"inputPricePer1mTokens":  15.0,
		"outputPricePer1mTokens": 75.0,
	})
	cache := newPricingCache()

	first := resolvePricing(
		context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false,
	)
	if first != [2]float64{15, 75} {
		t.Fatalf("first call: expected [15 75], got %v", first)
	}

	second := resolvePricing(
		context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false,
	)
	if second != [2]float64{15, 75} {
		t.Fatalf("second call: expected [15 75], got %v", second)
	}
	if *calls != 1 {
		t.Errorf("expected exactly 1 HTTP call, got %d", *calls)
	}
}

// ---------------------------------------------------------------------------
// Failure paths
// ---------------------------------------------------------------------------

func TestResolvePricing_404ReturnsZerosAndWarnsOnce(t *testing.T) {
	readLogs := captureLogs(t)
	baseURL, client, _ := makePricingServer(t, http.StatusNotFound, nil)
	cache := newPricingCache()

	first := resolvePricing(
		context.Background(), client, baseURL, "test-key",
		"cohere", "command-r-future", cache, false, false,
	)
	if first != [2]float64{0, 0} {
		t.Errorf("expected zeros, got %v", first)
	}

	// Re-call: should NOT emit a second warning for the same key.
	resolvePricing(
		context.Background(), client, baseURL, "test-key",
		"cohere", "command-r-future", cache, false, false,
	)

	logs := readLogs()
	count := strings.Count(logs, "live pricing unavailable for cohere:command-r-future")
	if count != 1 {
		t.Errorf("expected exactly 1 warning for this key, got %d (logs: %s)", count, logs)
	}
	if !strings.Contains(logs, "API returned 404") {
		t.Errorf("expected reason 'API returned 404' in logs, got: %s", logs)
	}
}

func TestResolvePricing_5xxReturnsZerosAndWarns(t *testing.T) {
	readLogs := captureLogs(t)
	baseURL, client, _ := makePricingServer(t, http.StatusInternalServerError, nil)
	cache := newPricingCache()

	result := resolvePricing(
		context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false,
	)
	if result != [2]float64{0, 0} {
		t.Errorf("expected zeros, got %v", result)
	}
	if !strings.Contains(readLogs(), "API returned 500") {
		t.Errorf("expected 'API returned 500' in logs, got: %s", readLogs())
	}
}

func TestResolvePricing_NetworkErrorReturnsZerosAndWarns(t *testing.T) {
	readLogs := captureLogs(t)
	// Use a transport that always errors.
	client := &http.Client{Transport: errorTransport{}}
	cache := newPricingCache()

	result := resolvePricing(
		context.Background(), client, "http://localhost:0", "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false,
	)
	if result != [2]float64{0, 0} {
		t.Errorf("expected zeros, got %v", result)
	}
	if !strings.Contains(readLogs(), "network error") {
		t.Errorf("expected 'network error' in logs, got: %s", readLogs())
	}
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic network failure")
}

func TestResolvePricing_200WithNullRatesTreatedAsMiss(t *testing.T) {
	readLogs := captureLogs(t)
	baseURL, client, _ := makePricingServer(t, http.StatusOK, map[string]any{
		"provider":               "anthropic",
		"model":                  "mystery",
		"inputPricePer1mTokens":  nil,
		"outputPricePer1mTokens": nil,
	})
	cache := newPricingCache()

	result := resolvePricing(
		context.Background(), client, baseURL, "test-key",
		"anthropic", "mystery", cache, false, false,
	)
	if result != [2]float64{0, 0} {
		t.Errorf("expected zeros, got %v", result)
	}
	if !strings.Contains(readLogs(), "null rates") {
		t.Errorf("expected 'null rates' in logs, got: %s", readLogs())
	}
}

// ---------------------------------------------------------------------------
// Short-circuit paths (no HTTP call)
// ---------------------------------------------------------------------------

func TestResolvePricing_DisableLiveSkipsFetchAndWarns(t *testing.T) {
	readLogs := captureLogs(t)
	baseURL, client, calls := makePricingServer(t, http.StatusOK, nil)
	cache := newPricingCache()

	result := resolvePricing(
		context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, true /* disableLive */, false,
	)
	if result != [2]float64{0, 0} {
		t.Errorf("expected zeros, got %v", result)
	}
	if *calls != 0 {
		t.Errorf("expected 0 HTTP calls when disabled, got %d", *calls)
	}
	if !strings.Contains(readLogs(), "disableLivePricing=true") {
		t.Errorf("expected 'disableLivePricing=true' in logs, got: %s", readLogs())
	}
}

func TestResolvePricing_DisabledClientSkipsFetchAndWarns(t *testing.T) {
	readLogs := captureLogs(t)
	baseURL, client, calls := makePricingServer(t, http.StatusOK, nil)
	cache := newPricingCache()

	result := resolvePricing(
		context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, true, /* disabled */
	)
	if result != [2]float64{0, 0} {
		t.Errorf("expected zeros, got %v", result)
	}
	if *calls != 0 {
		t.Errorf("expected 0 HTTP calls when client disabled, got %d", *calls)
	}
	if !strings.Contains(readLogs(), "client disabled") {
		t.Errorf("expected 'client disabled' in logs, got: %s", readLogs())
	}
}

func TestResolvePricing_MissingApiKeySkipsFetchAndWarns(t *testing.T) {
	readLogs := captureLogs(t)
	baseURL, client, calls := makePricingServer(t, http.StatusOK, nil)
	cache := newPricingCache()

	result := resolvePricing(
		context.Background(), client, baseURL, "", /* apiKey */
		"anthropic", "claude-opus-4-7", cache, false, false,
	)
	if result != [2]float64{0, 0} {
		t.Errorf("expected zeros, got %v", result)
	}
	if *calls != 0 {
		t.Errorf("expected 0 HTTP calls without API key, got %d", *calls)
	}
	if !strings.Contains(readLogs(), "no API key") {
		t.Errorf("expected 'no API key' in logs, got: %s", readLogs())
	}
}

// ---------------------------------------------------------------------------
// Per-(provider, model) warning isolation
// ---------------------------------------------------------------------------

func TestResolvePricing_DifferentModelsWarnIndependently(t *testing.T) {
	readLogs := captureLogs(t)
	baseURL, client, _ := makePricingServer(t, http.StatusNotFound, nil)
	cache := newPricingCache()

	for i := 0; i < 3; i++ {
		resolvePricing(
			context.Background(), client, baseURL, "test-key",
			"anthropic", "claude-opus-4-7", cache, false, false,
		)
	}
	for i := 0; i < 2; i++ {
		resolvePricing(
			context.Background(), client, baseURL, "test-key",
			"openai", "gpt-4o", cache, false, false,
		)
	}

	logs := readLogs()
	if c := strings.Count(logs, "live pricing unavailable for anthropic:claude-opus-4-7"); c != 1 {
		t.Errorf("anthropic warning count = %d, want 1", c)
	}
	if c := strings.Count(logs, "live pricing unavailable for openai:gpt-4o"); c != 1 {
		t.Errorf("openai warning count = %d, want 1", c)
	}
}

// ---------------------------------------------------------------------------
// estimateCost stays pure — kept after the pivot for the trackLLM path
// ---------------------------------------------------------------------------

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		name           string
		pricing        [2]float64
		inputTokens    int
		outputTokens   int
		want           float64
	}{
		{"zero rates yield zero cost", [2]float64{0, 0}, 1000, 1000, 0},
		{"standard rates", [2]float64{3.0, 15.0}, 1_000_000, 1_000_000, 18.0},
		{"sub-1M token math", [2]float64{15.0, 75.0}, 500, 1000, 0.0075 + 0.075},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateCost(tt.pricing, tt.inputTokens, tt.outputTokens)
			// Use a small tolerance for float comparisons.
			if diff := got - tt.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("estimateCost = %v, want %v", got, tt.want)
			}
		})
	}
}
