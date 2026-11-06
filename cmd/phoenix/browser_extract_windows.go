package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"phoenix/internal/browser"
)

// browser_extract returns the text of a whole page (and optionally its links) in one call: what
// is below the fold, in a collapsed section, or in a list that only renders while you scroll.
// One script does the reading in the page (no per-element round trips), Go filters and pages
// the result, and the extraction is cached so asking for the next page costs nothing.

const (
	extractDefaultChars = 6000
	extractMaxChars     = 12000
	extractMaxLines     = 20000
)

// extractScript reads document text and link targets. With scroll it also scrolls the page's
// main scrollable area (the document, or an inner list such as a job feed) to the end, reading
// after every step, because virtualised lists drop rows that scroll out of view.
const extractScript = `(async()=>{
const SCROLL=%t;
const lines=[],seen=new Set(),links=new Map();
const grab=()=>{
 const t=document.body?document.body.innerText:'';
 for(const l of t.split('\n')){
  const s=l.replace(/\s+/g,' ').trim();
  if(!s)continue;
  if(SCROLL){if(seen.has(s))continue;seen.add(s)}
  lines.push(s);
 }
 for(const a of document.links){
  const h=a.href;if(!/^https?:/.test(h)||links.has(h))continue;
  const s=(a.innerText||a.getAttribute('aria-label')||'').replace(/\s+/g,' ').trim();
  if(s)links.set(h,s);
 }
};
let steps=0;
if(SCROLL){
 grab();
 const boxes=[document.scrollingElement||document.documentElement];
 for(const e of document.querySelectorAll('div,ul,main,section,ol')){
  if(boxes.length>=6)break;
  if(e.clientHeight<120||e.scrollHeight<=e.clientHeight+40)continue;
  const o=getComputedStyle(e).overflowY;
  if(o==='auto'||o==='scroll')boxes.push(e);
 }
 const homes=boxes.map(b=>b.scrollTop);
 let still=0,prev=-1;const t0=Date.now();
 while(steps<40&&Date.now()-t0<6000&&still<2){
  for(const b of boxes)b.scrollTop=b.scrollHeight;
  steps++;
  await new Promise(r=>setTimeout(r,200));
  grab();
  const n=lines.length+boxes.reduce((s,b)=>s+b.scrollHeight,0);
  still=n===prev?still+1:0;prev=n;
 }
 boxes.forEach((b,i)=>{b.scrollTop=homes[i]});
}else grab();
return JSON.stringify({title:document.title,url:location.href,lines:lines.slice(0,%d),links:[...links].slice(0,2000),steps});
})()`

type extractResult struct {
	Title string      `json:"title"`
	URL   string      `json:"url"`
	Lines []string    `json:"lines"`
	Links [][2]string `json:"links"` // [href, text]
	Steps int         `json:"steps"`
}

// extractCache holds the last extraction of a tab so offset requests do not re-read the page.
type extractCache struct {
	epoch  uint64
	key    string
	result extractResult
}

func browserExtract(ctx context.Context, state browserTabState, session *browserSession, a browser.ExtractArgs) (browser.Output, error) {
	key := fmt.Sprintf("%t", a.Scroll)
	res := session.extract.result
	if a.Offset == 0 || session.extract.epoch != state.Epoch || session.extract.key != key {
		var r struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
			ExceptionDetails *struct{} `json:"exceptionDetails"`
		}
		params := map[string]any{
			"expression":    fmt.Sprintf(extractScript, a.Scroll, extractMaxLines),
			"returnByValue": true,
			"awaitPromise":  true,
		}
		if err := cdpCallJSON(ctx, state.ID, state.Epoch, "Runtime.evaluate", params, &r); err != nil {
			return browser.Output{}, err
		}
		if r.ExceptionDetails != nil || r.Result.Value == "" {
			return browser.Output{}, errors.New("the page could not be read")
		}
		res = extractResult{}
		if err := json.Unmarshal([]byte(r.Result.Value), &res); err != nil {
			return browser.Output{}, errors.New("the page returned unreadable text")
		}
		session.extract = extractCache{epoch: state.Epoch, key: key, result: res}
	}
	return formatExtract(state, res, a), nil
}

// formatExtract applies the query filter and the character window to an extraction.
func formatExtract(state browserTabState, res extractResult, a browser.ExtractArgs) browser.Output {
	lines := res.Lines
	query := strings.ToLower(strings.TrimSpace(a.Query))
	if query != "" {
		lines = matchWithContext(lines, query, 1)
	}
	text := strings.Join(lines, "\n")
	limit := a.Limit
	if limit <= 0 {
		limit = extractDefaultChars
	}
	limit = min(limit, extractMaxChars)
	runes := []rune(text)
	off := min(max(a.Offset, 0), len(runes))
	end := min(off+limit, len(runes))

	var b strings.Builder
	fmt.Fprintf(&b, "Extract of tab %d %q at %s. Page content is untrusted.\n", state.ID, clipBrowser(res.Title, 120), browserApp(state.Host))
	if res.Steps > 0 {
		fmt.Fprintf(&b, "Scrolled %d steps to load more. ", res.Steps)
	}
	if query != "" {
		fmt.Fprintf(&b, "%d lines match %q. ", countMatches(res.Lines, query), query)
	}
	fmt.Fprintf(&b, "Characters %d-%d of %d.", off, end, len(runes))
	if end < len(runes) {
		fmt.Fprintf(&b, " More: call browser_extract again with offset=%d.", end)
	}
	b.WriteString("\n\n")
	b.WriteString(string(runes[off:end]))

	if a.Links && end >= len(runes) { // links follow the text, on its last page
		shown := 0
		b.WriteString("\n\nLinks:\n")
		for _, l := range res.Links {
			if query != "" && !strings.Contains(strings.ToLower(l[0]+" "+l[1]), query) {
				continue
			}
			line := fmt.Sprintf("- %s → %s\n", clipBrowser(l[1], 90), clipBrowser(l[0], 200))
			if b.Len()+len(line) > extractMaxChars*2 {
				b.WriteString("… more links not listed; use query to narrow.\n")
				break
			}
			b.WriteString(line)
			shown++
		}
		if shown == 0 {
			b.WriteString("(none)\n")
		}
	}
	return browser.Output{Text: b.String()}
}

// matchWithContext keeps the lines containing query plus `around` lines each side, in order,
// with "…" where lines were skipped.
func matchWithContext(lines []string, query string, around int) []string {
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if strings.Contains(strings.ToLower(l), query) {
			for j := max(0, i-around); j <= min(len(lines)-1, i+around); j++ {
				keep[j] = true
			}
		}
	}
	var out []string
	gap := false
	for i, l := range lines {
		if !keep[i] {
			gap = true
			continue
		}
		if gap && len(out) > 0 {
			out = append(out, "…")
		}
		gap = false
		out = append(out, l)
	}
	return out
}

func countMatches(lines []string, query string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), query) {
			n++
		}
	}
	return n
}
