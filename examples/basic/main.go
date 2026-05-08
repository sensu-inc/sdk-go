// Package main demonstrates the Senzu Go SDK with the Anthropic integration.
//
// Set these environment variables before running:
//
//	SENZU_API_KEY=snz_...
//	SENZU_AGENT_ID=my-agent
//	ANTHROPIC_API_KEY=sk-ant-...
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/senzu-ai/sdk-go"
)

func main() {
	ctx := context.Background()

	// Create client — reads SENZU_API_KEY, SENZU_AGENT_ID, SENZU_BASE_URL from env.
	client := senzu.New(senzu.ClientOptions{
		FromEnv:   true,
		DebugMode: true,
	})
	defer client.Close(ctx)

	// --- High-level: context-propagating Run() wrapper ---
	err := client.Run(ctx, senzu.StartRunOptions{RunType: "demo"}, func(ctx context.Context, run *senzu.RunHandle) error {
		step := run.StartStep(senzu.StartStepOptions{Name: "answer", StepType: "tool"})
		defer step.End(ctx)

		// Wrap any arbitrary work in TrackTool
		answer, err := senzu.TrackTool(ctx, step, func() (string, error) {
			return "42", nil
		}, senzu.TrackToolOptions{ToolName: "calculator"})
		if err != nil {
			return err
		}
		fmt.Println("Answer:", answer)

		// Record feedback on the run
		run.RecordFeedback(senzu.RecordFeedbackOptions{
			Type:  "thumbs_up",
			Score: ptr(1.0),
		})

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// --- Low-level: manual run management (e.g., for serverless handlers) ---
	run := client.StartRun(senzu.StartRunOptions{RunType: "batch"})
	step := run.StartStep(senzu.StartStepOptions{Name: "process"})
	step.End(ctx)
	run.End(ctx, "completed")

	fmt.Println("Done. Events flushed.")
}

func ptr(f float64) *float64 { return &f }
