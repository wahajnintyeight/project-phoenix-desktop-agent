# Phoenix architecture

Status: current implementation reference, updated 2026-11-04. Sections below describe code in this repository unless marked **Planned** or **Open**. Earlier prototype notes are summarized at the end; they are historical and do not override current behavior.

Phoenix is a Windows desktop workspace for delegating browser and desktop work to an AI agent. The product is action-oriented: once a capability or MCP server is enabled, the agent can use it during a chat run without a per-action approval prompt. The user can see progress and tool calls, stop the run, and disable capabilities in Settings.

## 1. Runtime topology

```
Phoenix.exe
├─ Wails v3 window
│  └─ trusted Phoenix UI WebView2
│     ├─ browser chrome, tabs, settings and chat (HTML + TypeScript + CSS)
│     └─ U+001F/U+001E message bridge to Go
├─ Go application
│  ├─ tab and page-window manager
│  ├─ provider hub and per-turn chat runner
│  ├─ per-turn MCP/browser/desktop authorization broker
│  ├─ persistent history, chats, activity, settings and secret vault
│  └─ Windows integrations: WebView2/CDP, UI Automation, input and clipboard
├─ Page WebView2 controllers (one per web tab)
│  └─ untrusted sites, isolated from the trusted UI message channel
├─ MCP pool (app lifetime): user-installed stdio / Streamable HTTP servers,
│  connected at launch, tool lists cached, reconnected when they drop
├─ In-process MCP servers (per run, in-memory pipe, no child process)
│  ├─ phoenix-browser ── approval ── WebView2/CDP
│  └─ phoenix-files ── SQLite library
└─ Per-turn child process (only when computer use is on)
   └─ phoenix-desktop ── Windows UI Automation and native input
```

The Wails UI webview and page webviews are separate browser environments. The UI uses its own Wails user-data folder. Page tabs share Phoenix's persistent browser profile so site sessions survive restarts. The Go host owns tabs, provider state, settings, chat persistence, and all tool execution; the UI holds transient presentation state and sends user actions back to Go.

## 2. Technology

| Area | Current implementation |
|---|---|
| Desktop host | Go 1.27.3, Wails v3.1.0, Windows-only |
| Browser tabs | WebView2 controllers created and managed by Phoenix using the vendored `go-webview2` binding; page actions use WebView2's DevTools Protocol method |
| Phoenix UI | Strict TypeScript, HTML custom elements and CSS; no runtime framework or runtime npm dependencies |
| UI build | esbuild bundles TypeScript; Tailwind CSS v4 compiles `ui/app.css`; `tools/buildui` embeds both in `internal/ui/dist/index.html` |
| AI providers | Claude and Codex vendor CLIs; Gemini, OpenRouter, Mistral, Z.ai and Grok through one standard-library OpenAI-compatible HTTP driver |
| MCP | Official `modelcontextprotocol/go-sdk` for stdio and Streamable HTTP clients/servers |
| Configuration | YAML v3 in `config.yaml`; typed fields for providers, browser search, desktop tools and MCP servers |
| Secrets | Windows DPAPI-backed local vault; configuration stores references, not resolved secret values |
| Persistence | Bounded local JSON files for browser history, saved chats and activity; one SQLite database (`library.db`) for the file library |

The browser engine boundary is currently WebView2-specific. There is no alternate CEF implementation or cross-engine adapter. The application does not depend on `agent-desktop`; its native desktop tools are Phoenix-owned Go code.

## 3. Window, UI and message flow

Wails owns the frameless window, title bar, tray, single-instance behavior, UI webview, keyboard bindings and asset server. Phoenix creates each page WebView2 controller as a child window and positions it over the UI's `#page-slot`. The UI webview is kept below page windows in the native z-order. A frozen page image covers the webview while the overflow menu is open, because sibling WebViews cannot draw over one another.

The UI is a thin view, not a second application state owner:

