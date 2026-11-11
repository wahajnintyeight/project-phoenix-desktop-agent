package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSavedMessageTimestamps(t *testing.T) {
	oldChats, oldPath, oldReady := savedChats, chatStorePath, ready
	t.Cleanup(func() { savedChats, chatStorePath, ready = oldChats, oldPath, oldReady })
	savedChats, chatStorePath, ready = nil, "", false
	before := time.Now().UnixMilli()
	id, index, userAt := saveUserMessage("", "Hello", "Hello")
	answerAt := commitSavedTurn(id, index, "Hi", "Provider", "Model")
	c := findSavedChat(id)
	if c == nil || len(c.Messages) != 2 || userAt < before || c.Messages[0].At != userAt || answerAt < userAt || c.Messages[1].At != answerAt {
		t.Fatalf("timestamps not saved: user=%d answer=%d chat=%+v", userAt, answerAt, c)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var restored savedChat
	if err := json.Unmarshal(data, &restored); err != nil || restored.Messages[0].At != userAt || restored.Messages[1].At != answerAt {
		t.Fatalf("timestamps not restored: %+v, %v", restored.Messages, err)
	}
	var legacy chatTranscriptMessage
	if err := json.Unmarshal([]byte(`{"role":"user","content":"old"}`), &legacy); err != nil || legacy.At != 0 {
		t.Fatalf("legacy message: %+v, %v", legacy, err)
	}
}

func TestInterruptedRunCheckpointAndTaskGuard(t *testing.T) {
	oldChats, oldPath, oldReady := savedChats, chatStorePath, ready
	t.Cleanup(func() { savedChats, chatStorePath, ready = oldChats, oldPath, oldReady })
	savedChats, chatStorePath, ready = nil, "", false
	id, _, _ := saveUserMessage("", "Run task", "The original task")
	setPendingChatRun(id, "The original task", "task-1")
	checkpointChatTool(id, "Browser · click")
	c := findSavedChat(id)
	if c.Pending == nil || c.Pending.Request != "The original task" || c.Pending.LastTool != "Browser · click" || interruptedTaskChat("task-1") != id {
		t.Fatalf("pending run not preserved: %+v", c.Pending)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var loaded savedChat
	if err := json.Unmarshal(data, &loaded); err != nil || !cleanSavedChat(&loaded) || loaded.Pending == nil || loaded.Pending.TaskID != "task-1" {
		t.Fatalf("checkpoint did not survive reload: %+v, %v", loaded.Pending, err)
	}
	clearInterruptedTask("task-1")
	if c.Pending != nil || interruptedTaskChat("task-1") != "" {
		t.Fatal("manual rerun did not clear interruption")
	}
}
