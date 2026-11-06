package library

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTaskSchedules(t *testing.T) {
	created := at("2026-11-02 10:00").Unix() // a Monday
	daily := Task{Kind: TaskDaily, At: "09:00", Enabled: true, Created: created}
	if daily.Due(at("2026-11-02 23:00")) {
		t.Fatal("created after today's slot: not due until tomorrow")
	}
	if !daily.Due(at("2026-11-03 09:00")) {
		t.Fatal("daily should be due at its time")
	}
	daily.LastRun = at("2026-11-03 09:01").Unix()
	if daily.Due(at("2026-11-03 20:00")) {
		t.Fatal("already ran today")
	}
	// Closed for three days: one catch-up run, not three.
	if !daily.Due(at("2026-11-06 12:00")) {
		t.Fatal("missed slots should run once")
	}
	wk := Task{Kind: TaskWeekdays, At: "09:00", Enabled: true, Created: at("2026-11-06 10:00").Unix()} // Friday
	if wk.Due(at("2026-11-08 12:00")) {
		t.Fatal("weekend must not fire")
	}
	if !wk.Due(at("2026-11-09 09:00")) {
		t.Fatal("Monday should fire")
	}
	if got := wk.NextRun(at("2026-11-07 12:00")); !got.Equal(at("2026-11-09 09:00")) {
		t.Fatalf("next run %v", got)
	}
	iv := Task{Kind: TaskInterval, Every: 30, Enabled: true, Created: created}
	if iv.Due(at("2026-11-02 10:29")) || !iv.Due(at("2026-11-02 10:30")) {
		t.Fatal("interval boundary")
	}
	if (Task{Kind: TaskManual, Enabled: true}).Due(time.Now()) || (Task{Kind: TaskInterval, Every: 5}).Due(time.Now().Add(time.Hour)) {
		t.Fatal("manual and disabled never run by themselves")
	}
}

func TestTaskStore(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.SaveTask(Task{Prompt: "  "}); err == nil {
		t.Fatal("empty instruction must fail")
	}
	if _, err := st.SaveTask(Task{Prompt: "x", Kind: TaskInterval, Every: 1}); err == nil {
		t.Fatal("interval under 5 minutes must fail")
	}
	if _, err := st.SaveTask(Task{Prompt: "x", Kind: TaskDaily, At: "25:00"}); err == nil {
		t.Fatal("bad time must fail")
	}
	saved, err := st.SaveTask(Task{Prompt: "Check my inbox for replies", Kind: TaskDaily, At: "08:30", Enabled: true})
	if err != nil || saved.ID == "" || saved.Name == "" {
		t.Fatalf("save: %+v %v", saved, err)
	}
	st.RecordTaskRun(saved.ID, time.Now(), "ok", "3 new replies", 4000, "chat1")
	saved.Prompt = "Check my inbox and star replies"
	again, err := st.SaveTask(saved)
	if err != nil || again.Runs != 1 || len(again.History) != 1 || again.History[0].Chat != "chat1" || again.LastResult != "3 new replies" {
		t.Fatalf("edit keeps history: %+v %v", again, err)
	}
	if err := st.SetTaskEnabled(saved.ID, false); err != nil {
		t.Fatal(err)
	}
	if g, _ := st.Task(saved.ID); g.Enabled {
		t.Fatal("not disabled")
	}
	if err := st.DeleteTask(saved.ID); err != nil || len(st.Tasks()) != 0 {
		t.Fatal("delete")
	}
}

func TestTaskToolsCreateAndList(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, b := sdk.NewInMemoryTransports()
	srv := NewServer(st, "agent")
	if _, err := srv.Connect(context.Background(), a, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "1"}, nil).Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	out := callTool(t, cs, "tasks_create", map[string]any{"instructions": "Check the news", "schedule": "weekdays", "at": "09:00"})
	if !strings.Contains(out, "weekdays at 09:00") {
		t.Fatalf("create: %s", out)
	}
	if list := st.Tasks(); len(list) != 1 || !list[0].Enabled {
		t.Fatalf("a scheduled task made by the agent starts on: %+v", list)
	}
	if !strings.Contains(callTool(t, cs, "tasks_list", map[string]any{}), "Check the news") {
		t.Fatal("list")
	}
}
