package main

import (
	"os"
	"path/filepath"

	"phoenix/internal/config"
)

// WebView2 reads browser arguments when its environment is created.
func gpuDisabledAtStartup() bool {
	path, err := config.DefaultPath()
	if dir := os.Getenv("PHOENIX_TEST_PROFILE"); dir != "" {
		path, err = filepath.Join(dir, "config.yaml"), nil
	}
	if err != nil {
		return false
	}
	cfg, err := config.Load(path)
	return err == nil && cfg.Browser.DisableGPU
}

func pushGPUPreference() {
	if cfgStore == nil {
		post("t" + fs + "gpu-status" + fs + "Settings could not be loaded.")
		return
	}
	disabled := false
	cfgStore.View(func(c *config.Config) { disabled = c.Browser.DisableGPU })
	post("u"+fs+"gpu-acceleration"+fs+"checked"+fs+jsonString(!disabled), "u"+fs+"gpu-acceleration"+fs+"disabled"+fs+"false")
}

func setGPUPreference(fields []string) {
	if cfgStore == nil || len(fields) != 3 || (fields[2] != "0" && fields[2] != "1") {
		return
	}
	err := cfgStore.Update(func(c *config.Config) { c.Browser.DisableGPU = fields[2] == "0" })
	if err != nil {
		pushGPUPreference()
		post("t" + fs + "gpu-status" + fs + "Could not save the setting. Try again.")
		return
	}
	post("t" + fs + "gpu-status" + fs + "Restart Phoenix to apply this setting.")
}
