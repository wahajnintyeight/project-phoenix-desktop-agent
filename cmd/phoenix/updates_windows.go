package main

import (
	"context"
	"fmt"
	"sync/atomic"

	"phoenix/internal/update"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
)

var (
	updateFlowActive atomic.Bool
	updateInitErr    error
)

func initUpdateManager() error {
	current, err := installedVersion()
	if err != nil {
		return err
	}
	provider, err := endpoint.New(endpoint.Config{URL: update.ManifestURL, Channel: "stable", HTTPClient: update.HTTPClient()})
	if err != nil {
		return err
	}
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: current,
		Providers:      []updater.Provider{provider},
		PublicKey:      update.PublicKey,
		Window:         updater.WindowNone,
	}); err != nil {
		return err
	}
	app.Event.On(updater.EventDownloadProgress, func(event *application.CustomEvent) {
		progress, ok := event.Data.(updater.Progress)
		if !ok || progress.Total <= 0 {
			return
		}
		percent := progress.Written * 100 / progress.Total
		if percent > 100 {
			percent = 100
		}
		runLater(func() {
			post("d"+fs+fmt.Sprint(percent), "t"+fs+"update-percent"+fs+fmt.Sprintf("%d%%", percent))
		})
	})
	return nil
}

func checkForUpdates() {
	if updateInitErr != nil {
		post("t" + fs + "update-status" + fs + "Update checker unavailable: " + updateInitErr.Error())
		return
	}
	if !updateFlowActive.CompareAndSwap(false, true) {
		return
	}
	post("t" + fs + "update-version" + fs + fmt.Sprintf("Installed version %s", app.Updater.CurrentVersion()))
	post("t"+fs+"update-status"+fs+"Checking for updates…", "u"+fs+"update-check"+fs+"disabled"+fs+"true", "u"+fs+"update-install"+fs+"disabled"+fs+"true")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), update.CheckTimeout)
		defer cancel()
		release, err := app.Updater.Check(ctx)
		runLater(func() {
			updateFlowActive.Store(false)
			post("u"+fs+"update-check"+fs+"disabled"+fs+"false", "u"+fs+"update-install"+fs+"disabled"+fs+"false")
			if err != nil {
				post("t" + fs + "update-status" + fs + fmt.Sprintf("Could not check for updates: %v", err))
				return
			}
			if release != nil {
				post("t"+fs+"update-status"+fs+fmt.Sprintf("Phoenix %s is available. Download it when you’re ready.", release.Version), "u"+fs+"update-install"+fs+"hidden"+fs+"false", "t"+fs+"update-notice-text"+fs+"Update available", "u"+fs+"update-notice"+fs+"hidden"+fs+"false")
				return
			}
			post("t"+fs+"update-status"+fs+"Phoenix is up to date.", "u"+fs+"update-install"+fs+"hidden"+fs+"true", "u"+fs+"update-notice"+fs+"hidden"+fs+"true")
		})
	}()
}

func installUpdate() {
	if !updateFlowActive.CompareAndSwap(false, true) {
		return
	}
	post("t"+fs+"update-status"+fs+"Downloading and verifying update…", "t"+fs+"update-percent"+fs+"Starting…", "d"+fs, "u"+fs+"update-progress-wrap"+fs+"hidden"+fs+"false", "u"+fs+"update-check"+fs+"disabled"+fs+"true", "u"+fs+"update-install"+fs+"disabled"+fs+"true")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), update.DownloadTimeout)
		defer cancel()
		err := app.Updater.DownloadAndInstall(ctx)
		runLater(func() {
			updateFlowActive.Store(false)
			post("u"+fs+"update-check"+fs+"disabled"+fs+"false", "u"+fs+"update-install"+fs+"disabled"+fs+"false")
			if err != nil {
				post("t"+fs+"update-status"+fs+fmt.Sprintf("Update failed: %v", err), "u"+fs+"update-progress-wrap"+fs+"hidden"+fs+"true")
				return
			}
			post("t"+fs+"update-status"+fs+"Update verified and ready. Restart Phoenix to apply it.", "u"+fs+"update-progress-wrap"+fs+"hidden"+fs+"true", "u"+fs+"update-install"+fs+"hidden"+fs+"true", "u"+fs+"update-check"+fs+"disabled"+fs+"true", "u"+fs+"update-restart"+fs+"hidden"+fs+"false", "t"+fs+"update-notice-text"+fs+"Restart to update", "u"+fs+"update-notice"+fs+"hidden"+fs+"false")
		})
	}()
}

func restartWithUpdate() {
	if updateInitErr != nil || !updateFlowActive.CompareAndSwap(false, true) {
		return
	}
	quitting = true
	go func() {
		if err := app.Updater.Restart(context.Background()); err != nil {
			runLater(func() {
				updateFlowActive.Store(false)
				quitting = false
				post("t" + fs + "update-status" + fs + fmt.Sprintf("Could not restart into the update: %v", err))
			})
		}
	}()
}
