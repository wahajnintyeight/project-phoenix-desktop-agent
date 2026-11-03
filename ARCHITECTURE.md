# Phoenix: Architecture (draft v0.4)

Status: design proposal. Items marked **[unverified]** are hypotheses to confirm in the prototype. No performance numbers in this document have been measured.

Decisions locked in: **WebView2** as the engine; **vanilla TypeScript + HTML5 + CSS** with custom components (no framework) for Phoenix's own UI; agent tooling modeled on **vercel-labs/agent-desktop**; UI design discipline taken from **pbakaus/impeccable**.

## 1. Goals

- A Windows desktop workspace: browser + file manager + AI agent.
- **Feels instant.** Targets (to be measured, not assumed):
  - Visual response to a click: within 1 frame (about 16 ms at 60 Hz).
  - Go-handled action (tab switch, panel open): under 50 ms.
  - Keystroke to updated results (history search): under 50 ms.
- App logic in Go. UI in vanilla TypeScript, HTML5 and CSS with custom components. The UI is a thin view: it renders what Go sends and forwards events back.

## 2. Principles

1. **CSS first.** Anything doable with `:hover`, `:active`, `:has()`, `popover`, `<details>`, `<dialog>` stays in CSS and never waits on Go.
2. **The UI is a thin view.** Components render Go-supplied state and forward events. They hold only ephemeral view state (hover, drag in progress, scroll) and no application logic. TypeScript sets attributes, properties and geometry custom properties; styling lives in CSS.
3. **Go owns all state and logic.**
4. **Own your data.** History, bookmarks, vault, user profile and application tracker live in Phoenix's SQLite, not in the engine's profile.
5. **The browser engine sits behind an interface** so WebView2 can be swapped (CEF is the realistic alternative).
6. **Untrusted by default.** Web pages, page titles, URLs and LLM output are untrusted. Secrets never enter the model's context.

## 3. Components

```
Phoenix.exe (Go, Win32 host)
├─ UI WebView2        trusted; embedded HTML + CSS + bundled TypeScript; web messages to Go
├─ Page WebView2 × N  untrusted web; one per tab; no web messaging, no host objects
├─ Popup window(s)    borderless, pre-created WebView2 for menus that overlap page area
├─ Browser interface  Tab, Navigate, Snapshot, Act(ref), Screenshot, SetFiles, Downloads, Cookies
├─ Agent runtime      provider interface, tool registry, permission tiers, action log
├─ Store (SQLite)     history, bookmarks, vault, profile facts, applications, settings
├─ Files              workspace-confined access (os.Root), search index, Recycle Bin deletes
└─ Integrations       Google Drive (OAuth + Picker), MCP client (Resume Matcher)
```

All WebView2 instances share one environment and one user-data folder (the persistent browser profile).

## 4. Technology

| Layer | Choice | Notes |
|---|---|---|
| Language (host) | Go 1.24+ | `os.Root` for confined file access |
| WebView2 binding | `github.com/wailsapp/go-webview2` v1.0.23 (used by the prototype), plus raw COM calls where its generated code is wrong | Its `pkg/webview2` package has generated interfaces for `CallDevToolsProtocolMethod`, `NewWindowRequested`, `NavigationStarting`, `DownloadStarting`, profiles and `TrySuspend` (present, **not yet exercised**). The generated `Settings9` binding was found broken (see 14a), so each interface must be tested before use |
| Win32 / COM | `golang.org/x/sys/windows`, `go-ole` | Recycle Bin, ShellExecute, Credential Manager |
| Automation | CDP via `CallDevToolsProtocolMethod`, typed with `chromedp/cdproto` | Behind a `cdp.Session` interface |
| UI language | Strict TypeScript, vanilla, no runtime dependencies | Custom Elements for components |
| UI build | `esbuild` (written in Go, has a Go API) bundles TS; Tailwind CSS v4 CLI compiles the stylesheet at build time | Output embedded with `go:embed` (see 9e for how the CSS joins the bundle). esbuild strips types without checking them, so type-checking needs `tsc` (Node) or the native TypeScript compiler port **[unverified]**. Keep Node as a dev/CI-only dependency |
| Protocol types | One schema in Go, generated TypeScript types (e.g. `tygo`) **[unverified]** | Prevents the two sides drifting apart |
| Database | `modernc.org/sqlite` or `ncruces/go-sqlite3`, FTS5 | goose migrations, sqlc queries |
| Secrets | DPAPI / Credential Manager | API keys, OAuth tokens, vault |
| Config | YAML via `goccy/go-yaml` or the maintained `go.yaml.in/yaml/v3` fork **[unverified]** which is better maintained today | Must preserve comments on rewrite; atomic writes |
| LLM | Official SDKs behind own `Provider` interface | Capability flags: tools, vision, streaming, context |
| MCP | `modelcontextprotocol/go-sdk` | Streamable HTTP + OAuth |
| Drive | `x/oauth2` + Drive v3, `drive.file` scope + Picker | System browser, PKCE, loopback redirect |
| S3 (deferred) | AWS SDK for Go v2 (custom endpoint for S3-compatible services) | Behind the `Storage` interface; see 5a Cloud storage. Check binary-size impact |

**Wails v3 is the target shell (decided 2026-11-03; spike passed, see 14b).** The owner wants Phoenix to be a Wails v3 application. Current facts: stable v3.0.0 and v3.1.0 (1 Nov 2026) are released and need Go 1.26+. Wails v3 does **not** host several webviews inside one window: issue #1997 is open (a maintainer suggested "v3.5, needs contributors"), PR #4880 (`WebviewPanel`, Windows and Linux) is open with changes requested and merge conflicts, and PR #5008 (`webcontentsview`) is a draft. So Phoenix uses a hybrid: Wails owns the window, tray, menus, the UI webview, bindings/events and packaging; the browser tabs stay as our own WebView2 controllers (the existing `go-webview2` code in `cmd/spike/tabs_windows.go`) created as child windows of the Wails window through `WebviewWindow.NativeWindow()`. If #4880 merges, tabs can move to `WebviewPanel` without touching the UI. Migration plan in 14b.

## 5. UI layer (vanilla TypeScript + HTML5 + CSS)

### Structure

- Strict TypeScript, no framework, no runtime dependencies. esbuild produces one JS bundle and one CSS file, both embedded in the binary. A static `index.html` shell is served from the embedded files; Go populates it through messages.
- **Custom Elements** for all UI components, for example `<ph-tab-strip>`, `<ph-omnibox>`, `<ph-agent-timeline>`, `<ph-tool-call>`, `<ph-approval-card>`, `<ph-snapshot-view>`, `<ph-file-tree>`, `<ph-resume-diff>`, `<ph-command-palette>`, `<ph-split>`.
- **Light DOM by default**, styled from global CSS using cascade layers and design tokens, which keeps styling fully flexible. Use Shadow DOM with `::part()` only for self-contained widgets that must not be restyled from outside.
- Each component exposes typed properties. Go's `u` message sets a property and the component updates only the parts that changed (keyed child updates; no virtual DOM).
- Components dispatch DOM events. One bridge module turns them into protocol messages through delegated, passive listeners.
- Rules: render untrusted strings with `textContent` only; no `innerHTML` with untrusted data; never read layout (`getBoundingClientRect`, `offsetWidth`) in hot paths; keep no state beyond ephemeral view state.

### CSS

- **Since 2026-11-03 the stylesheet is Tailwind CSS v4, compiled to plain CSS before the build (9e).** Nothing from Tailwind runs in the app. The bullets below still describe the output we want.
- Layers (`reset, tokens, base, components, utilities`), custom properties for tokens and theming, container queries, `:has()`, `popover`, anchor positioning, `@starting-style`.
- CSS-only interactions stay CSS-only (menus, tab selection, accordions, dialogs).
- Animation uses `transform` and `opacity`, around 100 ms or less. Use `contain` and `content-visibility: auto` on long lists. Go paginates; there is no JS virtualization.
- Inline critical CSS; icons as inline SVG or CSS masks; no external requests. Fonts are self-hosted and subset, if a brand font is chosen.
- Bundle size cap in CI. Starting guess: about 50 KB minified and gzipped. This is an arbitrary figure to adjust once measured.

### Design direction (from Impeccable)

