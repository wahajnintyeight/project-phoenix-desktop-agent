# Changelog

Notable Phoenix changes are listed here. Versions use the `vMAJOR.MINOR.PATCH` release tags.

## [0.2.4] - 2026-11-16

### Added

- **Learning mode:** opt-in, user-reviewed lessons for reusable preferences and workflows. Lessons stay local, are scoped to tasks or shared sites, support correction and replacement, and have bounded recall plus an on-demand search tool.
- A configurable shared memory-context limit, short pinned guidance, and clearer provider-reported usage details.
- GPU acceleration controls in Settings → General.

### Changed

- Keep the collapsed agent chat usable while it runs and show its current reply in the compact bar.
- Keep reply actions in the input area above the composer and close them when they are no longer needed.
- Place stable browser guidance and pinned preferences before task-specific instructions to reduce changes to the prompt prefix.

### Fixed

- Resume partially applied library schema migrations without trying to add columns that already exist, preserving saved memory and files.
- Report Claude total input tokens only when uncached input, cache reads, and cache writes are all available.
- Avoid carrying unrelated remembered lessons into simple greetings.

### Notes

- Learning suggestions are off by default. Existing approved lessons remain available when new suggestions are paused.
- Memory limits are measured in bytes, not tokens. Learning uses lexical matching and may miss paraphrases.
- Provider cache savings and general learning quality are not guaranteed.

## [0.2.3] - 2026-11-13

### Added

- Mention tasks, MCP servers, and Files library items from anywhere in a chat draft with `@`.
- Keep using the collapsed agent drawer as a compact chat input with live processing and reply previews.
- Show two lightweight follow-up actions above the composer after a reply.

### Changed

- Generate one follow-up menu per assistant response and skip it when the response already contains choices.
- Place active reply choices in the input vault above the composer, including after restoring a chat.
- Use compact icons for the collapsed drawer controls.

## [0.2.2] - 2026-11-12

### Added

- Resizable floating agent drawer that overlays the browser, with saved sizing and drag controls.
- Contextual response menus that let users answer agent questions or choose a next step directly from a reply.

### Changed

- Simplified the agent header by moving secondary details into an on-demand panel.
- Agent response choices are recorded only when they answer a user question; action selections are sent as instructions.
- Browser content remains scrollable outside the floating agent drawer.

## [0.2.1] - 2026-11-12

### Added

- A Software Updates section in settings with controls to check, download, install, and restart for updates.
- Self-updates through the Wails updater, using signed manifests verified against Phoenix's embedded public key.
- A tag-triggered Windows release workflow that publishes the per-user installer, updater executable, and signed manifest.

### Changed

- Windows installers now default to per-user installation so Phoenix can replace its executable during an update without administrator access.

### Fixed

- The release workflow now supplies Phoenix's project name when generating Wails build assets, producing a correctly named installer.

### Upgrade notes

- Phoenix v0.1.0 predates the updater. Install v0.2.1 manually once to enable self-updates.
- Existing machine-wide installs must be reinstalled with the per-user installer before self-updates can replace the executable.
