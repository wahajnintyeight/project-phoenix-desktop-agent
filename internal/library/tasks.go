package library

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tasks: saved automations. A task is an instruction the agent carries out on its own, on demand
// or on a schedule, and reports back as one line. They live in the library's settings next to the
// profile; the agent can list and create them with tasks_* tools, the Tasks pane shows them.

const tasksKey = "tasks.json"
const maxTasks = 100

// Task instruction limits keep recurring prompts predictable in both storage and model context.
const (
	MaxTaskPromptRunes = 4000
	MaxTaskExtraRunes  = 2000
	MaxTaskTextTokens  = 1500 // rough estimate; model tokenizers differ
)

// Schedule kinds.
const (
	TaskManual   = "manual"   // only when the user presses Run
	TaskInterval = "interval" // every Every minutes
	TaskDaily    = "daily"    // every day at At
	TaskWeekdays = "weekdays" // Monday to Friday at At
)

// Task is one automation.
type Task struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
	Kind   string `json:"kind"`
	Every  int    `json:"every,omitempty"` // minutes, for interval
	At     string `json:"at,omitempty"`    // HH:MM local time, for daily and weekdays
	Icon   string `json:"icon,omitempty"`  // an emoji for the card
	Extra  string `json:"extra,omitempty"` // priority rules: constraints that govern the task goal
	Goal   bool   `json:"goal,omitempty"`  // keep running until the agent verifies completion or a blocker
	// Limit true restricts a run to the Tools listed ("browser", "files", "desktop" and
	// "mcp:<server>"); false gives it everything the user has switched on.
	Limit   bool     `json:"limit,omitempty"`
	Tools   []string `json:"tools,omitempty"`
	Notify  bool     `json:"notify,omitempty"` // flash the taskbar when a run ends
	Enabled bool     `json:"enabled"`          // a disabled task never runs by itself
	Created int64    `json:"created"`          // when the task or its schedule was set

	LastRun    int64     `json:"lastRun,omitempty"`
	LastStatus string    `json:"lastStatus,omitempty"` // ok | blocked | failed | stopped
	LastResult string    `json:"lastResult,omitempty"` // the agent's one-line report
	LastMs     int64     `json:"lastMs,omitempty"`
	Runs       int       `json:"runs,omitempty"`
	History    []TaskRun `json:"history,omitempty"` // newest first
}

// TaskRun is one finished run.
type TaskRun struct {
	At     int64  `json:"at"`
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Ms     int64  `json:"ms,omitempty"`
	Chat   string `json:"chat,omitempty"` // the saved session the run happened in
}

const maxTaskHistory = 15

var tasksMu sync.Mutex

