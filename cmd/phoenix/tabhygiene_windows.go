package main

import "time"

// Tab hygiene. Every page tab costs memory (a busy site such as LinkedIn is 500 MB or more), and
// an agent working through applications opens a tab per employer. Tabs the agent opened are
// therefore closed when it no longer needs them; tabs the user opened, or took over, never are.
//
//   - A tab is "agent-opened" if the agent's browser_open_tab made it, or a page opened it
//     (an Apply link, a popup) while an agent run was active.
//   - When a run ends, agent-opened tabs that are not the one on screen are closed. The one on
//     screen stays so the user can see the result, and is closed at the end of a later run once
//     they have moved away from it.
//   - At most maxWebTabs page tabs stay open: opening one more closes the oldest agent-opened
//     tab in the background.
//   - Clicking an agent-opened tab, or typing an address into it, makes it the user's.

const maxWebTabs = 5

// overCapVictim returns the tab to close when there are too many pages, or nil.
func overCapVictim(list []*tab, activeID int) *tab {
	pages := 0
	var oldest *tab
	for _, t := range list {
		if t.kind != kindWeb {
			continue
		}
		pages++
		if t.byAgent && t.id != activeID && (oldest == nil || t.openedAt.Before(oldest.openedAt)) {
			oldest = t
		}
	}
	if pages <= maxWebTabs {
		return nil
	}
	return oldest
}

// finishedAgentTabs lists the agent-opened tabs to close when a run ends: all but the one shown.
func finishedAgentTabs(list []*tab, activeID int) []int {
	var ids []int
	for _, t := range list {
		if t.kind == kindWeb && t.byAgent && t.id != activeID {
			ids = append(ids, t.id)
		}
	}
	return ids
}

// enforceTabCap closes the oldest background agent-opened tabs while there are too many pages.
func enforceTabCap() {
	for {
		victim := overCapVictim(tabs, activeID)
		if victim == nil {
			return // within the limit, or everything left is the user's or on screen
		}
		logf("tabs: closing agent tab %d (over the limit of %d)", victim.id, maxWebTabs)
		closeTab(victim.id)
	}
}

// closeFinishedAgentTabs runs when an agent run ends.
func closeFinishedAgentTabs() {
	for _, id := range finishedAgentTabs(tabs, activeID) {
		logf("tabs: closing agent tab %d (run finished)", id)
		closeTab(id)
	}
}

// markAgentTab flags a tab the agent's work opened.
func markAgentTab(t *tab) {
	t.byAgent = true
	t.openedAt = time.Now()
}

// adoptTab makes an agent-opened tab the user's, so it is never closed for them.
func adoptTab(t *tab) {
	if t != nil {
		t.byAgent = false
	}
}
