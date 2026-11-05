package main

import (
	"context"
	"sync"
	"time"

	"phoenix/internal/desktop"
)

// The application initializer. Everything a first message would otherwise wait for is done once,
// in the background, as soon as the window is up: the file library is opened, the approval
// service is created, every saved MCP server is connected and its tools listed, the desktop
// automation thread is started and the Codex CLI's tool list is read. A message sent in the
// first seconds waits only for the two essentials (library, approvals), which take milliseconds;
// a slow MCP server never holds a message back (it is skipped for that message and used from the
// next one, see providerhub.runMCPWait).

type appInitState struct {
	essential chan struct{} // closed when the library and the approval service are ready
	all       chan struct{} // closed when every step has finished (or failed)
}

var appInit = &appInitState{essential: make(chan struct{}), all: make(chan struct{})}

// startAppInit runs once, from the UI thread, after the providers are set up.
func startAppInit() {
	if hub == nil {
		close(appInit.essential)
		close(appInit.all)
		return
	}
	setAgentState("idle", "Starting tools…", false)
	go func() {
		began := time.Now()
		step := func(wg *sync.WaitGroup, name string, fn func() string) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				t := time.Now()
				detail := fn()
				if detail != "" {
					detail = " (" + detail + ")"
				}
				logf("init: %s %dms%s", name, time.Since(t).Milliseconds(), detail)
			}()
		}

		var essential sync.WaitGroup
		step(&essential, "library", func() string {
			if lib, err := openLibrary(); err != nil {
				logf("init: library: %v", err)
			} else {
				_ = lib.Describe() // reads the first pages of the database into cache
			}
			return ""
		})
		step(&essential, "approvals", func() string { approvalBroker(); return "" })
		go func() {
			essential.Wait()
			close(appInit.essential)
		}()

		var rest sync.WaitGroup
		step(&rest, "mcp servers", func() string {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			ready, total := hub.WarmMCPAndWait(ctx)
			return itoa(ready) + " of " + itoa(total) + " connected"
		})
		step(&rest, "providers", func() string {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			hub.WarmProviders(ctx)
			return ""
		})
		if desktopEnabled() {
			step(&rest, "desktop automation", func() string { desktop.WarmNative(); return "" })
		}
		essential.Wait()
		rest.Wait()
		close(appInit.all)
		logf("init: done in %dms", time.Since(began).Milliseconds())
		runLater(func() {
			if !chat.busy {
				setAgentState("idle", "Agent idle", false)
			}
		})
	}()
}

// awaitEssentialInit waits (at most max) for the library and approval service. It returns
// false if ctx ended first.
func awaitEssentialInit(ctx context.Context, max time.Duration) bool {
	select {
	case <-appInit.essential:
		return true
	default:
	}
	t := time.NewTimer(max)
	defer t.Stop()
	select {
	case <-appInit.essential:
		return true
	case <-t.C:
		return true // do not hang a message on a stuck step; the run reports its own errors
	case <-ctx.Done():
		return false
	}
}

func essentialInitDone() bool {
	select {
	case <-appInit.essential:
		return true
	default:
		return false
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