func clipRunesN(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// Normalize checks a task and fills in defaults. It is the one place that decides what a valid
// task is, for the UI and for the agent's tools alike.
func (t *Task) Normalize() error {
	t.Name = clipRunesN(strings.Join(strings.Fields(t.Name), " "), 60)
	t.Prompt = strings.TrimSpace(t.Prompt)
	t.Extra = strings.TrimSpace(t.Extra)
	if len([]rune(t.Prompt)) > MaxTaskPromptRunes {
		return fmt.Errorf("task instructions exceed %d characters", MaxTaskPromptRunes)
	}
	if len([]rune(t.Extra)) > MaxTaskExtraRunes {
		return fmt.Errorf("priority rules exceed %d characters", MaxTaskExtraRunes)
	}
	if (len([]rune(t.Prompt))+3)/4+(len([]rune(t.Extra))+3)/4 > MaxTaskTextTokens {
		return fmt.Errorf("task instructions and priority rules exceed about %d tokens; shorten them", MaxTaskTextTokens)
	}
	t.Icon = clipRunesN(strings.TrimSpace(t.Icon), 8)
	seen := map[string]bool{}
	tools := t.Tools[:0:0]
	for _, id := range t.Tools {
		id = strings.TrimSpace(id)
		if id != "" && len(id) <= 80 && !seen[id] && len(tools) < 50 {
			seen[id] = true
			tools = append(tools, id)
		}
	}
	t.Tools = tools
	if t.Prompt == "" {
		return fmt.Errorf("say what the task should do")
	}
	if t.Name == "" {
		t.Name = clipRunesN(strings.Join(strings.Fields(t.Prompt), " "), 40)
	}
	switch t.Kind {
	case "", TaskManual:
		t.Kind, t.Every, t.At = TaskManual, 0, ""
	case TaskInterval:
		if t.Every < 5 || t.Every > 7*24*60 {
			return fmt.Errorf("repeat every 5 minutes to 7 days")
		}
		t.At = ""
	case TaskDaily, TaskWeekdays:
		if _, _, ok := parseClock(t.At); !ok {
			return fmt.Errorf("give a time such as 09:30")
		}
		t.Every = 0
	default:
		return fmt.Errorf("schedule must be manual, interval, daily or weekdays")
	}
	if t.Goal {
		t.Kind, t.Every, t.At, t.Enabled = TaskManual, 0, "", false
	}
	return nil
}

func parseClock(s string) (h, m int, ok bool) {
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil {
		return 0, 0, false
	}
	return h, m, h >= 0 && h < 24 && m >= 0 && m < 60
}

// latestSlot is the most recent scheduled moment at or before now for a daily or weekday task.
func (t Task) latestSlot(now time.Time) time.Time {
	h, m, ok := parseClock(t.At)
	if !ok {
		return time.Time{}
	}
	slot := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	for i := 0; i < 8; i++ {
		if !slot.After(now) && (t.Kind != TaskWeekdays || (slot.Weekday() != time.Saturday && slot.Weekday() != time.Sunday)) {
			return slot
		}
		slot = slot.AddDate(0, 0, -1)
	}
	return time.Time{}
}

// base is when the schedule counts from: the last run, or when the schedule was last set.
func (t Task) base() int64 {
	if t.LastRun > t.Created {
		return t.LastRun
	}
	return t.Created
}

// Due reports whether the schedule says the task should run now. A task that was missed while
// Phoenix was closed runs once when it next starts, not once per missed slot.
func (t Task) Due(now time.Time) bool {
	if !t.Enabled || t.Kind == TaskManual {
		return false
	}
	base := t.base()
	switch t.Kind {
	case TaskInterval:
		return now.Unix() >= base+int64(t.Every)*60
	case TaskDaily, TaskWeekdays:
		slot := t.latestSlot(now)
		return !slot.IsZero() && slot.Unix() > base
	}
	return false
}

// NextRun is when the schedule fires next, or the zero time for a manual or disabled task.
func (t Task) NextRun(now time.Time) time.Time {
	if !t.Enabled || t.Kind == TaskManual {
		return time.Time{}
	}
	base := t.base()
	switch t.Kind {
	case TaskInterval:
		return time.Unix(base+int64(t.Every)*60, 0)
	case TaskDaily, TaskWeekdays:
		if t.Due(now) {
			return now
		}
		h, m, ok := parseClock(t.At)
		if !ok {
			return time.Time{}
		}
		for i := 0; i < 9; i++ {
			d := now.AddDate(0, 0, i)
			slot := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, now.Location())
			if slot.After(now) && (t.Kind != TaskWeekdays || (slot.Weekday() != time.Saturday && slot.Weekday() != time.Sunday)) {
				return slot
			}
		}
	}
	return time.Time{}
}

func loadTasks(s *Store) []Task {
	var list []Task
	_ = json.Unmarshal([]byte(s.Setting(tasksKey)), &list)
	return list
}

func saveTasks(s *Store, list []Task) error {
	b, _ := json.Marshal(list)
	return s.SetSetting(tasksKey, string(b))
}

// Tasks lists every saved task, oldest first.
func (s *Store) Tasks() []Task {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	return loadTasks(s)
}

