# Synchro

Synchro is a lightweight command-line program written in Go. It watches a local directory and continuously synchronizes file changes to a remote server through SSH/SFTP.

## Features

- Watches file creation, modification, deletion, and directory creation events
- Uploads changes through SFTP
- `--syncAll` uploads every included local file **then continues watching**
- Reconnects after an SSH/SFTP disconnect
- Supports private-key, password, and `none` authentication
- Supports legacy-compatible exclusions such as `node_modules`, `.git`, and `*.log`
- Uses the Go toolchain (Go 1.26.5)

## Security

For compatibility with the original Node implementation, the program currently accepts any SSH host key. That makes first use convenient but does **not** protect against a machine-in-the-middle attack. Use a trusted network until a known-hosts verification policy is configured.

## Build and install

```bash
go build -o synchro ./cmd/synchro

# To make the binary available system-wide, create a symbolic link:
ln -sf /path/to/synchro ~/.local/bin/synchro

./synchro --help

# Optional: install to $(go env GOPATH)/bin
go install github.com/szok/synchro/cmd/synchro@latest
```

## Usage

```bash
synchro --init                       # create .synchro.json
synchro --test                       # verify the configured SSH/SFTP connection
synchro --sync                       # watch and synchronize future changes
synchro --syncAll                    # upload all files, then watch
synchro --config=path/to/config.json --sync
synchro --json --sync                # machine-readable JSON events (one per line)
synchro --help
```

`--init` never overwrites an existing `.synchro.json` and creates it with mode `0600`.

### Stopping

The first `Ctrl+C` (SIGINT) or SIGTERM stops gracefully: no new uploads are
started, operations already running finish, then the connection is closed. A
second signal, or 15 seconds without finishing, quits immediately.

With `--stop-on-stdin-close`, closing Synchro's stdin triggers the same graceful
stop. This is meant for editors and other parent processes — especially on
Windows, which has no SIGTERM — and also stops Synchro if its parent dies.

### JSON output

`--json` replaces the logo and colored logs with one JSON object per line on
stdout (errors included). Every event has `time`, `level` (`info`, `success`,
`warn`, `error`) and `event`:

| `event`                              | Extra fields                                                        |
| ------------------------------------ | ------------------------------------------------------------------- |
| `start`                              | `version`                                                           |
| `info`, `success`, `warn`, `error`   | `message`                                                           |
| `connected`                          | `username`, `host`                                                  |
| `disconnected`                       | `retryInSeconds`                                                    |
| `syncAllStart`                       | `total`, `workers`                                                  |
| `syncAllDone`                        | `uploaded`, `total`                                                 |
| `watching`                           | `local`, `remote`, `exclude`                                        |
| `change`                             | `change` (`add`, `change`, `unlink`, `addDir`, `unlinkDir`), `path` |
| `upload`, `delete`, `mkdir`, `rmdir` | `path` (relative to `directory`)                                    |
| `stopping`                           | `reason`                                                            |
| `stopped`                            | —                                                                   |

```json
{"event":"upload","level":"info","path":"src/app.go","time":"2026-09-24T09:36:04.85682+02:00"}
```

## Configuration

```json
{
  "host": "192.168.1.1",
  "port": 22,
  "username": "synchro-user",
  "auth": "key",
  "privateKeyPath": "~/.ssh/id_rsa",
  "password": "",
  "directory": "./",
  "remoteDirectory": "/home/synchro-user/your-app/",
  "exclude": ["node_modules", ".git", ".idea", "*.log", ".synchro.json"],
  "concurrency": 8
}
```

| Field             | Description                                                                                       |
| ----------------- | ------------------------------------------------------------------------------------------------- |
| `host`            | Remote hostname or IP address.                                                                    |
| `port`            | SSH port; defaults to `22`.                                                                       |
| `username`        | SSH login username.                                                                               |
| `auth`            | `key`, `password`, or `none`.                                                                     |
| `privateKeyPath`  | Private-key path for `key` authentication. A leading `~/` is expanded.                            |
| `password`        | Login password for `password`, or key passphrase for `key`.                                       |
| `directory`       | Local directory to synchronize.                                                                   |
| `remoteDirectory` | Remote destination directory.                                                                     |
| `exclude`         | Excluded names or patterns with `*` wildcards.                                                    |
| `concurrency`     | Maximum simultaneous uploads during `--syncAll`; defaults to `8`. Set `1` for sequential uploads. |

> `.synchro.json` can contain credentials and is ignored by Git by default.

The `SYNCHRO_PASSWORD` environment variable, when set and non-empty, takes
precedence over `password` from the config file — so the secret can stay out
of the file entirely:

```bash
SYNCHRO_PASSWORD='s3cret' synchro --sync
```

