package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"phoenix/internal/config"
	"phoenix/internal/provider"
	"phoenix/internal/providerhub"
	"phoenix/internal/secrets"
)

// Provider management: the Settings tab asks Go to detect, sign in, switch auth mode, store
// keys and test. Everything slow (the CLIs take seconds) runs on a goroutine and reports
// back through runLater; WebView2 calls stay on the UI thread.

var (
	cfgStore   *config.Store // shared by the provider hub and the browser settings
	hub        *providerhub.Hub
	hubErr     error
	testCancel = map[provider.ID]context.CancelFunc{} // UI thread only
)

func initProviders() {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		hubErr = err
		return
	}
	vaultDir, err := secrets.DefaultDir()
	if err != nil {
		hubErr = err
		return
	}
	// Dev-only: keep test keys and settings out of the real profile.
	if dir := os.Getenv("PHOENIX_TEST_PROFILE"); dir != "" {
		cfgPath, vaultDir = filepath.Join(dir, "config.yaml"), filepath.Join(dir, "vault")
	}
	vault, err := secrets.Open(vaultDir)
	if err != nil {
		hubErr = err
		return
	}
	cfgStore, hubErr = config.OpenStore(cfgPath)
	if hubErr != nil {
		return
	}
	hub = providerhub.NewWithStore(cfgStore, vault)
	hub.Trace = logf
}

func openSettings() {
	for _, t := range tabs {
		if t.kind == kindSettings {
			activate(t.id)
			refreshProviders()
			pushBrowser()
			pushMCP("", false)
			pushDesktop()
			return
		}
	}
	t := &tab{id: nextID, kind: kindSettings, title: "Settings"}
	nextID++
	tabs = append(tabs, t)
	activate(t.id)
	refreshProviders()
	pushBrowser()
	pushMCP("", false)
	pushDesktop()
}

type providerView struct {
	provider.Status
	IsDefault bool `json:"isDefault"`
}

// refreshProviders detects every provider in the background and pushes the result, then
// the model lists. Re-detecting is slow (it runs the CLIs), so choosing a model does not use it.
func refreshProviders() { refreshProvidersForce(false) }

func refreshProvidersForce(forceModels bool) {
	if hub == nil {
		post("u" + fs + "settings-error" + fs + "hidden" + fs + "false")
		post("t" + fs + "settings-error" + fs + settingsUnavailable())
		pushQuickModelData("", []modelsView{})
		post("t" + fs + "step-provider-text" + fs + "Provider setup could not load. Open Settings to see the error.")
		return
	}
	post("u" + fs + "settings-error" + fs + "hidden" + fs + "true")
	for _, id := range hub.IDs() {
		post("a" + fs + "prov-" + string(id) + fs + "data-state" + fs + "checking")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		sts := hub.Statuses(ctx)
		setReady(sts)
		pickReadyDefault(sts)
		def := hub.Default()
		quickViews := make([]modelsView, len(sts))
		for i, st := range sts {
			choice := hub.Selected(st.ID)
			kind := "cli"
			models := []provider.Model{}
			if provider.IsHTTP(st.ID) {
				kind = "api"
			} else {
				models = append(models, provider.Model{ID: "", Name: "CLI default"})
			}
			quickViews[i] = modelsView{
				ID: string(st.ID), Name: providerName(st.ID), Models: models,
				Model: choice.Model, Effort: choice.Effort, Ready: st.Ready, Installed: st.Installed, Kind: kind, Loading: st.Ready,
			}
		}
		runLater(func() {
			for _, st := range sts {
				post(
					"u"+fs+"prov-"+string(st.ID)+fs+"data"+fs+jsonString(providerView{Status: st, IsDefault: st.ID == def}),
					"a"+fs+"prov-"+string(st.ID)+fs+"data-state"+fs+"idle",
				)
			}
			pushQuickModelData(def, quickViews)
		})
		pushModelsNow(ctx, forceModels)
	}()
}

// modelsView is one provider's model picker state, sent to the Settings card and to the
// quick picker. Everything in it comes from the CLI or from config.yaml, so the UI shows it
// with textContent only.
type modelsView struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Models []provider.Model `json:"models"`
	Model  string           `json:"model"`
	Effort string           `json:"effort"`
	Error  string           `json:"error,omitempty"`
	Ready  bool             `json:"ready"` // can run now (installed and signed in, or has a key)
	// Installed is whether the provider's command-line client was found (always false for API
	// providers). The setup card uses it to offer "Sign in" or "Get it".
	Installed bool   `json:"installed"`
	Kind      string `json:"kind"` // "cli" or "api"
	Loading   bool   `json:"loading,omitempty"`
}

func pushQuickModelData(def provider.ID, views []modelsView) {
	post("u" + fs + "quick-model" + fs + "data" + fs + jsonString(struct {
		Default   string       `json:"default"`
		Providers []modelsView `json:"providers"`
	}{string(def), views}))
}

