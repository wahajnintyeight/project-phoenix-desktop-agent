package main

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type choiceAnswer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func parseChoiceAnswers(raw string) []choiceAnswer {
	if len(raw) > 16384 {
		return nil
	}
	var answers []choiceAnswer
	if json.Unmarshal([]byte(raw), &answers) != nil || len(answers) == 0 || len(answers) > 4 {
		return nil
	}
	for i := range answers {
		answers[i].Question = strings.TrimSpace(answers[i].Question)
		answers[i].Answer = strings.TrimSpace(answers[i].Answer)
		if answers[i].Question == "" || answers[i].Answer == "" || utf8.RuneCountInString(answers[i].Question) > 160 || utf8.RuneCountInString(answers[i].Answer) > 500 {
			return nil
		}
	}
	return answers
}
