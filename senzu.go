// Package senzu provides AI agent observability telemetry for the Senzu platform.
//
// # Quick start
//
//	client := senzu.New(senzu.ClientOptions{FromEnv: true})
//	defer client.Close(ctx)
//
//	err := client.Run(ctx, senzu.StartRunOptions{RunType: "chat"}, func(ctx context.Context, run *senzu.RunHandle) error {
//	    step := run.StartStep(senzu.StartStepOptions{Name: "respond", StepType: "llm"})
//	    defer step.End(ctx)
//
//	    resp, err := senzu.TrackLLM(ctx, step, func() (*anthropic.Message, error) {
//	        return anthropicClient.Messages.New(ctx, params)
//	    }, senzu.TrackLLMOptions{Provider: "anthropic", Model: "claude-sonnet-4-6"})
//	    return err
//	})
//
// # Context propagation
//
// Unlike the TypeScript SDK (which uses AsyncLocalStorage) and the Python SDK
// (which uses contextvars.ContextVar), the Go SDK propagates the active RunHandle
// through context.Context. Any function that receives the ctx passed to the Run()
// callback can call client.GetActiveRun(ctx) to find the active run.
//
// # Generic Track functions
//
// TrackLLM, TrackTool, TrackRetrieval, and TrackEmbedding are package-level generic
// functions because Go's language spec does not allow type parameters on method
// receivers. This applies to all Go versions — upgrading Go will not change this.
//
//	result, err := senzu.TrackTool(ctx, step, func() (MyResult, error) {
//	    return callMyTool()
//	}, senzu.TrackToolOptions{ToolName: "my-tool"})
package senzu

// Version is the current SDK version.
const Version = "0.1.0"

// New is a convenience alias for NewClient.
func New(opts ClientOptions) *SenzuClient {
	return NewClient(opts)
}
