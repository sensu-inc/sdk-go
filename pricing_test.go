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
	"time"
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
	cache := newPricingCache(time.Hour)

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
	cache := newPricingCache(time.Hour)

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
	cache := newPricingCache(time.Hour)

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
	cache := newPricingCache(time.Hour)

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
	cache := newPricingCache(time.Hour)

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
	cache := newPricingCache(time.Hour)

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
	cache := newPricingCache(time.Hour)

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
	cache := newPricingCache(time.Hour)

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
	cache := newPricingCache(time.Hour)

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

// ---------------------------------------------------------------------------
// Cache TTL — uses newPricingCacheWithClock for deterministic time travel
// ---------------------------------------------------------------------------

// fakeClock returns a controllable time.Time source. now() returns the
// current value of *now; advance via the closure.
func fakeClock() (now func() time.Time, advance func(time.Duration)) {
	t0 := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	cur := t0
	now = func() time.Time { return cur }
	advance = func(d time.Duration) { cur = cur.Add(d) }
	return
}

func TestPricingCache_RefetchesAfterTTLExpires(t *testing.T) {
	baseURL, client, calls := makePricingServer(t, http.StatusOK, map[string]any{
		"inputPricePer1mTokens":  15.0,
		"outputPricePer1mTokens": 75.0,
	})
	now, advance := fakeClock()
	cache := newPricingCacheWithClock(60*time.Second, now)

	resolvePricing(context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false)
	if *calls != 1 {
		t.Fatalf("first call: expected 1 fetch, got %d", *calls)
	}

	// Within TTL → cache hit.
	advance(59 * time.Second)
	resolvePricing(context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false)
	if *calls != 1 {
		t.Errorf("at t=59s (within TTL): expected 1 fetch, got %d", *calls)
	}

	// Past TTL → refetch.
	advance(2 * time.Second)
	resolvePricing(context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false)
	if *calls != 2 {
		t.Errorf("at t=61s (past TTL): expected 2 fetches, got %d", *calls)
	}
}

func TestPricingCache_PicksUpUpdatedRatesOnRefetch(t *testing.T) {
	now, advance := fakeClock()
	cache := newPricingCacheWithClock(1*time.Second, now)

	// First server returns one rate.
	var step int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		step++
		if step == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"inputPricePer1mTokens":  15.0,
				"outputPricePer1mTokens": 75.0,
			})
			return
		}
		// After expiry, server has updated rates (e.g. customer
		// registered a discount via POST /api/v1/pricing/org-models).
		_ = json.NewEncoder(w).Encode(map[string]any{
			"inputPricePer1mTokens":  12.0,
			"outputPricePer1mTokens": 60.0,
		})
	}))
	defer ts.Close()

	before := resolvePricing(context.Background(), ts.Client(), ts.URL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false)
	if before != [2]float64{15, 75} {
		t.Fatalf("before: expected [15 75], got %v", before)
	}

	advance(2 * time.Second) // past 1s TTL
	after := resolvePricing(context.Background(), ts.Client(), ts.URL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false)
	if after != [2]float64{12, 60} {
		t.Errorf("after refetch: expected [12 60], got %v", after)
	}
}

func TestPricingCache_TTLZeroDisablesCaching(t *testing.T) {
	baseURL, client, calls := makePricingServer(t, http.StatusOK, map[string]any{
		"inputPricePer1mTokens":  15.0,
		"outputPricePer1mTokens": 75.0,
	})
	cache := newPricingCache(0)

	for i := 0; i < 5; i++ {
		resolvePricing(context.Background(), client, baseURL, "test-key",
			"anthropic", "claude-opus-4-7", cache, false, false)
	}
	if *calls != 5 {
		t.Errorf("TTL=0 should disable caching: expected 5 fetches, got %d", *calls)
	}
}

func TestPricingCache_TTLAppliesPerProviderModelIndependently(t *testing.T) {
	baseURL, client, calls := makePricingServer(t, http.StatusOK, map[string]any{
		"inputPricePer1mTokens":  1.0,
		"outputPricePer1mTokens": 2.0,
	})
	now, advance := fakeClock()
	cache := newPricingCacheWithClock(60*time.Second, now)

	// Prime both keys.
	resolvePricing(context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false)
	resolvePricing(context.Background(), client, baseURL, "test-key",
		"openai", "gpt-4o", cache, false, false)
	if *calls != 2 {
		t.Fatalf("priming: expected 2 fetches, got %d", *calls)
	}

	// Within TTL → both cache hits.
	advance(30 * time.Second)
	resolvePricing(context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false)
	resolvePricing(context.Background(), client, baseURL, "test-key",
		"openai", "gpt-4o", cache, false, false)
	if *calls != 2 {
		t.Errorf("within TTL: expected 2 fetches, got %d", *calls)
	}

	// Past TTL → both refetch.
	advance(60 * time.Second)
	resolvePricing(context.Background(), client, baseURL, "test-key",
		"anthropic", "claude-opus-4-7", cache, false, false)
	resolvePricing(context.Background(), client, baseURL, "test-key",
		"openai", "gpt-4o", cache, false, false)
	if *calls != 4 {
		t.Errorf("past TTL: expected 4 fetches, got %d", *calls)
	}
}

func TestClientOptions_PricingCacheTTL_SentinelHandling(t *testing.T) {
	tests := []struct {
		name        string
		optsTTLMs   int
		wantTTL     time.Duration
	}{
		{"unset (zero value) → default 1 hour", 0, time.Hour},
		{"NoPricingCache → disabled (zero duration)", NoPricingCache, 0},
		{"custom positive → that value", 60_000, 60 * time.Second},
		{"explicit 1ms → 1ms", 1, time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(ClientOptions{
				APIKey: "k", BaseURL: "http://x", AgentID: "a",
				DisableLivePricing: true,
				BatchSize:          100, FlushIntervalMs: 999_999,
				PricingCacheTTLMs: tt.optsTTLMs,
			})
			if c.pricing.ttl != tt.wantTTL {
				t.Errorf("ttl = %v, want %v", c.pricing.ttl, tt.wantTTL)
			}
		})
	}
}
