// Package sensu provides AI agent observability telemetry for the Sensu platform.
//
// # Quick start
//
//	client := sensu.New(sensu.ClientOptions{FromEnv: true})
//	defer client.Close(ctx)
//
//	err := client.Run(ctx, sensu.StartRunOptions{RunType: "chat"}, func(ctx context.Context, run *sensu.RunHandle) error {
//	    step := run.StartStep(sensu.StartStepOptions{Name: "respond", StepType: "llm"})
//	    defer step.End(ctx)
//
//	    resp, err := sensu.TrackLLM(ctx, step, func() (*anthropic.Message, error) {
//	        return anthropicClient.Messages.New(ctx, params)
//	    }, sensu.TrackLLMOptions{Provider: "anthropic", Model: "claude-sonnet-4-6"})
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
//	result, err := sensu.TrackTool(ctx, step, func() (MyResult, error) {
//	    return callMyTool()
//	}, sensu.TrackToolOptions{ToolName: "my-tool"})
package sensu

// Version is the current SDK version.
const Version = "0.6.0"

// New is a convenience alias for NewClient.
func New(opts ClientOptions) *SensuClient {
	return NewClient(opts)
}
