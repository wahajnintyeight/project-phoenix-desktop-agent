package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"phoenix/internal/browser"
	"phoenix/internal/library"
)

// ── Form filling and file upload ───────────────────────────────────────────
//
// Applying for a job, booking, registering: every one of these is a form. Filling it one field
// per tool call costs a round trip (and a model turn) per field, and a resume has to go into a
// file input that no typing can reach. These two tools remove both problems.

const maxFormFields = 40

// browserFillForm sets many fields in one call and reports each result, then lists the fields
// the page itself still considers invalid so the agent fixes them before submitting.
func browserFillForm(ctx context.Context, state browserTabState, session *browserSession, args browser.FormArgs) (browser.Output, error) {
	var report strings.Builder
	done, failed := 0, 0
	for _, f := range args.Fields {
		ref, err := snapshotRefForOperation(session, f.Ref, state.Epoch)
		if err != nil {
			failed++
			fmt.Fprintf(&report, "- failed (%s): %v\n", clipBrowser(f.Ref, 24), err)
			continue
		}
		label := browserElementLabel(ref)
		if err := fillOneField(ctx, state, ref, f.Value); err != nil {
			failed++
			fmt.Fprintf(&report, "- failed %s: %v\n", label, err)
			continue
		}
		done++
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Filled %d of %d fields.", done, len(args.Fields))
	if failed > 0 {
		out.WriteString(" Problems:\n" + report.String())
	}
	if invalid := invalidFormFields(ctx, state); len(invalid) > 0 {
		fmt.Fprintf(&out, "\nStill required or invalid on the page: %s.", strings.Join(invalid, ", "))
	} else if failed == 0 {
		out.WriteString("\nNo field on the page is flagged invalid.")
	}
	return browser.Output{Text: out.String(), IsError: done == 0 && failed > 0}, nil
}

func fillOneField(ctx context.Context, state browserTabState, ref browserRef, value string) error {
	switch ref.Role {
	case "textbox", "searchbox":
		_, err := browserEnterText(ctx, state, ref, value, true)
		return err
	case "combobox", "listbox":
		_, err := browserSelect(ctx, state, ref, value)
		return err
	case "checkbox", "switch", "radio":
		want := truthy(value)
		if ref.Role == "radio" {
			if !want {
				return nil
			}
		} else if want == ref.Checked {
			return nil // already in the wanted state; clicking would undo it
		}
		_, err := browserClick(ctx, state, ref)
		return err
	}
	return fmt.Errorf("a %s cannot be filled; use browser_click", ref.Role)
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "y", "on", "checked", "1":
		return true
	}
	return false
}

