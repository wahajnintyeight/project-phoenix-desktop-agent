// Command agenteval evaluates an HTTP model against synthetic browser fixtures.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"phoenix/internal/agenteval"
	"phoenix/internal/provider"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	id := os.Getenv("PHOENIX_EVAL_PROVIDER")
	if id == "" {
		id = string(provider.OpenRouter)
	}
	model, key := os.Getenv("PHOENIX_EVAL_MODEL"), os.Getenv("PHOENIX_EVAL_API_KEY")
	if model == "" || key == "" {
		return fmt.Errorf("set PHOENIX_EVAL_MODEL and PHOENIX_EVAL_API_KEY")
	}
	spec, ok := provider.SpecFor(provider.ID(id))
	if !ok {
		return fmt.Errorf("unknown HTTP provider %q", id)
	}
	settings := provider.Settings{Mode: provider.AuthAPIKey, APIKey: key, Model: model, BaseURL: os.Getenv("PHOENIX_EVAL_BASE_URL")}
	p := provider.OpenAICompat{Spec: spec}
	encoder := json.NewEncoder(os.Stdout)
	failed := false
	for _, scenario := range agenteval.Scenarios() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		result := agenteval.Run(ctx, p, settings, scenario)
		cancel()
		if err := encoder.Encode(result); err != nil {
			return err
		}
		failed = failed || !result.Passed
	}
	if failed {
		return fmt.Errorf("one or more scenarios failed; see the JSON results")
	}
	return nil
}
