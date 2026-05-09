// Package main demonstrates the Sensu Go SDK with the Anthropic integration.
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

	"github.com/sensu-inc/sdk-go"
)

func main() {
	ctx := context.Background()

	// Create client — reads SENZU_API_KEY, SENZU_AGENT_ID, SENZU_BASE_URL from env.
	client := sensu.New(sensu.ClientOptions{
		FromEnv:   true,
		DebugMode: true,
	})
	defer client.Close(ctx)

	// --- High-level: context-propagating Run() wrapper ---
	err := client.Run(ctx, sensu.StartRunOptions{RunType: "demo"}, func(ctx context.Context, run *sensu.RunHandle) error {
		step := run.StartStep(sensu.StartStepOptions{Name: "answer", StepType: "tool"})
		defer step.End(ctx)

		// Wrap any arbitrary work in TrackTool
		answer, err := sensu.TrackTool(ctx, step, func() (string, error) {
			return "42", nil
		}, sensu.TrackToolOptions{ToolName: "calculator"})
		if err != nil {
			return err
		}
		fmt.Println("Answer:", answer)

		// Record feedback on the run
		run.RecordFeedback(sensu.RecordFeedbackOptions{
			Type:  "thumbs_up",
			Score: ptr(1.0),
		})

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// --- Low-level: manual run management (e.g., for serverless handlers) ---
	run := client.StartRun(sensu.StartRunOptions{RunType: "batch"})
	step := run.StartStep(sensu.StartStepOptions{Name: "process"})
	step.End(ctx)
	run.End(ctx, "completed")

	fmt.Println("Done. Events flushed.")
}

func ptr(f float64) *float64 { return &f }
