package main

import (
	"context"
	"errors"

	"phoenix/internal/browser"
)

func openBrowserTab(ctx context.Context, args browser.OpenTabArgs) (any, error) {
	value, err := browserOnUI(ctx, func() (any, error) {
		t := &tab{id: nextID, kind: kindHome, title: "Home"}
		nextID++
		if args.URL != "" {
			t.kind, t.title, t.url, t.loading = kindWeb, "", args.URL, true
			if !prepareWebTab(t) {
				return nil, errors.New("could not create the browser tab")
			}
		}
		if args.URL != "" {
			markAgentTab(t)
		}
		tabs = append(tabs, t)
		activate(t.id)
		enforceTabCap()
		if args.URL != "" {
			t.cv.Navigate(args.URL)
		}
		return map[string]any{"tab_id": t.id, "active": true, "url": safeTabURL(t.url)}, nil
	})
	return value, err
}

func navigateBrowserTab(ctx context.Context, args browser.NavigateArgs) (any, error) {
	session := browserSessionFor(args.TabID)
	session.opMu.Lock()
	defer session.opMu.Unlock()
	value, err := browserOnUI(ctx, func() (any, error) {
		_, t := findTab(args.TabID)
		if t == nil {
			return nil, errors.New("the browser tab is no longer available")
		}
		if t.kind == kindSettings || t.kind == kindFiles || t.kind == kindTasks {
			return nil, errors.New("open a browser tab before navigating; Settings and Files are not page tabs")
		}
		if t.kind == kindHome {
			t.kind, t.title, t.url, t.loading = kindWeb, "", args.URL, true
			if !prepareWebTab(t) {
				t.kind, t.title, t.url, t.loading = kindHome, "Home", "", false
				return nil, errors.New("could not prepare the browser tab")
			}
			layoutAll()
		}
		t.url, t.loading = args.URL, true
		t.cv.Navigate(args.URL)
		pushTabs()
		if t.id == activeID {
			pushToolbar()
		}
		return map[string]any{"tab_id": t.id, "navigating_to": browserHost(args.URL)}, nil
	})
	if err == nil {
		session.snapshot = browserSnapshot{}
	}
	return value, err
}

func historyBrowserTab(ctx context.Context, args browser.TabArgs, action string) (any, error) {
	session := browserSessionFor(args.TabID)
	session.opMu.Lock()
	defer session.opMu.Unlock()
	value, err := browserOnUI(ctx, func() (any, error) {
		_, t := findTab(args.TabID)
		if t == nil || t.kind != kindWeb || t.wv == nil {
			return nil, errors.New("the browser tab has no web page")
		}
		var operation string
		switch action {
		case "browser_back":
			if !t.canBack {
				return nil, errors.New("there is no previous page in this tab")
			}
			operation = "GoBack"
			if err := hrErr(operation, comCall(t.wv, wvGoBack)); err != nil {
				return nil, err
			}
		case "browser_forward":
			if !t.canFwd {
				return nil, errors.New("there is no next page in this tab")
			}
			operation = "GoForward"
			if err := hrErr(operation, comCall(t.wv, wvGoForward)); err != nil {
				return nil, err
			}
		case "browser_reload":
			operation = "Reload"
			if err := hrErr(operation, comCall(t.wv, wvReload)); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("unsupported history operation")
		}
		return map[string]any{"tab_id": t.id, "operation": operation}, nil
	})
	if err == nil {
		session.snapshot = browserSnapshot{}
	}
	return value, err
}

func switchBrowserTab(ctx context.Context, args browser.TabArgs) (any, error) {
	session := browserSessionFor(args.TabID)
	session.opMu.Lock()
	defer session.opMu.Unlock()
	value, err := browserOnUI(ctx, func() (any, error) {
		_, t := findTab(args.TabID)
		if t == nil {
			return nil, errors.New("the browser tab is no longer available")
		}
		activate(t.id)
		return map[string]any{"tab_id": t.id, "title": t.label(), "active": true}, nil
	})
	return value, err
}

func closeBrowserTab(ctx context.Context, args browser.TabArgs) (any, error) {
	session := browserSessionFor(args.TabID)
	session.opMu.Lock()
	defer session.opMu.Unlock()
	value, err := browserOnUI(ctx, func() (any, error) {
		if _, t := findTab(args.TabID); t == nil {
			return nil, errors.New("the browser tab is no longer available")
		}
		closeTab(args.TabID)
		current := activeTab()
		if current == nil {
			return nil, errors.New("Phoenix could not select a browser tab")
		}
		return map[string]any{"closed_tab_id": args.TabID, "active_tab_id": current.id}, nil
	})
	return value, err
}
