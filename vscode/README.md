# Synchro for VS Code

Continuously sync your workspace to a remote server over SSH/SFTP. The
extension runs the [Synchro](https://github.com/szok/synchro) binary in the
background (it is bundled with the extension) and shows its status in VS Code.

## Getting started

1. Open the folder you want to sync.
2. Run **Synchro: Create config** from the Command Palette (`Ctrl/Cmd+Shift+P`)
   and fill in `host`, `username`, `auth` and `remoteDirectory` in the
   generated `.synchro.json`.
3. For password auth (or an encrypted key), run **Synchro: Set password** — the
   secret goes to the OS keychain instead of the file.
4. Run **Synchro: Test connection**, then **Synchro: Start sync** or
   **Synchro: Sync all and watch**.

## Commands

| Command                                | What it does                                                   |
| -------------------------------------- | -------------------------------------------------------------- |
| Synchro: Create config                 | Generates `.synchro.json` in the workspace folder and opens it |
| Synchro: Start sync                    | Watches the folder and uploads changes as they happen          |
| Synchro: Sync all and watch            | Uploads every file first, then watches                         |
| Synchro: Stop                          | Stops after in-flight uploads finish                           |
| Synchro: Test connection               | Checks SSH/SFTP login with the current config                  |
| Synchro: Set password / Clear password | Manages the password in the OS keychain                        |
| Synchro: Show log                      | Opens the "Synchro" tab in the bottom panel (also in Output)   |

The status bar item shows the state (stopped, connecting, full-sync progress,
syncing, reconnecting) and the number of errors; click it to start or stop.

## Settings

| Setting              | Default         | Description                                                                      |
| -------------------- | --------------- | -------------------------------------------------------------------------------- |
| `synchro.binaryPath` | empty           | Custom `synchro` binary. Empty uses the bundled one, then `synchro` from `PATH`. |
| `synchro.configPath` | `.synchro.json` | Config file relative to the workspace folder. Synchro runs from its directory.   |
| `synchro.autoStart`  | `false`         | Start syncing when a workspace with a config opens.                              |
| `synchro.quiet`      | `false`         | Do not log every file operation.                                                 |

A password stored with **Set password** overrides `password` from the config file.

## Notes

- One folder syncs at a time; in a multi-root workspace you pick which one.
- Synchro currently accepts any SSH host key — use it on trusted networks.

## Development

```bash
make vscode-bin       # from the repo root: build synchro into vscode/bin
cd vscode
npm install
npm run check         # type check
npm test              # unit tests
```

Open the `vscode/` folder in VS Code and press `F5` to launch an Extension
Development Host. `make vscode-package` (repo root) builds one `.vsix` per
platform into `vscode/dist/`; install one with **Extensions: Install from VSIX…**.

## License

Copyright (C) 2025-2026 Piotr Jarolewski. Free software under the
[GNU General Public License v3.0](LICENSE), with the additional terms in
[NOTICE](NOTICE).
