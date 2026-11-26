package quick

import (
	"encoding/json"
	"strings"
	"unicode"
)

// Generated parses short visible requests; their labels are also their entire intent.
func Generated(text string) []Action {
	if len(text) > 4096 {
		return nil
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text[i+1:]), "```"))
		}
	}
	var labels []string
	if json.Unmarshal([]byte(text), &labels) != nil || len(labels) < 2 || len(labels) > MaxActions {
		return nil
	}
	seen := map[string]bool{}
	actions := make([]Action, 0, len(labels))
	for _, label := range labels {
		label = strings.Join(strings.Fields(label), " ")
		if n := len([]rune(label)); n < 4 || n > 60 || seen[strings.ToLower(label)] || strings.IndexFunc(label, unicode.IsControl) >= 0 {
			return nil
		}
		seen[strings.ToLower(label)] = true
		actions = append(actions, Action{Label: label, Page: true, Prompt: "Help me with this request for the current page: " + label + ". " + doGuardrail})
	}
	return actions
}
