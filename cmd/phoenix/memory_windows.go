package main

import (
	"strconv"
	"strings"

	"phoenix/internal/library"
)

// User memory (Settings → Memory). Entries live in the library database (internal/library
// memory.go); a digest goes into every run's instructions and the agent reads and writes the rest
// through memory_* tools. What the user taps or types when offered options is recorded here too.
//
// UI → Go:  v memory-refresh | memory-save <id|0> <kind> <topic> <key> <value> | memory-delete <id> | memory-clear
// Go → UI:  u memory-settings data <json>   u memory-settings result <json>

const memoryBudget = 2200 // characters of memory placed in each run's instructions

// memoryNote is the block added to the system prompt, or "" when nothing is remembered.
func memoryNote() string {
	lib, err := openLibrary()
	if err != nil {
		return ""
	}
	d := lib.MemoryDigest(memoryBudget)
	if d == "" {
		return ""
	}
	return "\n\n" + d
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
	if lib, err := openLibrary(); err == nil {
		if m, err := lib.Memories(); err == nil {
			entries = m
		}
	}
	post("u" + fs + "memory-settings" + fs + "data" + fs + jsonString(map[string]any{"entries": entries, "budget": memoryBudget}))
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
		memoryResult(true, "Cleared. The agent remembers nothing about you now.")
	}
}
