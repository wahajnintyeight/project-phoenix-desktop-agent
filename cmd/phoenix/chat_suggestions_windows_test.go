package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSuggestionOptions(t *testing.T) {
	fallback := []string{"Tell me more", "What should I do next?"}
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{`["Search remote roles", "Review the closest match"]`, []string{"Search remote roles", "Review the closest match"}},
		{"```json\n[\"Try again\", \"Change filters\"]\n```", []string{"Try again", "Change filters"}},
		{`["same", "same"]`, fallback},
		{`["only one"]`, fallback},
		{"not json", fallback},
	} {
		if got := suggestionOptions(tc.input); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %v, want %v", tc.input, got, tc.want)
		}
	}
	got := suggestionOptions(`["` + strings.Repeat("a", 120) + `", "Other"]`)
	if len([]rune(got[0])) != 80 {
		t.Fatalf("option was not bounded: %q", got[0])
	}
}

func TestSuggestionRequestIsBoundedAndToolFree(t *testing.T) {
	req := suggestionRequest(strings.Repeat("a", 5000), strings.Repeat("b", 8000))
	if len(req.Prompt) > 5100 || req.MaxOutputTokens != 96 || req.Effort != "low" || req.UseMCP || len(req.History) != 0 || len(req.Tools) != 0 {
		t.Fatalf("unexpected suggestion request: %+v", req)
	}
}

func TestSuggestionCurrentRejectsStaleResults(t *testing.T) {
	oldChat, oldSuggestions := chat, chatSuggestions
	t.Cleanup(func() { chat, chatSuggestions = oldChat, oldSuggestions })
	chat.gen, chat.currentID, chat.pendingMessageIndex, chat.busy = 3, "session", 7, false
	chatSuggestions.sequence = 9
	if !suggestionCurrent(9, 3, "session", 7) {
		t.Fatal("current result rejected")
	}
	if suggestionCurrent(8, 3, "session", 7) || suggestionCurrent(9, 2, "session", 7) || suggestionCurrent(9, 3, "other", 7) || suggestionCurrent(9, 3, "session", 6) {
		t.Fatal("stale result accepted")
	}
	chat.busy = true
	if suggestionCurrent(9, 3, "session", 7) {
		t.Fatal("result accepted during a new turn")
	}
}

func TestSavedSuggestionsSurviveReload(t *testing.T) {
	oldChats, oldPath, oldReady := savedChats, chatStorePath, ready
	t.Cleanup(func() { savedChats, chatStorePath, ready = oldChats, oldPath, oldReady })
	savedChats, chatStorePath, ready = nil, "", false
	id, index, _ := saveUserMessage("", "Find roles", "Find roles")
	commitSavedTurn(id, index, "No matches", "Provider", "Model")
	c := findSavedChat(id)
	c.Messages[len(c.Messages)-1].Suggestions = []string{"Search remote roles", "Review closest match"}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var restored savedChat
	if err := json.Unmarshal(data, &restored); err != nil || !cleanSavedChat(&restored) {
		t.Fatalf("restore: %v", err)
	}
	if got := restored.Messages[len(restored.Messages)-1].Suggestions; !reflect.DeepEqual(got, []string{"Search remote roles", "Review closest match"}) {
		t.Fatalf("suggestions lost: %v", got)
	}
}
