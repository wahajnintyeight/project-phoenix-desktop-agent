package main

import (
	"phoenix/internal/library"
	"phoenix/internal/persona"
)

// The user's notes about themselves (background, likes, dislikes, how they work). They are kept
// in the library database as a private setting, edited in Settings → About you, and a budgeted
// part of them is added to the agent's instructions on every run (internal/persona decides what
// fits). The rest is available to the agent through the read-only user_profile tool.
//
// UI → Go:  v profile-save <text>    v profile-refresh
// Go → UI:  u profile-settings data <json>   u profile-settings result <json>

type profileData struct {
	Text     string   `json:"text"`
	Total    int      `json:"total"`  // characters saved
	Max      int      `json:"max"`    // most that can be saved
	Chars    int      `json:"chars"`  // characters sent with every request
	Tokens   int      `json:"tokens"` // rough tokens added to every request
	Budget   int      `json:"budget"` // characters that fit in one request
	Omitted  []string `json:"omitted"`
	Template string   `json:"template"`
}

// profileNote is the block added to the system prompt for a run, or "" when there are no notes.
func profileNote() string {
	lib, err := openLibrary()
	if err != nil {
		return ""
	}
	b := persona.Compose(lib.Setting(library.ProfileKey), persona.Budget)
	if b.Text == "" {
		return ""
	}
	return "\n\n" + b.Text
}

func pushProfile() {
	d := profileData{Max: persona.MaxStored, Budget: persona.Budget, Template: persona.Template, Omitted: []string{}}
	if lib, err := openLibrary(); err == nil {
		d.Text = lib.Setting(library.ProfileKey)
		b := persona.Compose(d.Text, persona.Budget)
		d.Total, d.Chars, d.Tokens = b.Total, b.Chars, b.Tokens
		if b.Omitted != nil {
			d.Omitted = b.Omitted
		}
	}
	post("u" + fs + "profile-settings" + fs + "data" + fs + jsonString(d))
}

func handleProfileOp(f []string) {
	if len(f) < 2 {
		return
	}
	switch f[1] {
	case "profile-refresh":
		pushProfile()
	case "profile-save":
		text := ""
		if len(f) > 2 {
			text = persona.Clean(f[2])
		}
		lib, err := openLibrary()
		if err == nil {
			err = lib.SetSetting(library.ProfileKey, text)
		}
		if err != nil {
			post("u" + fs + "profile-settings" + fs + "result" + fs + jsonString(testResult{Error: "Could not save: " + err.Error()}))
			return
		}
		pushProfile()
		msg := "Saved."
		if text == "" {
			msg = "Cleared. The agent no longer receives any notes about you."
		}
		post("u" + fs + "profile-settings" + fs + "result" + fs + jsonString(testResult{OK: true, Text: msg}))
	}
}
