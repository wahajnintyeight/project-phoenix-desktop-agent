package main

import (
	"testing"
	"time"
)

func page(id int, agent bool, age time.Duration) *tab {
	t := &tab{id: id, kind: kindWeb}
	if agent {
		t.byAgent, t.openedAt = true, time.Now().Add(-age)
	}
	return t
}

func TestTabsTheAgentOpenedAreClosedWhenItIsDoneButNotTheOneOnScreen(t *testing.T) {
	list := []*tab{{id: 1, kind: kindHome}, page(2, false, 0), page(3, true, time.Minute), page(4, true, 0)}
	got := finishedAgentTabs(list, 4)
	if len(got) != 1 || got[0] != 3 {
		t.Fatalf("closed %v, want only tab 3 (tab 2 is the user's, tab 4 is on screen)", got)
	}
	// Once the user has taken a tab over it is never closed for them.
	adoptTab(list[2])
	if got := finishedAgentTabs(list, 4); len(got) != 0 {
		t.Fatalf("an adopted tab was closed: %v", got)
	}
}

func TestTabCapClosesTheOldestBackgroundAgentTabOnly(t *testing.T) {
	list := []*tab{page(1, false, 0), page(2, true, 3*time.Minute), page(3, true, time.Minute), page(4, false, 0), page(5, false, 0), page(6, true, 0)}
	if v := overCapVictim(list, 6); v == nil || v.id != 2 {
		t.Fatalf("victim = %+v, want the oldest agent tab (2)", v)
	}
	// The tab on screen is never the victim, even if it is the oldest.
	if v := overCapVictim(list, 2); v == nil || v.id != 3 {
		t.Fatalf("victim = %+v, want tab 3 when tab 2 is on screen", v)
	}
	// Only the user's tabs over the limit: nothing to close.
	user := []*tab{page(1, false, 0), page(2, false, 0), page(3, false, 0), page(4, false, 0), page(5, false, 0), page(6, false, 0)}
	if v := overCapVictim(user, 1); v != nil {
		t.Fatalf("closed the user's tab %d", v.id)
	}
	// Within the limit: nothing to close.
	if v := overCapVictim(list[:5], 1); v != nil {
		t.Fatalf("closed a tab at the limit: %d", v.id)
	}
}