// invalidFormFields asks the page which fields it flags as invalid (required and empty, wrong
// format), by their label, so a form is not submitted with holes.
func invalidFormFields(ctx context.Context, state browserTabState) []string {
	const expr = `(()=>{const n=e=>{const l=e.labels&&e.labels[0]?e.labels[0].innerText:(e.getAttribute('aria-label')||e.name||e.id||e.type||'field');return l.replace(/\s+/g,' ').trim().slice(0,60)};` +
		`return [...document.querySelectorAll('input:invalid,select:invalid,textarea:invalid')].filter(e=>e.type!=='hidden'&&e.offsetParent!==null).slice(0,10).map(n)})()`
	var result runtimeResult
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true}, &result); err != nil {
		return nil
	}
	list, _ := result.Result.Value.([]any)
	var out []string
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// browserUpload puts a file from Phoenix's library (never an arbitrary path on disk) into a
// file input. ref may be the file input itself, or any control: if it is not a file input, the
// page's first file input is used, because sites hide the real input behind a styled button.
func browserUpload(ctx context.Context, state browserTabState, session *browserSession, args browser.UploadArgs) (browser.Output, error) {
	lib, err := openLibrary()
	if err != nil {
		return browser.Output{}, errors.New("Phoenix's file library is unavailable")
	}
	node, data, err := resolveLibraryFile(lib, args.File)
	if err != nil {
		return browser.Output{}, err
	}
	if library.Dangerous(node.Name) {
		return browser.Output{}, errors.New("Phoenix will not upload an executable or script file")
	}
	if len(data) == 0 || len(data) > 25<<20 {
		return browser.Output{}, errors.New("the file is empty or larger than 25 MB")
	}
	// The browser reads the file when the form is submitted, not when it is attached, so it must
	// stay on disk while the page is open. It lives in one private folder that is wiped when
	// Phoenix starts and when it quits.
	base, err := uploadDir()
	if err != nil {
		return browser.Output{}, errors.New("could not prepare the file for upload")
	}
	dir, err := os.MkdirTemp(base, "u-") // its own folder: a page may still hold an earlier upload open
	if err != nil {
		return browser.Output{}, errors.New("could not prepare the file for upload")
	}
	path := filepath.Join(dir, library.SafeName(node.Name))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return browser.Output{}, errors.New("could not prepare the file for upload")
	}

	params := map[string]any{"files": []string{path}}
	targeted := false
	if args.Ref != "" {
		ref, err := snapshotRefForOperation(session, args.Ref, state.Epoch)
		if err != nil {
			return browser.Output{}, err
		}
		var desc domNodeDescription
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.describeNode", map[string]any{"backendNodeId": ref.BackendNodeID}, &desc); err == nil && isFileInput(desc.Node) {
			params["backendNodeId"] = ref.BackendNodeID
			targeted = true
		}
	}
	if !targeted {
		id, err := firstFileInput(ctx, state)
		if err != nil {
			return browser.Output{}, err
		}
		params["nodeId"] = id
	}
	if _, _, err := cdpCall(ctx, state.ID, state.Epoch, "DOM.setFileInputFiles", params); err != nil {
		return browser.Output{}, err
	}
	return browser.Output{Text: fmt.Sprintf("Attached %s (%s) to the page's file field. Check the page shows it, then continue.", node.Path, humanBytes(int64(len(data))))}, nil
}

var (
	uploadOnce sync.Once
	uploadPath string
	uploadErr  error
)

func uploadDir() (string, error) {
	uploadOnce.Do(func() {
		base := filepath.Join(os.TempDir(), "phoenix-uploads")
		_ = os.RemoveAll(base) // leftovers from a run that did not quit cleanly
		uploadErr = os.MkdirAll(base, 0o700)
		uploadPath = base
	})
	return uploadPath, uploadErr
}

// cleanupUploads removes the files handed to pages; called when Phoenix quits.
func cleanupUploads() {
	if uploadPath != "" {
		_ = os.RemoveAll(uploadPath)
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

func isFileInput(n domNode) bool {
	if strings.ToLower(n.LocalName) != "input" {
		return false
	}
	for i := 0; i+1 < len(n.Attributes); i += 2 {
		if strings.EqualFold(n.Attributes[i], "type") && strings.EqualFold(n.Attributes[i+1], "file") {
			return true
		}
	}
	return false
}

// firstFileInput finds the page's file inputs and returns the first one's node id.
func firstFileInput(ctx context.Context, state browserTabState) (int, error) {
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.getDocument", map[string]any{"depth": 0}, &doc); err != nil {
		return 0, err
	}
	var found struct {
		NodeIDs []int `json:"nodeIds"`
	}
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "DOM.querySelectorAll", map[string]any{"nodeId": doc.Root.NodeID, "selector": "input[type=file]"}, &found); err != nil {
		return 0, err
	}
	if len(found.NodeIDs) == 0 {
		return 0, errors.New("this page has no file field. Click the page's upload or attach button first, then try again")
	}
	return found.NodeIDs[0], nil
}

