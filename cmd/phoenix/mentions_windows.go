package main

import (
	"strings"
	"unicode/utf8"
)

type mentionItem struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Ref    string `json:"ref"`
	Detail string `json:"detail,omitempty"`
}

func pushMentions(query, seq string) {
	if utf8.RuneCountInString(query) > 100 {
		query = string([]rune(query)[:100])
	}
	query = strings.TrimSpace(query)
	needle := strings.ToLower(query)
	items := make([]mentionItem, 0, 80)
	if lib, err := openLibrary(); err == nil {
		for _, t := range lib.Tasks() {
			if len(items) >= 20 {
				break
			}
			if strings.Contains(strings.ToLower(t.Name), needle) {
				items = append(items, mentionItem{Kind: "Task", Name: t.Name, Ref: t.ID})
			}
		}
		if hits, err := lib.MentionSearch(query, 50); err == nil {
			for _, h := range hits {
				kind := "File"
				if h.Dir {
					kind = "Folder"
				}
				items = append(items, mentionItem{Kind: kind, Name: h.Name, Ref: h.Path, Detail: h.Path})
			}
		}
	}
	if hub != nil {
		for _, name := range []string{"phoenix-browser", "phoenix-files", "phoenix-desktop"} {
			if strings.Contains(name, needle) {
				detail := "Built-in"
				if name == "phoenix-desktop" && !desktopEnabled() {
					detail = "Disabled"
				}
				items = append(items, mentionItem{Kind: "MCP", Name: name, Ref: name, Detail: detail})
			}
		}
		for _, s := range hub.MCPServers() {
			if strings.Contains(strings.ToLower(s.Name), needle) {
				detail := "Enabled"
				if !s.Enabled {
					detail = "Disabled"
				}
				items = append(items, mentionItem{Kind: "MCP", Name: s.Name, Ref: s.Name, Detail: detail})
			}
		}
	}
	post("u" + fs + "mention-picker" + fs + "data" + fs + jsonString(struct {
		Seq   string        `json:"seq"`
		Items []mentionItem `json:"items"`
	}{seq, items}))
}
