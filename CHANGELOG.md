# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.3.3] - 2026-09-25

### Added

- Prebuilt binaries for macOS, Linux and Windows (amd64 and arm64) with SHA-256 checksums, attached to GitHub releases by a workflow on `v*` tags; `make release` builds the same archives locally
- `make vscode-test` (runs the VS Code extension tests) and `make vscode-check` (type-check, format-check and tests)

## [1.3.2] - 2026-09-24

### Added

- `--json` flag: one JSON event per line on stdout (`start`, `connected`, `upload`, `syncAllDone`, `stopped`, …) for editor integrations
- `--stop-on-stdin-close` flag: closing stdin stops Synchro gracefully (for parent processes, especially on Windows)
- `SYNCHRO_PASSWORD` environment variable, which takes precedence over `password` from the config file
- VS Code extension in `vscode/` with Command Palette commands, a status bar item and a log panel; `make vscode-install-deps`, `make vscode-bin` and `make vscode-package`

### Changed

- The first Ctrl+C/SIGTERM now stops gracefully: no new uploads start, in-flight ones finish (also during `--syncAll`); a second signal or a 15s timeout forces quit
- License changed from MIT to GPL-3.0, with additional terms in `NOTICE` (author attribution, marking modified versions, no rights to the "Synchro" name)

## [1.3.1] - 2026-09-22

### Changed

- Startup version line is no longer printed as a timestamped log entry; it's now shown inline with the header (e.g. `synchro 1.3.1 — sync local files to a remote server over SFTP`)

## [1.3.0] - 2026-09-16

### Added

- Build now embeds the released version (from `CHANGELOG.md`) into the binary via `-ldflags`; `--version`/`-v` prints it, and it's logged at startup

## [1.2.0] - 2026-09-16

### Added

- `--quiet`, `-q` flag to suppress per-file upload/delete/mkdir/rmdir logs
- Full sync completion summary now reports how many files were uploaded successfully (e.g. `Full sync complete: 42/42 files uploaded.`)

### Fixed

- Watcher event logs (`[add]`, `[change]`, `[unlink]`, `[addDir]`, `[unlinkDir]`) and the upload failure message now print paths relative to the watched directory instead of the full absolute path

## [1.1.0] - 2026-04-27

### Added

- `--help` flag to display CLI usage information with logo
- Animated terminal spinner displayed while the watcher is idle
- ESLint and Prettier configurations for code quality and formatting

## [1.0.0] - 2026-04-23

### Added

- CLI tool for watching a local directory and syncing changes to a remote server via SSH/SFTP
- `--init` flag to generate a sample `.synchro.json` config file
- `--sync` flag to watch and sync file changes only
- `--syncAll` flag to perform a full sync and then watch for changes
- `--config=<path>` flag to use a custom config file path
- Support for three authentication methods: SSH key, password, and none
- Glob-style exclusion patterns (e.g. `node_modules`, `*.log`)
- Auto-reconnect on SSH disconnect with 3-second retry delay
- TypeScript source with compiled output to `dist/`
- Unit tests with Vitest
- GitHub Actions workflow for running tests on Node 18, 20, and 22
