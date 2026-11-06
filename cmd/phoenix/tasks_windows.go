package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"phoenix/internal/library"
)

// Tasks: saved automations the agent carries out on its own. A run is an ordinary chat turn in a
// fresh session (so the user watches the same steps, can Stop it, and can read it afterwards),
// with an automation brief that forbids questions and asks for a one-line report. The report is
// stored on the task and shown in the Tasks pane. Scheduled tasks start from a timer on the UI
// thread when the agent is idle; a manual run while the agent is busy waits in the message queue.
//
// UI → Go:  k refresh | save <id|""> <name> <prompt> <kind> <every> <at> | delete <id>
//           k enable <id> <0|1> | run <id> | stop
// Go → UI:  u tasks data <json>   u tasks result <json>

const tasksViewID = "tasks"

// taskRun is the task the agent is working on right now ("" id when none).
var taskRun struct {
	id      string
	name    string
	started time.Time
}

type taskView struct {
	library.Task
	Schedule string `json:"schedule"`
	Next     int64  `json:"next,omitempty"`
}

type tasksData struct {
	Tasks   []taskView `json:"tasks"`
	Running string     `json:"running,omitempty"`
	Waiting string     `json:"waiting,omitempty"` // a manual run waiting for the agent
}

func pushTasks() {
	d := tasksData{Tasks: []taskView{}, Running: taskRun.id}
	if lib, err := openLibrary(); err == nil {
		now := time.Now()
		for _, t := range lib.Tasks() {
			v := taskView{Task: t, Schedule: library.DescribeSchedule(t)}
			if n := t.NextRun(now); !n.IsZero() {
				v.Next = n.Unix()
			}
			d.Tasks = append(d.Tasks, v)
		}
	}
	for _, q := range chatQueue.items {
		if q.task != "" {
			d.Waiting = q.task
		}
	}
	post("u" + fs + tasksViewID + fs + "data" + fs + jsonString(d))
}

func taskResult(ok bool, text string) {
	r := testResult{OK: ok, Text: text}
	if !ok {
		r = testResult{Error: text}
	}
	post("u" + fs + tasksViewID + fs + "result" + fs + jsonString(r))
}

func handleTaskOp(f []string) {
	if len(f) < 2 {
		return
	}
	arg := func(i int) string {
		if len(f) > i {
			return f[i]
		}
		return ""
	}
	if f[1] == "refresh" {
		pushTasks()
		return
	}
	if f[1] == "stop" {
		handleChatOp([]string{"g", "stop"})
		return
	}
	lib, err := openLibrary()
	if err != nil {
		taskResult(false, "Tasks are unavailable: "+err.Error())
		return
	}
	switch f[1] {
	case "save": // save <id> <name> <prompt> <kind> <every> <at>
		every, _ := strconv.Atoi(arg(6))
		t := library.Task{ID: arg(2), Name: arg(3), Prompt: arg(4), Kind: arg(5), Every: every, At: arg(7)}
		scheduled := t.Kind != "" && t.Kind != library.TaskManual
		t.Enabled = scheduled // a new or newly scheduled task starts on
		if old, ok := lib.Task(t.ID); ok && old.Kind != library.TaskManual {
			t.Enabled = scheduled && old.Enabled
		}
		if _, err := lib.SaveTask(t); err != nil {
			taskResult(false, err.Error())
			return
		}
		pushTasks()
		taskResult(true, "Saved.")
	case "delete":
		if arg(2) == taskRun.id && taskRun.id != "" {
			taskResult(false, "That task is running. Stop it first.")
			return
		}
		_ = lib.DeleteTask(arg(2))
		pushTasks()
	case "enable":
		_ = lib.SetTaskEnabled(arg(2), arg(3) == "1")
		pushTasks()
	case "run":
		if t, ok := lib.Task(arg(2)); ok {
			runTask(t)
		}
	}
}

// ── Running ────────────────────────────────────────────────────────────────

