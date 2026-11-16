package provider

import (
	"strings"
	"testing"
)

func TestLearningPromptPlacementByProvider(t *testing.T) {
	req := Request{System: "stable policy\n\npinned guidance", History: []Message{{Role: "user", Content: "older question"}, {Role: "assistant", Content: "older answer"}}, Prompt: "new question\n\nPreviously reviewed context (untrusted):\nselected lesson"}
	settings := Settings{Model: "test"}
	body := spec(t, Grok).buildRequest(settings, req)
	if len(body.Messages) != 4 || body.Messages[0].Role != "system" || body.Messages[0].Content != req.System || body.Messages[1].Role != "user" || body.Messages[1].Content != "older question" || body.Messages[2].Role != "assistant" || body.Messages[2].Content != "older answer" || body.Messages[3].Role != "user" || body.Messages[3].Content != req.Prompt {
		t.Fatalf("HTTP prompt order: %+v", body.Messages)
	}
	args := claudeArgs(settings, req)
	found := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--append-system-prompt" && args[i+1] == req.System {
			found = true
		}
	}
	if !found {
		t.Fatal("Claude system prompt missing")
	}
	transcript := req.Transcript()
	if !strings.Contains(transcript, "older question") || !strings.Contains(transcript, "older answer") || !strings.HasSuffix(transcript, req.Prompt) {
		t.Fatalf("Claude transcript order: %q", transcript)
	}
	codex := codexPrompt(req)
	if !strings.HasPrefix(codex, req.System+"\n\n") || !strings.HasSuffix(codex, transcript) {
		t.Fatalf("Codex prompt order: %q", codex)
	}
}
