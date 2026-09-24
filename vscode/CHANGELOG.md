# Changelog

All notable changes to the Synchro VS Code extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.2] - 2026-09-24

### Changed

- [@pjarolewski]: Requires Node.js 24 or newer for development (`engines.node`)
- [@pjarolewski]: Upgraded TypeScript to 7.0 and `@vscode/vsce` to 4.0
- [@pjarolewski]: Added Prettier (`npm run format`; `npm run check` now also verifies formatting) and formatted the sources
- [@pjarolewski]: Documented every function, class and exported type with JSDoc comments

## [0.3.1] - 2026-09-24

### Changed

- [@pjarolewski]: Status bar uses a single cloud-upload icon, dimmed when stopped and green while syncing; lost connection shows a disconnect icon

## [0.3.0] - 2026-09-24

### Added

- [@pjarolewski]: Multi-root workspaces: every folder with a config can sync at the same time, with its own status bar item labelled with the folder's initials (full name in the tooltip), `[folder]`-prefixed log lines, a folder picker in Stop (including "All folders") and per-folder `synchro.autoStart`

## [0.2.0] - 2026-09-24

### Added

- [@pjarolewski]: Added a "Synchro" tab in the bottom panel with a colored live log, the CLI logo and a Clear log button; Show log now opens it

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