## VS Code extension

The [`vscode/`](vscode/) directory contains a VS Code extension (TypeScript)
that runs Synchro in the background. All sync logic stays in the Go binary; the
extension starts it as `synchro --json --stop-on-stdin-close --sync`, reads the
JSON events from its stdout and shows them in VS Code. User-facing docs live in
[`vscode/README.md`](vscode/README.md).

### Features

- **Commands** (`Ctrl/Cmd+Shift+P`, category "Synchro"): Create config, Start
  sync, Sync all and watch, Stop, Test connection, Set password, Clear password,
  Show log.
- **Status bar item** showing stopped / connecting / full-sync progress
  (`12/40`) / syncing / reconnecting, plus an error count. Click it to start or
  stop.
- **"Synchro" output channel** with readable logs. Notifications appear only for
  a lost connection, an incomplete full sync, or an unexpected exit (with a
  shortcut to settings when the binary is missing).
- **Password in the OS keychain**: *Set password* stores it with VS Code's
  SecretStorage and passes it to the binary as `SYNCHRO_PASSWORD`, which
  overrides `password` from the config file.
- **Graceful stop**: the extension closes the binary's stdin (works on Windows
  too, and when VS Code quits). After 20s it sends SIGTERM and after 25s SIGKILL,
  but the binary itself force-quits after 15s.
- **Config changes**: editing the running config offers a restart.
- **Settings**: `synchro.binaryPath`, `synchro.configPath`, `synchro.autoStart`,
  `synchro.quiet`.

Limitations: one folder syncs at a time (in a multi-root workspace you pick
which one), and the `publisher` in `vscode/package.json` must match your
Marketplace publisher before publishing.

### Where the extension gets the binary

The binary is **bundled into the extension package**. It is built from this
repository, from the same commit as the extension, so the flags and the JSON
format always match. Nothing is downloaded at runtime.

- `make vscode-package` cross-compiles `synchro` for each target into
  `vscode/bin/` and runs `vsce package --target <platform>`. The result is one
  `.vsix` per platform in `vscode/dist/`: `darwin-arm64`, `darwin-x64`,
  `linux-x64`, `linux-arm64`, `win32-x64`. Each one contains only its own binary
  (about 4 MB). The Marketplace serves users the package for their platform.
- `make vscode-bin` builds the binary for your machine only, into `vscode/bin/`,
  for F5 debugging.
- `vscode/bin/` and `vscode/dist/` are git-ignored build outputs.

At runtime the extension (`vscode/src/binary.ts`) looks for the binary in this
order:

1. the `synchro.binaryPath` setting, if set;
2. the bundled `bin/synchro` (`bin/synchro.exe` on Windows). It restores the
   executable bit if packaging dropped it;
3. `synchro` from `PATH`.

### Running it

```bash
# Development: Extension Development Host
make vscode-bin
cd vscode && npm install
code .                # then press F5 and open a project in the new window

# Install like a regular extension
make vscode-install-deps      # once, and after package-lock.json changes
make vscode-package
code --install-extension vscode/dist/synchro-darwin-arm64-0.1.0.vsix
```

Checks: `npm run check` (type check), `npm test` (unit tests) and
`npm run build` in `vscode/`; CI runs them in the `vscode` job.

### Layout

| File                      | Role                                                                         |
| ------------------------- | ---------------------------------------------------------------------------- |
| `vscode/package.json`     | Manifest: commands, settings, activation (`workspaceContains:.synchro.json`) |
| `vscode/src/extension.ts` | Commands and reactions to events                                             |
| `vscode/src/process.ts`   | Starting and stopping the binary, splitting output into events               |
| `vscode/src/events.ts`    | Types and formatting for `--json` events                                     |
| `vscode/src/status.ts`    | Status bar item                                                              |
| `vscode/src/binary.ts`    | Finding the binary                                                           |

## Development

```bash
gofmt -w .
go vet ./...
go test ./...
go build ./cmd/synchro
```

Tests are ordinary Go unit tests and do not need a running SSH server.

## License

Copyright (C) 2025-2026 Piotr Jarolewski.

Synchro, including the VS Code extension, is free software licensed under the
[GNU General Public License v3.0](LICENSE). You may use, study, share and
modify it. If you distribute it or a modified version, the source code must be
available under the same license, and existing copyright notices must be kept.

[NOTICE](NOTICE) adds three terms, as allowed by GPL section 7:

- keep the attribution "by Piotr Jarolewski" in `synchro --help` and in the
  documentation;
- mark modified versions as modified and do not present them as the original;
- the name "Synchro" and its logo are not licensed, so a distributed fork needs
  a different name. Saying it is "based on Synchro" is fine.
