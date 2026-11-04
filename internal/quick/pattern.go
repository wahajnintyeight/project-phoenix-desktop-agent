package quick

import (
	"strconv"
	"strings"
)

// Pattern is a provider-neutral action recipe. Format turns it into plain-text instructions
// so every model receives the same goal, sources, workflow, deliverables, and guardrails.
type Pattern struct {
	Goal         string
	Sources      []string
	Steps        []string
	Deliverables []string
	Guardrails   []string
}

func Format(pattern Pattern) string {
	var out strings.Builder
	out.WriteString("Goal: ")
	out.WriteString(strings.TrimSpace(pattern.Goal))
	writePatternSection(&out, "Use these sources", pattern.Sources, false)
	writePatternSection(&out, "Workflow", pattern.Steps, true)
	writePatternSection(&out, "Return", pattern.Deliverables, false)
	writePatternSection(&out, "Guardrails", pattern.Guardrails, false)
	return out.String()
}

func writePatternSection(out *strings.Builder, title string, items []string, numbered bool) {
	if len(items) == 0 {
		return
	}
	out.WriteString("\n\n")
	out.WriteString(title)
	out.WriteString(":")
	for i, item := range items {
		out.WriteString("\n")
		if numbered {
			out.WriteString(strconv.Itoa(i + 1))
			out.WriteString(". ")
		} else {
			out.WriteString("- ")
		}
		out.WriteString(strings.TrimSpace(item))
	}
}