func providerName(id provider.ID) string {
	if spec, ok := provider.SpecFor(id); ok {
		return spec.Name
	}
	if id == provider.Codex {
		return "Codex"
	}
	return "Claude"
}

// readyMu guards lastReady: which providers could run at the last check. The quick picker
// lists only those, so it does not offer a provider that has no key or sign-in.
var (
	readyMu       sync.Mutex
	lastReady     = map[provider.ID]bool{}
	lastInstalled = map[provider.ID]bool{}
)

func setReady(sts []provider.Status) {
	readyMu.Lock()
	defer readyMu.Unlock()
	for _, st := range sts {
		lastReady[st.ID] = st.Ready
		lastInstalled[st.ID] = st.Installed
	}
}

func isInstalled(id provider.ID) bool {
	readyMu.Lock()
	defer readyMu.Unlock()
	return lastInstalled[id]
}

// pickReadyDefault makes sure the default provider is one that can run. A user who already has
// Codex signed in but never opened Settings should not hit "Claude is not ready" on the first
// message. The current default is kept whenever it works; otherwise the first ready provider in
// display order (Claude, Codex, then API providers) is chosen.
func pickReadyDefault(sts []provider.Status) {
	if hub == nil {
		return
	}
	def := hub.Default()
	for _, st := range sts {
		if st.ID == def && st.Ready {
			return
		}
	}
	for _, st := range sts {
		if st.Ready {
			if err := hub.SetDefault(st.ID); err != nil {
				logf("default provider: %v", err)
			} else {
				logf("default provider %s was not ready; using %s", def, st.ID)
			}
			return
		}
	}
}

// setupPages are where a missing command-line client is obtained. The UI sends only the
// provider id, never an address.
var setupPages = map[provider.ID]string{
	provider.Claude: "https://claude.com/claude-code",
	provider.Codex:  "https://github.com/openai/codex",
}

// openSetupPage opens a provider's install page in a Phoenix tab.
func openSetupPage(id string) {
	if u, ok := setupPages[provider.ID(id)]; ok {
		createWebTab(u)
	}
}

func isReady(id provider.ID) bool {
	readyMu.Lock()
	defer readyMu.Unlock()
	return lastReady[id]
}

// pushModels sends the model lists from a goroutine (listing Codex models runs the CLI the
// first time; later calls come from the cache).
func pushModels(force bool) {
	if hub == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		pushModelsNow(ctx, force)
	}()
}

// pushModelsNow runs on a worker goroutine; it posts through runLater.
func pushModelsNow(ctx context.Context, force bool) {
	ids := hub.IDs()
	views := make([]modelsView, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ms, err := hub.Models(ctx, id, force)
			ch := hub.Selected(id)
			kind := "cli"
			if provider.IsHTTP(id) {
				kind = "api"
			}
			v := modelsView{ID: string(id), Name: providerName(id), Models: ms, Model: ch.Model, Effort: ch.Effort, Ready: isReady(id), Installed: isInstalled(id), Kind: kind}
			if err != nil {
				v.Models = []provider.Model{}
				switch {
				case errors.Is(err, provider.ErrNotInstalled):
				case errors.Is(err, provider.ErrNotReady):
					v.Error = "Add an API key to load this provider's models. You can also type a model ID."
				default:
					v.Error = "Could not load the model list: " + err.Error() + " You can still type a model ID."
				}
			}
			views[i] = v
		}()
	}
	wg.Wait()
	def := string(hub.Default())
	runLater(func() {
		for _, v := range views {
			post("u" + fs + "prov-" + v.ID + fs + "models" + fs + jsonString(v))
		}
		pushQuickModelData(provider.ID(def), views)
	})
}

// pushBrowser sends the browser settings and the config location to Settings.
func pushBrowser() {
	if cfgStore == nil {
		return
	}
	var u string
	cfgStore.View(func(c *config.Config) { u = c.SearchURL() })
	post(
		"u"+fs+"browser-settings"+fs+"data"+fs+jsonString(map[string]string{"searchUrl": u}),
		"t"+fs+"config-path"+fs+cfgStore.Path(),
	)
	pushProfile()
}

// setSearchURL validates and saves the address bar's search engine.
func setSearchURL(u string) {
	if cfgStore == nil {
		return
	}
	u = strings.TrimSpace(u)
	if !config.ValidSearchURL(u) {
		post("u" + fs + "browser-settings" + fs + "result" + fs + jsonString(testResult{Error: "The search address must start with https:// and contain {query} once."}))
		return
	}
	if err := cfgStore.Update(func(c *config.Config) { c.Browser.SearchURL = u }); err != nil {
		logf("save search url: %v", err)
		post("u" + fs + "browser-settings" + fs + "result" + fs + jsonString(testResult{Error: err.Error()}))
		return
	}
	pushBrowser()
	post("u" + fs + "browser-settings" + fs + "result" + fs + jsonString(testResult{OK: true, Text: "Saved."}))
}

