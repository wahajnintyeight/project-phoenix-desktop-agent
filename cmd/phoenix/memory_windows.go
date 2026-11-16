package main

import (
	"strconv"
	"strings"

	"phoenix/internal/library"
	"phoenix/internal/provider"
)

// User memory (Settings → Memory). Entries live in the library database (internal/library
// memory.go); relevant entries are included in a run and the agent reads and writes the rest
// through memory_* tools. What the user taps or types when offered options is recorded here too.
//
// UI → Go:  v memory-refresh | memory-save | memory-delete | memory-clear | memory-learning | memory-lesson
// Go → UI:  u memory-settings data <json>   u memory-settings result <json>

const memoryBudget = 2200 // characters of memory placed in each run's instructions

// memoryNote is the block added to the system prompt, or "" when nothing is remembered.
func memoryNote(text string, history []provider.Message, canLearn bool) string {
	lib, err := openLibrary()
	if err != nil {
		return ""
	}
	query := text
	if len(strings.Fields(text)) <= 5 {
		for i, seen := len(history)-1, 0; i >= 0 && seen < 2; i-- {
			if history[i].Role == "user" {
				query += " " + history[i].Content
				seen++
			}
		}
	}
	var note strings.Builder
	if d := lib.RelevantMemoryDigest(query, memoryBudget); d != "" {
		note.WriteString("\n\n" + d)
	}
	if d := lib.RelevantLessons(query, 1400); d != "" {
		note.WriteString("\n\n" + d)
	}
	if canLearn && lib.LearningEnabled() {
		note.WriteString("\n\nLearn from meaningful user corrections or verified task outcomes. When a concise reusable preference, experience or procedure would help later, call learning_propose with the trigger, lesson, and observed evidence. Proposals require user review; never save credentials, sensitive page contents, or unsupported claims. Do not propose a lesson for routine chat or every turn.")
	}
	return note.String()
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
	if lib, err := openLibrary(); err == nil {
		if m, err := lib.Memories(); err == nil {
			entries = m
		}
		if l, err := lib.Lessons(); err == nil {
			lessons = l
		}
		enabled = lib.LearningEnabled()
	}
	post("u" + fs + "memory-settings" + fs + "data" + fs + jsonString(map[string]any{"entries": entries, "lessons": lessons, "learningEnabled": enabled, "budget": memoryBudget}))
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
		if err = lib.ReviewLesson(id, arg(3), arg(4), arg(5)); err != nil {
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
