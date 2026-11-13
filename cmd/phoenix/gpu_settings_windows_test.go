package main

import (
	"path/filepath"
	"testing"

	"phoenix/internal/config"
)

func TestGPUPreferenceAtStartup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PHOENIX_TEST_PROFILE", dir)
	if gpuDisabledAtStartup() {
		t.Fatal("GPU acceleration should be enabled by default")
	}
	store, err := config.OpenStore(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(c *config.Config) { c.Browser.DisableGPU = true }); err != nil {
		t.Fatal(err)
	}
	if !gpuDisabledAtStartup() {
		t.Fatal("saved preference was not loaded at startup")
	}
	if err := store.Update(func(c *config.Config) { c.Browser.DisableGPU = false }); err != nil {
		t.Fatal(err)
	}
	if gpuDisabledAtStartup() {
		t.Fatal("re-enabled GPU acceleration was not loaded")
	}
}
