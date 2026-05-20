package sensu

import (
	"context"
)

// Context-bound tracking shortcuts — SDK_CONSOLIDATION_PLAN.md Phase 2 PR 1.
//
// The existing free-function generics (TrackTool, TrackRetrieval,
// TrackEmbedding, TrackGuardrail in step.go) require the caller to manage
// the StepHandle lifecycle manually:
//
//   step := run.StartStep(sensu.StartStepOptions{Name: "search", StepType: "tool"})
//   defer step.End(ctx)
//   result, err := sensu.TrackTool(ctx, step, fn, opts)
//
// The TypeScript and Python SDKs expose client-level shortcuts that
// auto-resolve the active run and auto-manage the step (see SensuClient.
// trackTool / client.track_tool in those SDKs). Go can't put type
// parameters on methods, so the equivalent here is free functions
// that take a ctx and pull the active RunHandle from it (via
// RunFromContext) — same convention as the existing Track*[T] helpers.
//
// The "Ctx" suffix tells the reader "this assumes ctx-injected run."
//
// Usage:
//
//   err := client.Run(ctx, sensu.StartRunOptions{}, func(ctx context.Context, _ *sensu.RunHandle) error {
//       result, err := sensu.TrackToolCtx(ctx, func() (string, error) {
//           return mySearchTool(query)
//       }, sensu.TrackToolOptions{ToolName: "search"})
//       _ = result
//       return err
//   })
//
// Fall-through behavior: if no active run is in ctx (caller forgot
// client.Run() or is using a no-run pattern), the wrapper just calls fn
// directly with no telemetry — matches the no-run behavior in the
// existing Track* helpers and the trinity SDKs.

// TrackToolCtx wraps TrackTool with auto-step lifecycle. See file header
// for the design rationale.
func TrackToolCtx[T any](
	ctx context.Context,
	fn func() (T, error),
	opts TrackToolOptions,
) (T, error) {
	run := RunFromContext(ctx)
	if run == nil {
		return fn()
	}
	name := opts.ToolName
	if name == "" {
		name = "tool"
	}
	step := run.StartStep(StartStepOptions{Name: name, StepType: "tool"})
	defer step.End(ctx)
	return TrackTool(ctx, step, fn, opts)
}

// TrackRetrievalCtx wraps TrackRetrieval with auto-step lifecycle.
func TrackRetrievalCtx[T any](
	ctx context.Context,
	fn func() (T, error),
	opts TrackRetrievalOptions,
) (T, error) {
	run := RunFromContext(ctx)
	if run == nil {
		return fn()
	}
	step := run.StartStep(StartStepOptions{Name: "retrieval", StepType: "retrieval"})
	defer step.End(ctx)
	return TrackRetrieval(ctx, step, fn, opts)
}

// TrackEmbeddingCtx wraps TrackEmbedding with auto-step lifecycle.
func TrackEmbeddingCtx[T any](
	ctx context.Context,
	fn func() (T, error),
	opts TrackEmbeddingOptions,
) (T, error) {
	run := RunFromContext(ctx)
	if run == nil {
		return fn()
	}
	step := run.StartStep(StartStepOptions{Name: "embedding", StepType: "embedding"})
	defer step.End(ctx)
	return TrackEmbedding(ctx, step, fn, opts)
}

// TrackGuardrailCtx wraps TrackGuardrail with auto-step lifecycle.
// Guardrail isn't generic in the underlying signature — the result type
// is the fixed pass/fail/modified literal — so this wrapper isn't either.
func TrackGuardrailCtx(
	ctx context.Context,
	fn func() (string, error),
	opts TrackGuardrailOptions,
) (string, error) {
	run := RunFromContext(ctx)
	if run == nil {
		return fn()
	}
	name := opts.GuardrailID
	if name == "" {
		name = "guardrail"
	}
	step := run.StartStep(StartStepOptions{Name: name, StepType: "guardrail"})
	defer step.End(ctx)
	return TrackGuardrail(ctx, step, fn, opts)
}
