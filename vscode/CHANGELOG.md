# Changelog

All notable changes to the Synchro VS Code extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-09-25

First stable release, published on the Visual Studio Marketplace.

### Added

- Extension icon based on the SYNCHRO logo, dark gallery banner, homepage and issue tracker links, and more search keywords

## [0.4.2] - 2026-09-25

### Changed

- Folder tabs in the Synchro log keep their folder's colour while that folder syncs and turn grey when it is stopped, like its status bar item, instead of only the selected tab being coloured

## [0.4.1] - 2026-09-25

### Changed

- Folder tabs in the Synchro log are grey unless selected; only the selected tab shows its folder's colour
- The selected tab's underline uses the theme's badge colour (as on the Git changed-files count) instead of the panel's active border

## [0.4.0] - 2026-09-25

### Added

- Multi-root workspaces: the Synchro log tab has a sub-tab per folder plus "All", where each line is prefixed with its folder name in the folder's colour; a red or yellow dot marks a hidden folder tab that received errors or warnings
- "Show log" in a folder's notifications opens that folder's log tab; Clear log empties only the selected tab

### Changed

- Log lines are coloured like the CLI: dimmed time, coloured icon, dimmed `[upload]`-style tag, plain text
- The 5000-line log limit applies per folder, so a busy folder no longer pushes out a quiet one's log

## [0.3.2] - 2026-09-24

### Changed

- Requires Node.js 24 or newer for development (`engines.node`)
- Upgraded TypeScript to 7.0 and `@vscode/vsce` to 4.0
- Added Prettier (`npm run format`; `npm run check` now also verifies formatting) and formatted the sources
- Documented every function, class and exported type with JSDoc comments

## [0.3.1] - 2026-09-24

### Changed

- Status bar uses a single cloud-upload icon, dimmed when stopped and green while syncing; lost connection shows a disconnect icon

## [0.3.0] - 2026-09-24

### Added

- Multi-root workspaces: every folder with a config can sync at the same time, with its own status bar item labelled with the folder's initials (full name in the tooltip), `[folder]`-prefixed log lines, a folder picker in Stop (including "All folders") and per-folder `synchro.autoStart`

## [0.2.0] - 2026-09-24

### Added

- Added a "Synchro" tab in the bottom panel with a colored live log, the CLI logo and a Clear log button; Show log now opens it

## [0.1.0] - 2026-09-24

### Added

- Commands: Create config, Start sync, Sync all and watch, Stop, Test connection, Set/Clear password, Show log
- Status bar item with connection, full-sync progress and error count; click to start/stop
- "Synchro" output channel with readable logs from `synchro --json`
- Password stored in the OS keychain (SecretStorage) and passed as `SYNCHRO_PASSWORD`
- Graceful stop by closing the binary's stdin, also on Windows and when VS Code quits
- Settings: `synchro.binaryPath`, `synchro.configPath`, `synchro.autoStart`, `synchro.quiet`
- Offer to restart syncing when the running config file changes
- Licensed under GPL-3.0 (see `LICENSE` and `NOTICE`)
