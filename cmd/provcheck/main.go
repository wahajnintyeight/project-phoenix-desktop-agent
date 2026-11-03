// Command provcheck is a developer check for provider management: it detects the installed
// CLIs and, with -run, streams a prompt through one. It uses a throwaway config and vault,
// so it never touches the user's real Phoenix settings.
//
//	go run ./cmd/provcheck                      # detect both providers
//	go run ./cmd/provcheck -run claude -prompt "Say hi"
//	go run ./cmd/provcheck -models codex        # list the models a provider offers
//	go run ./cmd/provcheck -run claude -model haiku -effort low
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"phoenix/internal/provider"
	"phoenix/internal/providerhub"
	"phoenix/internal/secrets"
)

func main() {
	run := flag.String("run", "", "provider to run a prompt through: claude or codex")
	prompt := flag.String("prompt", "Reply with exactly the word: ok", "prompt for -run")
	models := flag.String("models", "", "provider whose model list to print: claude or codex")
	model := flag.String("model", "", "model for -run (empty: the CLI default)")
	effort := flag.String("effort", "", "reasoning effort for -run (empty: the model default)")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall timeout")
	flag.Parse()

	tmp, err := os.MkdirTemp("", "phx-provcheck-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(tmp)
	vault, err := secrets.Open(filepath.Join(tmp, "vault"))
	if err != nil {
		fatal(err)
	}
	hub, err := providerhub.New(filepath.Join(tmp, "config.yaml"), vault)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	start := time.Now()
	for _, st := range hub.Statuses(ctx) {
		// The account name is deliberately not printed.
		fmt.Printf("%-7s installed=%-5v version=%-22q signedIn=%-5v method=%-9s plan=%-5s ready=%v\n",
			st.ID, st.Installed, st.CLIVersion, st.Login.SignedIn, st.Login.Method, st.Login.Plan, st.Ready)
		if st.Problem != "" {
			fmt.Printf("        problem: %s\n", st.Problem)
		}
		if st.CLIPath != "" {
			fmt.Printf("        cli: %s\n", st.CLIPath)
		}
	}
	fmt.Printf("detected in %v\n", time.Since(start).Round(time.Millisecond))

	if *models != "" {
		start = time.Now()
		ms, err := hub.Models(ctx, provider.ID(*models), true)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("\n%d models for %s (in %v):\n", len(ms), *models, time.Since(start).Round(time.Millisecond))
		for _, m := range ms {
			fmt.Printf("  %-26q %-22s ctx=%-8d default=%-7s efforts=%v\n", m.ID, m.Name, m.ContextWindow, m.DefaultEffort, m.Efforts)
		}
	}
	if *run == "" {
		return
	}
	id := provider.ID(*run)
	fmt.Printf("\nrunning %s (model %q, effort %q): %q\n", id, *model, *effort, *prompt)
	start = time.Now()
	var first time.Duration
	res, err := hub.Run(ctx, id, provider.Request{Prompt: *prompt, Model: *model, Effort: *effort}, func(ev provider.Event) {
		if first == 0 && (ev.Kind == provider.EvTextDelta || ev.Kind == provider.EvText) {
			first = time.Since(start)
		}
		switch ev.Kind {
		case provider.EvTextDelta, provider.EvText:
			fmt.Printf("  [%s] %q\n", ev.Kind, ev.Text)
		case provider.EvRateLimit:
			for _, l := range ev.Limits {
				fmt.Printf("  [limit] %s %.0f%% (%s)\n", l.Window, l.Utilization*100, l.Status)
			}
		case provider.EvUsage:
			fmt.Printf("  [usage] in=%d out=%d cached=%d cost=$%.5f\n", ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.CachedTokens, ev.Usage.CostUSD)
		case provider.EvError:
			fmt.Printf("  [error] %s\n", ev.Text)
		case provider.EvStart:
			fmt.Printf("  [start] model=%q\n", ev.Model)
		default:
			fmt.Printf("  [%s]\n", ev.Kind)
		}
	})
	fmt.Printf("done in %v (first text after %v): text=%q isError=%v err=%v\n", time.Since(start).Round(time.Millisecond), first.Round(time.Millisecond), res.Text, res.IsError, err)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "provcheck:", err)
	os.Exit(1)
}
