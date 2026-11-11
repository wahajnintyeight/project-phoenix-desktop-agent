package main

import (
	"context"
	"strings"

	"phoenix/internal/provider"
)

type goalCall func(context.Context, []provider.Message, string, func(provider.Event)) (provider.Result, error)

// runGoalLoop continues ordinary provider turns until the model reports completion or a blocker.
func runGoalLoop(ctx context.Context, history []provider.Message, prompt string, call goalCall, onEvent func(provider.Event), onContinue func()) (provider.Result, error) {
	runHistory := append([]provider.Message(nil), history...)
	previousAnswer := ""
	for {
		usedTool, denied := false, false
		result, err := call(ctx, runHistory, prompt, func(ev provider.Event) {
			if ev.Kind == provider.EvToolUse {
				usedTool = true
			}
			if ev.Kind == provider.EvToolResult && ev.IsError && strings.Contains(strings.ToLower(ev.Text), "declined") {
				denied = true
			}
			if onEvent != nil {
				onEvent(ev)
			}
		})
		if err != nil || result.IsError {
			return result, err
		}
		if denied {
			result.Text = "GOAL_BLOCKED: The user declined a required action."
			return result, nil
		}
		if _, _, complete := goalOutcome(result.Text); complete {
			return result, nil
		}
		answer := strings.TrimSpace(result.Text)
		if !usedTool && answer == previousAnswer {
			result.Text = "GOAL_BLOCKED: The agent repeated its previous response without a new action; inspect the goal and adjust its instructions or available tools."
			return result, nil
		}
		previousAnswer = answer
		runHistory = append(runHistory, provider.Message{Role: "user", Content: prompt}, provider.Message{Role: "assistant", Content: result.Text})
		runHistory = trimHistory(runHistory)
		prompt = "Continue working toward the goal. Reassess what remains, take the next useful action, and verify it. End only with GOAL_DONE: or GOAL_BLOCKED: when appropriate."
		if onContinue != nil {
			onContinue()
		}
	}
}