- Go sends batched state and event updates; TypeScript creates safe DOM and manages local details such as scroll, focused controls, popovers, and resize gestures.
- The agent panel has Chat and Activity views. Chat shows streamed replies, immediate processing status, expandable tool-call details, copy actions, and the active model.
- The right panel can be resized with a drag handle. Display scale and compact spacing affect Phoenix chrome only; page zoom stays independent.
- Quick actions under the page are selected by Go's page classifier. `internal/quick/pattern.go` formats provider-neutral job-search and page workflows.
- Slash commands are defined in one typed registry in `ui/src/slash-commands.ts`; it includes prompt actions and local Phoenix controls such as settings, model selection, stop, and listing tested MCP servers.
- The UI uses the committed `PRODUCT.md` and `DESIGN.md` as the product and visual references. Tailwind runs only at build time; runtime markup uses semantic component classes.

### UI ↔ Go protocol

The bridge in `ui/src/protocol.ts` uses field separator U+001F and record separator U+001E. Go queues inbound work through `runLater` so Wails callbacks do not perform nested WebView2 work. Go sends batches through `WebviewWindow.ExecJS` to `window.__phx`.

| Direction | Op | Purpose |
|---|---|---|
| UI → Go | `c` | Activate an allow-listed UI action |
| UI → Go | `n` | Submit address-bar text |
| UI → Go | `l` | Report the page slot rectangle in CSS pixels |
| UI → Go | `m` | Report menu open, close, or painted-freeze state |
| UI → Go | `i`, `p` | Type-ahead diagnostic and bridge ping |
| UI → Go | `g` | Chat, quick action, stop, new/restore chat, or clear history |
| UI → Go | `v` | Provider, MCP, and computer-use settings operations |
| UI → Go | `r` | UI-ready notification |
| Go → UI | `u` | Set an allow-listed component property using JSON |
| Go → UI | `a`, `t`, `f`, `s`, `o` | Set an allow-listed attribute, text, focus, window state, or ping response |

The UI accepts only a fixed property and attribute allowlist. There is no operation for injecting arbitrary HTML. Untrusted text is inserted with `textContent`; model output is rendered by the bounded Markdown renderer. Page webviews have WebView messaging disabled and no Go host objects.

## 4. Browser automation

### Tool surface

The agent reaches Phoenix's actual open tabs through a built-in MCP server named `phoenix-browser`. The exposed tools are:

