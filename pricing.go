package sensu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
)

// pricingCache is a session-scoped, read-heavy cache of resolved model
// pricing. It also tracks which (provider, model) pairs have already
// emitted a failure-warning log line so repeated misses don't spam the
// logs.
type pricingCache struct {
	mu     sync.RWMutex
	store  map[string][2]float64 // "provider:model" → [inputPer1M, outputPer1M]
	warned map[string]struct{}   // "provider:model" → already-warned set
}

func newPricingCache() *pricingCache {
	return &pricingCache{
		store:  make(map[string][2]float64),
		warned: make(map[string]struct{}),
	}
}

func (c *pricingCache) get(key string) ([2]float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.store[key]
	return v, ok
}

func (c *pricingCache) set(key string, v [2]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = v
}

// warnOnce returns true if this (provider, model) is being warned about for
// the first time. Subsequent calls return false so callers can no-op.
func (c *pricingCache) warnOnce(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.warned[key]; ok {
		return false
	}
	c.warned[key] = struct{}{}
	return true
}

// resolvePricing returns [inputPer1M, outputPer1M] for a model.
//
// Priority: session cache → live API → [0, 0] (with warning).
//
// Per SDK_CONSOLIDATION_PLAN.md §3c, the SDK no longer ships a bundled
// pricing table — customers are assumed online; the live
// /api/v1/pricing/models endpoint covers the common case (including
// custom models registered via POST /api/v1/pricing/org-models). On any
// failure (API unreachable, 4xx/5xx, null rates, disableLive, disabled,
// or missing apiKey), this returns [0, 0] and logs a warning at most
// once per (provider, model) per client lifetime. The server's ingest
// pipeline reconciles cost from llm_calls + the catalog at query time,
// so dashboards stay correct even when an SDK call sends 0.
func resolvePricing(
	ctx context.Context,
	httpClient *http.Client,
	baseURL, apiKey, provider, model string,
	cache *pricingCache,
	disableLive bool,
	disabled bool,
) [2]float64 {
	key := provider + ":" + model

	if cached, ok := cache.get(key); ok {
		return cached
	}

	if disableLive || disabled || apiKey == "" {
		reason := "disableLivePricing=true"
		switch {
		case disabled:
			reason = "client disabled"
		case apiKey == "":
			reason = "no API key"
		}
		warnPricingMiss(cache, key, reason)
		return [2]float64{0, 0}
	}

	u := fmt.Sprintf("%s/api/v1/pricing/models/%s/%s",
		baseURL, url.PathEscape(provider), url.PathEscape(model))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		warnPricingMiss(cache, key, fmt.Sprintf("build request: %v", err))
		return [2]float64{0, 0}
	}
	req.Header.Set("X-API-Key", apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		warnPricingMiss(cache, key, fmt.Sprintf("network error: %v", err))
		return [2]float64{0, 0}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		warnPricingMiss(cache, key, fmt.Sprintf("API returned %d", resp.StatusCode))
		return [2]float64{0, 0}
	}

	var data struct {
		InputPricePer1mTokens  *float64 `json:"inputPricePer1mTokens"`
		OutputPricePer1mTokens *float64 `json:"outputPricePer1mTokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		warnPricingMiss(cache, key, fmt.Sprintf("decode response: %v", err))
		return [2]float64{0, 0}
	}
	if data.InputPricePer1mTokens == nil || data.OutputPricePer1mTokens == nil {
		warnPricingMiss(cache, key, "API returned 200 with null rates")
		return [2]float64{0, 0}
	}

	pair := [2]float64{*data.InputPricePer1mTokens, *data.OutputPricePer1mTokens}
	cache.set(key, pair)
	return pair
}

func warnPricingMiss(cache *pricingCache, key, reason string) {
	if !cache.warnOnce(key) {
		return
	}
	log.Printf("[sensu:sdk] live pricing unavailable for %s (%s); "+
		"cost estimates for this model will be 0 until the API call succeeds. "+
		"If this is a custom model, register it via POST /api/v1/pricing/org-models.",
		key, reason)
}

func estimateCost(pricing [2]float64, inputTokens, outputTokens int) float64 {
	return (float64(inputTokens)/1_000_000)*pricing[0] +
		(float64(outputTokens)/1_000_000)*pricing[1]
}
