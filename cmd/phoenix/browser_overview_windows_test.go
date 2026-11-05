package main

import (
	"strings"
	"testing"
)

func TestAXFieldValueHidesSensitiveAndMasked(t *testing.T) {
	if got := axFieldValue("textbox", "Search jobs", "golang"); got != "golang" {
		t.Fatalf("ordinary field value dropped: %q", got)
	}
	for _, c := range []struct{ role, name, value string }{
		{"textbox", "Password", "hunter2"},
		{"textbox", "Card number", "4111"},
		{"textbox", "Verification code", "123456"},
		{"textbox", "Name", "••••••"},
		{"button", "Submit", "x"},
	} {
		if got := axFieldValue(c.role, c.name, c.value); got != "" {
			t.Errorf("%+v leaked %q", c, got)
		}
	}
}

func TestPageOverviewDescribesStateAndHidesSecrets(t *testing.T) {
	p := pageOverview{
		pageStatus: pageStatus{URL: "https://example.com/jobs", Title: "Jobs", Ready: "loading", ScrollY: 0, ViewW: 1200, ViewH: 800, PageH: 3000, Frames: 1,
			Dialogs: []pageDialog{{Label: "Cookies", Modal: true}}, Blockers: []string{"sign-in-form"}},
		Fields:   []pageField{{Label: "Email", Type: "email", Value: "a@b.c", Required: true}, {Label: "Password", Type: "password", Secret: true}},
		Headings: []pageHeading{{Level: 1, Text: "Open roles", InView: true}},
	}
	got := formatPageOverview(browserTabState{ID: 3}, p)
	for _, want := range []string{"Modal dialog open", "Loading state: loading", "2200 px more below", "sign-in form", `= "a@b.c"`, "value hidden", "▸h1 Open roles"} {
		if !strings.Contains(got, want) {
			t.Errorf("overview is missing %q:\n%s", want, got)
		}
	}
}
