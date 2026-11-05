package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"phoenix/internal/config"
	"phoenix/internal/library"
	"phoenix/internal/mcp"
)

// libraryDBPath is the SQLite file that holds every file the user and the agent keep. It sits
// next to config.yaml, so a test profile gets its own. The files exist only inside this database:
// nothing is written to a folder on disk.
func libraryDBPath() string {
	if cfgStore != nil {
		return filepath.Join(filepath.Dir(cfgStore.Path()), "library.db")
	}
	p, err := config.DefaultPath()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "library.db")
}

var (
	libOnce  sync.Once
	libStore *library.Store
	libErr   error
)

// openLibrary opens the shared library once for this process.
func openLibrary() (*library.Store, error) {
	libOnce.Do(func() {
		clearOpenDir()
		path := libraryDBPath()
		if path == "" {
			libErr = fmt.Errorf("no place to keep the library")
			return
		}
		libStore, libErr = library.Open(path)
		if libErr != nil {
			logf("library: %v", libErr)
		}
	})
	return libStore, libErr
}

// filesServer is the per-run MCP child that serves the file tools. The database path is passed
// by environment; the child opens the same database (WAL, so both sides can work at once).
func filesServer() (mcp.Resolved, error) {
	exe, err := os.Executable()
	if err != nil {
		return mcp.Resolved{}, err
	}
	return mcp.Resolved{
		Server:    mcp.Server{Name: library.ServerName, Transport: mcp.Stdio, Command: exe, Args: []string{"mcp-files"}, Enabled: true},
		EnvValues: map[string]string{"PHOENIX_LIBRARY_DB": libraryDBPath()},
	}, nil
}

// runFilesMCP is `phoenix.exe mcp-files`.
func runFilesMCP() int {
	db := os.Getenv("PHOENIX_LIBRARY_DB")
	if db == "" {
		fmt.Fprintln(os.Stderr, "files server: no library")
		return 1
	}
	st, err := library.Open(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "files server:", err)
		return 1
	}
	defer st.Close()
	if err := library.NewServer(st, "agent").Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "files server:", err)
		return 1
	}
	return 0
}
