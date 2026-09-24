# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Synchro is a Go CLI tool that watches a local directory and continuously syncs
changes to a remote server via SSH/SFTP. The entrypoint is `cmd/synchro/main.go`;
all behaviour lives in the `internal/` packages. Compiled binary: `./synchro`.

## Running

```bash
go build -o synchro ./cmd/synchro   # Build the binary
go test ./...                       # Run the test suite
go vet ./...                        # Static checks

./synchro --init                    # Generate a sample .synchro.json config
./synchro --syncAll                 # Full sync then watch
./synchro --sync                    # Watch and sync changes only
./synchro --test                    # Verify SSH/SFTP connectivity and exit
./synchro --config=<path>           # Use a custom config file
./synchro --json --sync             # JSON events on stdout (for the VS Code extension)
```

## Architecture

- **`cmd/synchro`** — flag parsing, wiring, signal handling. `run` takes
  `io.Writer` streams so it is testable.
- **`internal/config`** — `Load` reads, normalizes and validates `.synchro.json`;
  `BuildSSHConfig` (in `sftpclient`) maps auth to an `ssh.ClientConfig`. Auth
  methods: `key`, `password`, `none`. `SYNCHRO_PASSWORD` env overrides `password`.
- **`internal/sftpclient`** — `Client` maintains a persistent SSH+SFTP session.
  On disconnect it reconnects after 3s. `ssh.Client.Wait` plus a 30s keepalive
  request detect dropped links. Operations issued before the connection is ready
  are queued and flushed once SFTP opens. `Run` schedules an `Operation`.
- **`internal/watcher`** — `fsnotify`-based recursive watcher. Maps events to
  `Operations`: create→upload/mkdir, write→upload, remove/rename→delete/rmdir.
  Editor backup files (`*~`, `.#*`, `*.swp`, …) are ignored.
- **`internal/syncer`** — `Upload`, `DeleteFile`, `CreateDir`, `DeleteDir`, and
  `SyncAll` (bulk upload with bounded parallelism). `MkdirAll` ensures remote
  parent dirs exist.
- **`internal/paths`** — `RemotePath` maps local→remote POSIX paths (and refuses
  to escape the remote root). `IsExcluded`/`MatchGlob` handle `*` glob patterns
  with a compiled-regexp cache.
- **`internal/logx`** — timestamped colored terminal output and the ASCII logo;
  with `--json` every log call emits one JSON event per line on stdout instead.
- **`vscode/`** — VS Code extension (TypeScript, esbuild). Spawns
  `synchro --json --stop-on-stdin-close`, parses events (`src/events.ts`), stops
  by closing stdin. `make vscode-bin` for F5 debugging, `make vscode-package`
  for per-platform `.vsix` files. Checks: `npm run check && npm test` in `vscode/`
  (`check` = `tsc` + `prettier --check`; `npm run format` rewrites). Requires Node ≥ 24.
- **Shutdown** — first SIGINT/SIGTERM (or stdin EOF with `--stop-on-stdin-close`)
  cancels work but keeps the connection until in-flight ops finish; a second
  signal or 15s timeout force-quits.

## Configuration schema (`.synchro.json`)

```json
{
  "host": "hostname",
  "port": 22,
  "username": "user",
  "auth": "key|password|none",
  "privateKeyPath": "~/.ssh/id_rsa",
  "password": "",
  "directory": "/local/path",
  "remoteDirectory": "/remote/path",
  "exclude": ["node_modules", ".git", "*.log", ".synchro.json"],
  "concurrency": 8
}
```

## Dependencies

- `github.com/pkg/sftp` — SFTP client
- `golang.org/x/crypto/ssh` — SSH client
- `github.com/fsnotify/fsnotify` — cross-platform file watcher