[Impeccable](https://github.com/pbakaus/impeccable) (Apache-2.0) provides a shared design vocabulary for coding agents: steering commands (`typeset`, `layout`, `colorize`, `animate`, `distill`, `bolder`, `quieter` and others), review commands (`critique`, `audit`), finishing commands (`polish`, `harden`, `clarify`, `adapt`, `optimize`), project context files (`PRODUCT.md`, `DESIGN.md`) and a deterministic detector (62 rules per its README) for generic "AI-generated" design patterns.

How Phoenix uses it:

1. Write `PRODUCT.md` (purpose, users, tone) and `DESIGN.md` (type scale, color, spacing, motion, component rules) before building components.
2. Avoid the anti-patterns it lists: one font (Inter) for everything, purple-to-blue gradients, cards nested in cards, gray text on colored backgrounds, a rounded-square icon tile above every heading, cramped padding, skipped heading levels, low contrast, tiny text.
3. Run `npx impeccable detect` on the source and on the UI served in dev mode as a CI check; use `critique` and `audit` at milestones. This needs Node, so keep it dev-only.
4. Review small targets: Impeccable's README says a single section reviewed closely beats a whole page at a glance.
5. Caution: its "live" mode can launch coding agents with permission checks and sandboxing disabled. Review that before enabling it.

I have only read the README summary and the docs overview. Numeric rules (sizes, spacing, timing) live on pages I could not retrieve (`/docs/detector` and the command pages) and have not been reviewed.

**Applied 2026-11-03 (see 9c for the work).** `npx impeccable detect internal/ui/dist/index.html` (the built page, so rendered-page rules run) went from 3 findings (an `<img>` with no `src`, two `cramped-padding`) to 0. Scanning the source files only runs regex rules, and its `flat-type-hierarchy` hit on the raw `index.html` is a false positive because that file has no CSS; the built page passes. The detector does not see hidden views (Settings is `display: none` in a static scan), so those were checked by eye. `critique` and `audit` are agent commands, not part of the detector; the review below was done by hand in their spirit.

## 5a. Application shell: UI specification

### Confirmed decisions

| Topic | Decision |
|---|---|
| Window | Borderless, custom-drawn title bar and toolbar |
| Tabs | In the title bar (Chrome style) |
| Agent panel | Right side, collapsible |
| Theme | Dark by default. Tokens structured so other themes can be added later |
| Close button | Keeps Phoenix running in the system tray in the background |
| Portable mode | Later |
| Settings | One `config.yaml` (storage paths, providers, MCP servers, agent, UI). API keys by vault reference, `env:` or literal |
| Deferred (optional) | S3 connector, connector backups, AWS profile or SSO sign-in |
| Also required | Configurable storage paths, notifications (task completion, update required), setup wizard |

### Layout

```
┌──────────────────────────────────────────────────────────────────┐
│ ◈ │ [Tab 1][Tab 2][Tab 3] +            ● Agent: idle    ─  □  ✕  │ ← drag region
├──────────────────────────────────────────────────────────────────┤
│ ←  →  ⟳ │ [ omnibox ................................ ] ☆ ⬇ 👤 ◧ │
├────┬─────────────────────────────────────────┬───────────────────┤
│Rail│ Page / file view / settings             │ Agent panel       │
│ 🌐 │                                         │ Activity | Chat | │
│ 📁 │                                         │ Approvals | Asks  │
│ ✔  │                                         │ [ Stop ]          │
└────┴─────────────────────────────────────────┴───────────────────┘
```

- Left rail switches Browser, Files, Tasks.
- Agent status pill states: idle, running, waiting for approval, waiting for an answer, error.
- **Stop is always visible while the agent runs**, in both the status pill and the panel.
- Agent panel sections: Activity (timeline of tool calls), Chat, Approvals (queue), Missing answers (queue).
- Page views occupy the central slot; the UI WebView2 draws everything else.

### Window chrome (Win32 host)

- Keep a thick-frame window style and remove the visible frame by handling `WM_NCCALCSIZE`. This preserves Aero Snap, shadow, keyboard resize and maximize behavior. Avoid plain `WS_POPUP`.
- Drag region: use WebView2's non-client region support (`ICoreWebView2Settings9::IsNonClientRegionSupportEnabled`; Win32 interface introduced in SDK 1.0.2420.47; default false; takes effect after the next navigation). CSS `app-region: drag` areas then behave like the native title bar: dragging, right-click system menu, double-click to maximize or restore. Mark every interactive element inside or overlapping a drag area `app-region: no-drag`. **Known issue:** a GitHub report says WebView2 v152+ enforces CSS inheritance for `app-region: drag`, so children that overflow a drag region (tooltips, dropdowns) inherit it and can swallow clicks; the suggested fix is `no-drag` on interactive overlays. Still to test on the target runtime. Fallback: the host hit-tests the title bar using rectangles the UI reports.
- Resize edges: inset the UI WebView2 by a few pixels so the parent window receives hit-tests at the borders.
- **Windows 11 snap-layout flyout (hover on maximize).** Microsoft's guidance is to return `HTMAXBUTTON` from `WM_NCHITTEST` over a custom maximize button. The WebView2 child window covers the client area, so Windows never sees the HTML button. Projects report this workaround: a small transparent native child window placed over the HTML maximize button that answers `HTMAXBUTTON`, handles the click (`WM_SYSCOMMAND`) and tells the page the hover and pressed state so the DOM button can draw itself. Reports conflict: pywebview measured that the flyout only appeared over DWM's own button position, and an Electron-related decision record says CSS `app-region` only yields `HTCAPTION`, with no maximize region. Wails documents a composition-hosting approach that maps DOM rectangles to hit-test values, which I have not examined. **Treat the flyout as optional polish.** Drag-to-edge snap and `Win+Z` should still work. It can only be tested on Windows 11, and the dev machine runs Windows 10 (19045), so a Windows 11 test machine or VM is needed.
- Windows 10 (the dev machine is 19045) has no rounded corners or Mica. The design must look correct without them.
- Handle DPI changes between monitors, a minimum window size, and fullscreen video in page tabs.
- Single instance (mutex): launching again focuses the existing window.

### Tray and background behavior

- Close button hides the window to the tray; Phoenix and any running agent task continue.
- Show a one-time hint on first close ("Phoenix is still running in the tray").
- Tray menu: Open, Agent status, Stop agent, Quit. Quit while a task runs asks for confirmation.
- When hidden with no agent running, suspend page tabs to save memory with `TrySuspend`. Documented behavior: the controller's `IsVisible` must already be false or the call fails with `ERROR_INVALID_STATE`; it can complete with `isSuccessful = false` and `S_OK` when a "Sleeping Tabs" condition blocks suspension, so failure must be handled quietly; the WebView resumes automatically when made visible, and some calls such as `Navigate` also resume it, so check `IsSuspended` before calling APIs that could wake it. Microsoft also suggests a periodic resume and re-suspend for hidden views that need fresh data. Order of operations: set `IsVisible = false`, then `TrySuspend`. Keep tabs alive while a task runs. Whether agent (CDP) calls resume a suspended tab is untested.
- "Start with Windows" is a setting, off by default.

### Storage paths

| Category | Default | Contents |
|---|---|---|
| Browser profile | `%LOCALAPPDATA%\Phoenix\browser` | WebView2 user data folder (sessions, site logins) |
| State | `%LOCALAPPDATA%\Phoenix\data` | SQLite: history, bookmarks, vault, settings, agent logs |
| Workspace | Chosen in the wizard (suggested `Documents\Phoenix`) | Files shared with the agent; may be several folders |
| Downloads | Windows Downloads folder | Browser downloads |
| Cache and logs | `%LOCALAPPDATA%\Phoenix\cache`, `...\logs` | Disposable |

- Custom paths are recorded in `config.yaml` (see "Configuration file" below), which lives at a fixed location so the app can find the rest.
- **Changing the browser profile path needs a restart and a migration**, because WebView2 fixes its data folder at environment creation. Procedure: copy to the new location with progress, verify, switch on restart, keep the old copy until the user confirms.
- **Scope: local fixed drives only** for the browser profile, database, workspace and downloads. Network and removable locations are out of scope: the path picker rejects them (drive-type check) with a plain message. Remote storage is handled separately as optional cloud connectors (below).
- Validate every path: writable, enough free space, and a long-path-aware manifest so paths over 260 characters work.
- **Spinning hard disks:** the browser profile and SQLite database do a lot of small random reads and writes. Detect the drive type and advise (not block) putting them on an SSD if one exists. Use SQLite WAL mode. **[unverified]** the exact Windows API for detecting a spinning disk.
- **OneDrive folder redirection:** `Documents` is often redirected into OneDrive. Detect synced paths. Warn for the database and browser profile (sync can corrupt them). For the workspace, warn that cloud-only "files on demand" placeholders download when read, which can stall the agent.
- Warn when a database or profile path is inside a cloud-synced folder (OneDrive, Dropbox); sync can corrupt SQLite and browser profiles.
- The vault uses Windows DPAPI, tied to the Windows user and machine. Moving data to another PC breaks decryption. Offer an optional master password for portability.
- Portable mode (marker file next to the executable) is deferred.

### Configuration file (`config.yaml`)

One YAML file holds the settings: storage paths, AI providers, MCP servers, agent safety defaults, UI and notification preferences.

- **Location:** `%APPDATA%\Phoenix\config.yaml`. This is a fixed location because the file itself says where the database is. Override with `--config` or `PHOENIX_CONFIG`.
- **First run:** the setup wizard asks the user for the data folder and writes it to `storage.data_dir`. The SQLite files go in `<data_dir>\db` (`phoenix.db` plus its `-wal` and `-shm` files). The browser profile, cache and logs default to subfolders of `data_dir` unless overridden. Workspace folders are listed explicitly. Whether history gets its own database file is undecided.
- **Example (illustrative; field names are a proposal):**

```yaml
version: 1

storage:
  data_dir: 'D:\Phoenix'               # single quotes: Windows backslashes stay literal
  # browser_profile_dir: 'E:\PhoenixBrowser'   # optional override
  workspace_dirs:
    - 'D:\Phoenix\workspace'

providers:
  default: claude
  claude:                              # first implemented providers: claude and codex (see 9a)
    auth: cli                          # cli (use the CLI's own sign-in) | api_key
    api_key: vault:provider-claude     # reference; the key itself is in the vault
    model: haiku                       # optional; an alias, a full model id, or empty for the CLI default
    effort: high                       # optional reasoning effort; empty for the model default
    cli_path: ""                       # optional; code-execution surface, see Security
  local:
    kind: openai-compatible
    base_url: http://localhost:11434/v1
    api_key: env:OLLAMA_API_KEY        # optional; read from the environment
    model: <model-id>

browser:
  search_url: 'https://duckduckgo.com/?q={query}'   # https only, one {query}; presets in Settings

mcp_servers:
  resume-matcher:
    transport: http
    url: <endpoint>
    auth: vault:resume-matcher
    enabled: true

agent:
  require_approval: [submit, send, delete, pay]

ui:
  theme: dark
  close_to_tray: true
```

- **Secrets in the file:** the value of any key field can be `vault:<name>` (default; the wizard stores the key in the Windows-encrypted vault and writes the reference), `env:NAME`, or a literal string. Literals are accepted, since the file is yours, but the UI warns and offers "move to vault". The config is easy to copy, back up, screenshot or sync by accident, which is why a reference is the default. Secrets are masked in the UI and never written to logs.
- **Behavior:** a `version` field for migrations; validation with line and column in error messages; unknown keys produce a warning; atomic writes (temp file then rename) with a `.bak`; comments preserved when Phoenix rewrites the file; a file watcher (`fsnotify`) so an outside edit shows "config changed, reload?". `storage.data_dir` and `storage.browser_profile_dir` need a restart and the migration flow above. Providers, MCP servers, agent, UI and notification settings reload without a restart. Set limits on YAML aliases and nesting.
- **The config is a code-execution surface.** An MCP server entry with a `command` launches a process. So:
  - Agent tools can never read or write `config.yaml`. It sits outside every workspace and is denied explicitly.
  - Adding or changing an MCP server or provider endpoint is a user action in the UI, with a confirmation that shows the exact command or URL.
  - On reload, a new or changed MCP entry needs confirmation before its first launch.

### Cloud storage (optional connectors)

**Status: S3 and connector backups are deferred (optional, not v1). Google Drive stays in scope. When S3 arrives, start with access keys only, with AWS profile or SSO support later (assumption).** Local fixed drives are the primary storage. Cloud is optional and only when the user supplies credentials: **Google Drive** and **S3** (including S3-compatible services through a custom endpoint, for example MinIO, Cloudflare R2, Backblaze B2 or Wasabi; compatibility **[unverified]** per service).

**What cloud is for:**
- Workspace folders the user and agent can browse (remote documents, résumés).
- A destination for exports and downloads.
- Optional encrypted backups of Phoenix state (see below).

**What cloud is never used for:** the live browser profile or the SQLite database. Latency, locking and sync corruption make that unsafe.

| | Local drive | S3 / S3-compatible | Google Drive |
|---|---|---|---|
| Addressing | Path, confined with `os.Root` | `s3://bucket/prefix/key` | File IDs; duplicate names allowed |
| Folders | Real | Prefix and delimiter listing only | Real, ID-based |
| Rename / move | Atomic | Copy then delete | Metadata update |
| Auth | None | Access key and secret (vault), or an AWS profile **[unverified]** whether profile or SSO sign-in is practical in-app | OAuth, system browser, PKCE |
| Access scope | Workspace folders | One bucket and prefix, chosen by the user | `drive.file` plus the Picker: only files the user picks or Phoenix creates. Browsing everything needs a broader restricted scope and Google verification |
| Overwrites | Replace | Replace silently unless bucket versioning is on | Creates a new revision |

**Design:**
- A small `Storage` interface: `List`, `Stat`, `Open` (range reads), `Write`, `Move`, `Delete`, plus a `Capabilities` struct (atomic rename, real folders, versioning). Backends: `local`, `s3`, `drive`. The agent's file tools call the interface and never a specific backend.
- Remote files are staged: before the browser uploads a remote file or the agent reads it, Phoenix downloads it to a staging folder under the cache path, with progress, and cleans it up afterwards.
- **Safety:** connectors are read-only by default. Remote writes, overwrites and deletes are in the sensitive tier and need approval that names the bucket and prefix (or Drive file). Recommend a least-privilege IAM policy limited to one prefix, and bucket versioning. Credentials live only in the vault and are never visible to the model. Each connector can be enabled, disabled or made read-only, and is tested during setup.
- No automatic two-way sync in v1. Transfers are explicit: import, export, backup.
- **Backups (proposal):** a scheduled snapshot of the database (SQLite online backup), encrypted with a key derived from a user passphrase, written to a chosen local folder or a connector. The browser profile is excluded by default because it is large and sensitive.
- **Size impact:** the AWS SDK for Go v2 and the Google API client add to the binary. Measure the effect against the install-size budget; a lighter S3 client is an option **[unverified]**.

### Notifications

| Event | Priority | Behavior |
|---|---|---|
| Agent needs approval or an answer | High | Persistent; the agent is blocked until the user responds |
| Task completed or failed | Normal | Toast with a link to the run |
| Update available | Low | Non-blocking banner |
| Update required (security or compatibility) | High | Blocking prompt with an "update now" action |

- Three layers: in-app toasts, a notification center (searchable history), and native Windows toasts when the window is hidden or unfocused. Add a taskbar badge and progress.
- Native toasts for an unpackaged app need a registered AppUserModelID, or toasts appear under a generic host name. Candidate Go libraries, found by search, with maintenance status and licenses **not** checked:
  - `mtunjic/winrt-toast`: calls the Windows Runtime directly (no PowerShell). `RegisterAppID(appID, displayName)` registers a per-user AUMID. The default AUMID is PowerShell's, so toasts show "Windows PowerShell" with no custom icon.
  - `hellolib/toast`: Windows Runtime based. `RegisterAppID` writes `HKCU\Software\Classes\AppUserModelId\<appID>` with a display name and icon. Clickable actions use Windows protocol activation.
  - `go-toast/toast`, `electricbubble/go-toast`: older; `winrt-toast`'s README says most Go toast libraries shell out to `powershell.exe`, which is slow and fragile.
- Action buttons via protocol activation mean registering a URI scheme (for example `phoenix://`); the single-instance logic forwards the activation to the running window. MSIX packaging would simplify identity and activation. Spike `winrt-toast` and `hellolib/toast` first.
- Per-type preferences and a do-not-disturb mode. Clicking a notification opens the relevant task.

### Setup wizard (first run; re-runnable from Settings)

1. Welcome and system check (WebView2 runtime, disk space, network)
2. Data location: the user picks the folder for the SQLite database and app data (default under `%LOCALAPPDATA%\Phoenix`), with free-space display; Phoenix writes the choice to `config.yaml`
3. Browser profile: start fresh, or import bookmarks, history and passwords (Chrome, Edge, Firefox)
4. Workspace folders: what the agent may access
5. AI provider: detect a running Ollama or LM Studio, enter cloud API keys (stored in the vault and referenced from `config.yaml`), test the connection, show detected capabilities (tools, vision)
6. About you: facts used in applications, and the résumé upload
7. Connections (optional): Google Drive; Resume Matcher (written as an MCP server entry in `config.yaml`, with a confirmation showing the URL). S3 is deferred
8. Safety defaults: approvals before submit or send, tool-group toggles
9. Notifications: send a test toast
10. Summary and finish

Rules: resumable (progress stored in Go), back and next on every step, inline validation, masked secrets, optional steps skippable. Built from the same components in the same borderless window.

### UI open items

- Tone and visual direction for `DESIGN.md` (see `PRODUCT.md`; currently an assumption).
- Whether to invest in the Windows 11 snap-layout flyout (native overlay window workaround; needs a Windows 11 test machine).
- Behavior of `app-region` on the runtime version we require (inheritance issue reported on v152+).
- Toast library choice (`winrt-toast` vs `hellolib/toast`) and packaging format (MSIX or installer).
- Whether agent CDP calls wake a suspended tab.

## 6. Message protocol (UI ↔ Go)

### Why this design

Wails v3's own docs describe two paths: reflection-based **service bindings** (general purpose, full feature set) and **raw messages** ("minimal overhead but requires manual handling"), recommended for high-frequency, performance-critical use. A raw handler receives a string and replies through events. Phoenix follows the raw-message approach directly on WebView2: no reflection and no promise-per-call layer. The Wails docs give no benchmark numbers, so the codec choice below is a hypothesis to measure.

### Transport

- UI to Go: `window.chrome.webview.postMessage(string)`, received by `WebMessageReceived`, read with `TryGetWebMessageAsString`.
- Go to UI: `PostWebMessageAsString(string)`, received by the bridge's `message` listener as `e.data`.
- Microsoft documents that messages from one page are delivered in order. Ordering across frames is not guaranteed, so the UI uses a single frame.
- Large binary payloads (screenshots, thumbnails, file previews) go through **shared buffers** (`PostSharedBufferToScript`), not message strings. **[unverified]** for this app's sizes; benchmark against base64 over messages.
- Use string messages, not JSON objects, on hot paths.

### Wire format (v0)

Compact delimited strings, no JSON parse on hot paths:

- Field separator: `U+001F`. Record separator (batching several ops in one message): `U+001E`.
- The last field may carry free text, or an embedded JSON blob for rare complex payloads.

UI to Go:

| Op | Fields | Meaning |
|---|---|---|
| `c` | `action`, `id` | Click / activate |
| `i` | `field`, `value`, `seq` | Input changed (type-ahead). `seq` increases each keystroke |
| `n` | `text` | Address bar submit; Go decides URL vs search |
| `l` | `x`, `y`, `w`, `h` | Page-slot rectangle in CSS px; Go positions the active page window over it |
| `m` | `open` / `close` / `frozen` | Menu popover state; `frozen` acknowledges the freeze image is painted |
| `k` | `key`, `mods` | Key not handled by host accelerators |
| `d` | `kind`, `phase`, `x`, `y` | Drag (tab reorder, splitter), coalesced to 1 per frame |
| `r` | `version` | UI ready; Go replies with initial state |

Go to UI (several ops per message, flushed once per frame):

| Op | Fields | Meaning |
|---|---|---|
| `u` | `id`, `prop`, `json` | Set a property on a component; it updates itself. Preferred op |
| `t` | `id`, `text` | Set text content (safe for untrusted strings) |
| `a` | `id`, `attr`, `value` | Set attribute |
| `h` | `id`, `html` | Replace inner HTML with a fragment produced by Phoenix code (optional) |
| `p` | `id`, `where`, `html` | Insert (append / before / after) (optional) |
| `x` | `id` | Remove element |
| `v` | `--name`, `value` | Set a CSS custom property |
| `f` | `id` | Focus element |

### Rules

- **Coalesce on both sides.** The bridge sends only the latest pointer position per frame. Go flushes patches once per frame, so streaming tokens cost one render per frame.
- **Cancel stale work.** For type-ahead, Go compares `seq` and drops superseded queries. The UI does not debounce.
- **Keyed updates.** Update by element id. Never rebuild large lists.
- **Untrusted text is data, never HTML.** Page titles, URLs and LLM output arrive via `t`, `a` or `u`, and components render them with `textContent`.
- **Validate everything in Go.** Unknown ops or actions are ignored. Treat the UI as a client, not trusted code.
- **Version the protocol** in the `r` handshake. Generate TypeScript message types from one Go schema.
- The codec is a small package behind an interface, so JSON can replace it if the benchmark shows no meaningful difference.

## 7. Window layout and the popup problem

The UI WebView2 and each page WebView2 are sibling child windows. A sibling cannot draw over another WebView2, so menus and dropdowns that overlap the page area need either:

- **Pre-created borderless popup window(s)** with their own WebView2, shown and positioned on demand (preferred for the first prototype), or
- **DirectComposition visual hosting**, so all surfaces can be z-ordered. **[unverified]** whether the available Go bindings expose the composition-controller interfaces.

This is the largest technical risk in the UI design. Test it first.

**Status (Prototype 2): freeze-frame works for the ⋯ menu.** On popover open Go captures the active page (`CapturePreview`, JPEG, into an in-memory `IStream`), sends it to the UI as a data URL for `#freeze`, waits for the UI's `m frozen` acknowledgement (image painted, two animation frames), then hides the page window. On close it shows the page and hides the image. Cost: about 46 ms to capture a 878 × 706 page, during which the menu is not yet visible. Pre-capturing on pointer-enter of the menu button would hide that. A frozen page is not interactive, which is acceptable for menus but not for an overlay the user scrolls the page through. The popup-window approach remains the fallback for those.

## 8. Browser automation and agent tool surface

Modeled on [vercel-labs/agent-desktop](https://github.com/vercel-labs/agent-desktop) (Apache-2.0; a Rust CLI and C library that lets agents operate desktop apps through OS accessibility trees, with a Windows adapter). Phoenix adopts its **contract**, not its code, for browser tabs.

### Contract

- **Observe, act on a ref, observe again to verify.** No pixel matching by default.
- **Snapshots** return a JSON tree with a `snapshot_id`, a `ref_count`, and elements that carry a `ref` and `available_actions`.
- **Refs are scoped to their snapshot** (for example `@s8f3k2p9:e8`). A bare ref is rejected as `INVALID_ARGS`. If the page changed, the tool returns `STALE_REF` or `AMBIGUOUS_TARGET` and the agent must re-snapshot.
- **Snapshot options:** skeleton (outline only), compact, root (drill into one region by ref), interactive-only. These keep token cost low for local models.
- **Structured errors:** `code`, `disposition.retry` (safe / unsafe / never) and a `suggestion`. The agent retries only when retry is safe.
- **Headless first.** Actions on refs go through CDP into the tab and do not move the user's mouse or steal focus.
- **Privacy:** snapshots, screenshots and clipboard contents can hold private data. Redact them in logs.

### Proposed tool groups (names are provisional)

| Group | Tools |
|---|---|
| Observe | `snapshot`, `find`, `get`, `is`, `screenshot`, `list_tabs` |
| Act on a ref | `click`, `double_click`, `right_click`, `type`, `set_value`, `clear`, `focus`, `select`, `toggle`, `check`, `uncheck`, `expand`, `collapse`, `scroll`, `scroll_to`, `hover`, `drag`, `press` |
| Tabs and navigation | `tab_open`, `tab_close`, `tab_switch`, `navigate`, `back`, `forward`, `reload` |
| Files | `upload` (workspace file into a file input), `await_download` |
| Control | `wait` (element, state or navigation), `batch`, `trace` / session log |
| Phoenix-only | `ask_user`, `fill_credential` (Phoenix fills; the model sees a placeholder), `request_approval`, file tools, Drive tools, MCP tools |

### Implementation

- Snapshot built from the CDP accessibility tree plus DOM snapshot, using a Phoenix-defined format (role, name, state, bounds) so Chromium's raw AX structure never leaks into prompts.
- Actions via CDP trusted input (`Input.dispatchMouseEvent` / `dispatchKeyEvent`). File uploads via `DOM.setFileInputFiles`.
- Screenshots only for vision-capable models (JPEG, reduced quality by default).
- Prefer in-process CDP over a remote-debugging port, which would expose the logged-in browser to every local process.
- All behind `cdp.Session` and the browser interface so another engine can be substituted.

### Native desktop apps (secondary feature)

agent-desktop's other command groups (`launch`, `list-windows`, `focus-window`, notifications, clipboard) map to the "other desktop apps" feature. Its C FFI library (`libagent_desktop_ffi`) or CLI could serve as a sidecar. **[unverified]** Windows maturity and the cost of a Rust build dependency. Evaluate only when that feature starts. For web pages CDP is the better fit than UI Automation: more precise, trusted input, no focus needed.

## 9. Agent runtime

- Tool registry defined as Go structs with JSON Schema. Tool groups are user-toggleable.
- **Permission tiers:** read-only (automatic); page actions (within allowed scope); sensitive (submit, send, delete, pay, credentials) always require user approval.
- Per-run domain allowlist, step budget, visible action log, stop control (`context.Cancel`).
- Page content is untrusted data, never instructions.
- **User profile store:** structured facts (name, address, work authorization, salary expectations, links). An `ask_user` tool plus a "missing answers" queue. The agent never invents values.
- **Application tracker:** job, URL, status, résumé version, date, notes.
- Passwords are filled by Phoenix from its vault. The model sees placeholders only.

## 9a. Providers (implemented 2026-11-03)

Code: `internal/provider` (CLI drivers and event parsers), `internal/providerhub` (config + vault + providers), `internal/secrets` (DPAPI vault), `internal/config` (`config.yaml`), `cmd/spike/providers_windows.go` and `ui/src/provider-card.ts` (Settings tab), `cmd/provcheck` (live developer check).

**Approach.** The first two providers, **Claude** and **Codex**, run through the vendors' own command-line clients (`claude`, `codex`), not through SDKs. Each has two auth modes, chosen per provider in Settings:

| Mode | How it works |
|---|---|
| `cli` (default) | Reuse the sign-in the user already has (Claude subscription or Console login; ChatGPT plan). Phoenix asks the CLI for its state (`claude auth status --json`, `codex login status`) and runs the CLI, which uses its own stored login. "Sign in" runs `claude auth login --claudeai` or `codex login` in a visible console window |
| `api_key` | The key is in the vault (`vault:provider-<id>` in `config.yaml`), or an `env:` reference. It reaches the CLI only as `ANTHROPIC_API_KEY` / `CODEX_API_KEY` in that one child process's environment, never on the command line |

**Phoenix does not read `.credentials.json`, `.claude.json` or Codex's `auth.json`.** The user asked for "easy auth" from those files; the same ease comes from asking the CLI whether it is signed in, with no token handling in Phoenix. Taking a subscription's OAuth token out of those files to call the API from another program is also not what Anthropic permits for subscription plans; spawning the official CLI is. Mode is decided by Phoenix: in `cli` mode ambient `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` and similar variables are removed from the child's environment so the chosen sign-in is the one used.

**Isolation.** Phoenix's agent gets only the tools Phoenix passes it, not the CLI's own. Claude runs with `-p --output-format stream-json --verbose --include-partial-messages --no-session-persistence --disable-slash-commands --setting-sources= --tools= --strict-mcp-config`; Codex with `exec --json --ephemeral --ignore-user-config --ignore-rules -s read-only --skip-git-repo-check -C <empty temp dir>`. Prompts go on stdin. A run's working directory is a fresh empty temp directory, removed afterwards. Phoenix's own tools will be supplied as an MCP server (`--mcp-config`); not built yet.

**Events.** Both CLIs' streams are normalized to `start`, `text_delta` / `text`, `thinking`, `tool_use`, `tool_result`, `usage`, `rate_limit`, `error`, `done`. Claude streams text deltas and reports 5-hour and 7-day subscription utilization; Codex reports whole messages only.

**Verified live (Claude Code 2.1.316, Codex 0.164.0, Windows 10):**

| Check | Result |
|---|---|
| Detection | Both found, versions read, signed-in state and plan read; all providers detected in about 0.4 s |
| Claude run | "ok" reply; isolation confirmed in the init event (0 tools, 0 MCP servers, 0 skills, 0 slash commands); init event about 0.7 s after launch; first text 1.6 s (Haiku) to about 4 s (default model), so most of the wait is model latency, not CLI start-up |
| Codex run | "ok" reply; first text after about 4 to 6 s; usage and thread id parsed |
| API-key mode (Claude) | A deliberately fake key produced "Invalid API key", proving the key reaches the CLI through the environment and the subscription is not used as a fallback |
| Vault | The key file is DPAPI ciphertext; `config.yaml` holds only `vault:provider-claude`; the key is in no log |
| Settings UI | Switch mode, save key (input clears), remove key (returns to CLI mode, deletes the vault file), Test, Stop, re-check |
| Tests | Go unit tests cover stream parsing (fixtures from real runs, plus synthetic tool-use lines), config round trips, vault, hub, and that no key ever appears on a command line or leaks across modes |

**Not verified:** the "Sign in" buttons end to end (they would change the user's real login); Codex API-key mode (needs a real key; `CODEX_API_KEY` for `codex exec` is taken from the CLI's documentation, not observed); real `tool_use` / `tool_result` lines from Claude (parsed from the documented format only, since all tools were off); a sign-out detected after the fact.

**Known limits and next steps.**
- Every run starts a new CLI process (about 0.7 s before the init event). An agent loop needs one long-lived process (`--input-format stream-json` for Claude, `codex mcp-server` or app-server mode for Codex).
- With `--setting-sources=` the user's hooks and settings are not loaded, but the init event still listed 3 plugins and 5 agents; whether those can add behavior in a run was not checked.
- Model and effort are chosen in Settings and the quick picker (section 9b). Claude's default model cost about $0.03 for the test prompt, a Haiku run $0.0007.
- `config.yaml` is rewritten from a typed struct: unknown top-level sections are kept, **comments are not**. The technology table's comment-preserving requirement is not met yet.
- The vault is a file per secret next to the config, not in SQLite yet. DPAPI protects against other Windows users, not other programs running as the same user.
- Dev switch: `PHOENIX_TEST_PROFILE=<dir>` redirects the config and vault, so tests never touch the real profile.

## 9b. Model picker and Settings menu (implemented 2026-11-03)

Code: `provider.Models` (per provider), `providerhub.Hub.SetModel/Selected/Models`, `config.Store`, `cmd/spike/providers_windows.go`, `ui/src/model-list.ts`, `quick-model.ts`, `browser-settings.ts`.

**Where models come from.** Claude Code has no command that lists models, so Phoenix offers its documented aliases (`fable`, `opus`, `sonnet`, `haiku`) plus "CLI default"; the concrete model that answered is read from the run's start event and shown after a Test (`haiku` resolved to `claude-haiku-5-5`). Codex ships a catalog: `codex debug models` (about 0.8 s) is parsed for slug, display name, description, context window, supported and default reasoning efforts; models Codex marks hidden are omitted, models not supported in the API are omitted in API-key mode, and order follows the catalog's priority. The hub caches each list per auth mode; Re-check refreshes it. Any other valid model id can be typed in, because a model can be newer than the CLI's list.

**Where it is used.** `Settings.Model/Effort` become `--model` / `--effort` for Claude and `-m` / `-c model_reasoning_effort="<e>"` for Codex. Both are validated (`ValidModelID`, `ValidEffort`) in the config loader, the hub and again in `Run`, because they become process arguments: a value starting with `-` or containing a space or quote is refused before any process starts.

**Settings menu.** The Settings tab has a left nav (AI providers, Browser, General) switched locally by a `data-section` attribute. Each provider card has a collapsible "Model: Haiku · Extra high" list with effort buttons and a custom-id field. Browser holds the address-bar search engine (`browser.search_url`: presets, or a custom https address containing one `{query}`; replaces the old `PHOENIX_SEARCH_URL` placeholder). General shows the config path and opens its folder (the path comes from the store, never from the UI).

**Quick picker.** The agent panel footer shows "Claude · Haiku · Extra high"; its popover switches provider (which also sets the default), model and effort, sharing the same component and the same `v model` message as Settings.

**Config store.** `config.Store` (mutex, `View`, `Update` that saves before returning and rolls back on a save error) is shared by the provider hub and the browser settings, so two writers cannot clobber each other.

**Verified live.** Claude `--model haiku --effort low` ran and resolved to `claude-haiku-5-5`; Codex `-m gpt-6-luna -c model_reasoning_effort="low"` ran; an unknown Claude model returns a readable "issue with the selected model" result; an unknown Codex model returns the API message (the raw JSON body is now unwrapped); `--help` as a model is refused. In the app (test profile): the lists render, choices persist to `config.yaml`, a typed-in model works, an invalid one is refused in the UI, a Test shows the model used, switching provider in the quick picker saves the default, and a search from the address bar used the configured engine.

**Not done.** Per-conversation model overrides (the agent loop does not exist yet), cost and limit hints in the picker, and showing why a catalog entry is unavailable.

## 9c. More providers (Gemini, OpenRouter, Mistral, Z.ai, Grok) and a UI pass (implemented 2026-11-03)

Code: `internal/provider/openai.go` (driver, specs, parser), `config` (`providers.<id>`, `base_url`), `providerhub` (`SetBaseURL`, cache keyed by mode, base URL and key presence), `cmd/spike/providers_windows.go`, `ui/src/provider-card.ts`, `model-list.ts`, `quick-model.ts`.

**Approach.** These five publish an OpenAI-compatible chat API, so one standard-library driver (`OpenAICompat`) serves all of them: `POST {base}/chat/completions` with server-sent events, `GET {base}/models`. No third-party LLM library was adopted: the Go libraries found (llmhub, omnillm, gollm, litellm) each miss at least one of the five and would put an unvetted dependency next to API keys. There is no vendor CLI for these, so the only auth is an API key (vault or `env:`), sent only as a `Bearer` header to the configured base URL.

| Provider | Default base URL | Source of the URL | Effort sent |
|---|---|---|---|
| Gemini | `https://generativelanguage.googleapis.com/v1beta/openai` | Google's OpenAI-compatibility page (streaming, tools and a models list confirmed there) | `reasoning_effort` |
| OpenRouter | `https://openrouter.ai/api/v1` | from memory, not checked | `reasoning: {effort}` |
| Mistral | `https://api.mistral.ai/v1` | from memory, not checked | none |
| Z.ai | `https://api.z.ai/api/paas/v4` (Coding Plan: `.../api/coding/paas/v4`; China: `https://open.bigmodel.cn/api/paas/v4`) | third-party docs found by search | none |
| Grok | `https://api.x.ai/v1` | from memory, not checked | none |

**Decisions.**
- **No default model is hard-coded.** Model names change quickly and my knowledge of them is dated, so the list comes from the provider's `/models` (Gemini's `models/` prefix is stripped; embedding and moderation models, Mistral non-chat models and OpenRouter image-output models are dropped; descriptions are clipped). Until a model is chosen, a run fails with "choose a model first". If the list cannot load, the UI says so and a model ID can still be typed.
- **Effort is sent only where a provider is known to accept it**, and only if the user picked one; Mistral, Z.ai and Grok get no extra field because they may reject unknown ones. The Gemini and OpenRouter parameter names are from memory and have not been run against the real services.
- **The base URL receives the API key**, so it is validated everywhere it is accepted (config load, hub, driver): `https`, or `http` only for loopback (local servers); no credentials, query or fragment. Redirects are never followed, and error text has the key scrubbed.
- **Config.** HTTP providers live under `providers:` by id (`gemini`, `openrouter`, `mistral`, `zai`, `grok`); their `auth` is always `api_key`. Unknown keys under `providers:` are dropped on load.
- **Not built:** tool calls (the driver streams text only; Phoenix will own the tool loop for these providers, unlike Claude and Codex whose CLIs run theirs), cost caps, retries, and per-provider rate-limit display (OpenRouter returns `usage.cost`, which is parsed).

**Verified.** Unit tests against a local fake server: streaming, usage and cost, mid-stream errors, missing `[DONE]`, error bodies without the key, redirect refusal, model-list parsing for all providers, base-URL validation, config and hub behavior. In the app, a local fake provider on `127.0.0.1` exercised the whole path: save a base URL and a key, the model list (60 models, 40 shown with a filter), choosing a model, Test (`Model used: fake-model-07`), the quick picker, and the key's absence from the log and `config.yaml` (the vault file is DPAPI ciphertext). **Not verified:** any real Gemini, OpenRouter, Mistral, Z.ai or Grok account, since no keys were available. The first real test should be Gemini (free tier).

**UI pass.** Reviewed against Impeccable's vocabulary:
- *typeset*: one scale (12, 13, 17, 24 px; each heading step is at least 1.25 times the one below), a display face for headings (`Segoe UI Variable Display`, falling back to Segoe UI), mono only for IDs and paths.
- *layout*: settings is a left nav plus one readable column; providers are one grouped list with hairline dividers (Sign in with a subscription / Use an API key), each row opening into its details, instead of a card per provider; no nested cards.
- *distill*: the latency probes moved into a collapsed Diagnostics section; the start page is a short "Get started" list that reflects real state (is a provider ready?).
- *colorize/quieter*: the accent is used for focus, selection, the primary action and the brand; status is a dot plus a word, never color alone.
- *clarify/onboard*: the agent panel's tabs work and each has an empty state that says what will appear and what is not connected yet; Settings is reachable from the rail.
- *polish*: SVG icons replace emoji and Unicode arrows, popovers fade in about 100 ms (and honor reduced motion), the placeholder `<img>` has a `src`.
- The quick picker lists only providers that are ready, plus the default.

**Open.** A full `critique` pass with screenshots at other window sizes and DPI, a keyboard-only walkthrough of the new rows, a light theme, and checking the layout below the 1270 px width used here.

## 9d. Chat with the agent (implemented 2026-11-03)

The agent panel's first tab is a chat. It talks to the default provider and model (the quick picker under the panel), through the same `hub.Run` the Test button uses, so Claude Code, Codex and the five API providers all work.

- **Go owns the conversation** (`cmd/spike/chat_windows.go`); the UI (`ui/src/chat.ts`, `<ph-chat>`) only draws what Go reports and sends `g send <text>`, `g stop`, `g new`. Go posts a batch of events as `u chat events [...]` (`user`, `start`, `delta`, `done`, `error`, `stopped`, `reset`). One conversation, held in memory only: it is gone when Phoenix closes.
- **History is replayed on every turn**, because the CLIs are run without session storage. `provider.Request.History` becomes real chat messages for the HTTP providers and a `Transcript()` text for the CLIs. Limits: 24 messages and 100,000 characters (newest kept, starting on a user turn); one message is cut at 20,000 characters.
- **Streaming** deltas are coalesced to about 20 UI updates a second. The log autoscrolls only while the user is already near the bottom. Text is placed with `textContent` and `white-space: pre-wrap`: model output is never parsed as HTML (verified: `<b>` and `<i>` in a reply stay literal). No markdown rendering yet.
- **Stop** is the Send button turning into Stop, plus the panel's Stop button (`agent.stop`); both cancel the run's context, which kills the CLI process or closes the HTTP stream. A stopped partial reply stays in the history.
- **Honest about its limits:** the system prompt tells the model it cannot click, type or open files yet, and the empty state says so. Acting (a tool loop with approvals) is the next milestone.
- **Share this page** (`cmd/spike/page_windows.go`): a checkbox under the message box, off until the user ticks it (remembered). When on, `g send <text> 1` makes Go run one fixed script in the active tab's top frame (`ExecuteScript`, vtable slot 29; 4 s timeout) and read back URL, title, selection (5,000 characters) and visible text (30,000 characters). It is appended to that turn's prompt only, fenced as untrusted data (copies of the fence inside the page are broken, and the model is told never to follow instructions in it); the history keeps only what the user typed, so the page is read afresh each turn. The user sees `Shared page: <title> (<host>)` under their message, never the full address. Only the first frame is read: cross-origin iframes, canvas and images are not. Home and Settings tabs share nothing and say so. Page text is never logged.
- **Privacy:** the log records only the op name (`msg g send`), never the text. Checked against a run of several messages.
- **Verified** against a local fake OpenAI-compatible server: two-turn history (server saw 2 then 4 messages), streaming, Stop mid-run, status pill and Stop button state, metadata line (time, tokens). Unit tests cover `trimHistory`, `friendlyChatError` and `Transcript`/`buildRequest` history. **Not verified:** a real provider reply (Gemini is the first thing to try), long conversations, very long single replies.

## 9e. Tailwind and the UI reshape (implemented 2026-11-03)

**Decision.** The user asked for Tailwind CSS and a full reshape of the UI, guided by Impeccable. I measured Untitled UI React first (9d's neighbour spike): it is MIT, copy-in, on React Aria and Tailwind v4, but cost 529 KB of JS against 25 KB for the vanilla panel and streamed no faster once the vanilla chat got `content-visibility: auto`. So Tailwind is used as a **build-time CSS compiler** and the custom elements stay vanilla. React can still come later without redoing the tokens, because they are Tailwind theme variables.

**Pipeline.** `ui/app.css` is the Tailwind entry: `@import "tailwindcss" source(none); @source "./index.html"; @source "./src";` so only the shell and the TypeScript sources are scanned for classes. `npm run build:css` writes `ui/.cache/app.css` (minified; git-ignored). `tools/buildui` inlines that CSS and the esbuild JS into `internal/ui/dist/index.html` and puts SHA-256 hashes of both into the CSP, so the page is still one file with no `unsafe-inline` and no network request. `scripts/build.ps1` runs `npm ci --ignore-scripts` when `node_modules` is missing (28 packages, install scripts off), then `tsc`, `build:css`, `buildui`, `go build`. Tailwind is pinned to an exact version (4.3.4).

**Rules for the stylesheet.**
- Design tokens live in `@theme` as OKLCH custom properties (`--color-*: initial` first, so no default palette leaks in). Components are `@layer components` classes that `@apply` utilities; markup carries semantic class names (`.btn`, `.chat-msg`, `.rail-btn`), not long utility strings. Anything used inside `@apply` that is not a built-in utility must be declared with `@utility` (`drag`, `no-drag`, `tabular`, `scroll`).
- **Go and the UI agree on `id`s and `data-` attributes, never on class names.** Go addresses `chat`, `quick-model`, `agent-pill` (`data-state`), `agent-stop`, `prov-*`, and so on; the settings navigation is found by `data-settings-nav`. A restyle that renames a class must not break behaviour (it did once: the settings navigation went dead when its class was dropped, which is why it now has a data hook).
- State is shown with `data-*` attributes and `aria-*` (`aria-pressed`, `data-streaming`, `data-tone`), styled with attribute selectors.

**Design.** The design world, tokens and rules are in `DESIGN.md`. In short: Operate mode (a tool used for hours, so quiet neutral surfaces tinted toward one hue, one accent colour reserved for the primary action, selection, focus and "running"), one family (Segoe UI Variable), one icon set drawn in `ui/src/icons.ts`, a filled dot for state and a hollow ring for "not set up".

**Measured.** Built page: 48 KB CSS, 27.7 KB JS, 92 KB HTML in total. `npx impeccable detect` on the built page: 0 findings (two `cramped-padding` reports on the Get started steps were cleared by drawing the divider as an inset hairline instead of a `border-top`). Text contrast against its real background: smallest ratio 5.1 (12 px metadata), body text 8 to 16. Checked by eye in a real window: home, Settings (providers, Browser), the model popover and a streamed chat reply, against a fake provider.

**Not yet checked.** A keyboard-only walkthrough, other window sizes and DPI, and Windows 11. There is no light theme.

## 9f. Quick actions (implemented 2026-11-03)

Purpose: less talking, more action. A bar of one-click buttons sits under the page at all times (`<ph-quick-actions>`, `ui/src/quick-actions.ts`). Each button is a request the user would otherwise type; the buttons change with what the tab looks like.

- **Go chooses, the UI draws.** `internal/quick` is pure Go: `Classify(Signals)` guesses the page kind (job posting, article, product, form, sign-in, video, search results, generic, or start for home/settings/blank) and `For(kind, hasSelection)` returns up to six actions, most likely first (drawn as the one accent-outlined button). With text selected, the actions on the selection come first. Tests cover classification (including near misses such as a LinkedIn feed or a careers page with the wrong title) and that every id exists.
- **Signals are read locally.** `cmd/spike/actions_windows.go` runs one fixed script in the tab (JSON-LD `@type`s, `og:type`, visible form fields, password field, `<article>` length, price markup, selection length). The result is used to pick buttons and is not sent to a model, not logged and not kept beyond the kind. `runScript` in `page_windows.go` is the shared helper for such fixed scripts and for reading the page.
- **When it runs.** On tab switch and navigation start (a guess from address and title shows at once, so the old page's buttons never linger), on load end and 1.5 s later (sites that build themselves late), and when the pointer enters the bar (the cue that the user may have just selected something). An unchanged answer redraws nothing, so buttons do not move under the pointer.
- **A click sends an id.** `g act <id>`; Go looks it up and the wording lives in `quick.catalog`, so this path cannot carry arbitrary text. The chat shows the short label as the user's message and the model gets the full request. Actions that read the page attach it for that turn without the Share page toggle. Prompts that need personal facts say to ask up to three questions instead of inventing them. The log records `chat action <id>` only.
- **One request at a time.** The buttons are disabled while a reply streams and come back on done, stop or error.
- **Honest about what they do.** The buttons only ask the model; none of them acts on a page yet. Prompts that would imply acting (for example form answers) say the user pastes them in. When the tool loop with approvals arrives, an action becomes a task instead of a prompt, behind the same ids.
- **Verified** with a fake provider and test pages: job, article, form and plain pages classify correctly; a selection switches the set; a click sends the label, attaches the page and selection and the model sees both; chips disable during a stream and return after Stop.

**Next:** user-defined actions in `config.yaml`, keyboard shortcuts for the first nine, ordering by use, and actions that run tasks once the agent can act.

## 10. Data (SQLite)

Tables: history (+ FTS5), bookmarks (+ FTS5), vault entries (encrypted), profile facts, applications, agent runs and actions, settings. Imports: Netscape bookmark HTML; Chrome/Edge `History` (copy first; timestamps are microseconds since 1601); Firefox `places.sqlite`; password CSV exports (no decryption of browser stores).

## 11. Files, Drive, Resume Matcher

- **Files:** `os.Root` confinement, `fsnotify`, FTS5 index, text extraction (PDF via `pdfcpu` or bundled `pdftotext`, DOCX via zip+XML), delete to Recycle Bin.
- **Drive:** `drive.file` scope with the Google Picker, sign-in through the system browser (Google blocks OAuth in embedded webviews).
- **Resume Matcher:** MCP client behind a `ResumeService` interface. Endpoint and auth method are still unknown. Résumé held as JSON Resume, diffed for review, rendered to PDF with WebView2 `PrintToPdf`.

## 12. Security summary

- UI WebView2: CSP `default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:`; no inline handlers, no `eval`; navigation away from the embedded origin blocked; DevTools off in release.
- Page WebView2s: web messaging disabled, host objects disabled; cannot reach Go.
- Message handler: origin check, op/action allowlist, argument validation.
- `config.yaml` is outside agent reach (no read or write by agent tools). Provider and MCP server changes need user confirmation. Secrets are referenced from the vault by default and masked in the UI and logs.
- No bulk scraping of sites whose terms forbid it. Sensitive actions gated by approval.

## 13. Engine strategy

- Start on WebView2 behind the browser interface.
- If startup, new-tab time or memory miss targets after tuning (pre-create environment and a spare tab, discard background tabs, check GPU and hosting mode), prototype CEF. Candidate Go binding with claimed Windows support: `energye/energy` **[unverified]**. `bnema/purego-cef` is Linux-only.
- Servo is not a near-term option (unstable embedding API, no CDP, compatibility unknown).

## 14. Prototype plan with pass/fail checks

1. UI WebView2 + TypeScript bridge: one custom element (`<ph-tab-strip>`), one click action to Go, one `u` update back.
2. Build pipeline: esbuild from Go, embedded output, type-check step in CI, bundle size check.
3. Measure click-to-paint and Go round-trip. Compare delimited-string vs JSON codec; compare shared buffer vs messages for image payloads.
4. Overlapping dropdown via a popup window.
5. Type-ahead over 100k history rows.
6. Persistent profile: logins survive restart; `window.open` becomes a tab; file upload via a normal `<input type=file>`.
7. Agent loop on two real multi-step forms (one Greenhouse, one Workday) using snapshot / ref / act / verify, including `STALE_REF` recovery.
8. `PRODUCT.md` and `DESIGN.md` written; `impeccable detect` passes on the UI.
9. Idle memory with 5 tabs; startup time to first paint.

## 14a. Prototype 1 results (2026-11-03)

Code: `cmd/spike` (Go host), `ui/` (HTML, CSS, TypeScript), `tools/buildui` (esbuild bundler), `scripts/build.ps1`. Built and run on Windows 10 (19045), Go 1.27.3, WebView2 runtime 156.0.4314.58, `github.com/wailsapp/go-webview2` v1.0.23 (pure Go, no cgo).

**Measured or observed:**

| Check | Result |
|---|---|
| Executable size | 3.9 MB (stripped). UI bundle: JS 1.5 KB, CSS 6.4 KB (minified). TypeScript strict type-check clean |
| Borderless window | Works. Needs `WM_NCCALCSIZE` handling plus a `SetWindowPos(SWP_FRAMECHANGED)` after creation; without it the native title bar stays |
| Resize zones | All 8 edge and corner zones plus centre return the right `WM_NCHITTEST` codes (WebView inset by 5 px so the parent receives them). Maximize and restore work, including the maximized-frame inset |
| CSS drag region (`app-region: drag`) | Works on runtime 156: a real mouse drag over the title bar moved the window by exactly the cursor delta (120, 84). A real click on the HTML maximize button (`no-drag`) toggled maximize |
| `IsNonClientRegionSupportEnabled` | Works through a raw COM call (vtable slots 37 get, 38 put; read-back true). The generated `ICoreWebView2Settings9` binding in go-webview2 v1.0.23 is **wrong** (truncated vtable; pointer passed where a BOOL is expected), so generated bindings need verification before use |
| Go round trip (UI to Go and back, 200 sequential, 3 runs) | p50 0.10 ms, p95 0.20 to 0.30 ms, max 0.2 to 0.5 ms. Caveats: `performance.now()` in Chromium is coarsened to 0.1 ms, so the median is at the resolution limit; idle machine; synthetic clicks; measures messaging only, not click-to-photon |
| Go to UI messages | Environment info (runtime version, DPI, non-client status) and window-state updates reached the UI as designed |
| CSS-only tab selection, `popover` menu | Both work |

**Found and fixed:** a WebView2 created while its parent window is hidden reports `document.visibilityState = "hidden"` (no frames, screenshots hang) until `Show()` is called on the controller after the window is shown.

**Not yet tested:** a drag-region overlay inheritance case (WebView2Feedback issue 5696; the menu in the prototype sits in the toolbar, outside the drag region); Windows 11 snap layouts (no Windows 11 machine); `TrySuspend`; popup window over a page tab (freeze-frame tested in Prototype 2); DPI change between monitors; tray; toasts; true click-to-photon latency; hiding to tray.

**Dev-only tooling in the spike:** `PHOENIX_LOG=1` writes a log to `%TEMP%`; `PHOENIX_DEBUG_PORT=<port>` opens a loopback DevTools port. The port must never ship in a release build.

## 14b. Prototype 2 results: tabs and page windows (2026-11-03)

Code: `cmd/spike/tabs_windows.go` (tab list, page controllers, slot layout, freeze-frame), `cmd/spike/com_windows.go` (raw COM helpers, reusable event-handler object, `IStream` reader), `ui/src/tab-strip.ts` (`<ph-tab-strip>`). Verified by driving the UI over CDP and inspecting page targets; window-only captures.

| Check | Result |
|---|---|
| One WebView2 controller per page tab, same data folder and browser args | Works. Closing a tab (`Controller.Close`) removes its CDP target |
| Page positioned over `#page-slot` | Page viewport matched the slot exactly (878 × 706 CSS px at 100% DPI, `visibilityState` visible). Not yet tested above 100% DPI |
| Address bar → URL or search; back, forward, reload | Work; history state (`HistoryChanged`) drives the button disabled state; address shows the canonical URL after Enter (the box is blurred on Enter so Go's value is not skipped) |
| `window.open` | `NewWindowRequested` handled in Go (`put_Handled`) and opened as a new tab |
| Tab select, close, closing the last tab | Work; closing the last tab leaves a Home tab |
| Page isolation from Go | `IsWebMessageEnabled=false` reads back false. The `window.chrome.webview` object still exists in the page and `postMessage` does not throw, but the host callback never fires (logged as a security event if it ever does) |
| Menu over a page tab | Works via freeze-frame, see section 7 |

**Found:** go-webview2's `Chromium.MessageReceived` echoes every received message back to the sender (`sender.PostWebMessageAsString(message)`). Harmless for the UI today (no op collides) but wasteful and it would echo page messages if messaging were ever enabled; the host will own this handler when the library is replaced or vendored. The generated and hand-written settings getters use a 1-byte `bool` for a 4-byte `BOOL` out-parameter; treat read-backs from them with suspicion.

**Observed once, not reproduced:** in the first run a stray Google tab appeared after one click on the new-tab button. Four later runs, including the same sequence, produced only the expected tabs. Cause unknown; a debug log of every `createWebTab` call now exists to catch it.

**Not yet tested:** DPI above 100% and moving between monitors, page focus and keyboard shortcuts (Ctrl+T, Ctrl+L, Ctrl+W), downloads, permission prompts, popups from `target=_blank` links by real clicks, `TrySuspend` of background tabs, memory with 5 tabs.

## 14c. Prototype 2b: shortcuts, DevTools, and a real-display bug (2026-11-03)

**Bug found by the user ("google.com doesn't open anything"):** pages loaded and reported `visibilityState: visible`, but were never on screen. The UI webview's window sat above the page windows in z-order and its opaque slot background covered them. Earlier CDP checks could not see this, because CDP renders a page whether or not it is visible on the display. Resizing a WebView2 controller raises its window, so the order is re-asserted: the UI window is sent to `HWND_BOTTOM` after startup, on every size change and on every layout pass. **Lesson: verify page visibility with a window-level capture or the child-window z-order, never only through CDP.** Confirmed afterwards: z-order lists the page window first, and a `PrintWindow` capture of Phoenix shows the page.

**Shortcuts** (`cmd/spike/keys_windows.go`), attached to the UI controller and to each page controller through `AcceleratorKeyPressed`:

| Keys | Action |
|---|---|
| Ctrl+T | New tab, address bar focused |
| Ctrl+W, Ctrl+F4 | Close tab |
| Ctrl+Tab, Ctrl+PgDn / with Shift, Ctrl+PgUp | Next / previous tab |
| Ctrl+1 to 8, Ctrl+9 | Nth tab, last tab |
| Ctrl+L, F6, Alt+D | Focus the address bar |
| F5, Ctrl+R | Reload |
| Alt+Left, Alt+Right | Back, forward |
| F12, Ctrl+Shift+I | DevTools for the active page |

Handled keys are marked handled so the page does not also see them, including auto-repeats (which do not re-run the action). Actions run from the message loop, not inside the key callback. The UI webview's own browser accelerators (Ctrl+R, F5, F12) are disabled outside debug runs.

Verified with real keystrokes while Phoenix was the foreground window: Ctrl+T, Ctrl+Tab, Ctrl+1, Ctrl+Shift+Tab, Ctrl+W, Ctrl+L (Win32 focus moved from the page into the UI webview), F12 (a DevTools window opened for the page). Not verified by keystroke: F5/Ctrl+R, Alt+Left/Right, Ctrl+9, Ctrl+PgUp/PgDn, F6, Alt+D (same code path, different keys).

**Right-click → Inspect:** page webviews now have DevTools and the default context menu enabled. DevTools opening was verified (F12); the native context menu itself could not be captured, so the presence of the "Inspect" entry is expected from the WebView2 default, not observed.

**Vendored library.** go-webview2 is now copied to `third_party/go-webview2` and wired through a `replace` in `go.mod`, with three defects addressed or worked around: (1) `errorCallback` called `os.Exit(1)` on any COM error; a user's session ended this way with "The group or resource is not in the correct state" after closing a tab. It now exits only before the environment is ready, and every error is logged with a stack. (2) It echoed every UI message back to its sender; removed. (3) Its accelerator handling resets `Handled` to false after the callback, which is why shortcuts use a raw handler instead. The crash was not reproduced in later stress runs (create, navigate and close in several orders), so its exact trigger is unknown; the log now captures a stack if it recurs.

## 14b. Wails v3 hybrid spike (2026-11-03)

Code: `experiments/wails-hybrid` (its own Go module, Wails v3.1.0, the vendored `go-webview2` for tabs). Windows 10 (19045), Go 1.27.3, WebView2 156.

**Passed.** (1) A frameless Wails window loads a custom UI (custom title bar, `--wails-draggable`, window buttons through the runtime). (2) UI to Go: `System.invoke(string)` from `/wails/runtime.js` arrives in `Options.RawMessageHandler` as a raw string, the same shape as our U+001F protocol, so `protocol.ts` is the only UI file that must change. (3) Go to UI: `WebviewWindow.ExecJS` delivered messages (events are the other option). (4) `NativeWindow()` returns the HWND; `edge.NewChromium().Embed(hwnd)` created a tab controller as a child, positioned from the UI's slot rectangle with DPI scaling, and example.com rendered in it with a different user-data folder from Wails's own. (5) Airspace: after embedding, the Wails UI webview sat above the tab and hid it; `SetWindowPos(uiView, HWND_BOTTOM)` (our existing `lowerUI` idea) fixed it, found by listing the window's direct children before the first tab exists. (6) Maximize and restore re-laid out the tab to the slot and kept the z-order. (7) `Options.Windows.AdditionalBrowserArgs` takes `--remote-debugging-port`, so the same CDP test tooling works; `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` does not reach the Wails webview.

**Not tested yet.** Keyboard focus hand-off between the UI webview and a tab (Wails has `webview_focus_handoff.go`), resize borders and Windows 11 snap layouts on a Wails frameless window, a menu popover drawn over a tab (our freeze-image trick is still needed), tray and single-instance, DPI changes across monitors, the installer, and the binary size (spike exe 18 MB against 8 MB now).

**Migration plan.**
1. *Shell.* New `cmd/phoenix` Wails app. Window, frameless options, tray (`SystemTray`), single instance and menus from Wails; delete the hand-written Win32 chrome (`win32_windows.go`, most of `main_windows.go`). Keep `tabs_windows.go`, `chat_windows.go`, providers, vault and `internal/*` unchanged; replace `post()` with `ExecJS`/events and the `WM_APP` message pump with `application.InvokeAsync`.
2. *Protocol.* `protocol.ts` swaps `chrome.webview.postMessage` for `System.invoke` and the listener for a global the host calls. The wire format and every component stay.
3. *UI delivery.* Serve the built single HTML through the Wails asset server; the CSP gains `'self'` for `/wails/runtime.js`.
4. *Packaging.* Wails's NSIS installer and updater, signed builds.
5. *Later.* Replace our tab controllers with `WebviewPanel` if PR #4880 lands.

## 15. Open decisions

- Pre-created popup windows vs DirectComposition hosting (freeze-frame covers menus; interactive overlays still open).
- Type-checking toolchain for TypeScript (tsc on Node vs native compiler port).
- Whether the agent shares the user's browser profile or uses a separate one.
- Resume Matcher MCP endpoint and authentication.
- Reuse agent-desktop's FFI for native-app control, later.
- CEF trial: only if WebView2 measurements justify it.

## Sources consulted

- WebView2 `ICoreWebView2Settings9`: https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2settings9
- WebView2Feedback issue 5696 (`app-region` inheritance): https://github.com/MicrosoftEdge/WebView2Feedback/issues/5696
- Snap layouts for custom title bars (Microsoft): https://github.com/MicrosoftDocs/windows-dev-docs/blob/docs/hub/apps/desktop/modernize/ui/apply-snap-layout-menu.md
- pywebview frameless snap PR: https://github.com/r0x0r/pywebview/pull/1862
- Wails frameless windows: https://v3.wails.io/features/windows/frameless/
- WebView2 `ICoreWebView2_3` (`TrySuspend`): https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2_3
- WebView2 user data folders: https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/user-data-folder
- Go toast libraries: https://pkg.go.dev/github.com/mtunjic/winrt-toast and https://pkg.go.dev/github.com/hellolib/toast

- agent-desktop: https://github.com/vercel-labs/agent-desktop
- Impeccable: https://github.com/pbakaus/impeccable
- Wails raw messages and binding system: https://wails.io/guides/raw-messages/ and https://wails.io/contributing/binding-system/ (summarized from search results; the pages could not be fetched directly)
- WebView2 JavaScript API: https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/javascript/webview
- WebView2 `WebMessageReceived`: https://learn.microsoft.com/dotnet/api/microsoft.web.webview2.core.corewebview2.webmessagereceived
