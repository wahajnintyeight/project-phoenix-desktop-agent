package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationHistoryAndSettings(t *testing.T) {
	oldData, oldPath, oldReady := notificationData, notificationPath, ready
	t.Cleanup(func() { notificationData, notificationPath, ready = oldData, oldPath, oldReady })
	notificationPath, ready = "", false
	notificationData = notificationState{Settings: notificationSettings{Enabled: true, Tasks: true, Chat: true, Approvals: true}}
	for i := 0; i < notificationLimit+5; i++ {
		notifyUser("task", "Task finished", "task-id", false)
	}
	if len(notificationData.Items) != notificationLimit || notificationData.Items[0].Read {
		t.Fatalf("history not bounded: %+v", notificationData.Items)
	}
	handleNotificationOp([]string{"j", "read-all"})
	for _, item := range notificationData.Items {
		if !item.Read {
			t.Fatal("entry remained unread")
		}
	}
	settings, _ := json.Marshal(notificationSettings{Enabled: true, Chat: true})
	handleNotificationOp([]string{"j", "settings", string(settings)})
	notifyUser("task", "Task failed", "", false)
	if len(notificationData.Items) != notificationLimit || !notificationData.Items[0].Read {
		t.Fatal("disabled category recorded a notification")
	}
	notifyUser("chat", "Agent response ready", "chat-id", false)
	if notificationData.Items[0].Kind != "chat" || notificationData.Items[0].Read {
		t.Fatal("enabled category did not record unread notification")
	}
	handleNotificationOp([]string{"j", "clear"})
	if len(notificationData.Items) != 0 {
		t.Fatal("history not cleared")
	}
}

func TestNotificationsSurviveRestart(t *testing.T) {
	oldData, oldPath, oldChatPath, oldReady := notificationData, notificationPath, chatStorePath, ready
	t.Cleanup(func() {
		notificationData, notificationPath, chatStorePath, ready = oldData, oldPath, oldChatPath, oldReady
	})
	dir := t.TempDir()
	chatStorePath, notificationPath, ready = filepath.Join(dir, "chats.json"), filepath.Join(dir, "notifications.json"), false
	notificationData = notificationState{Settings: notificationSettings{Enabled: true, Chat: true}}
	notifyUser("chat", "Agent response ready", "chat-id", false)
	wantID := notificationData.Items[0].ID
	flushAgentDataWrites()
	notificationData = notificationState{}
	initNotifications()
	if !notificationData.Settings.Chat || notificationData.Settings.Tasks || len(notificationData.Items) != 1 || notificationData.Items[0].ID != wantID || notificationData.Items[0].Read {
		t.Fatalf("notification state was not restored: %+v", notificationData)
	}
}

func TestNotificationTestExplainsUnavailableService(t *testing.T) {
	oldData, oldAvailable, oldStartupError, oldWin := notificationData, notificationsAvailable, notificationStartupError, win
	t.Cleanup(func() {
		notificationData, notificationsAvailable, notificationStartupError, win = oldData, oldAvailable, oldStartupError, oldWin
	})
	notificationData.Settings.Enabled = true
	notificationsAvailable = false
	notificationStartupError = "test startup failure"
	win = nil

	result := sendTestNotification()
	if result.OK || !strings.Contains(result.Message, notificationStartupError) {
		t.Fatalf("unavailable service result = %+v", result)
	}
}
