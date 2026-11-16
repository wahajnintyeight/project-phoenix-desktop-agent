package main

import (
	"strconv"
	"strings"

	"phoenix/internal/library"
	"phoenix/internal/provider"
)

func selectedSkillContext(text string, history []provider.Message) string {
	lib, err := openLibrary()
	if err != nil {
		return ""
	}
	query := text
	if memoryFollowUp(text) {
		for i, seen := len(history)-1, 0; i >= 0 && seen < 2; i-- {
			if history[i].Role == "user" {
				query += " " + history[i].Content
				seen++
			}
		}
	}
	return lib.RelevantSkill(query, library.SkillContextBytes)
}

func pushSkills() {
	items := []library.Skill{}
	if lib, err := openLibrary(); err == nil {
		if found, err := lib.Skills(); err == nil {
			items = found
		}
	}
	post("u" + fs + "skills-settings" + fs + "data" + fs + jsonString(items))
}

func skillResult(ok bool, message string) {
	r := testResult{OK: ok, Text: message}
	if !ok {
		r = testResult{Error: message}
	}
	post("u" + fs + "skills-settings" + fs + "result" + fs + jsonString(r))
}

func handleSkillsOp(f []string) {
	if len(f) < 2 {
		return
	}
	if f[1] == "skills-refresh" {
		pushSkills()
		return
	}
	lib, err := openLibrary()
	if err != nil {
		skillResult(false, "Skills are unavailable: "+err.Error())
		return
	}
	arg := func(i int) string {
		if len(f) > i {
			return f[i]
		}
		return ""
	}
	switch f[1] {
	case "skills-save":
		id, _ := strconv.ParseInt(arg(2), 10, 64)
		if len(f) < 6 {
			skillResult(false, "Enter a name, when-to-use description, and instructions.")
			return
		}
		if _, err = lib.SaveSkill(id, arg(3), arg(4), arg(5)); err != nil {
			skillResult(false, "Could not save skill: "+err.Error())
			return
		}
		pushSkills()
		skillResult(true, "Skill saved.")
	case "skills-enabled":
		id, _ := strconv.ParseInt(arg(2), 10, 64)
		err = lib.SetSkillEnabled(id, arg(3) == "1")
		if err == nil {
			pushSkills()
			skillResult(true, "Skill updated.")
		} else {
			skillResult(false, "Could not update skill: "+err.Error())
		}
	case "skills-delete":
		id, _ := strconv.ParseInt(arg(2), 10, 64)
		err = lib.DeleteSkill(id)
		if err == nil {
			pushSkills()
			skillResult(true, "Skill deleted.")
		} else {
			skillResult(false, "Could not delete skill: "+err.Error())
		}
	default:
		if strings.HasPrefix(f[1], "skills-") {
			logf("unknown skills operation: %s", f[1])
		}
	}
}
