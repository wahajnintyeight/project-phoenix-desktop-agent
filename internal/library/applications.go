package library

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Applications log and remembered answers. Automating applications means the agent must not
// apply twice to the same job and must not ask the user the same question (work authorisation,
// salary, notice period) again. Both live in the library's settings, next to the profile.

const applicationsKey = "applications.json"
const maxApplications = 1000

// Application is one job (or form) the agent worked on.
type Application struct {
	Company string `json:"company,omitempty"`
	Role    string `json:"role,omitempty"`
	URL     string `json:"url,omitempty"`
	Status  string `json:"status"`
	Note    string `json:"note,omitempty"`
	At      int64  `json:"at"`
}

var appsMu sync.Mutex

type logArgs struct {
	Company string `json:"company" jsonschema:"company name"`
	Role    string `json:"role" jsonschema:"job title"`
	URL     string `json:"url,omitempty" jsonschema:"address of the listing; the same address updates the earlier entry"`
	Status  string `json:"status" jsonschema:"applied, started (not finished), skipped or failed"`
	Note    string `json:"note,omitempty" jsonschema:"one short line, for example why it failed or what is left to do"`
}

type listAppsArgs struct {
	Query string `json:"query,omitempty" jsonschema:"company, role or address to look for; empty lists the latest"`
}

func registerApplicationTools(srv *sdk.Server, s *Store) {
	tool(srv, "applications_log", "Record a job application (or other form submission) you did, started, skipped or failed, so it is never repeated and the user can see the result. Call it after the final click.", func(a logArgs) (string, error) {
		switch a.Status {
		case "applied", "started", "skipped", "failed":
		default:
			return "", fmt.Errorf("status must be applied, started, skipped or failed")
		}
		if strings.TrimSpace(a.Company+a.Role+a.URL) == "" {
			return "", fmt.Errorf("give at least a company, role or address")
		}
		appsMu.Lock()
		defer appsMu.Unlock()
		list := loadApplications(s)
		entry := Application{Company: clipRunes(strings.TrimSpace(a.Company), 120), Role: clipRunes(strings.TrimSpace(a.Role), 160),
			URL: clipRunes(strings.TrimSpace(a.URL), 500), Status: a.Status, Note: clipRunes(strings.TrimSpace(a.Note), 300), At: time.Now().Unix()}
		replaced := false
		for i := range list {
			if entry.URL != "" && list[i].URL == entry.URL {
				list[i], replaced = entry, true
				break
			}
		}
		if !replaced {
			list = append(list, entry)
		}
		if len(list) > maxApplications {
			list = list[len(list)-maxApplications:]
		}
		b, _ := json.Marshal(list)
		if err := s.SetSetting(applicationsKey, string(b)); err != nil {
			return "", err
		}
		return "Recorded: " + describeApplication(entry), nil
	})
	tool(srv, "applications_list", "List jobs and forms already applied to, newest first. Check this BEFORE applying so nothing is applied to twice.", func(a listAppsArgs) (string, error) {
		appsMu.Lock()
		defer appsMu.Unlock()
		list := loadApplications(s)
		q := strings.ToLower(strings.TrimSpace(a.Query))
		var b strings.Builder
		n := 0
		for i := len(list) - 1; i >= 0 && n < 30; i-- {
			e := list[i]
			if q != "" && !strings.Contains(strings.ToLower(e.Company+" "+e.Role+" "+e.URL), q) {
				continue
			}
			b.WriteString("- " + describeApplication(e) + "\n")
			n++
		}
		if n == 0 {
			return "Nothing recorded yet.", nil
		}
		return b.String(), nil
	})
	registerMemoryTools(srv, s)
}

func loadApplications(s *Store) []Application {
	var list []Application
	_ = json.Unmarshal([]byte(s.Setting(applicationsKey)), &list)
	return list
}

func describeApplication(e Application) string {
	d := e.Status + ": " + strings.TrimSpace(e.Role+" at "+e.Company)
	if e.Company == "" || e.Role == "" {
		d = e.Status + ": " + strings.TrimSpace(e.Role+e.Company)
	}
	if e.URL != "" {
		d += " (" + e.URL + ")"
	}
	if e.Note != "" {
		d += " - " + e.Note
	}
	return d + " [" + time.Unix(e.At, 0).Format("2006-01-02") + "]"
}
