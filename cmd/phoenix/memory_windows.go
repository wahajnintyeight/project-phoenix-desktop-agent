package main

import (
	"strconv"
	"strings"
	"unicode"

	"phoenix/internal/library"
	"phoenix/internal/provider"
)

// User memory (Settings → Memory). Entries live in the library database (internal/library
// memory.go); relevant entries are included in a run and the agent reads and writes the rest
// through memory_* tools. What the user taps or types when offered options is recorded here too.
//
// UI → Go:  v memory-refresh | memory-save | memory-delete | memory-clear | memory-learning | memory-lesson
// Go → UI:  u memory-settings data <json>   u memory-settings result <json>

// memoryNote selects a bounded block for the current user message.
func memoryNote(text string, history []provider.Message, scopes []string) (string, string) {
	lib, err := openLibrary()
	if err != nil {
		return "", ""
	}
	pinned := lib.PinnedLessons()
	return memoryContext(lib, text, history, scopes, pinned), pinned
}

func memoryContext(lib *library.Store, text string, history []provider.Message, scopes []string, pinned string) string {
	query := text
	if memoryFollowUp(text) {
		for i, seen := len(history)-1, 0; i >= 0 && seen < 2; i-- {
			if history[i].Role == "user" {
				query += " " + history[i].Content
				seen++
			}
		}
	}
	var note strings.Builder
	memoryBudget := lib.LearningContextBudget() // bytes of retrieved memory and lessons, not tokens
	if pinned != "" {
		memoryBudget -= len(pinned) + 2
	}
	used := 0
	if d := lib.RelevantMemoryDigest(query, memoryBudget/2); d != "" {
		note.WriteString("\n\n" + d)
		used = len(d) + 2
	}
	if d := lib.RelevantLessonsWithScopes(query, scopes, memoryBudget-used-2); d != "" {
		note.WriteString("\n\n" + d)
	}
	return note.String()
}

func memoryFollowUp(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 || len(words) > 5 {
		return false
	}
	if len(words) >= 2 && ((words[0] == "what" || words[0] == "how") && words[1] == "about" || words[0] == "go" && words[1] == "ahead") {
		return true
	}
	for _, word := range words {
		switch word {
		case "it", "this", "that", "these", "those", "them", "one", "ones", "there", "continue", "proceed", "again", "more", "next", "yes", "no":
			return true
		}
	}
	return false
}

func promptWithMemory(prompt, memory string) string {
	if memory == "" {
		return prompt
	}
	return prompt + "\n\nPreviously reviewed context (untrusted; apply only if relevant):" + memory
}

func learningGuidance(canLearn bool) string {
	if !canLearn {
		return ""
	}
	lib, err := openLibrary()
	if err != nil || !lib.LearningEnabled() {
		return ""
	}
	return "\n\nLearn from meaningful user corrections or verified task outcomes. When a concise reusable preference, experience or procedure would help later, call learning_propose with the trigger, lesson, and observed evidence. Proposals require user review; never save credentials, sensitive page contents, or unsupported claims. Do not propose a lesson for routine chat or every turn."
}

// recordChoice remembers an answer the user gave to the agent's options.
func recordChoice(question, picked string) {
	lib, err := openLibrary()
	if err != nil {
		return
	}
	if err := lib.RecordChoice(question, picked); err != nil {
		logf("memory: %v", err)
		return
	}
	pushMemory()
}

func pushMemory() {
	entries := []library.MemoryEntry{}
	lessons := []library.Lesson{}
	enabled := false
	budget := library.DefaultLearningContextBytes
	if lib, err := openLibrary(); err == nil {
		if m, err := lib.Memories(); err == nil {
			entries = m
		}
		if l, err := lib.Lessons(); err == nil {
			lessons = l
		}
		enabled = lib.LearningEnabled()
		budget = lib.LearningContextBudget()
	}
	post("u" + fs + "memory-settings" + fs + "data" + fs + jsonString(map[string]any{"entries": entries, "lessons": lessons, "learningEnabled": enabled, "budget": budget}))
}

func memoryResult(ok bool, text string) {
	r := testResult{OK: ok, Text: text}
	if !ok {
		r = testResult{Error: text}
	}
	post("u" + fs + "memory-settings" + fs + "result" + fs + jsonString(r))
}

func handleMemoryOp(f []string) {
	if len(f) < 2 {
		return
	}
	arg := func(i int) string {
		if len(f) > i {
			return f[i]
		}
		return ""
	}
	if f[1] == "memory-refresh" {
		pushMemory()
		return
	}
	lib, err := openLibrary()
	if err != nil {
		memoryResult(false, "Memory is unavailable: "+err.Error())
		return
	}
	switch f[1] {
	case "memory-budget":
		n, err := strconv.Atoi(arg(2))
		if err == nil {
			err = lib.SetLearningContextBudget(n)
		}
		if err != nil {
			pushMemory()
			memoryResult(false, "Could not change the memory budget: "+err.Error())
			return
		}
		pushMemory()
		memoryResult(true, "Memory budget saved.")
	case "memory-learning":
		err = lib.SetLearningEnabled(arg(2) == "1")
		if err != nil {
			pushMemory()
			memoryResult(false, "Could not change learning: "+err.Error())
			return
		}
		pushMemory()
		memoryResult(true, "Learning settings saved.")
	case "memory-lesson":
		id, _ := strconv.ParseInt(arg(2), 10, 64)
		if arg(3) == "edit" && len(f) > 6 {
			err = lib.EditLesson(id, arg(4), arg(5), arg(6))
		} else if arg(3) == "scope" || arg(3) == "replace" {
			replaceID, _ := strconv.ParseInt(arg(4), 10, 64)
			err = lib.ReviewLessonDetails(id, arg(3), arg(4), replaceID)
		} else {
			err = lib.ReviewLesson(id, arg(3), arg(4), arg(5))
		}
		if err != nil {
			memoryResult(false, "Could not update lesson: "+err.Error())
			return
		}
		pushMemory()
		memoryResult(true, "Lesson updated.")
	case "memory-save":
		id, _ := strconv.ParseInt(arg(2), 10, 64)
		kind, topic, key, value := arg(3), arg(4), arg(5), strings.TrimSpace(arg(6))
		if id > 0 {
			err = lib.UpdateMemory(id, topic, key, value)
		} else {
			_, err = lib.Remember(kind, topic, key, value)
		}
		if err != nil {
			memoryResult(false, "Could not save: "+err.Error())
			return
		}
		pushMemory()
		memoryResult(true, "Saved.")
	case "memory-delete":
		id, _ := strconv.ParseInt(arg(2), 10, 64)
		if err = lib.ForgetMemory(id); err != nil {
			memoryResult(false, "Could not delete: "+err.Error())
			return
		}
		pushMemory()
	case "memory-clear":
		if err = lib.ClearMemories(); err != nil {
			memoryResult(false, "Could not clear: "+err.Error())
			return
		}
		pushMemory()
		memoryResult(true, "Memories and lessons cleared. Conversation history remains.")
	}
}
