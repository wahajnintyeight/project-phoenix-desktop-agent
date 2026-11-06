package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"

	"phoenix/internal/library"
)

// exportFromLibrary is a user action: the destination only comes from a native
// picker attached to Phoenix. Disk I/O and library reads run off the UI thread.
func exportFromLibrary(lib *library.Store, selected []string) {
	if filesState.exporting || len(selected) == 0 {
		return
	}
	selected = append([]string(nil), selected...)
	filesState.exporting = true
	pushFiles("", false, "", nil)
	go func() {
		message, failed := "", false
		defer func() {
			runLater(func() {
				filesState.exporting = false
				pushFiles(message, failed, "", nil)
			})
		}()
		nodes, err := exportSelection(lib, selected)
		if err != nil {
			message, failed = "Could not export: "+err.Error(), true
			return
		}
		if len(nodes) == 0 {
			return
		}
		// KnownFolderPath respects redirected/OneDrive Desktops. An empty fallback
		// lets Windows choose its normal starting folder if Desktop is unavailable.
		desktop, _ := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
		var destination string
		if len(nodes) == 1 && !nodes[0].Dir {
			destination, err = app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
				Title: "Export file", Directory: desktop, Filename: library.ExportName(nodes[0].Name),
				CanCreateDirectories: true, Window: win,
			}).PromptForSingleSelection()
		} else {
			destination, err = app.Dialog.OpenFile().SetTitle("Choose where to export").SetDirectory(desktop).
				CanChooseFiles(false).CanChooseDirectories(true).AttachToWindow(win).PromptForSingleSelection()
		}
		if err != nil {
			// Wails returns its internal file-dialog cancellation error on Windows.
			if err.Error() != "cancelled by user" {
				message, failed = "Could not open the export picker: "+err.Error(), true
			}
			return
		}
		if destination == "" {
			return
		}
		if len(nodes) == 1 && !nodes[0].Dir {
			// The native Save dialog confirms any replacement before returning.
			_, data, readErr := lib.Bytes(nodes[0].Path)
			if readErr == nil {
				readErr = writeExportFile(destination, data)
			}
			if readErr != nil {
				message, failed = "Could not export: "+readErr.Error(), true
				return
			}
			message = "Exported to " + destination
			return
		}
		for i, n := range nodes {
			saved, exportErr := lib.Export(n.Path, destination)
			if exportErr != nil {
				message = fmt.Sprintf("Exported %d of %d items to %s. Could not export %s: %v", i, len(nodes), destination, n.Name, exportErr)
				failed = true
				return
			}
			message = "Exported to " + saved
		}
		if len(nodes) > 1 {
			message = "Exported " + plural(len(nodes), "item", "items") + " to " + destination
		}
	}()
}

// Prepare the whole file before replacing a destination approved in the Save
// dialog, so a write failure cannot truncate the user's existing copy.
func writeExportFile(destination string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".phoenix-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), destination)
}

// A selected folder already includes its selected descendants.
func exportSelection(lib *library.Store, selected []string) ([]library.Node, error) {
	var nodes []library.Node
	seen := map[int64]bool{}
	for _, p := range selected {
		n, err := lib.Stat(p)
		if err != nil {
			return nil, err
		}
		if n.ID == 0 {
			return nil, fmt.Errorf("select a file or folder to export")
		}
		if !seen[n.ID] {
			nodes = append(nodes, n)
			seen[n.ID] = true
		}
	}
	var roots []library.Node
	for _, n := range nodes {
		covered := false
		for _, parent := range nodes {
			if parent.Dir && parent.ID != n.ID && strings.HasPrefix(strings.ToLower(n.Path), strings.ToLower(parent.Path)+"/") {
				covered = true
				break
			}
		}
		if !covered {
			roots = append(roots, n)
		}
	}
	return roots, nil
}
