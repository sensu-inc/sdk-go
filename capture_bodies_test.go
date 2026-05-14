package sensu_test

// Tests for per-call CaptureBodies opt-in on TrackTool.
// Implements the SDK side of TOOL_IO_CAPTURE_PLAN.md §5.3 + §5.4 + §11.
//
// Two layers:
//   1. Direct tests on SerializeToolBodiesForCapture — pinned semantics
//      for the cross-SDK invariants (default off, opt-in, JSON-marshal
//      both, 256 KB truncation marker, skip-both on Marshal error).
//   2. End-to-end wire-shape tests that drive TrackTool against the
//      test HTTP server and assert what tool.call.completed looks like
//      on the wire — whether input_body / output_body are present and
//      what shape they take.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sensu-inc/sdk-go"
)

// ---------------------------------------------------------------------------
// Layer 1 — pure helper
// ---------------------------------------------------------------------------

func TestSerialize_OptOutReturnsEmptyAndFalse(t *testing.T) {
	in, out, ok := sensu.SerializeToolBodiesForCapture(
		map[string]string{"q": "a"}, map[string]int{"ok": 1}, false,
	)
	if in != "" || out != "" || ok {
		t.Fatalf("got (%q, %q, %v), want (\"\", \"\", false)", in, out, ok)
	}
}

func TestSerialize_OptInWithJSONSerializable(t *testing.T) {
	in, out, ok := sensu.SerializeToolBodiesForCapture(
		map[string]string{"query": "find user@example.com"},
		map[string]any{"matches": 1, "top": map[string]string{"email": "user@example.com"}},
		true,
	)
	if !ok {
		t.Fatal("expected ok=true")
	}
	wantIn := `{"query":"find user@example.com"}`
	if in != wantIn {
		t.Fatalf("input_body = %q, want %q", in, wantIn)
	}
	if !strings.Contains(out, "user@example.com") {
		t.Fatalf("output_body missing email substring: %q", out)
	}
}

func TestSerialize_OptInWithPrimitives(t *testing.T) {
	in, out, ok := sensu.SerializeToolBodiesForCapture("hello", 42, true)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if in != `"hello"` {
		t.Fatalf("input_body = %q, want %q", in, `"hello"`)
	}
	if out != "42" {
		t.Fatalf("output_body = %q, want %q", out, "42")
	}
}

func TestSerialize_OptInWithNilArgsCapturesJSONNull(t *testing.T) {
	// nil any serializes to "null". Cross-SDK parity with sdk-ts
	// (explicit `null` captures) and sdk-python (explicit `None`
	// captures). Use CaptureBodies=false at the call site if you
	// don't want capture on a given call.
	in, out, ok := sensu.SerializeToolBodiesForCapture(nil, map[string]int{"ok": 1}, true)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if in != "null" {
		t.Fatalf("input_body = %q, want %q", in, "null")
	}
	if out != `{"ok":1}` {
		t.Fatalf("output_body = %q, want %q", out, `{"ok":1}`)
	}
}

func TestSerialize_OptInWithUnmarshalableArgsSkipsBoth(t *testing.T) {
	// Channels are unmarshalable by encoding/json. Same skip-both
	// behavior as sdk-ts (circular structures) and sdk-python
	// (RecursionError). Never half-capture.
	ch := make(chan int)
	defer close(ch)
	in, out, ok := sensu.SerializeToolBodiesForCapture(
		map[string]any{"ch": ch}, map[string]int{"ok": 1}, true,
	)
	if in != "" || out != "" || ok {
		t.Fatalf("got (%q, %q, %v), want skip-both", in, out, ok)
	}
}

func TestSerialize_OptInWithUnmarshalableResultSkipsBoth(t *testing.T) {
	in, out, ok := sensu.SerializeToolBodiesForCapture(
		map[string]int{"q": 1}, func() {}, true,  // a function value
	)
	if in != "" || out != "" || ok {
		t.Fatalf("got (%q, %q, %v), want skip-both", in, out, ok)
	}
}

func TestSerialize_BodyExactlyAtCapIsPreservedVerbatim(t *testing.T) {
	// json.Marshal wraps a string in quotes — for a final wire length
	// of exactly 262144, the inner string is 262142 bytes.
	inner := strings.Repeat("x", 262_142)
	in, _, ok := sensu.SerializeToolBodiesForCapture(inner, "ok", true)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(in) != 262_144 {
		t.Fatalf("len(input_body) = %d, want 262144", len(in))
	}
	if strings.HasSuffix(in, "[truncated]") {
		t.Fatalf("input_body unexpectedly truncated: ends with %q", in[len(in)-20:])
	}
}

func TestSerialize_OversizeBodyTruncatedAtCapWithMarker(t *testing.T) {
	giant := strings.Repeat("x", 300_000)
	in, out, ok := sensu.SerializeToolBodiesForCapture(giant, "ok", true)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(in) != 262_144 {
		t.Fatalf("len(input_body) = %d, want 262144", len(in))
	}
	// Cross-SDK marker — same UTF-8 byte sequence as sdk-ts / sdk-python.
	if !strings.HasSuffix(in, " …[truncated]") {
		t.Fatalf("input_body missing truncation marker: ends with %q", in[len(in)-20:])
	}
	// The other side wasn't oversize → preserved unchanged.
	if out != `"ok"` {
		t.Fatalf("output_body = %q, want %q", out, `"ok"`)
	}
}

