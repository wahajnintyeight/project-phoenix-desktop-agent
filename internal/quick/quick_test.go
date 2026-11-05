package quick

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		s    Signals
		want Kind
	}{
		{"blank", Signals{URL: "about:blank"}, KindStart},
		{"bad url", Signals{URL: "::"}, KindStart},
		{"json-ld job", Signals{URL: "https://example.com/x", LDTypes: []string{"JobPosting"}}, KindJob},
		{"ats host", Signals{URL: "https://boards.greenhouse.io/acme/jobs/1"}, KindJob},
		{"linkedin jobs", Signals{URL: "https://www.linkedin.com/jobs/view/123"}, KindJob},
		{"linkedin feed is not a job", Signals{URL: "https://www.linkedin.com/feed/"}, KindGeneric},
		{"careers path needs a job-like title", Signals{URL: "https://acme.com/careers/senior-engineer", Title: "Senior Engineer | Acme"}, KindJob},
		{"careers path with other title", Signals{URL: "https://acme.com/careers/", Title: "Acme"}, KindGeneric},
		{"youtube watch", Signals{URL: "https://www.youtube.com/watch?v=abc"}, KindVideo},
		{"youtube home", Signals{URL: "https://www.youtube.com/"}, KindGeneric},
		{"search", Signals{URL: "https://duckduckgo.com/?q=go+tests"}, KindSearch},
		{"google search", Signals{URL: "https://www.google.com/search?q=x"}, KindSearch},
		{"login", Signals{URL: "https://acme.com/signin", Password: true, Fields: 2}, KindLogin},
		{"signup form with password is a form", Signals{URL: "https://acme.com/join", Password: true, Fields: 9}, KindForm},
		{"product ld", Signals{URL: "https://shop.example/p/1", LDTypes: []string{"Product"}}, KindProduct},
		{"product price", Signals{URL: "https://shop.example/p/1", Price: true}, KindProduct},
		{"article ld", Signals{URL: "https://news.example/a", LDTypes: []string{"NewsArticle"}}, KindArticle},
		{"article og", Signals{URL: "https://news.example/a", OGType: "article"}, KindArticle},
		{"long article tag", Signals{URL: "https://blog.example/a", ArticleLen: 4000}, KindArticle},
		{"short article tag", Signals{URL: "https://blog.example/a", ArticleLen: 300}, KindGeneric},
		{"form", Signals{URL: "https://acme.com/apply", Fields: 6}, KindForm},
		{"plain", Signals{URL: "https://example.com/"}, KindGeneric},
		{"file url", Signals{URL: "file:///c:/x.html"}, KindStart},
		{"pdf", Signals{URL: "https://acme.com/report.pdf"}, KindPDF},
		{"github repo", Signals{URL: "https://github.com/golang/go"}, KindRepo},
		{"github issue", Signals{URL: "https://github.com/golang/go/issues/12"}, KindIssue},
		{"github home", Signals{URL: "https://github.com/"}, KindGeneric},
		{"stack overflow", Signals{URL: "https://stackoverflow.com/questions/1/x"}, KindQA},
		{"reddit thread", Signals{URL: "https://www.reddit.com/r/go/comments/abc/x/"}, KindQA},
		{"gmail", Signals{URL: "https://mail.google.com/mail/u/0/"}, KindMail},
		{"recipe", Signals{URL: "https://food.example/r/1", LDTypes: []string{"Recipe"}}, KindRecipe},
		{"hotel", Signals{URL: "https://travel.example/h/1", LDTypes: []string{"Hotel"}}, KindTravel},
		{"cart", Signals{URL: "https://shop.example/cart", Price: true}, KindCheckout},
		{"pricing", Signals{URL: "https://saas.example/pricing"}, KindPricing},
		{"docs host", Signals{URL: "https://docs.acme.com/start", CodeBlock: 2, Reading: 3000}, KindDocs},
		{"docs path needs content", Signals{URL: "https://acme.com/docs"}, KindGeneric},
		{"many code blocks", Signals{URL: "https://blog.example/post", CodeBlock: 5}, KindDocs},
		{"linkedin profile", Signals{URL: "https://www.linkedin.com/in/someone"}, KindProfile},
		{"table page", Signals{URL: "https://stats.example/t", TableRows: 40, Reading: 2000}, KindData},
		{"listing by cards", Signals{URL: "https://shop.example/c/shoes", Cards: 20}, KindListing},
		{"listing by schema", Signals{URL: "https://shop.example/c/x", LDTypes: []string{"ItemList"}}, KindListing},
	}
	for _, c := range cases {
		if got := Classify(c.s); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestEveryKindHasValidActions(t *testing.T) {
	kinds := []Kind{KindStart, KindGeneric, KindArticle, KindJob, KindJobSearch, KindProduct, KindForm, KindLogin, KindVideo, KindSearch, KindRepo, KindIssue, KindDocs, KindQA, KindMail, KindRecipe, KindTravel, KindListing, KindCheckout, KindPricing, KindData, KindProfile, KindPDF}
	for _, k := range kinds {
		for _, sel := range []bool{false, true} {
			list := For(k, sel)
			if len(list) == 0 || len(list) > MaxActions {
				t.Fatalf("%s sel=%v: %d actions", k, sel, len(list))
			}
			seen := map[string]bool{}
			for _, a := range list {
				if a.ID == "" || a.Label == "" || a.Prompt == "" {
					t.Errorf("%s: incomplete action %+v", k, a)
				}
				if seen[a.ID] {
					t.Errorf("%s: duplicate action %s", k, a.ID)
				}
				seen[a.ID] = true
				if got, ok := Lookup(a.ID); !ok || got.ID != a.ID {
					t.Errorf("Lookup(%q) failed", a.ID)
				}
			}
		}
	}
	// Every id named in byKind must exist, or For would silently drop it.
	for k, ids := range byKind {
		for _, id := range ids {
			if _, ok := catalog[id]; !ok {
				t.Errorf("%s names unknown action %q", k, id)
			}
		}
	}
}

func TestStartActionsNeedNoPage(t *testing.T) {
	for _, a := range For(KindStart, true) { // a selection means nothing without a page
		if a.Page {
			t.Errorf("start action %s claims to need a page", a.ID)
		}
	}
}

func TestSelectionActionsComeFirst(t *testing.T) {
	list := For(KindJob, true)
	if list[0].ID != "sel-explain" {
		t.Fatalf("first action with a selection = %s", list[0].ID)
	}
	if list[len(list)-1].ID == "sel-rewrite" {
		t.Fatal("kind actions were crowded out entirely")
	}
	if got := For(KindJob, false)[0].ID; got != "role" {
		t.Fatalf("first job action = %s", got)
	}
}

func TestPromptsNeverPromiseActions(t *testing.T) {
	for id, a := range catalog {
		if a.Prompt == "" {
			t.Errorf("%s has no prompt", id)
		}
	}
}

func TestContextNamesTheSite(t *testing.T) {
	got := Context(KindRepo, false, Signals{URL: "https://www.github.com/golang/go"})
	if got != "Repository · github.com" {
		t.Fatalf("got %q", got)
	}
	if got := Context(KindArticle, false, Signals{URL: "https://x.example/a", Site: "The Example Times"}); got != "Article · The Example Times" {
		t.Fatalf("got %q", got)
	}
	if got := Context(KindStart, false, Signals{}); got != "Start" {
		t.Fatalf("got %q", got)
	}
}