// taskBrief is the instruction that makes a run autonomous. Nobody answers during a run, so the
// agent must decide, act, and report; a real blocker ends the run with a stated reason.
func taskBrief(t library.Task) string {
	var b strings.Builder
	b.WriteString("AUTOMATION RUN of the saved task \"" + t.Name + "\". Nobody is watching and nobody will answer: decide and act.\n")
	b.WriteString("Do the whole task with your tools, in the user's own browser and accounts, all the way to the end. ")
	b.WriteString("Use the user's profile and memory; never ask for something you have or can find. Do not ask questions, do not offer options and do not end with a choices block. ")
	b.WriteString("If something truly stops you (a login wall, a CAPTCHA, information you cannot find), stop at once rather than guess.\n")
	if t.LastResult != "" {
		b.WriteString("Last run (" + time.Unix(t.LastRun, 0).Format("2 Jan 15:04") + "): " + t.LastStatus + ": " + t.LastResult + "\nSkip anything that report shows is already done.\n")
	}
	b.WriteString("Your final message is ONE line of at most 200 characters and nothing else: \"DONE: \" followed by the result with its key facts (counts, names, links), or \"BLOCKED: \" followed by the reason.\n\n")
	b.WriteString("TASK:\n" + t.Prompt)
	return b.String()
}

// runTask starts a task now, or queues it behind the current turn.
func runTask(t library.Task) {
	if hub == nil {
		recordTaskEnd(t.ID, time.Now(), "failed", settingsUnavailable(), 0)
		return
	}
	if taskRun.id == t.ID || queuedTask(t.ID) {
		return // already running or waiting
	}
	display := "Task: " + t.Name
	if chat.busy {
		enqueueTask(display, taskBrief(t), t.ID)
		pushTasks()
		return
	}
	beginTask(t, display, taskBrief(t))
}

func queuedTask(id string) bool {
	for _, q := range chatQueue.items {
		if q.task == id {
			return true
		}
	}
	return false
}

// beginTask opens a fresh session for the run and sends the brief.
func beginTask(t library.Task, display, text string) {
	chatQueue.carry = ""
	startNewSession()
	taskRun.id, taskRun.name, taskRun.started = t.ID, t.Name, time.Now()
	pushTasks()
	sendTurn(display, text, false)
	if !chat.busy { // the turn was refused before it started
		finishTask("failed", "The agent could not start.", 0)
	}
}

var choicesFence = regexp.MustCompile("(?s)```choices.*?```")

// taskOutcome reads the agent's closing line: its status and the report.
func taskOutcome(answer string) (status, report string) {
	answer = strings.TrimSpace(choicesFence.ReplaceAllString(answer, ""))
	lines := strings.Split(answer, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(strings.Trim(lines[i], "*_ `"))
		up := strings.ToUpper(l)
		switch {
		case strings.HasPrefix(up, "BLOCKED"):
			return "blocked", strings.TrimSpace(strings.TrimLeft(l[len("BLOCKED"):], ": -*_"))
		case strings.HasPrefix(up, "DONE"):
			return "ok", strings.TrimSpace(strings.TrimLeft(l[len("DONE"):], ": -*_"))
		}
	}
	if answer == "" {
		return "ok", "Finished with no report."
	}
	return "ok", clippedText(strings.Join(strings.Fields(answer), " "), 300)
}

// finishTask records how the current task run ended. It is called wherever a chat turn ends.
func finishTask(status, report string, ms int64) {
	if taskRun.id == "" {
		return
	}
	id, started := taskRun.id, taskRun.started
	taskRun.id, taskRun.name = "", ""
	recordTaskEnd(id, started, status, report, ms)
}

func recordTaskEnd(id string, started time.Time, status, report string, ms int64) {
	if lib, err := openLibrary(); err == nil {
		lib.RecordTaskRun(id, started, status, report, ms)
	}
	pushTasks()
}

// ── Schedule ───────────────────────────────────────────────────────────────

// startTaskScheduler checks the schedules every 15 seconds. The first check waits a little so a
// task missed while Phoenix was closed does not start before the window has settled.
func startTaskScheduler() {
	go func() {
		time.Sleep(40 * time.Second)
		for range time.Tick(15 * time.Second) {
			runLater(checkSchedules)
		}
	}()
}

func checkSchedules() {
	if chat.busy || len(chatQueue.items) > 0 || taskRun.id != "" {
		return
	}
	lib, err := openLibrary()
	if err != nil {
		return
	}
	now := time.Now()
	for _, t := range lib.Tasks() {
		if t.Due(now) {
			logf("task due: %s", t.ID)
			runTask(t)
			return
		}
	}
}
