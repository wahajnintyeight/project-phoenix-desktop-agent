package main

import (
	"strings"
	"time"

	"phoenix/internal/quick"
)

// Quick actions: the bar of one-click buttons under the toolbar. Go decides which buttons the
// current tab gets (internal/quick) and the UI only draws them. A click comes back as
// `g act <id>`; the wording of the request lives in Go, so the UI cannot send arbitrary text
// through this path.
//
// To pick the buttons, a fixed script reads a few facts from the page (structured-data types,
// form fields, the selection length). That stays on this machine: it is not sent to a model,
// not logged, and only the resulting kind is kept.

const probeScript = `(() => {
  const meta = (n) => {
    const e = document.querySelector('meta[property="' + n + '"],meta[name="' + n + '"]');
    return e && e.content ? String(e.content).slice(0, 40) : "";
  };
  const ld = [];
  const walk = (o, d) => {
    if (!o || d > 4 || ld.length > 16) return;
    if (Array.isArray(o)) { for (const x of o.slice(0, 20)) walk(x, d + 1); return; }
    if (typeof o !== "object") return;
    for (const t of [].concat(o["@type"] || [])) if (typeof t === "string") ld.push(t.slice(0, 40));
    if (o["@graph"]) walk(o["@graph"], d + 1);
  };
  try {
    const scripts = Array.from(document.querySelectorAll('script[type="application/ld+json"]')).slice(0, 6);
    for (const s of scripts) { const raw = s.textContent || ""; if (raw.length < 200000) walk(JSON.parse(raw), 0); }
  } catch (e) {}
  let fields = 0, password = false;
  for (const i of Array.from(document.querySelectorAll("input,textarea,select")).slice(0, 200)) {
    const ty = (i.type || "").toLowerCase();
    if (ty === "hidden" || ty === "submit" || ty === "button" || ty === "image" || ty === "reset" || ty === "search") continue;
    if (i.offsetParent === null) continue;
    fields++;
    if (ty === "password") password = true;
  }
  const art = document.querySelector("article");
  const root = document.querySelector("main,[role=main]") || document.body;
  // Counted with capped selectors: a huge page costs about the same as a small one.
  let rows = 0;
  for (const t of Array.from(document.querySelectorAll("table")).slice(0, 3)) rows += Math.min(t.rows ? t.rows.length : 0, 200);
  const cards = document.querySelectorAll('article,[class*="card" i],[class*="result" i],[data-testid*="card" i],[itemtype*="Product"],li>a>img');
  const h1 = document.querySelector("h1");
  return {
    site: meta("og:site_name"),
    h1: h1 && h1.textContent ? String(h1.textContent).trim().replace(/\s+/g, " ").slice(0, 80) : "",
    desc: meta("og:description") || meta("description"),
    tableRows: rows,
    codeBlock: Math.min(document.querySelectorAll("pre").length, 50),
    cards: Math.min(cards.length, 60),
    reading: root && root.textContent ? Math.min(root.textContent.length, 200000) : 0,
    og: meta("og:type"),
    ld: ld,
    fields: fields,
    password: password,
    articleLen: art ? (art.textContent || "").length : 0,
    video: !!document.querySelector("video"),
    price: !!document.querySelector('[itemprop="price"],meta[property="product:price:amount"],meta[property="og:price:amount"]'),
    selLen: String(getSelection() || "").length
  };
})()`

type probeSnap struct {
	Site       string   `json:"site"`
	H1         string   `json:"h1"`
	Desc       string   `json:"desc"`
	TableRows  int      `json:"tableRows"`
	CodeBlock  int      `json:"codeBlock"`
	Cards      int      `json:"cards"`
	Reading    int      `json:"reading"`
	OG         string   `json:"og"`
	LD         []string `json:"ld"`
	Fields     int      `json:"fields"`
	Password   bool     `json:"password"`
	ArticleLen int      `json:"articleLen"`
	Video      bool     `json:"video"`
	Price      bool     `json:"price"`
	SelLen     int      `json:"selLen"`
}

type actionJSON struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Primary bool   `json:"primary,omitempty"`
}

type actionsJSON struct {
	Context string       `json:"context"`
	Items   []actionJSON `json:"items"`
}

var quickState struct {
	key string // what the bar shows now, so an unchanged answer redraws nothing
}

// refreshActions works out the buttons for the active tab and sends them to the bar. It is
// cheap enough to call on every tab switch, navigation and when the pointer nears the bar.
// fresh is true when the tab or address just changed: the buttons for the old page would be
// wrong, so a guess from the address and title is shown at once while the probe runs. A
// refresh of the same page keeps what is showing until the probe answers, so the buttons do
// not flicker under the pointer.
func refreshActions(fresh bool) {
	t := activeTab()
	if t == nil {
		return
	}
	if t.kind != kindWeb || t.wv == nil || t.url == "" || t.url == "about:blank" {
		pushActions(quick.KindStart, false, quick.Signals{})
		return
	}
	id, addr, title := t.id, t.url, t.title
	if fresh {
		sig := quick.Signals{URL: addr, Title: title}
		pushActions(quick.Classify(sig), false, sig)
	}
	if t.loading {
		return // the document in the tab is the old one; the probe runs when the load ends
	}
	var snap probeSnap
	runScript(t, probeScript, &snap, func(err error) {
		if err != nil || activeID != id {
			return // the page is busy or the user has moved on: keep what is showing
		}
		sig := quick.Signals{
			URL: addr, Title: title, OGType: snap.OG, LDTypes: snap.LD, Fields: snap.Fields,
			Password: snap.Password, ArticleLen: snap.ArticleLen, Video: snap.Video, Price: snap.Price,
			SelLen: snap.SelLen, Site: snap.Site, H1: snap.H1, Desc: snap.Desc, TableRows: snap.TableRows,
			CodeBlock: snap.CodeBlock, Cards: snap.Cards, Reading: snap.Reading,
		}
		pushActions(quick.Classify(sig), snap.SelLen >= 3, sig)
	})
}

func pushActions(kind quick.Kind, selection bool, sig quick.Signals) {
	list := quick.For(kind, selection)
	items := make([]actionJSON, len(list))
	context := quick.Context(kind, selection, sig)
	var key strings.Builder
	key.WriteString(string(kind) + "|" + context)
	if selection {
		key.WriteString("+sel")
	}
	for i, a := range list {
		items[i] = actionJSON{ID: a.ID, Label: a.Label, Primary: i == 0}
	}
	if k := key.String(); k == quickState.key {
		return
	} else {
		quickState.key = k
	}
	post("u" + fs + "quick-actions" + fs + "actions" + fs + jsonString(actionsJSON{Context: context, Items: items}))
}

// scheduleActionRefresh re-reads a page once it has had time to render. Many sites build
// their content after the load event, so the first probe can see an empty shell.
func scheduleActionRefresh(tabID int, after time.Duration) {
	time.AfterFunc(after, func() {
		runLater(func() {
			if activeID == tabID {
				refreshActions(false)
			}
		})
	})
}
