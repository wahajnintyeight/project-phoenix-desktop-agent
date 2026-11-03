// Package quick decides which one-click actions Phoenix offers for the page the user is on.
//
// The aim is less typing: the common request for a kind of page is one click, and it is
// worded once, here, instead of by the user each time. Classification uses only cheap signals
// read locally from the page (address, title, structured-data types, form fields); nothing is
// sent to a model to choose the buttons. Everything here is pure, so it is tested without a
// browser.
package quick

import (
	"net/url"
	"strings"
)

// Kind is what a page looks like it is. It is a guess, shown to the user as such.
type Kind string

const (
	KindStart   Kind = "start"   // home, settings, blank tab: nothing to read
	KindGeneric Kind = "generic" // a page we could not tell more about
	KindArticle Kind = "article"
	KindJob     Kind = "job"
	KindProduct Kind = "product"
	KindForm    Kind = "form"
	KindLogin   Kind = "login"
	KindVideo   Kind = "video"
	KindSearch  Kind = "search"
)

// Signals are the facts read from a page. URL and Title come from the tab; the rest from a
// fixed script run in the page (see cmd/spike/actions_windows.go).
type Signals struct {
	URL        string
	Title      string
	OGType     string   // og:type
	LDTypes    []string // schema.org @type values from JSON-LD
	Fields     int      // visible form fields
	Password   bool     // a password field is visible
	ArticleLen int      // characters inside the first <article>
	Video      bool
	Price      bool
	SelLen     int // characters the user has selected
}

// Action is one button. Prompt is what the model is told; Label is what the user sees and
// what appears as their message. Page is true when the action needs the page's text.
type Action struct {
	ID     string
	Label  string
	Prompt string
	Page   bool
}

// MaxActions is how many buttons one context shows. More is a menu, not a shortcut.
const MaxActions = 6

var jobHosts = []string{
	"greenhouse.io", "lever.co", "myworkdayjobs.com", "ashbyhq.com", "workable.com",
	"smartrecruiters.com", "indeed.", "glassdoor.", "wellfound.com", "rozee.pk", "bayt.com",
}

var searchHosts = []string{"google.", "bing.com", "duckduckgo.com", "search.brave.com", "yahoo.com", "ecosia.org"}

