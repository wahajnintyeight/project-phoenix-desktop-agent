package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"phoenix/internal/provider"
)

func turns(n int, size int) []provider.Message {
	var h []provider.Message
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		h = append(h, provider.Message{Role: role, Content: strings.Repeat("x", size)})
	}
	return h
}

func TestTrimHistoryKeepsShortConversations(t *testing.T) {
	h := turns(4, 10)
	got := trimHistory(h)
	if len(got) != 4 {
		t.Fatalf("kept %d of 4", len(got))
	}
	got[0].Content = "changed"
	if h[0].Content == "changed" {
		t.Fatal("trimHistory must return a copy")
	}
}

func TestGoalLoopContinuesAfterEmptySearch(t *testing.T) {
	for _, final := range []string{
		"GOAL_BLOCKED: No additional useful sources are available.",
		"GOAL_DONE: Found and verified a suitable role at Acme.",
	} {
		calls, continuations := 0, 0
		result, err := runGoalLoop(context.Background(), nil, "Search for a suitable role", func(_ context.Context, history []provider.Message, prompt string, emit func(provider.Event)) (provider.Result, error) {
			calls++
			if calls == 1 {
				emit(provider.Event{Kind: provider.EvToolUse, ToolName: "jobs_search"})
				emit(provider.Event{Kind: provider.EvToolResult, Text: "No eligible postings found."})
				return provider.Result{Text: "No eligible postings found in the first source."}, nil
			}
			if len(history) != 2 || !strings.Contains(history[1].Content, "No eligible postings") {
				t.Fatalf("second call did not receive first search result: %#v", history)
			}
			if !strings.Contains(prompt, "Continue working toward the goal") {
				t.Fatalf("second call prompt did not request continued work: %q", prompt)
			}
			if strings.HasPrefix(final, "GOAL_DONE") {
				emit(provider.Event{Kind: provider.EvToolUse, ToolName: "jobs_search"})
				emit(provider.Event{Kind: provider.EvToolResult, Text: "Verified matching role at Acme."})
			}
			return provider.Result{Text: final}, nil
		}, nil, func() { continuations++ })
		if err != nil || calls != 2 || continuations != 1 || result.Text != final {
			t.Fatalf("final=%q calls=%d continuations=%d err=%v", result.Text, calls, continuations, err)
		}
	}
}

func TestGoalLoopStopsOnDeniedAction(t *testing.T) {
	calls := 0
	result, err := runGoalLoop(context.Background(), nil, "Find and apply", func(_ context.Context, _ []provider.Message, _ string, emit func(provider.Event)) (provider.Result, error) {
		calls++
		emit(provider.Event{Kind: provider.EvToolUse, ToolName: "apply"})
		emit(provider.Event{Kind: provider.EvToolResult, IsError: true, Text: "User declined this action."})
		return provider.Result{Text: "Continuing."}, nil
	}, nil, nil)
	if err != nil || calls != 1 || !strings.HasPrefix(result.Text, "GOAL_BLOCKED:") {
		t.Fatalf("result=%q calls=%d err=%v", result.Text, calls, err)
	}
}

func TestTrimHistoryLimitsCountAndStartsOnUser(t *testing.T) {
	got := trimHistory(turns(chatMaxHistory+5, 10))
	if len(got) > chatMaxHistory {
		t.Fatalf("kept %d, limit %d", len(got), chatMaxHistory)
	}
	if len(got) == 0 || got[0].Role != "user" {
		t.Fatalf("history must start on a user turn, got %+v", got[:1])
	}
}

func TestTrimHistoryLimitsCharacters(t *testing.T) {
	got := trimHistory(turns(10, chatMaxChars/4))
	total := 0
	for _, m := range got {
		total += len(m.Content)
	}
	if total > chatMaxChars {
		t.Fatalf("kept %d characters, limit %d", total, chatMaxChars)
	}
	if len(got) > 0 && got[0].Role != "user" {
		t.Fatal("must start on a user turn")
	}
}

func TestTrimHistoryEmpty(t *testing.T) {
	if got := trimHistory(nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestFriendlyChatError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("wrapped: %w", provider.ErrNotReady), "Open Settings"},
		{provider.ErrNotInstalled, "not installed"},
		{context.DeadlineExceeded, "too long"},
		{errors.New("choose a model first"), "no model chosen"},
		{errors.New("HTTP 401: Incorrect API key"), "HTTP 401: Incorrect API key"},
	}
	for _, c := range cases {
		if got := friendlyChatError("openrouter", c.err); !strings.Contains(got, c.want) {
			t.Errorf("%v -> %q, want it to contain %q", c.err, got, c.want)
		}
	}
	if got := friendlyChatError("openrouter", provider.ErrNotReady); !strings.HasPrefix(got, "OpenRouter") {
		t.Errorf("should name the provider: %q", got)
	}
}

func TestTrimHistoryCondensesDroppedTurns(t *testing.T) {
	h := turns(chatMaxHistory+6, 10)
	h[0].Content = "find me golang jobs in Berlin"
	got := trimHistory(h)
	if len(got) == 0 || got[0].Role != "user" {
		t.Fatal("must start on a user turn")
	}
	if !strings.Contains(got[0].Content, "condensed automatically") || !strings.Contains(got[0].Content, "golang jobs in Berlin") {
		t.Fatalf("older turns were dropped without a digest: %q", got[0].Content)
	}
	total := 0
	for _, m := range got {
		total += len(m.Content)
	}
	if total > chatMaxChars {
		t.Fatalf("history with digest is %d characters, limit %d", total, chatMaxChars)
	}
	if got := trimHistory(turns(4, 10)); strings.Contains(got[0].Content, "condensed") {
		t.Fatal("a short conversation must not get a digest")
	}
}