- Observe: `browser_tabs`, `browser_read`, `browser_snapshot`, `browser_screenshot`, `browser_wait`.
- Page interaction: `browser_click`, `browser_extract` (the whole page's text and optionally links in one call, with `query` filtering, paging by offset, and `scroll` to collect lazy-loaded or virtualised lists; the result is cached per tab so the next page is instant), `browser_click_text` (find a button or link by its label and click it in one call), `browser_fill_form` (many fields in one call), `browser_upload` (attach a library file to a file field), `browser_drag`, `browser_type`, `browser_fill`, `browser_press`, `browser_scroll`, `browser_select`, `browser_copy`, `browser_paste`.
- Tabs and navigation: `browser_open_tab`, `browser_close_tab`, `browser_switch_tab`, `browser_navigate`, `browser_back`, `browser_forward`, `browser_reload`.

The MCP child never receives a WebView2 or CDP handle. It sends a bounded request to a loopback-only Go bridge with a random per-run bearer token. Go validates the method and arguments, resolves the requested tab, and executes a named operation. Per-tab operations are serialized. CDP is called on demand; release builds do not expose a remote-debugging port.

`browser_snapshot` formats the accessibility tree into a bounded outline and issues opaque refs for controls. A ref is accepted only if it came from the latest snapshot for that tab and still matches the current navigation epoch. The model cannot send arbitrary CDP methods or arbitrary JavaScript. Password and authentication fields are rejected for type/fill; URLs are restricted to HTTP(S). Page content and tool results are untrusted input.

Browser actions use WebView2's real input events. Because CDP does not move the visible Windows cursor, Phoenix injects a short-lived, pointer-events-disabled cursor marker into the page at the same coordinates. It animates between targets, pulses on actions, follows reduced-motion preferences, and clears its animation layer after use. This makes clicks, text entry, selection, scrolling, dragging, and focused keyboard/clipboard work visible in the browser pane without intercepting the page.

The shared-page feature is separate from browser tools: when enabled, Go runs a fixed script in the active top frame and adds bounded visible text, title, URL and selection to that turn only. It does not read cross-origin frames, canvas or images. The transcript does not store the raw shared page text.

### Current limits

Every tool result is replayed to the model on each later step, so results are kept small: a snapshot lists at most 150 controls and 16 KiB of text, and a busy page is searched with `browser_snapshot query=…` or `browser_click_text`, not listed. There is no arbitrary script tool, file download tool, coordinate-only canvas tool, or cross-engine browser driver. Screenshots cover one tab's visible area and depend on the selected model accepting images. Site controls that do not expose useful accessibility information may require screenshot-based reasoning; canvas-heavy sites remain limited.

## 5. Providers and chat runs

The provider hub normalizes settings and run events across seven providers:

- Claude and Codex use their vendor CLI. Phoenix asks the CLI for login/model status and launches it with an isolated working directory, bounded settings and only the tools Phoenix supplies. Phoenix does not read vendor credential files.
- Gemini, OpenRouter, Mistral, Z.ai and Grok use the OpenAI-compatible `/chat/completions` and `/models` endpoints through Phoenix's standard-library HTTP driver. API keys are sent in the request authorization header and are not placed on command lines or in logs.

A chat run replays bounded transcript history because CLI sessions are not persisted by Phoenix's providers. It starts one cancellable run at a time, streams text and tool events to the panel, and records provider usage when available. The Stop control cancels the provider and tool contexts; the per-run token becomes invalid when the run ends.

### Message queue, steering and the context window

`cmd/phoenix/chat_queue_windows.go`. A message sent while the agent is busy is queued (up to 20), shown above the composer, and runs when the turn ends; a failed turn stops the line. **Steer** on a queued message interrupts the running turn (what the agent had said stays in the history) and runs that message next. **Stop** cancels the turn and clears the queue; new chat and clear history clear it too. UI → Go: `g send` (queues when busy), `g steer <id>`, `g unqueue <id>`; Go → UI: chat event `queue`.

A conversation has a 256k-token window. Phoenix estimates it (characters / 4, plus 8k for instructions, tools, profile and memory) and, before a turn that would pass 230k, asks the model for a handoff summary (the digest in `condenseHistory` if that fails), opens a new session that starts from the summary and the last exchange, and sends the message there. The old session stays in the session list. The reply shows `context Nk/256k` once a chat passes 64k. History replay and the saved transcript were raised to match (400 messages, about 1M characters per session). `PHOENIX_COMPACT_TOKENS` lowers the threshold for tests.

### Function calls and tool loop

Every implemented provider path can expose the MCP tools it supports: Claude/Codex use their CLI MCP integration through an authenticated Phoenix proxy, while OpenAI-compatible providers receive generated function schemas and return function calls to Phoenix's loop. The HTTP loop preserves assistant/tool messages across requests and combines usage. It currently bounds a turn to 32 tool rounds, 96 calls, 100 definitions, 64 KiB arguments, 32 KiB per tool reply and 128 KiB of tool-result text. Image output is bounded before it is passed back to a vision-capable model.

Function names sent to APIs are generated aliases; Phoenix maps them to the exact server/tool pair. Arguments must be valid JSON objects. Unknown functions, invalid schemas, oversized data and unsupported call types are rejected or returned as tool errors. Tool events are streamed into the chat and summarized separately in Activity.

Tool availability is provider-level, not classified per model. If an API model rejects the first request because it does not support tools (HTTP 400, 404 or 422 mentioning tools or functions), the OpenAI-compatible driver retries once without tools and tells the user in the reply, instead of failing the message. Image-input support is not probed.

Runs continue while Phoenix is minimized, covered or hidden in the tray: page WebView2s start with `--disable-features=CalculateNativeWinOcclusion`, `--disable-backgrounding-occluded-windows`, `--disable-renderer-backgrounding` and `--disable-background-timer-throttling`, so a page keeps its timers and frames (verified minimized: timers and animation frames kept running, `visibilityState` stayed visible). A non-active tab is hidden by `place()`; `browser_screenshot` brings such a tab forward first because a hidden tab has no surface to capture.

When a run that took more than 8 seconds ends (or an approval is requested) while Phoenix is in the background, the taskbar button flashes.

### Enabled tools run automatically

A normal chat turn creates a per-run authorization token in automatic mode. Enabled browser tools, enabled desktop tools and user-installed MCP servers can execute without an approval card for each call. The token is limited to that run and is revoked when it ends. The Stop control cancels active calls. The server must still be installed/enabled in Settings before its tools are offered.

This is an intentional product behavior: Phoenix should act on the user's request and show its work, instead of asking the user to approve routine calls one by one. It does not make configured MCP servers safe: a stdio server runs a command with the user's Windows permissions, and a remote MCP server may perform any action its own API allows.

## 6. MCP server management

MCP servers are user-configured in Settings and stored under `mcp_servers` in `config.yaml`. Phoenix accepts stdio commands and Streamable HTTP URLs. The install flow can import published `mcpServers` JSON or add a server manually; additions are reviewed before saving and start disabled. A server's enabled state and connection test are shown in the management UI.

**Tasks.** A task is a saved instruction the agent runs on its own (`internal/library/tasks.go`, stored in the library settings as `tasks.json`; `cmd/phoenix/tasks_windows.go`; the Tasks page `ui/src/tasks.ts`, a tab of kind `tasks` opened from the rail like Files). The page shows cards with status, schedule, tools and last report, a details drawer with the full instructions and the last 15 runs (each with "Open session"), and an editor with four parts: what (icon, name, instructions, optional extra instructions, starter ideas), when (on demand, every N minutes/hours, daily, weekdays), which tools (everything switched on, or a chosen set of Browser, Files, Computer use and individual MCP servers) and whether to flash the taskbar when it ends. Every run is a chat turn in a fresh session, so it is watchable, stoppable and kept in the session list. The tool choice is enforced per run: `startChatRun` skips the browser, files or desktop server a task did not pick, and `provider.Request.LimitMCP/OnlyMCP` makes `Hub.Run` offer only the chosen MCP servers. The message is an automation brief (`taskBrief`): decide and act, no questions, no choices block, stop at once on a real blocker, follow the extra instructions, and end with one line `DONE: …` or `BLOCKED: …`; the previous report is included so a repeating task skips what is done. `taskOutcome` parses that line into the stored status (`ok`, `blocked`, `failed`, `stopped`). A 15-second timer on the UI thread starts the first due task when the agent is idle; a task missed while Phoenix was closed runs once at the next start, never once per missed slot, and a new or changed schedule counts from the moment it was set. A manual Run while the agent is busy waits in the message queue. The agent can manage tasks itself with `tasks_create`, `tasks_list` and `tasks_delete` ("every weekday at 9 do this").

**Editing.** Add and edit share one form (`ui/src/mcp-settings.ts`; views: list, detail, editor, plus a review screen while candidates are pending). Editing offers "Web address | Program" tabs so a server's transport can be switched, and the name is editable (suggested from the address or package by `ui/src/mcp-names.ts`). Add also accepts pasted `mcpServers` JSON, which needs no separate name. Headers and environment values are key/value rows; saved values show as hidden and are kept when left blank. `Hub.MCPUpdate(oldName, server, env, headers)` applies a rename and transport switch atomically (compare-and-swap on the old name), keeps the OAuth ref only while the server is still HTTP at the same URL, and deletes any `vault:mcp-` credential no longer referenced. After an install the UI opens the new server's details.

Environment variables and HTTP headers can refer to values in the DPAPI-backed vault or to environment variables. Credentials are resolved in memory when a server is tested or used in a run. OAuth-capable HTTP servers use the MCP client's authorization flow; access tokens and refresh material are kept in the DPAPI-backed vault, not in clear text in `config.yaml`. The UI's tested-MCP list and `/mcps` command report enabled servers that passed their latest connection test.

**First run and sign-in.** Phoenix uses the user's own account and never reads `.claude` or `.codex` credential files: a sign-in that already exists is found by asking the CLI (`claude auth status`, `codex login status`). After each provider check, `pickReadyDefault` replaces a default that cannot run with the first ready provider (Claude, Codex, then API providers). If nothing is ready, the empty chat shows `<ph-setup-card>` instead of the example tasks: "Sign in with Claude/ChatGPT" when the CLI is installed (the CLI's own web sign-in), "Get Claude Code/Codex" when it is not (opens the vendor's page in a Phoenix tab; the UI sends only a provider id), and "Use an API key" (Settings; Gemini, OpenRouter, Mistral, Z.ai, Grok need nothing installed). It re-checks when the window regains focus (at most every 5 seconds) and hides itself as soon as any provider is ready.

**Connection lifecycle.** Starting an MCP server (an `npx` download, a Python environment, a TLS handshake) and listing its tools is slow, so Phoenix does it once, not per message. `mcp.Pool` (`internal/mcp/pool.go`) is owned by the provider hub: at launch it connects every enabled server in the background (`Hub.WarmMCP`), keeps the session and the cached tool list, and a run just takes them (about 10 ms measured with a stdio server). A server that exits is dropped from the pool when its session ends and reconnected on next use (measured 47 ms); one that failed is not retried for 20 seconds so a broken entry cannot stall every message; any change to a server's command, URL, secrets or enabled state reconnects or closes it (`Pool.Sync`, called after install, update, remove and enable); `Hub.Close` stops everything at quit. Connections are made with their own timeout, not the chat run's, so cancelling a message never aborts a start-up later messages want.

**Application initializer.** `cmd/phoenix/init_windows.go` runs once from `uiReady`, after the providers exist, and does in parallel everything a first message would otherwise wait for: open the file library, create the approval service, connect every saved MCP server (`WarmMCPAndWait`), read the Codex CLI's tool list, start the desktop-automation thread. Each step logs its duration (`init: mcp servers 840ms (3 of 3 connected)`). A message waits only for the library and approvals (milliseconds, at most 10 s) and never for MCP servers: one still starting is skipped for that message and noted to the model, then used from the next.

Phoenix's own tools are different: they depend on one run's approvals and cancellation, so they are created per run, but in-process (`Resolved.InProcess`, connected over an in-memory pipe) rather than as child processes. Browser and file tools no longer spawn `phoenix.exe` per message; only the desktop tools still do, and only when computer use is on. The model sees tools named for what they do (`browser_click`, `files_list`; a user server's tools as `<server>_<tool>`, within the 64-character limit), not opaque numbered aliases. Codex's own built-in tools (shell, browser, computer use, image generation, plugins) are switched off with `--disable`, checked against `codex features list` so an unknown name cannot stop a run: Phoenix's agent has only the tools Phoenix gives it, and each built-in tool costs prompt tokens on every message.

At run time, Phoenix lists server tools, converts schemas to provider-compatible functions where needed, calls the selected server, bounds returned text/images and scrubs configured secrets from results. MCP output is untrusted. Server processes are launched without a shell, but a configured executable still runs as the current Windows user.

## 7. Native computer use

Phoenix's `phoenix-desktop` MCP server is implemented in Go and Windows APIs; it does not invoke or require `agent-desktop`. It exposes:

- Find open windows and capture a named application window.
- Read an application's UI Automation controls and act on snapshot refs.
- Click, scroll, drag, type, set a control value, and press supported keys.
- Copy/paste in an application and read/write the system clipboard.

The server is another mode of `phoenix.exe` (`mcp-desktop`). It uses Windows UI Automation, window capture, native input and clipboard APIs. There is no shell tool and no whole-screen capture tool. Inputs, refs, keys, app names and coordinates are validated in Go. Desktop tools are enabled by default in a new configuration and can be disabled in Settings. When enabled, calls run automatically within the authenticated chat turn and under the current Windows user's permissions.

## 8. Quick actions and interaction visibility

`internal/quick` classifies the open page from a small local signal set and selects a bounded set of actions. It distinguishes job search results from individual listings and prioritizes resume-aware job actions when a resume MCP is enabled. `internal/quick/pattern.go` formats those actions into provider-neutral goals, workflow steps, deliverables and guardrails; no vendor-specific function schema is embedded in a prompt. Quick actions do not submit applications or send messages.

The chat command list is an enum-backed registry in `ui/src/slash-commands.ts`. Commands route either to local Phoenix actions, the quick-action pattern service, or the current provider/model selector. Tool-call cards show the MCP server, tool name, input and result in an expandable section. Chat/activity persistence stores summaries rather than raw tool data.

## 9. State, persistence and configuration

Phoenix uses bounded JSON files for history, chats and activity, and one SQLite database for the file library:

| Data | Location | Contents and bounds |
|---|---|---|
| Settings | `%APPDATA%\Phoenix\config.yaml` | Providers, browser search URL, desktop toggle and user MCP servers. Unknown top-level sections are retained; comments are not preserved on rewrite. |
| Secret vault | `%LOCALAPPDATA%\Phoenix\vault` | API keys, MCP environment values, HTTP headers and OAuth records encrypted with Windows DPAPI. Config stores vault references. |
| Browser history | `history.json` beside settings | Up to 100 most recently visited HTTP(S) URLs. Fragments are removed; query strings are retained. The new-tab view shows up to eight. |
| Saved chats | `chats.json` beside settings | Up to 40 conversations, 64 messages per chat and 120,000 bytes of transcript/context per chat. |
| Agent activity | `agent-activity.json` beside settings | Up to 500 safe summaries; Activity displays the newest 100. Raw tool arguments/results, page text and approval descriptions are excluded. |
| File library | `library.db` beside settings (pure-Go `modernc.org/sqlite`, WAL) | Every file and folder the user adds or the agent saves, with kind, note and source address. Files exist only as rows in this database (100 MB per file); nothing is written to a folder on disk. |
| Page profile | `%LOCALAPPDATA%\Phoenix\spike\browser` | WebView2 cookies and site sessions. The legacy `spike` path is retained to preserve existing sign-ins. |
| UI profile | `%LOCALAPPDATA%\Phoenix\ui` | Wails UI webview data. |

Chat and activity writes are bounded, coalesced off the UI thread and flushed during a normal quit. A saved transcript contains what the user sent and what the assistant replied; it does not separately persist raw tool payloads or the shared-page snapshot. Assistant text may still quote information it used. Browser history is a distinct store and can be cleared independently.

There is no migration system beyond the library's `user_version`, résumé facts database, application tracker, or backup subsystem in the current code.

## 10. Security boundaries

- The trusted Phoenix UI is served from embedded assets with a restrictive CSP. Page WebViews are separate, have WebView messaging disabled and receive no native host object.
- The UI-to-Go bridge accepts a small operation set. Go validates actions and tool payloads; all model, page, MCP and clipboard content is untrusted.
- Per-run random tokens authorize the built-in MCP child processes over loopback. A token cannot be reused after the run ends. The browser bridge checks loopback origin/host conditions and accepts only known operations.
- API keys and MCP credentials are resolved at the last responsible moment. They are not sent to the model, written to chat/activity logs, or placed on provider command lines. API providers receive keys only in an authorization header.
- Browser text entry refuses password, one-time-code, payment and other sensitive fields. Navigation is HTTP(S)-only. Browser tools expose named operations, not raw CDP or arbitrary JavaScript.
- Native desktop interaction can see or change other applications when enabled. It runs as the current Windows user; MCP stdio commands are also user-level code-execution surfaces. Phoenix validates input and provides a visible Stop control, but it does not impose a separate confirmation before each action in a normal chat run.

## 11. Build and execution

- `scripts/build.ps1` runs the Wails build, including TypeScript type-checking, Tailwind CSS compilation, UI bundling and Go build. Node/npm and Go are build-time requirements; the application does not load npm packages at runtime.
- `Taskfile.yml` exposes `build`, `dev`, `run` and `package` through the Wails task files. The dev loop serves `internal/ui/dist` through `tools/devserve`.
- `go:embed` packages the generated UI. The Wails window uses its own user-data directory; browser tabs use the retained Phoenix browser profile.
- `PHOENIX_TEST_PROFILE` redirects local settings and history/chat data for isolated runs. `PHOENIX_LOG=1` enables a temporary diagnostic log. `PHOENIX_DEBUG_PORT` enables loopback DevTools only in development/debug workflows and must not be set for releases.

## 12. Current gaps and open work

- **Files:** the SQLite file library (`internal/library`, MCP server `phoenix-files`, started as `phoenix.exe mcp-files`) and the Files explorer in the UI exist. Document indexing beyond file text and Drive integration are not implemented.
- **Product data:** no résumé profile store, application tracker, library backup/export flow, setup wizard, native notification center, updater or signed release pipeline.
- **Tool capabilities:** provider support is detected at the provider level, not consistently per model. Models without function-call support can fail when tools are offered.
- **Browser:** no arbitrary script, file download, trace/replay or robust coordinate interaction for canvas-only applications. Canvas is not represented by the accessibility snapshot. Not yet measured: tokens and time per step on real sites after the snapshot size cut.
- **Notifications:** only a taskbar flash; no Windows toast (needs an app identity the unpackaged exe does not have).
- **Desktop interaction:** different DPI/multi-monitor coverage and elevated windows (UIPI) need more validation; computer-use commands are Win32-specific.
- **Window/runtime:** Windows 11 snap-layout flyout, per-monitor DPI transitions, production installer/package and profile-path migration need completion.
- **Performance:** there is no current multi-tab memory/startup/latency baseline.

The next architecture changes should preserve the tool boundary: a provider may call only the schemas Phoenix has registered for that run; tool execution remains in Go-owned adapters; the model never receives a raw browser/debugging or operating-system handle.

## 13. Implementation record

- The original Win32 shell prototype established the raw string bridge, child page WebViews, tab layout, WebView2 z-order workaround and named CDP operations.
- The shell migrated to Wails v3 for the application window, tray, single instance, UI webview and build tooling. Phoenix retained its own WebView2 page controllers as child windows.
- The project vendored its WebView2 binding after fixing message echoing, accelerator handling and process-exit behavior that interrupted tab work.
- The initial Claude/Codex CLI integrations were extended with five OpenAI-compatible HTTP providers, shared model/settings handling, MCP client management, function-call execution, native computer use, persisted chat/activity/history, provider-neutral quick-action patterns and slash commands.
- `agent-desktop` and `impeccable` informed product decisions and design vocabulary, respectively; neither is a runtime dependency. `agent-desktop` is not required to install, build, or use Phoenix's native computer-use tools.

## References

- [Phoenix design context](DESIGN.md) and [product context](PRODUCT.md)
- [WebView2 reference](https://learn.microsoft.com/en-us/microsoft-edge/webview2/)
- [Wails v3](https://v3.wails.io/)
- [Model Context Protocol Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [vercel-labs/agent-desktop](https://github.com/vercel-labs/agent-desktop) — inspiration only; not a dependency
- [pbakaus/impeccable](https://github.com/pbakaus/impeccable) — design workflow inspiration

## File library

Files live inside Phoenix, in the SQLite database `library.db` (table `nodes`: a tree of folders and files with content, kind, note, source address, origin `user`/`agent`, and soft-delete columns). They are not visible in Explorer; only the Files view in the app, or a SQLite tool, shows them. Both the host and the per-run MCP child (`phoenix.exe mcp-files`, env `PHOENIX_LIBRARY_DB`) open the same file (WAL, busy timeout).

`internal/library` validates every path (no `..`, drive letters, colons, control characters, depth 12, length 240) and keeps names unique per folder, case-insensitively. Agent tools: `files_list`, `files_read` (text only, paged), `files_write` (create/overwrite/append, 1 MiB per call), `files_mkdir`, `files_move`, `files_copy`, `files_delete`, `files_search`, `files_find` (by kind or words), `files_set_info`, `files_save_url`. Deletes and overwrites go to the trash (soft delete, restorable); only the user can empty it. `files_save_url` downloads https addresses only (no credentials, no cookies, 25 MB, private/loopback addresses refused at connect time, programs and scripts refused) and records the source, so a file such as a generated résumé is kept and found again by kind. Each run adds the server and a system-prompt note listing what the library holds, so the agent knows it has somewhere to store things.

The Files view (`ui/src/files-view.ts`, host `cmd/phoenix/files_view_windows.go`) is an explorer: sidebar with All files, Recent, kinds, a folder tree and the trash; back/forward/up, a path bar and search; list and icon views with sortable columns; multi-select, rename, copy, cut and paste, drag onto folders, context menus, keyboard shortcuts; a details pane with a preview and editable kind and note; add files by dialog or drag and drop. Opening a file in another program writes a temporary copy under `%LOCALAPPDATA%\Phoenix\open`, cleared at start-up; Save as writes a copy where the user chooses. Programs and scripts are never launched from the library.
