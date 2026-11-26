package quick

import "testing"

func TestGenerated(t *testing.T) {
	for _, text := range []string{`["Summarise this role", "Compare requirements"]`, "```json\n[\"Summarise this role\", \"Compare requirements\"]\n```"} {
		got := Generated(text)
		if len(got) != 2 || got[0].Label != "Summarise this role" || !got[0].Page {
			t.Fatalf("Generated(%q) = %+v", text, got)
		}
	}
	for _, text := range []string{`["only one"]`, `["Same label", "same label"]`, `{"actions":[]}`, `["ok", "other action"]`} {
		if got := Generated(text); got != nil {
			t.Errorf("Generated(%q) = %+v, want nil", text, got)
		}
	}
}
