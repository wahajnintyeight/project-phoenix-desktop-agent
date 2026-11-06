# Phoenix

[![GitHub stars](https://img.shields.io/github/stars/wahajnintyeight/project-phoenix-desktop-agent?style=flat)](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/stargazers)
[![GitHub forks](https://img.shields.io/github/forks/wahajnintyeight/project-phoenix-desktop-agent?style=flat)](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/network/members)
[![GitHub issues](https://img.shields.io/github/issues/wahajnintyeight/project-phoenix-desktop-agent?style=flat)](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/issues)
[![Last commit](https://img.shields.io/github/last-commit/wahajnintyeight/project-phoenix-desktop-agent?style=flat)](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/commits/main)
![Windows](https://img.shields.io/badge/platform-Windows-0078D4?logo=windows&logoColor=white)
![Go 1.27.3](https://img.shields.io/badge/Go-1.27.3-00ADD8?logo=go&logoColor=white)
![Wails v3](https://img.shields.io/badge/Wails-v3-DF5C4C?logo=wails&logoColor=white)

**A Windows desktop workspace for browsing and getting help from AI models, with an agent designed to work alongside you.**

Phoenix brings a multi-tab browser and AI provider controls into one native desktop app. It is being built around a simple idea: keep the person in control, make the agent's work visible, and make actions visible while the agent works.

> **Project status:** Early development. The browser, provider setup, and model chat are implemented. Claude, Codex, and OpenAI-compatible API providers can inspect and operate Phoenix browser tabs through built-in browser tools. Native computer use can inspect and operate Windows apps without an external desktop agent. Browser file transfers are still in progress. There is no published release at this time.

## What works today

- Wails v3 desktop app (system tray, single instance, installer) with a redesigned dark interface (warm mauve-shadow neutrals, one mauve accent) and a multi-tab browser powered by WebView2.
- Recently visited sites show their website favicons and can be removed individually or cleared from history. Settings → General includes an application restart control.
- Address bar for URLs and web searches, with a configurable HTTPS search provider.
- A quick-action bar under the page: one-click requests that change with the page you are on (a job posting, an article, a form, search results, or text you have selected), so you send a request without typing it.
- Provider setup, model selection, connection checks, and a quick model picker.
- Streaming chat with the selected model, with an optional **Share page** control to include the active page's readable text in your message.
- Resize the assistant panel by dragging its left edge; the width is remembered. The divider also supports arrow keys, Shift for larger steps, and double-click to reset.
- Saved chats and an activity timeline survive restarts. Phoenix restores the latest chat on launch; older conversations are available from the Chats menu. Activity records contain only tool labels, permission decisions and run outcomes.
- **Built-in browser tools:** Claude, Codex, and OpenAI-compatible API providers can list tabs, read a bounded accessibility snapshot, extract paged text and links from whole pages, capture a reduced-size screenshot, navigate, click a control by its visible label or snapshot reference, drag between elements, copy or paste, type into ordinary text fields, fill forms in batches, validate required fields, attach files from Phoenix's library, press supported keys, scroll, wait for page elements and use native select controls. Phoenix uses the same WebView2 profile as the visible browser, so user sign-ins carry over. Browser reads and actions run automatically when requested; no browser extension, external desktop agent, or remote-debugging port is required.
- **Computer use** (enabled by default in new installations): native tools list and capture windows, discover controls, click, scroll, drag, type, copy, and paste. Supported providers can use them automatically while working on your request. Vision-capable models can also inspect screenshots. Actions are visible in Activity and can be stopped.
- Replies are formatted: headings, lists, tables, code blocks with a Copy button.
- **MCP servers:** install as many as you like (paste the JSON from a server's install instructions, or enter a program or web address), review the exact command before it is saved, test the connection and list its tools, then choose which servers the agent may use. OAuth-protected web servers open sign-in in the browser and keep refreshable tokens in the encrypted vault. Claude, Codex, and OpenAI-compatible API providers can use enabled servers; Enabled-server calls run automatically and appear in chat.
- **Application tracking:** the agent can check and record job applications or other form submissions, and save user supplied answers in profile notes for future forms. Application records and remembered notes stay in Phoenix's local file library.
- **Memory:** Phoenix remembers facts, answers, preferences, and choices you choose to save. Review, edit, or delete these entries under **Settings → Memory**; a bounded selection helps the agent avoid asking for the same information again.
- Claude and OpenAI Codex through their command-line clients, plus API-key connections for Gemini, OpenRouter, Mistral, Z.ai, Grok, and DeepSeek. DeepSeek models are discovered from its API; add a key in Settings → AI providers.
- API keys stored in a Windows DPAPI-encrypted vault; Phoenix does not put vault-backed keys in `config.yaml` or show them again in the UI.

Page sharing remains opt-in. It includes the active page's readable text directly with your message; browser tools let supported providers inspect and operate tabs automatically. Passwords, payment details, and authentication codes are not entered by the browser agent. Browser uploads can attach files selected from Phoenix's library; downloading files from pages is not available yet. Tabs opened by the agent are cleaned up after it finishes unless you keep one active.

## Build and run

### Requirements

- Windows 10 or 11.
- The Microsoft Edge WebView2 Runtime. Install the [Evergreen Runtime](https://developer.microsoft.com/microsoft-edge/webview2/) if it is not already present.
- Go 1.27.3 or later and Node.js with npm.
- Claude Code and/or Codex CLI installed and signed in if you want to use those providers. API providers instead need their own API key.

### Build from source

Phoenix is a [Wails v3](https://v3.wails.io) application. Install the Wails CLI once:

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

Then:

```powershell
git clone https://github.com/wahajnintyeight/project-phoenix-desktop-agent.git
cd project-phoenix-desktop-agent
wails3 dev        # run in development mode: rebuilds and restarts when a file changes
wails3 build      # build bin/phoenix.exe
wails3 package    # build the Windows installer (needs NSIS)
```

`wails3 build` type-checks the UI, compiles its Tailwind CSS, bundles everything into one HTML page, then builds `bin/phoenix.exe`. The first build runs `npm ci --ignore-scripts` if `node_modules` is missing, which installs the development tools (TypeScript and the Tailwind CSS CLI). Nothing from npm ships inside the app: Tailwind is only a build-time compiler. `pwsh ./scripts/build.ps1 [-Run]` does the same and installs the CLI if it is missing.

## Provider setup

Open **Settings → AI providers** in Phoenix. Claude and Codex use their respective CLI sign-ins by default. The other providers use API keys. Select a provider and model in Settings or use the quick picker beneath the agent panel.

For provider development, see [the AI provider skill](.agents/skills/phoenix-ai-providers/SKILL.md). It is stored in the repository so contributors can use it after cloning.

Provider API keys are encrypted with Windows DPAPI for the current Windows user. DPAPI protects the stored vault data from being read as plain text, but it is tied to that Windows account and machine; copying the vault to another PC will not make it portable.

Phoenix stores its configuration at `%APPDATA%\Phoenix\config.yaml` and the encrypted key vault under `%LOCALAPPDATA%\Phoenix\vault`.

### Local data and memory

Visited-site history, saved chat transcripts and agent activity summaries are stored locally under `%APPDATA%\Phoenix`. Phoenix keeps up to 100 visited sites, 40 chats (up to 64 messages and 120,000 bytes of transcript text per chat), and 500 activity entries; the Activity tab shows the latest 100. Chat and activity files are each capped at 32 MiB. The Activity tab has a **Clear saved chats & activity** control. Browser history can be cleared separately from the History view.

Transcript storage includes the messages you send and the assistant's replies. Text shared from a page is sent for that turn but is not saved as a transcript message; page details repeated in an assistant reply remain part of that reply. Activity summaries never save tool arguments, tool results, page contents, or approval descriptions. These files are local JSON, not encrypted; the folder is protected by the current Windows account's normal file permissions.

Phoenix also stores files, application records, and Memory entries in its local library database. Memory is editable in Settings and can be deleted at any time.

## Development checks

```powershell
npm run typecheck
npm run build:css
go test ./cmd/phoenix ./internal/...
npx impeccable detect internal/ui/dist/index.html
```

The last command checks the built page against Impeccable's design rules and should report no findings. The full build command also type-checks the UI before bundling it and compiling the Windows app.

## Interface

The interface is vanilla TypeScript custom elements styled with Tailwind CSS v4, compiled to plain CSS at build time and inlined into a single HTML page, so the app makes no network request for its own UI. [DESIGN.md](DESIGN.md) records the colour, type and component rules.

## About the creator

Phoenix is created and maintained by [Wahaj](https://github.com/wahajnintyeight). The project explores a desktop workspace where browsing, AI assistance, and user control live in the same place: models can work with open pages and apps while their actions stay visible in the interface.

## Project notes

- [PRODUCT.md](PRODUCT.md) describes the product direction and current scope.
- [DESIGN.md](DESIGN.md) describes the visual design system.
- [ARCHITECTURE.md](ARCHITECTURE.md) records implementation decisions and open questions.
- Contributions and bug reports are welcome through [GitHub Issues](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/issues).

There is no project-level license file yet. Until one is added, do not assume the project is licensed for reuse.
