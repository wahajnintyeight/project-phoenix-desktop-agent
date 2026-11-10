package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

const notificationLimit = 200

type notificationSettings struct {
	Enabled   bool `json:"enabled"`
	Tasks     bool `json:"tasks"`
	Chat      bool `json:"chat"`
	Approvals bool `json:"approvals"`
}

type notificationEntry struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	At     int64  `json:"at"`
	Read   bool   `json:"read"`
	Target string `json:"target,omitempty"`
}

type notificationState struct {
	Settings notificationSettings `json:"settings"`
	Items    []notificationEntry  `json:"items"`
}

var notificationData = notificationState{Settings: notificationSettings{Enabled: true, Tasks: true, Chat: true, Approvals: true}, Items: []notificationEntry{}}
var notificationPath string
var nativeNotifications = notifications.New()
var notificationsAvailable bool
var notificationStartupError string
var pendingNotification string
var notificationWorkerMu sync.RWMutex
var notificationWorker chan notificationRequest
var notificationWorkerDone chan struct{}

type notificationRequest struct {
	options notifications.NotificationOptions
	result  chan error
}

type notificationLifecycle struct{}

func (*notificationLifecycle) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
	if err := nativeNotifications.ServiceStartup(ctx, opts); err != nil {
		notificationStartupError = err.Error()
		logf("notification startup: %v", err)
		return nil
	}
	notificationsAvailable = true
	notificationStartupError = ""
	startNotificationWorker()
	return nil
}

func (*notificationLifecycle) ServiceShutdown() error {
	if notificationsAvailable {
		notificationsAvailable = false
		stopNotificationWorker()
		return nativeNotifications.ServiceShutdown()
	}
	return nil
}

func initNotifications() {
	if chatStorePath != "" {
		notificationPath = filepath.Join(filepath.Dir(chatStorePath), "notifications.json")
		info, err := os.Stat(notificationPath)
		if err == nil && info.Size() <= 256<<10 {
			data, ok := readAgentData(notificationPath)
			var saved notificationState
			if ok && json.Unmarshal(data, &saved) == nil {
				notificationData = saved
				if len(notificationData.Items) > notificationLimit {
					notificationData.Items = notificationData.Items[:notificationLimit]
				}
			}
		}
	}
	if notificationData.Items == nil {
		notificationData.Items = []notificationEntry{}
	}
}

func saveNotifications() {
	if notificationPath == "" {
		return
	}
	snapshot := notificationData
	snapshot.Items = append([]notificationEntry(nil), notificationData.Items...)
	queueAgentDataWrite(agentDataWrite{path: notificationPath, value: snapshot})
}

func pushNotifications() {
	if ready {
		post("u" + fs + "notifications" + fs + "data" + fs + jsonString(notificationData))
	}
}

func notifyUser(kind, title, target string, alert bool) {
	s := notificationData.Settings
	if !s.Enabled || (kind == "task" && !s.Tasks) || (kind == "chat" && !s.Chat) || (kind == "approval" && !s.Approvals) {
		return
	}
	entry := notificationEntry{ID: newSavedChat().ID, Kind: kind, Title: title, At: time.Now().UnixMilli(), Target: target}
	notificationData.Items = append([]notificationEntry{entry}, notificationData.Items...)
	if len(notificationData.Items) > notificationLimit {
		notificationData.Items = notificationData.Items[:notificationLimit]
	}
	saveNotifications()
	pushNotifications()
	if alert && win != nil && !win.IsFocused() {
		if !notificationsAvailable {
			win.Flash(true)
		} else if err := sendNativeNotification(notifications.NotificationOptions{ID: entry.ID, Title: "Phoenix", Body: title}); err != nil {
			logf("notification: %v", err)
			win.Flash(true)
		}
	}
}

func handleNotificationOp(f []string) {
	if len(f) < 2 {
		return
	}
	switch f[1] {
	case "test":
		result := sendTestNotification()
		post("u" + fs + "notifications" + fs + "result" + fs + jsonString(result))
		return
	case "settings":
		if len(f) < 3 {
			return
		}
		var s notificationSettings
		if json.Unmarshal([]byte(f[2]), &s) != nil {
			return
		}
		notificationData.Settings = s
	case "read-all":
		for i := range notificationData.Items {
			notificationData.Items[i].Read = true
		}
	case "clear":
		notificationData.Items = []notificationEntry{}
	case "open":
		if len(f) < 3 {
			return
		}
		openNotification(f[2])
		return
	default:
		return
	}
	saveNotifications()
	pushNotifications()
}

type notificationTestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func sendTestNotification() notificationTestResult {
	if !notificationData.Settings.Enabled {
		return notificationTestResult{Message: "Enable notifications to send a test."}
	}
	if !notificationsAvailable {
		if win != nil {
			win.Flash(true)
		}
		message := "Windows notifications are unavailable."
		if notificationStartupError != "" {
			message += " " + notificationStartupError
		}
		return notificationTestResult{Message: message}
	}
	if err := sendNativeNotification(notifications.NotificationOptions{
		ID: newSavedChat().ID, Title: "Phoenix", Body: "Test notification. Windows notifications are connected.",
	}); err != nil {
		logf("test notification: %v", err)
		if win != nil {
			win.Flash(true)
		}
		return notificationTestResult{Message: "Windows could not send the test notification: " + err.Error()}
	}
	return notificationTestResult{OK: true, Message: "Sent to Windows. If no toast appeared, check Windows notification settings for Phoenix."}
}

func startNotificationWorker() {
	requests, done := make(chan notificationRequest), make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer close(done)
		for request := range requests {
			request.result <- nativeNotifications.SendNotification(request.options)
		}
	}()
	notificationWorkerMu.Lock()
	notificationWorker, notificationWorkerDone = requests, done
	notificationWorkerMu.Unlock()
}

func stopNotificationWorker() {
	notificationWorkerMu.Lock()
	requests, done := notificationWorker, notificationWorkerDone
	notificationWorker, notificationWorkerDone = nil, nil
	if requests != nil {
		close(requests)
	}
	notificationWorkerMu.Unlock()
	if done != nil {
		<-done
	}
}

func sendNativeNotification(options notifications.NotificationOptions) error {
	notificationWorkerMu.RLock()
	requests := notificationWorker
	if requests == nil {
		notificationWorkerMu.RUnlock()
		return fmt.Errorf("notification worker is unavailable")
	}
	result := make(chan error, 1)
	requests <- notificationRequest{options: options, result: result}
	notificationWorkerMu.RUnlock()
	return <-result
}

func openNotification(id string) {
	if !ready {
		pendingNotification = id
		return
	}
	for i := range notificationData.Items {
		n := &notificationData.Items[i]
		if n.ID != id {
			continue
		}
		if n.Kind != "task" && n.Target != "" && chat.busy && n.Target != chat.currentID {
			showWindow()
			post("n" + fs + "busy")
			return
		}
		n.Read = true
		showWindow()
		if n.Kind == "task" {
			openTasks()
			post("n" + fs + "task" + fs + n.Target)
		} else {
			if n.Target != "" && !chat.busy {
				if c := findSavedChat(n.Target); c != nil {
					activateSavedChat(c)
				}
			}
			post("n" + fs + "chat")
		}
		saveNotifications()
		pushNotifications()
		return
	}
}
