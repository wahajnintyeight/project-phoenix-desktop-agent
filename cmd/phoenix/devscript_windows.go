package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"phoenix/internal/browser"
)

// Development-only: PHOENIX_TEST_SCRIPT=<file.json> runs a short list of browser tool calls
// against the real window and writes what each returned to <file>.out. It exists so the form
// tools can be checked end to end on a local test page without a model, and does nothing unless
// the variable is set. It calls the same functions the agent's tools call.
//
// Steps: {"do":"open","url":..} {"do":"snap","find":..} {"do":"click","find":..}
// {"do":"fill_form","fields":[{"find":..,"value":..}]} {"do":"upload","file":..,"find":..}
// {"do":"read"} {"do":"tabs"} {"do":"wait","ms":..}  (tab: optional, default the active tab).

type devStep struct {
	Do     string `json:"do"`
	URL    string `json:"url"`
	Find   string `json:"find"`
	File   string `json:"file"`
	MS     int    `json:"ms"`
	Tab    int    `json:"tab"`
	Fields []struct {
		Find  string `json:"find"`
		Value string `json:"value"`
	} `json:"fields"`
}

var devRef = regexp.MustCompile(`ref=(@s\w+:e\d+)`)

func startDevScript() {
	path := os.Getenv("PHOENIX_TEST_SCRIPT")
	if path == "" {
		return
	}
	go func() {
		time.Sleep(4 * time.Second)
		raw, err := os.ReadFile(path)
		if err != nil {
			return
		}
		var steps []devStep
		if err := json.Unmarshal(raw, &steps); err != nil {
			_ = os.WriteFile(path+".out", []byte("bad script: "+err.Error()), 0o600)
			return
		}
		var out strings.Builder
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		tabID := 0
		call := func(tool string, args any) (browser.Output, error) {
			b, _ := json.Marshal(args)
			return performBrowserTool(ctx, browser.Call{Tool: tool, Arguments: b})
		}
		refFor := func(find string) string {
			o, err := call("browser_snapshot", browser.SnapshotArgs{TabID: tabID, Query: find})
			if err != nil {
				return ""
			}
			if m := devRef.FindStringSubmatch(o.Text); m != nil {
				return m[1]
			}
			return ""
		}
		for i, s := range steps {
			if s.Tab > 0 {
				tabID = s.Tab
			}
			var o browser.Output
			var err error
			switch s.Do {
			case "open":
				o, err = call("browser_open_tab", browser.OpenTabArgs{URL: s.URL})
				var m struct {
					TabID int `json:"tab_id"`
				}
				if json.Unmarshal([]byte(o.Text), &m) == nil && m.TabID > 0 {
					tabID = m.TabID
				}
				time.Sleep(2500 * time.Millisecond)
			case "snap":
				o, err = call("browser_snapshot", browser.SnapshotArgs{TabID: tabID, Query: s.Find})
			case "putfile":
				if lib, e := openLibrary(); e == nil {
					p, _ := lib.Put("docs", "resume.pdf", []byte("%PDF-1.4 test resume"), "user", "")
					_ = lib.SetInfo(p, "resume", "")
					o.Text = "added " + p
				}
			case "read":
				o, err = call("browser_read", browser.TabArgs{TabID: tabID})
			case "tabs":
				o, err = call("browser_tabs", browser.NoArgs{})
			case "wait":
				time.Sleep(time.Duration(s.MS) * time.Millisecond)
			case "click":
				ref := refFor(s.Find)
				o, err = call("browser_click", browser.RefArgs{TabID: tabID, Ref: ref})
			case "fill_form":
				var fields []browser.FormField
				_, _ = call("browser_snapshot", browser.SnapshotArgs{TabID: tabID})
				for _, f := range s.Fields {
					fields = append(fields, browser.FormField{Ref: refFor(f.Find), Value: f.Value})
				}
				// refFor re-snapshots, so only the last snapshot's refs stay valid; take one full one.
				o, err = call("browser_snapshot", browser.SnapshotArgs{TabID: tabID})
				fields = fields[:0]
				for _, f := range s.Fields {
					m := regexp.MustCompile(`"` + regexp.QuoteMeta(f.Find) + `"[^\n]*ref=(@s\w+:e\d+)`).FindStringSubmatch(o.Text)
					if m != nil {
						fields = append(fields, browser.FormField{Ref: m[1], Value: f.Value})
					}
				}
				o, err = call("browser_fill_form", browser.FormArgs{TabID: tabID, Fields: fields})
			case "upload":
				a := browser.UploadArgs{TabID: tabID, File: s.File}
				if s.Find != "" {
					a.Ref = refFor(s.Find)
				}
				o, err = call("browser_upload", a)
			}
			fmt.Fprintf(&out, "== %d %s %s\n%s", i, s.Do, s.Find, o.Text)
			if err != nil {
				fmt.Fprintf(&out, "\nERROR: %v", err)
			}
			out.WriteString("\n")
			_ = os.WriteFile(path+".out", []byte(out.String()), 0o600)
		}
		_ = os.WriteFile(path+".out", []byte(out.String()+"\nDONE\n"), 0o600)
	}()
}
