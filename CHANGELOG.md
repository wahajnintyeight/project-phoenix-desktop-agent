# Changelog

Notable Phoenix changes are listed here. Versions use the `vMAJOR.MINOR.PATCH` release tags.

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
