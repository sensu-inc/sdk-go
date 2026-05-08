package senzu

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

func utcNowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// mergeEvent combines base and extra into a new event map.
// Extra fields override base on collision; nil values in extra are skipped.
func mergeEvent(base telemetryEvent, extra telemetryEvent) telemetryEvent {
	result := make(telemetryEvent, len(base)+len(extra))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		if v != nil {
			result[k] = v
		}
	}
	return result
}

// usageResult holds extracted token/cost data from an LLM response.
type usageResult struct {
	inputTokens       int
	outputTokens      int
	cachedInputTokens *int
	totalTokens       int
	costUSDEstimate   *float64
}

// extractUsage inspects an LLM response struct via reflection to find token
// usage fields. Supports both Anthropic SDK and OpenAI SDK response shapes
// without importing those packages in the core module.
//
// Anthropic: Message.Usage.{InputTokens, OutputTokens, CacheCreationInputTokens, CacheReadInputTokens}
// OpenAI:    ChatCompletion.Usage.{PromptTokens, CompletionTokens}
func extractUsage(result any) usageResult {
	if result == nil {
		return usageResult{}
	}
	v := reflect.ValueOf(result)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return usageResult{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return usageResult{}
	}

	usageField := v.FieldByName("Usage")
	if !usageField.IsValid() {
		return usageResult{}
	}
	u := usageField
	if u.Kind() == reflect.Ptr {
		if u.IsNil() {
			return usageResult{}
		}
		u = u.Elem()
	}
	if u.Kind() != reflect.Struct {
		return usageResult{}
	}

	// Anthropic shape
	inputF := u.FieldByName("InputTokens")
	outputF := u.FieldByName("OutputTokens")
	if inputF.IsValid() && outputF.IsValid() {
		input := int(inputF.Int())
		if cacheCreate := u.FieldByName("CacheCreationInputTokens"); cacheCreate.IsValid() {
			input += int(cacheCreate.Int())
		}
		output := int(outputF.Int())
		out := usageResult{
			inputTokens:  input,
			outputTokens: output,
			totalTokens:  input + output,
		}
		if cacheRead := u.FieldByName("CacheReadInputTokens"); cacheRead.IsValid() && cacheRead.Int() > 0 {
			v := int(cacheRead.Int())
			out.cachedInputTokens = &v
		}
		return out
	}

	// OpenAI shape
	promptF := u.FieldByName("PromptTokens")
	completionF := u.FieldByName("CompletionTokens")
	if promptF.IsValid() && completionF.IsValid() {
		input := int(promptF.Int())
		output := int(completionF.Int())
		return usageResult{
			inputTokens:  input,
			outputTokens: output,
			totalTokens:  input + output,
		}
	}

	return usageResult{}
}

// estimateBytes approximates the serialized size of a value for output_size_bytes.
func estimateBytes(v any) int {
	if v == nil {
		return 0
	}
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}

// formatDebugEvent returns a one-line summary for DebugMode console output.
func formatDebugEvent(ev telemetryEvent) string {
	etype, _ := ev["event_type"].(string)
	switch etype {
	case EventLLMRequestCompleted:
		in, _ := ev["input_tokens"].(int)
		out, _ := ev["output_tokens"].(int)
		return fmt.Sprintf("llm.request.completed  provider=%v model=%v tokens=%d latency=%vms",
			ev["provider"], ev["model"], in+out, ev["latency_ms"])
	case EventToolCallCompleted:
		return fmt.Sprintf("tool.call.completed  tool=%v latency=%vms status=%v",
			ev["tool_name"], ev["latency_ms"], ev["status"])
	case EventRunStarted:
		id, _ := ev["run_id"].(string)
		if len(id) > 8 {
			id = id[:8]
		}
		return fmt.Sprintf("agent.run.started  run=%s", id)
	case EventRunCompleted, EventRunFailed:
		id, _ := ev["run_id"].(string)
		if len(id) > 8 {
			id = id[:8]
		}
		return fmt.Sprintf("%s  run=%s", etype, id)
	default:
		return etype
	}
}

// intPtr is a convenience helper for *int literals.
func intPtr(i int) *int { return &i }

// float64Ptr is a convenience helper for *float64 literals.
func float64Ptr(f float64) *float64 { return &f }