// Classify guesses the kind of page. Order matters: the most specific signal wins.
func Classify(s Signals) Kind {
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return KindStart
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	title := strings.ToLower(s.Title)
	has := func(want ...string) bool {
		for _, t := range s.LDTypes {
			for _, w := range want {
				if strings.EqualFold(t, w) {
					return true
				}
			}
		}
		return false
	}
	hostIn := func(list []string) bool {
		for _, h := range list {
			if strings.Contains(host, h) {
				return true
			}
		}
		return false
	}

	switch {
	case has("JobPosting"),
		hostIn(jobHosts),
		strings.Contains(host, "linkedin.") && strings.HasPrefix(path, "/jobs"),
		containsAny(path, "/job/", "/jobs/", "/careers/", "/vacancy", "/vacancies") && containsAny(title, "job", "hiring", "career", "apply", "vacanc", "engineer", "manager", "developer"):
		return KindJob
	case strings.Contains(host, "youtube.") && strings.HasPrefix(path, "/watch"),
		strings.Contains(host, "vimeo.com"),
		strings.HasPrefix(strings.ToLower(s.OGType), "video"):
		return KindVideo
	case hostIn(searchHosts) && (path == "/search" || strings.Contains(u.RawQuery, "q=")):
		return KindSearch
	case s.Password && s.Fields <= 4:
		return KindLogin
	case has("Product", "Offer") || s.Price || strings.EqualFold(s.OGType, "product"):
		return KindProduct
	case has("Article", "NewsArticle", "BlogPosting", "ScholarlyArticle", "TechArticle", "Report") || strings.EqualFold(s.OGType, "article") || s.ArticleLen >= 2500:
		return KindArticle
	case s.Fields >= 4:
		return KindForm
	}
	return KindGeneric
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// Label names the kind for the user: a guess, so the UI words it as "Looks like …".
func Label(k Kind, hasSelection bool) string {
	if hasSelection {
		return "Selected text"
	}
	switch k {
	case KindArticle:
		return "Article"
	case KindJob:
		return "Job posting"
	case KindProduct:
		return "Product page"
	case KindForm:
		return "Form"
	case KindLogin:
		return "Sign-in page"
	case KindVideo:
		return "Video"
	case KindSearch:
		return "Search results"
	case KindGeneric:
		return "Page"
	}
	return "Start"
}

// For returns the buttons for a kind, most useful first (the first is drawn as the primary
// one). With a selection, the actions on that text come first.
func For(k Kind, hasSelection bool) []Action {
	var ids []string
	if hasSelection && k != KindStart {
		ids = append(ids, "sel-explain", "sel-simplify", "sel-translate", "sel-rewrite")
	}
	ids = append(ids, byKind[k]...)
	out := make([]Action, 0, MaxActions)
	for _, id := range ids {
		if a, ok := catalog[id]; ok {
			out = append(out, a)
		}
		if len(out) == MaxActions {
			break
		}
	}
	return out
}

// Lookup finds an action by id. The UI sends only the id; the wording stays here.
func Lookup(id string) (Action, bool) {
	a, ok := catalog[id]
	return a, ok
}

var byKind = map[Kind][]string{
	KindStart:   {"capabilities", "plan", "followup"},
	KindGeneric: {"summarise", "keypoints", "simplify", "translate", "nextstep"},
	KindArticle: {"tldr", "keypoints", "simplify", "factcheck", "counter", "translate"},
	KindJob:     {"role", "skills", "coverletter", "redflags", "recruiter", "questions"},
	KindProduct: {"proscons", "worth", "lookfor", "questions-buy"},
	KindForm:    {"formask", "formdraft", "formrisk"},
	KindLogin:   {"legit", "summarise"},
	KindVideo:   {"videoabout", "videocomment", "keypoints"},
	KindSearch:  {"bestresult", "searchsum", "refine"},
}

const honest = " If you need facts about me that are not on the page, ask up to three short questions instead of inventing them."

var catalog = map[string]Action{
	// No page needed.
	"capabilities": {ID: "capabilities", Label: "What can you do?", Prompt: "In a few short bullets, say what you can help me with in this app right now, and what you cannot do yet."},
	"plan":         {ID: "plan", Label: "Plan my job search", Prompt: "Help me plan a focused job search for this week. Ask me up to three short questions first (role, location, what I have), then give a concrete plan."},
	"followup":     {ID: "followup", Label: "Write a follow-up email", Prompt: "Write a short, polite follow-up email after a job interview, with placeholders in [brackets] for names and details."},

	// Any page.
	"summarise": {ID: "summarise", Label: "Summarise", Page: true, Prompt: "Summarise this page in a short paragraph, then list anything I should act on."},
	"keypoints": {ID: "keypoints", Label: "Key points", Page: true, Prompt: "List the key points of this page as short bullets."},
	"simplify":  {ID: "simplify", Label: "Explain simply", Page: true, Prompt: "Explain what this page says in plain, simple language."},
	"translate": {ID: "translate", Label: "Translate", Page: true, Prompt: "Translate the main content of this page into English. If it is already English, say so and give a one-line summary instead."},
	"nextstep":  {ID: "nextstep", Label: "What should I do here?", Page: true, Prompt: "What is this page asking or offering, and what is the most sensible next step for me?"},

	// Selected text.
	"sel-explain":   {ID: "sel-explain", Label: "Explain selection", Page: true, Prompt: "Explain the text I have selected on the page. Use the rest of the page only for context."},
	"sel-simplify":  {ID: "sel-simplify", Label: "Simplify selection", Page: true, Prompt: "Rewrite the text I have selected on the page in plain, simple language."},
	"sel-translate": {ID: "sel-translate", Label: "Translate selection", Page: true, Prompt: "Translate the text I have selected on the page into English (or into Urdu if it is already English)."},
	"sel-rewrite":   {ID: "sel-rewrite", Label: "Improve selection", Page: true, Prompt: "Rewrite the text I have selected on the page so it is clearer and better written, keeping its meaning."},

	// Articles.
	"tldr":      {ID: "tldr", Label: "TL;DR", Page: true, Prompt: "Give me a three-sentence TL;DR of this article."},
	"factcheck": {ID: "factcheck", Label: "Fact-check claims", Page: true, Prompt: "List the main factual claims in this article and say which are well supported, which are uncertain, and which I should verify. Say plainly that you cannot browse to check them."},
	"counter":   {ID: "counter", Label: "Other side", Page: true, Prompt: "What is the strongest counter-argument to this article's main point?"},

	// Job postings.
	"role":        {ID: "role", Label: "Summarise the role", Page: true, Prompt: "Summarise this job: title, company, location or remote, pay if stated, main responsibilities, and must-have requirements."},
	"skills":      {ID: "skills", Label: "Skills they want", Page: true, Prompt: "List the skills and experience this job asks for, split into must-have and nice-to-have."},
	"coverletter": {ID: "coverletter", Label: "Draft cover letter", Page: true, Prompt: "Write a concise cover letter for the job on this page." + honest},
	"redflags":    {ID: "redflags", Label: "Red flags?", Page: true, Prompt: "Read this job posting critically. Point out red flags, vague claims, unrealistic requirements, and anything missing (pay, scope, team)."},
	"recruiter":   {ID: "recruiter", Label: "Message the recruiter", Page: true, Prompt: "Draft a short, friendly message to the recruiter or hiring manager for this job, with placeholders in [brackets] for names." + honest},
	"questions":   {ID: "questions", Label: "Interview questions", Page: true, Prompt: "List the questions I am most likely to be asked for this job, each with a one-line hint on how to answer."},

	// Products.
	"proscons":      {ID: "proscons", Label: "Pros and cons", Page: true, Prompt: "Give the pros and cons of this product, based only on this page."},
	"worth":         {ID: "worth", Label: "Is it worth it?", Page: true, Prompt: "Is this product good value? Use only what is on the page, and say what is missing."},
	"lookfor":       {ID: "lookfor", Label: "What to check", Page: true, Prompt: "What should I check before buying this (specs, seller, returns, hidden costs)?"},
	"questions-buy": {ID: "questions-buy", Label: "Questions to ask", Page: true, Prompt: "What questions should I ask the seller before buying this?"},

	// Forms.
	"formask":   {ID: "formask", Label: "What does it ask?", Page: true, Prompt: "Explain what this form is asking for, field by field, and which fields are required."},
	"formdraft": {ID: "formdraft", Label: "Draft my answers", Page: true, Prompt: "Draft answers for the fields on this form that need written text." + honest + " I will copy them in myself."},
	"formrisk":  {ID: "formrisk", Label: "Anything risky?", Page: true, Prompt: "Is there anything risky or unusual in this form (data I am giving away, consent, fees, fine print)?"},

	// Sign-in pages.
	"legit": {ID: "legit", Label: "Does this look legit?", Page: true, Prompt: "Does this sign-in page look legitimate? Look at the address, wording and anything odd. Never ask me for a password."},

	// Video.
	"videoabout":   {ID: "videoabout", Label: "What is this about?", Page: true, Prompt: "From the title, description and any text visible on this page, say what this video is about. Note that you cannot watch it."},
	"videocomment": {ID: "videocomment", Label: "Draft a comment", Page: true, Prompt: "Draft a short, thoughtful comment I could leave on this video, based on what the page says."},

	// Search results.
	"bestresult": {ID: "bestresult", Label: "Which result is best?", Page: true, Prompt: "Looking at these search results, which looks most useful for what I searched, and why?"},
	"searchsum":  {ID: "searchsum", Label: "Summarise results", Page: true, Prompt: "Summarise what these search results say, and note where they disagree."},
	"refine":     {ID: "refine", Label: "Better search", Page: true, Prompt: "Suggest three better searches I could try instead, and say what each would find."},
}
