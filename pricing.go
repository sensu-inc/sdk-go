package senzu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
)

// pricingCache is a session-scoped, read-heavy cache of resolved model pricing.
type pricingCache struct {
	mu    sync.RWMutex
	store map[string][2]float64 // "provider:model" → [inputPer1M, outputPer1M]
}

func newPricingCache() *pricingCache {
	return &pricingCache{store: make(map[string][2]float64)}
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

// bundledPricing mirrors MODEL_PRICING in client.ts — used when live pricing
// is disabled or a network call fails.
var bundledPricing = map[string][2]float64{
	"claude-opus-4-7":             {15.00, 75.00},
	"claude-opus-4-6":             {15.00, 75.00},
	"claude-sonnet-4-6":           {3.00, 15.00},
	"claude-haiku-4-5-20251001":   {0.80, 4.00},
	"claude-3-5-sonnet-20241022":  {3.00, 15.00},
	"claude-3-5-haiku-20241022":   {0.80, 4.00},
	"claude-3-opus-20240229":      {15.00, 75.00},
	"gpt-4o":                      {2.50, 10.00},
	"gpt-4o-mini":                 {0.15, 0.60},
	"gpt-4-turbo":                 {10.00, 30.00},
	"gpt-3.5-turbo":               {0.50, 1.50},
	"gemini-1.5-pro":              {1.25, 5.00},
	"gemini-1.5-flash":            {0.075, 0.30},
}

// resolvePricing returns [inputPer1M, outputPer1M] for a model.
// Priority: session cache → live API → bundled table → [0, 0].
func resolvePricing(
	ctx context.Context,
	httpClient *http.Client,
	baseURL, apiKey, provider, model string,
	cache *pricingCache,
	disableLive bool,
	disabled bool,
) [2]float64 {
	if disableLive || disabled || apiKey == "" {
		return lookupBundled(model)
	}

	key := provider + ":" + model
	if cached, ok := cache.get(key); ok {
		return cached
	}

	// Fetch from /api/v1/pricing/models/{provider}/{model}
	u := fmt.Sprintf("%s/api/v1/pricing/models/%s/%s",
		baseURL, url.PathEscape(provider), url.PathEscape(model))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err == nil {
		req.Header.Set("X-API-Key", apiKey)
		resp, err := httpClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var data struct {
				InputPricePer1mTokens  *float64 `json:"inputPricePer1mTokens"`
				OutputPricePer1mTokens *float64 `json:"outputPricePer1mTokens"`
			}
			if json.NewDecoder(resp.Body).Decode(&data) == nil &&
				data.InputPricePer1mTokens != nil &&
				data.OutputPricePer1mTokens != nil {
				pair := [2]float64{*data.InputPricePer1mTokens, *data.OutputPricePer1mTokens}
				cache.set(key, pair)
				return pair
			}
		}
	}

	p := lookupBundled(model)
	cache.set(key, p) // cache fallback to avoid repeated failed fetches
	return p
}

func lookupBundled(model string) [2]float64 {
	if p, ok := bundledPricing[model]; ok {
		return p
	}
	return [2]float64{0, 0}
}

func estimateCost(pricing [2]float64, inputTokens, outputTokens int) float64 {
	return (float64(inputTokens)/1_000_000)*pricing[0] +
		(float64(outputTokens)/1_000_000)*pricing[1]
}
