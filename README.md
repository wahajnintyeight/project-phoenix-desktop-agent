# Phoenix

[![GitHub stars](https://img.shields.io/github/stars/wahajnintyeight/project-phoenix-desktop-agent?style=flat)](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/stargazers)
[![GitHub forks](https://img.shields.io/github/forks/wahajnintyeight/project-phoenix-desktop-agent?style=flat)](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/network/members)
[![GitHub issues](https://img.shields.io/github/issues/wahajnintyeight/project-phoenix-desktop-agent?style=flat)](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/issues)
[![Last commit](https://img.shields.io/github/last-commit/wahajnintyeight/project-phoenix-desktop-agent?style=flat)](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/commits/main)
![Windows](https://img.shields.io/badge/platform-Windows-0078D4?logo=windows&logoColor=white)
![Go 1.27.3](https://img.shields.io/badge/Go-1.27.3-00ADD8?logo=go&logoColor=white)

**A Windows desktop workspace for browsing and getting help from AI models, with an agent designed to work alongside you.**

Phoenix brings a multi-tab browser and AI provider controls into one native desktop app. It is being built around a simple idea: keep the person in control, make the agent's work visible, and ask before sensitive actions.

> **Project status:** Early development. The browser, provider setup, and model chat are implemented. Phoenix can answer questions about the active page when you choose to share it. With Computer use turned on it can look at and operate your Windows apps (asking first); it cannot yet click or type inside browser pages itself or work with files, and approvals for other providers are still in progress. There is no installer or published release at this time.

## What works today

- Wails v3 desktop app (system tray, single instance, installer) with a redesigned dark interface (calm neutrals, one ember accent) and a multi-tab browser powered by WebView2.
- Address bar for URLs and web searches, with a configurable HTTPS search provider.
- A quick-action bar under the page: one-click requests that change with the page you are on (a job posting, an article, a form, search results, or text you have selected), so you send a request without typing it.
- Provider setup, model selection, connection checks, and a quick model picker.
- Streaming chat with the selected model, with an optional **Share page** control to include the active page's readable text in your message.
- **Computer use** (off until you turn it on, needs the free `agent-desktop` tool): the agent can screenshot and analyze an app window, read its controls, click, scroll, drag, type, copy and paste. You approve each step in the chat (looking once per app, acting every time), and it never gets a shell or a whole-screen capture. Claude only for now.
- Replies are formatted: headings, lists, tables, code blocks with a Copy button.
- **MCP servers:** install as many as you like (paste the JSON from a server's install instructions, or enter a program or web address), review the exact command before it is saved, test the connection and list its tools, then choose which servers the agent may use. Secrets go to the encrypted vault. Used when the default provider is Claude for now; every tool call is shown in the chat.
- Claude and OpenAI Codex through their command-line clients, plus API-key connections for Gemini, OpenRouter, Mistral, Z.ai, and Grok.
- API keys stored in a Windows DPAPI-encrypted vault; Phoenix does not put vault-backed keys in `config.yaml` or show them again in the UI.

Page sharing is opt-in. Phoenix sends the page's readable text with your message so the selected provider can answer questions about it; the agent cannot interact with the page or access files yet.

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

Provider API keys are encrypted with Windows DPAPI for the current Windows user. DPAPI protects the stored vault data from being read as plain text, but it is tied to that Windows account and machine; copying the vault to another PC will not make it portable.

Phoenix stores its configuration at `%APPDATA%\Phoenix\config.yaml` and the encrypted key vault under `%LOCALAPPDATA%\Phoenix\vault`.

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

## Project notes

- [PRODUCT.md](PRODUCT.md) describes the product direction and current scope.
- [DESIGN.md](DESIGN.md) describes the visual design system.
- [ARCHITECTURE.md](ARCHITECTURE.md) records implementation decisions and open questions.
- Contributions and bug reports are welcome through [GitHub Issues](https://github.com/wahajnintyeight/project-phoenix-desktop-agent/issues).

There is no project-level license file yet. Until one is added, do not assume the project is licensed for reuse.