func TestSerialize_BothOversizeAreTruncatedIndependently(t *testing.T) {
	bigIn  := strings.Repeat("a", 300_000)
	bigOut := strings.Repeat("b", 300_000)
	in, out, ok := sensu.SerializeToolBodiesForCapture(bigIn, bigOut, true)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(in) != 262_144 || len(out) != 262_144 {
		t.Fatalf("len(in)=%d len(out)=%d, want both 262144", len(in), len(out))
	}
	if !strings.HasSuffix(in, " …[truncated]") || !strings.HasSuffix(out, " …[truncated]") {
		t.Fatal("expected both bodies to end with the cross-SDK marker")
	}
}

// ---------------------------------------------------------------------------
// Layer 2 — TrackTool wire shape via the test HTTP server
// ---------------------------------------------------------------------------

func findToolCompleted(events []map[string]any) (map[string]any, bool) {
	for _, e := range events {
		if e["event_type"] == sensu.EventToolCallCompleted {
			return e, true
		}
	}
	return nil, false
}

func TestTrackTool_DefaultHasNoBodyFields(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	step := run.StartStep(sensu.StartStepOptions{})
	c.Flush(ctx)
	*batches = nil

	_, err := sensu.TrackTool(ctx, step, func() (map[string]int, error) {
		return map[string]int{"matches": 1}, nil
	}, sensu.TrackToolOptions{ToolName: "crm_lookup"})
	if err != nil {
		t.Fatal(err)
	}
	c.Flush(ctx)

	evt, ok := findToolCompleted(allEvents(*batches))
	if !ok {
		t.Fatal("missing tool.call.completed")
	}
	if _, has := evt["input_body"]; has {
		t.Fatalf("input_body unexpectedly present: %v", evt["input_body"])
	}
	if _, has := evt["output_body"]; has {
		t.Fatalf("output_body unexpectedly present: %v", evt["output_body"])
	}
	if evt["status"] != "success" {
		t.Fatalf("status = %v, want success", evt["status"])
	}
}

func TestTrackTool_OptInCarriesBothBodies(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	step := run.StartStep(sensu.StartStepOptions{})
	c.Flush(ctx)
	*batches = nil

	type out struct {
		Matches int    `json:"matches"`
		Email   string `json:"email"`
	}
	_, err := sensu.TrackTool(ctx, step, func() (out, error) {
		return out{Matches: 1, Email: "user@example.com"}, nil
	}, sensu.TrackToolOptions{
		ToolName:      "crm_lookup",
		Args:          map[string]string{"query": "find user@example.com"},
		CaptureBodies: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Flush(ctx)

	evt, ok := findToolCompleted(allEvents(*batches))
	if !ok {
		t.Fatal("missing tool.call.completed")
	}
	if evt["input_body"] != `{"query":"find user@example.com"}` {
		t.Fatalf("input_body = %v", evt["input_body"])
	}
	// Marshaled out struct round-trips back to the same shape.
	var got out
	if err := json.Unmarshal([]byte(evt["output_body"].(string)), &got); err != nil {
		t.Fatalf("output_body not valid JSON: %v", err)
	}
	if got.Matches != 1 || got.Email != "user@example.com" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestTrackTool_OptInWithUnmarshalableResultSkipsBothBodies(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	step := run.StartStep(sensu.StartStepOptions{})
	c.Flush(ctx)
	*batches = nil

	// Result containing a channel — encoding/json.Marshal returns
	// an *UnsupportedTypeError. Both bodies must be skipped, status
	// stays "success" because the tool itself didn't error.
	_, err := sensu.TrackTool(ctx, step, func() (map[string]any, error) {
		return map[string]any{"ch": make(chan int)}, nil
	}, sensu.TrackToolOptions{
		ToolName:      "weird_tool",
		Args:          map[string]string{"q": "a"},
		CaptureBodies: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Flush(ctx)

	evt, ok := findToolCompleted(allEvents(*batches))
	if !ok {
		t.Fatal("missing tool.call.completed")
	}
	if _, has := evt["input_body"]; has {
		t.Fatalf("input_body present but result was unmarshalable: %v", evt["input_body"])
	}
	if _, has := evt["output_body"]; has {
		t.Fatalf("output_body present but result was unmarshalable: %v", evt["output_body"])
	}
	if evt["status"] != "success" {
		t.Fatalf("status = %v, want success", evt["status"])
	}
}

func TestTrackTool_OptInOnErrorPathStillCapturesArgs(t *testing.T) {
	c, batches, ts := makeClientWithServer(t)
	defer ts.Close()

	ctx := context.Background()
	run := c.StartRun(sensu.StartRunOptions{})
	step := run.StartStep(sensu.StartStepOptions{})
	c.Flush(ctx)
	*batches = nil

	// When fn returns an error, the zero value of T is what we
	// marshal as result. For T=string that's "" → output_body is
	// the JSON literal `""`. status is "error". args are captured.
	// Diverges from sdk-ts (where an undefined-shaped result would
	// skip) but matches sdk-python's None-as-"null" behavior — the
	// Go zero value is a real serializable value.
	_, err := sensu.TrackTool(ctx, step, func() (string, error) {
		return "", fmt.Errorf("tool blew up")
	}, sensu.TrackToolOptions{
		ToolName:      "failing_tool",
		Args:          map[string]string{"query": "a"},
		CaptureBodies: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	c.Flush(ctx)

	evt, ok := findToolCompleted(allEvents(*batches))
	if !ok {
		t.Fatal("missing tool.call.completed")
	}
	if evt["status"] != "error" {
		t.Fatalf("status = %v, want error", evt["status"])
	}
	if evt["input_body"] != `{"query":"a"}` {
		t.Fatalf("input_body = %v", evt["input_body"])
	}
	if evt["output_body"] != `""` {
		t.Fatalf("output_body = %v, want \"\"", evt["output_body"])
	}
}