// openConfigFolder shows config.yaml's folder in Explorer. The path comes from the store,
// never from the UI.
func openConfigFolder() {
	if cfgStore == nil {
		return
	}
	dir := filepath.Dir(cfgStore.Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logf("open config folder: %v", err)
		return
	}
	// explorer.exe exits 1 even when it works, so the error is not checked.
	_ = exec.Command("explorer.exe", dir).Start()
}

func settingsUnavailable() string {
	if hubErr != nil {
		return "Settings could not be loaded: " + hubErr.Error()
	}
	return "Settings could not be loaded."
}

// handleProviderOp handles UI → Go "v" messages: v <sub> <provider> [value].
// The value of "key" is a secret: it is validated, handed to the vault, and never logged.
func handleProviderOp(f []string) {
	if len(f) < 2 {
		return
	}
	sub := f[1]
	switch sub {
	case "refresh":
		refreshProvidersForce(true)
		return
	case "search": // v search <url>
		if len(f) > 2 {
			setSearchURL(f[2])
		}
		return
	}
	if strings.HasPrefix(sub, "profile-") {
		handleProfileOp(f)
		return
	}
	if strings.HasPrefix(sub, "mcp-") {
		handleMCPOp(f)
		return
	}
	if strings.HasPrefix(sub, "desktop-") {
		handleDesktopOp(f)
		return
	}
	if hub == nil || len(f) < 3 {
		return
	}
	id := provider.ID(f[2])
	arg := ""
	if len(f) > 3 {
		arg = f[3]
	}

	var err error
	switch sub {
	case "mode":
		err = hub.SetMode(id, provider.AuthMode(arg))
	case "key":
		err = hub.SetAPIKey(id, strings.TrimSpace(arg))
	case "clearkey":
		err = hub.ClearAPIKey(id)
	case "default":
		err = hub.SetDefault(id)
	case "baseurl": // v baseurl <provider> <url>; empty restores the default
		err = hub.SetBaseURL(id, arg)
	case "model": // v model <provider> <model> <effort>
		effort := ""
		if len(f) > 4 {
			effort = f[4]
		}
		if err = hub.SetModel(id, arg, effort); err == nil {
			pushModels(false)
			return
		}
	case "login":
		startLogin(id)
		return
	case "test":
		startTest(id)
		return
	default:
		return
	}
	if err != nil {
		logf("provider %s %s: %v", sub, id, err)
		post("u" + fs + "prov-" + string(id) + fs + "result" + fs + jsonString(testResult{Error: err.Error()}))
		return
	}
	refreshProviders()
}

func startLogin(id provider.ID) {
	post("a" + fs + "prov-" + string(id) + fs + "data-state" + fs + "login")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := hub.Login(ctx, id)
		runLater(func() {
			if err != nil {
				logf("login %s: %v", id, err)
				post("u" + fs + "prov-" + string(id) + fs + "result" + fs + jsonString(testResult{Error: "Sign-in did not complete: " + err.Error()}))
			}
			refreshProviders()
		})
	}()
}

type testResult struct {
	OK     bool             `json:"ok"`
	Text   string           `json:"text,omitempty"`
	Error  string           `json:"error,omitempty"`
	Millis int64            `json:"ms,omitempty"`
	Limits []provider.Limit `json:"limits,omitempty"`
	Cost   float64          `json:"cost,omitempty"`
	Model  string           `json:"model,omitempty"` // the concrete model that answered
}

func startTest(id provider.ID) {
	if cancel, running := testCancel[id]; running {
		cancel() // a second click stops the test
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	testCancel[id] = cancel
	post("a" + fs + "prov-" + string(id) + fs + "data-state" + fs + "testing")
	go func() {
		defer cancel()
		start := time.Now()
		var limits []provider.Limit
		var model string
		res, err := hub.Run(ctx, id, provider.Request{Prompt: "Reply with exactly the word: ok"}, func(ev provider.Event) {
			switch ev.Kind {
			case provider.EvRateLimit:
				limits = ev.Limits
			case provider.EvStart:
				model = ev.Model
			}
		})
		out := testResult{OK: err == nil && !res.IsError, Text: strings.TrimSpace(res.Text), Millis: time.Since(start).Milliseconds(), Limits: limits, Cost: res.Usage.CostUSD, Model: model}
		switch {
		case err != nil:
			out.Error = err.Error()
		case res.IsError:
			out.Error = res.Text
		}
		runLater(func() {
			delete(testCancel, id)
			post(
				"a"+fs+"prov-"+string(id)+fs+"data-state"+fs+"idle",
				"u"+fs+"prov-"+string(id)+fs+"result"+fs+jsonString(out),
			)
		})
	}()
}