// Task returns one task by id.
func (s *Store) Task(id string) (Task, bool) {
	for _, t := range s.Tasks() {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// SaveTask adds a task (empty ID) or replaces the user-editable fields of an existing one. Its
// run history is kept.
func (s *Store) SaveTask(t Task) (Task, error) {
	if err := t.Normalize(); err != nil {
		return Task{}, err
	}
	tasksMu.Lock()
	defer tasksMu.Unlock()
	list := loadTasks(s)
	if t.ID != "" {
		for i := range list {
			if list[i].ID == t.ID {
				old := list[i]
				t.Created, t.LastRun, t.LastStatus, t.LastResult, t.LastMs, t.Runs, t.History = old.Created, old.LastRun, old.LastStatus, old.LastResult, old.LastMs, old.Runs, old.History
				if t.Kind != old.Kind || t.Every != old.Every || t.At != old.At {
					t.Created = time.Now().Unix() // a new schedule counts from now, so it does not fire for the past
				}
				list[i] = t
				return t, saveTasks(s, list)
			}
		}
		return Task{}, fmt.Errorf("that task no longer exists")
	}
	if len(list) >= maxTasks {
		return Task{}, fmt.Errorf("too many tasks: delete one first")
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	t.ID, t.Created = hex.EncodeToString(b[:]), time.Now().Unix()
	list = append(list, t)
	return t, saveTasks(s, list)
}

// DeleteTask removes a task.
func (s *Store) DeleteTask(id string) error {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	list := loadTasks(s)
	for i := range list {
		if list[i].ID == id {
			return saveTasks(s, append(list[:i:i], list[i+1:]...))
		}
	}
	return nil
}

// SetTaskEnabled switches a task's schedule on or off. Turning it on counts from now.
func (s *Store) SetTaskEnabled(id string, on bool) error {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	list := loadTasks(s)
	for i := range list {
		if list[i].ID == id {
			if on && !list[i].Enabled {
				list[i].Created = time.Now().Unix()
			}
			list[i].Enabled = on
			return saveTasks(s, list)
		}
	}
	return nil
}

// RecordTaskRun stores how a run ended. at is when it started.
func (s *Store) RecordTaskRun(id string, at time.Time, status, result string, ms int64, chat string) {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	list := loadTasks(s)
	for i := range list {
		if list[i].ID == id {
			list[i].LastRun, list[i].LastStatus, list[i].LastMs = at.Unix(), status, ms
			list[i].LastResult = clipRunesN(strings.TrimSpace(result), 400)
			list[i].Runs++
			run := TaskRun{At: at.Unix(), Status: status, Result: list[i].LastResult, Ms: ms, Chat: chat}
			list[i].History = append([]TaskRun{run}, list[i].History...)
			if len(list[i].History) > maxTaskHistory {
				list[i].History = list[i].History[:maxTaskHistory]
			}
			_ = saveTasks(s, list)
			return
		}
	}
}

type taskArgs struct {
	Name          string `json:"name,omitempty" jsonschema:"short title"`
	Instructions  string `json:"instructions" jsonschema:"what the agent must do, written so it can be done with no further questions: where to go, what to do, what counts as finished"`
	Goal          bool   `json:"goal,omitempty" jsonschema:"keep working across turns until completion is verified or a genuine blocker is reached; goals are manual and never scheduled"`
	PriorityRules string `json:"priority_rules,omitempty" jsonschema:"hard constraints, exclusions, limits, or deadlines that take precedence over the task goal"`
	Schedule      string `json:"schedule,omitempty" jsonschema:"manual (default), interval, daily or weekdays"`
	EveryMinutes  int    `json:"every_minutes,omitempty" jsonschema:"for interval: minutes between runs, at least 5"`
	At            string `json:"at,omitempty" jsonschema:"for daily and weekdays: the local time as HH:MM, 24-hour"`
}

type taskIDArgs struct {
	ID string `json:"id" jsonschema:"the task id from tasks_list"`
}

func registerTaskTools(srv *sdk.Server, s *Store) {
	tool(srv, "tasks_create", "Save a task: an instruction Phoenix can run on its own later, by the user's button or on a schedule. Use goal=true when the user wants continued work until completion is verified; goals are manual and never scheduled. Put hard constraints, exclusions, limits, and deadlines in priority_rules; they govern the task goal. It starts switched on if it has a schedule.", func(a taskArgs) (string, error) {
		t := Task{Name: a.Name, Prompt: a.Instructions, Goal: a.Goal, Extra: a.PriorityRules, Kind: a.Schedule, Every: a.EveryMinutes, At: a.At}
		t.Enabled = t.Kind != "" && t.Kind != TaskManual
		saved, err := s.SaveTask(t)
		if err != nil {
			return "", err
		}
		return "Saved task \"" + saved.Name + "\" (" + DescribeSchedule(saved) + "), id " + saved.ID, nil
	})
	tool(srv, "tasks_list", "List the saved tasks with their schedule and the result of the last run.", func(struct{}) (string, error) {
		list := loadedTasks(s)
		if len(list) == 0 {
			return "No tasks yet.", nil
		}
		var b strings.Builder
		for _, t := range list {
			b.WriteString("- " + t.ID + " " + t.Name + " [" + DescribeSchedule(t) + "]")
			if t.LastStatus != "" {
				b.WriteString(" last: " + t.LastStatus + " " + t.LastResult)
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	})
	tool(srv, "tasks_delete", "Delete a saved task.", func(a taskIDArgs) (string, error) {
		return "Deleted.", s.DeleteTask(a.ID)
	})
}

func loadedTasks(s *Store) []Task { return s.Tasks() }

// DescribeSchedule is the schedule in words.
func DescribeSchedule(t Task) string {
	if t.Goal {
		return "goal (runs until completion)"
	}
	switch t.Kind {
	case TaskInterval:
		if t.Every%60 == 0 {
			return fmt.Sprintf("every %d h", t.Every/60)
		}
		return fmt.Sprintf("every %d min", t.Every)
	case TaskDaily:
		return "daily at " + t.At
	case TaskWeekdays:
		return "weekdays at " + t.At
	}
	return "manual"
}
