package providerhub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phoenix/internal/config"
	"phoenix/internal/provider"
	"phoenix/internal/secrets"
)

type fake struct {
	id         provider.ID
	seen       provider.Settings
	modelCalls int
}

func (f *fake) ID() provider.ID { return f.id }
func (f *fake) Detect(_ context.Context, s provider.Settings) provider.Status {
	f.seen = s
	return provider.Status{ID: f.id, Name: string(f.id), Installed: true, Mode: s.Mode, HasAPIKey: s.APIKey != "", Ready: true}
}
func (f *fake) Models(context.Context, provider.Settings) ([]provider.Model, error) {
	f.modelCalls++
	return []provider.Model{{ID: "", Name: "CLI default"}, {ID: "big", Name: "Big", Efforts: []string{"low", "high"}}}, nil
}
func (f *fake) Login(context.Context, provider.Settings) error { return nil }
func (f *fake) Run(_ context.Context, s provider.Settings, _ provider.Request, _ func(provider.Event)) (provider.Result, error) {
	f.seen = s
	return provider.Result{Text: "ok"}, nil
}

func newHub(t *testing.T) (*Hub, *fake, *fake, string) {
	t.Helper()
	dir := t.TempDir()
	vault, err := secrets.Open(filepath.Join(dir, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	cl, cx := &fake{id: provider.Claude}, &fake{id: provider.Codex}
	path := filepath.Join(dir, "config.yaml")
	h, err := New(path, vault, cl, cx)
	if err != nil {
		t.Fatal(err)
	}
	return h, cl, cx, path
}

const key = "sk-ant-api03-FAKE-TEST-KEY-1234567890"

func TestKeyGoesToVaultAndConfigHoldsOnlyAReference(t *testing.T) {
	h, cl, _, path := newHub(t)
	if err := h.SetAPIKey(provider.Claude, key); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), key) {
		t.Fatalf("config.yaml contains the key:\n%s", raw)
	}
	if !strings.Contains(string(raw), "vault:provider-claude") {
		t.Fatalf("config.yaml has no vault reference:\n%s", raw)
	}
	if err := h.SetMode(provider.Claude, provider.AuthAPIKey); err != nil {
		t.Fatal(err)
	}
	st := h.Status(context.Background(), provider.Claude)
	if cl.seen.APIKey != key || cl.seen.Mode != provider.AuthAPIKey {
		t.Errorf("provider did not receive the resolved key: %+v", cl.seen)
	}
	if !st.HasAPIKey || st.KeySource != "vault" {
		t.Errorf("status: %+v", st)
	}
	// The status type has no field that could carry the key.
	if strings.Contains(strings.ReplaceAll(fmtStatus(st), " ", ""), key) {
		t.Error("status leaks the key")
	}
}

func fmtStatus(s provider.Status) string {
	return strings.Join([]string{string(s.ID), s.Name, s.CLIPath, s.CLIVersion, s.Login.Account, s.Login.Method, s.KeySource, s.Problem}, "|")
}

func TestClearKeyReturnsToCLIMode(t *testing.T) {
	h, _, _, path := newHub(t)
	_ = h.SetAPIKey(provider.Codex, key)
	_ = h.SetMode(provider.Codex, provider.AuthAPIKey)
	if err := h.ClearAPIKey(provider.Codex); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load(path)
	if pc := c.Provider(provider.Codex); pc.APIKey != "" || pc.Auth != provider.AuthCLI {
		t.Errorf("%+v", pc)
	}
	if h.vault.Has(config.VaultName(provider.Codex)) {
		t.Error("the vault entry survived")
	}
}

func TestValidation(t *testing.T) {
	h, _, _, _ := newHub(t)
	if h.SetMode(provider.Claude, "root") == nil {
		t.Error("invalid mode accepted")
	}
	if h.SetMode("evil", provider.AuthCLI) == nil || h.SetAPIKey("evil", key) == nil || h.SetDefault("evil") == nil {
		t.Error("unknown provider accepted")
	}
	for _, k := range []string{"", "short", strings.Repeat("x", 600)} {
		if h.SetAPIKey(provider.Claude, k) == nil {
			t.Errorf("key of length %d accepted", len(k))
		}
	}
}

func TestStatusesAreInDisplayOrderAndSettingsPersist(t *testing.T) {
	h, _, _, path := newHub(t)
	sts := h.Statuses(context.Background())
	if len(sts) != 2 || sts[0].ID != provider.Claude || sts[1].ID != provider.Codex {
		t.Fatalf("%+v", sts)
	}
	if err := h.SetDefault(provider.Codex); err != nil {
		t.Fatal(err)
	}
	h2, err := New(path, h.vault, &fake{id: provider.Claude}, &fake{id: provider.Codex})
	if err != nil {
		t.Fatal(err)
	}
	if h2.Default() != provider.Codex {
		t.Error("default did not persist")
	}
}

func TestModelChoicePersistsAndIsValidated(t *testing.T) {
	h, _, _, path := newHub(t)
	if err := h.SetModel(provider.Codex, "gpt-x.1", "high"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][2]string{{"--help", ""}, {"a b", ""}, {"ok", "High"}, {"ok", "x;y"}, {"-m", "low"}} {
		if h.SetModel(provider.Codex, bad[0], bad[1]) == nil {
			t.Errorf("accepted model %q effort %q", bad[0], bad[1])
		}
	}
	if h.SetModel("evil", "x", "") == nil {
		t.Error("unknown provider accepted")
	}
	h2, err := New(path, h.vault, &fake{id: provider.Claude}, &fake{id: provider.Codex})
	if err != nil {
		t.Fatal(err)
	}
	if c := h2.Selected(provider.Codex); c.Model != "gpt-x.1" || c.Effort != "high" {
		t.Errorf("not persisted: %+v", c)
	}
	if c := h2.Selected(provider.Claude); c.Model != "" || c.Effort != "" {
		t.Errorf("claude changed: %+v", c)
	}
}

func TestModelsAreCachedUntilRefresh(t *testing.T) {
	h, cl, _, _ := newHub(t)
	ctx := context.Background()
	a, err := h.Models(ctx, provider.Claude, false)
	if err != nil || len(a) != 2 {
		t.Fatalf("%v %v", a, err)
	}
	cl.modelCalls = 0
	_, _ = h.Models(ctx, provider.Claude, false)
	if cl.modelCalls != 0 {
		t.Error("cache not used")
	}
	_, _ = h.Models(ctx, provider.Claude, true)
	if cl.modelCalls != 1 {
		t.Error("refresh did not ask the provider")
	}
	_ = h.SetMode(provider.Claude, provider.AuthAPIKey) // catalog may differ by mode
	_, _ = h.Models(ctx, provider.Claude, false)
	if cl.modelCalls != 2 {
		t.Error("auth-mode change did not invalidate the cache")
	}
}