// resolveLibraryFile turns what the agent says ("resume", "cv.pdf", "documents/cv.pdf") into
// one library file. A kind or words that match several files pick the newest, since the user's
// latest resume is almost always the one they mean.
func resolveLibraryFile(lib *library.Store, want string) (library.Node, []byte, error) {
	want = strings.TrimSpace(want)
	if n, data, err := lib.Bytes(want); err == nil {
		return n, data, nil
	}
	var hits []library.Node
	collect := func(h []library.Hit, err error) {
		if err != nil {
			return
		}
		for _, x := range h {
			if !x.Dir {
				hits = append(hits, x.Node)
			}
		}
	}
	collect(lib.Search(want, "", 20))
	if len(hits) == 0 {
		collect(lib.Search("", want, 20)) // want names a kind, such as resume
	}
	if len(hits) == 0 {
		return library.Node{}, nil, fmt.Errorf("no file in Phoenix's library matches %q. Ask the user to add it in the Files view, or list files with files_list", want)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if !hits[i].Modified.Equal(hits[j].Modified) {
			return hits[i].Modified.After(hits[j].Modified)
		}
		return hits[i].ID > hits[j].ID // added later
	})
	n, data, err := lib.Bytes(hits[0].Path)
	return n, data, err
}

// ── New tabs opened by an action ───────────────────────────────────────────

type tabMarkKey struct{}

// markTabs notes the next tab id before an action, so the feedback after it can say that the
// action opened a new tab (an Apply link opening the employer's site, a popup).
func markTabs(ctx context.Context) context.Context {
	v, err := browserOnUI(ctx, func() (any, error) { return nextID, nil })
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, tabMarkKey{}, v.(int))
}

// newTabsSince describes tabs created at or after the marked id.
func newTabsSince(ctx context.Context) string {
	mark, ok := ctx.Value(tabMarkKey{}).(int)
	if !ok {
		return ""
	}
	list, err := browserTabs(ctx)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, t := range list {
		if t.ID >= mark && t.Kind == kindWeb {
			fmt.Fprintf(&b, "\nA new tab opened: tab %d %q at %s%s. Continue there (browser_snapshot tab_id %d); the previous tab is still open.", t.ID, t.Title, t.URL, activeNote(t), t.ID)
		}
	}
	return b.String()
}

func activeNote(t browserTabState) string {
	if t.Active {
		return " (now the active tab)"
	}
	return ""
}

// ── Embedded frames ────────────────────────────────────────────────────────

// frameNotice lists embedded frames when the page's own outline came up thin, because
// application forms often live in an iframe whose controls the main outline cannot show.
func frameNotice(ctx context.Context, state browserTabState) string {
	var tree struct {
		FrameTree struct {
			ChildFrames []struct {
				Frame struct {
					URL string `json:"url"`
				} `json:"frame"`
			} `json:"childFrames"`
		} `json:"frameTree"`
	}
	if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Page.getFrameTree", map[string]any{}, &tree); err != nil {
		return ""
	}
	var urls []string
	for _, c := range tree.FrameTree.ChildFrames {
		u := c.Frame.URL
		if validBrowserURL(u) && !noisyFrame(u) {
			urls = append(urls, safeTabURL(u))
		}
		if len(urls) == 4 {
			break
		}
	}
	if len(urls) == 0 {
		return ""
	}
	b, _ := json.Marshal(urls)
	return "Embedded frames on this page (their controls are not in this list): " + string(b) +
		". If the form you need is inside one, open that address in the tab with browser_navigate and continue there.\n"
}

func noisyFrame(u string) bool {
	h := browserHost(u)
	for _, n := range []string{"doubleclick", "googletagmanager", "google.com/recaptcha", "recaptcha", "facebook.com/tr", "analytics", "adsystem", "criteo", "hcaptcha", "challenges.cloudflare"} {
		if strings.Contains(h, n) || strings.Contains(strings.ToLower(u), n) {
			return true
		}
	}
	return false
}
