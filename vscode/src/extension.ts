import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { resolveBinary } from './binary';
import { formatEvent, SynchroEvent } from './events';
import { LogView } from './logView';
import { Launch, runOnce, SynchroProcess } from './process';
import { Status } from './status';

const DEFAULT_CONFIG = '.synchro.json';

let controller: Controller | undefined;

export function activate(context: vscode.ExtensionContext): void {
  controller = new Controller(context);
  const c = controller;
  const command = (id: string, run: () => unknown) => vscode.commands.registerCommand(id, run);
  context.subscriptions.push(
    c,
    command('synchro.createConfig', () => c.createConfig()),
    command('synchro.start', () => c.start('--sync')),
    command('synchro.syncAll', () => c.start('--syncAll')),
    command('synchro.stop', () => c.stop()),
    command('synchro.toggle', () => (c.running ? c.stop() : c.start('--sync'))),
    command('synchro.testConnection', () => c.testConnection()),
    command('synchro.setPassword', () => c.setPassword()),
    command('synchro.clearPassword', () => c.clearPassword()),
    command('synchro.showLog', () => c.showLog()),
    command('synchro.clearLog', () => c.logView.clear()),
  );
  void c.autoStart();
}

export async function deactivate(): Promise<void> {
  await controller?.stop();
}

class Controller implements vscode.Disposable {
  private readonly output = vscode.window.createOutputChannel('Synchro');
  readonly logView = new LogView();
  private readonly status = new Status();
  private readonly disposables: vscode.Disposable[] = [this.output, this.logView, this.status];
  private configWatchers: vscode.Disposable[] = [];

  private process?: SynchroProcess;
  private configFile?: string;
  private stopRequested = false;
  private lastError?: string;
  private watchingTarget?: string;
  private disconnectNotified = false;
  private syncAllDone = 0;

  constructor(private readonly context: vscode.ExtensionContext) {
    this.disposables.push(
      vscode.workspace.onDidChangeWorkspaceFolders(() => this.refreshWorkspace()),
      vscode.workspace.onDidChangeConfiguration((e) => {
        if (e.affectsConfiguration('synchro.configPath')) {
          this.refreshWorkspace();
        }
      }),
    );
    this.refreshWorkspace();
  }

  get running(): boolean {
    return this.process !== undefined;
  }

  async autoStart(): Promise<void> {
    const folders = foldersWithConfig();
    if (folders.length !== 1) {
      return;
    }
    const autoStart = vscode.workspace.getConfiguration('synchro', folders[0].uri).get<boolean>('autoStart', false);
    if (autoStart) {
      await this.start('--sync');
    }
  }

  async start(mode: '--sync' | '--syncAll'): Promise<void> {
    if (this.process) {
      const action = await vscode.window.showInformationMessage('Synchro is already running.', 'Restart', 'Show log');
      if (action === 'Restart') {
        await this.stop();
        await this.start(mode);
      } else if (action === 'Show log') {
        void this.showLog();
      }
      return;
    }
    const folder = await pickConfiguredFolder('Select the folder to sync');
    if (!folder) {
      return;
    }
    const configFile = configPath(folder);
    const args: string[] = [mode];
    if (vscode.workspace.getConfiguration('synchro').get<boolean>('quiet', false)) {
      args.push('--quiet');
    }
    const launch = await this.launch(configFile, args);

    this.configFile = configFile;
    this.stopRequested = false;
    this.lastError = undefined;
    this.watchingTarget = undefined;
    this.disconnectNotified = false;
    this.status.set({ kind: 'connecting' });
    this.log(`--- ${launch.binary} ${args.join(' ')}  (${configFile})`);

    const proc = new SynchroProcess(launch, {
      event: (e) => this.handleEvent(e),
      text: (line) => this.handleText(line),
    });
    this.process = proc;
    void proc.exited.then((code) => this.handleExit(proc, code));
  }

  async stop(): Promise<void> {
    const proc = this.process;
    if (!proc) {
      return;
    }
    this.stopRequested = true;
    this.status.set({ kind: 'stopping' });
    await proc.stop();
  }

  async createConfig(): Promise<void> {
    const folders = vscode.workspace.workspaceFolders ?? [];
    if (folders.length === 0) {
      void vscode.window.showWarningMessage('Open a folder first — Synchro syncs a workspace folder.');
      return;
    }
    const folder = await pickFolder(folders, 'Select the folder for the Synchro config');
    if (!folder) {
      return;
    }
    const configFile = configPath(folder);
    if (fs.existsSync(configFile)) {
      await vscode.window.showTextDocument(vscode.Uri.file(configFile));
      void vscode.window.showInformationMessage(`${path.basename(configFile)} already exists.`);
      return;
    }
    // `synchro --init` always writes .synchro.json into its working directory,
    // so generate it in a scratch directory and move it to the configured path.
    const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'synchro-init-'));
    try {
      const result = await runOnce(
        { binary: resolveBinary(this.context), args: ['--init'], cwd: scratch, env: process.env },
        { event: (e) => this.logEvent(e), text: (line) => this.log(line) },
      );
      const generated = path.join(scratch, DEFAULT_CONFIG);
      if (result.code !== 0 || !fs.existsSync(generated)) {
        await this.reportFailure('Could not create the config', result.events);
        return;
      }
      fs.mkdirSync(path.dirname(configFile), { recursive: true });
      fs.copyFileSync(generated, configFile, fs.constants.COPYFILE_EXCL);
      fs.chmodSync(configFile, 0o600);
    } finally {
      fs.rmSync(scratch, { recursive: true, force: true });
    }
    this.refreshWorkspace();
    await vscode.window.showTextDocument(vscode.Uri.file(configFile));
    const action = await vscode.window.showInformationMessage(
      'Synchro config created. Fill in the server details, then test the connection. For password auth use "Synchro: Set password" to keep the secret out of the file.',
      'Test connection',
      'Set password',
    );
    if (action === 'Test connection') {
      await this.testConnection();
    } else if (action === 'Set password') {
      await this.setPassword();
    }
  }

  async testConnection(): Promise<void> {
    const folder = await pickConfiguredFolder('Select the folder whose connection to test');
    if (!folder) {
      return;
    }
    const launch = await this.launch(configPath(folder), ['--test']);
    const result = await vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title: 'Synchro: testing connection…' },
      () =>
        runOnce(launch, {
          event: (e) => this.logEvent(e),
          text: (line) => this.log(line),
        }),
    );
    const success = result.events.find((e) => e.event === 'success');
    if (result.code === 0 && success && success.event === 'success') {
      void vscode.window.showInformationMessage(`Synchro: ${success.message}`);
      return;
    }
    await this.reportFailure('Connection test failed', result.events);
  }

  async setPassword(): Promise<void> {
    const folder = await pickConfiguredFolder('Select the folder to store the password for');
    if (!folder) {
      return;
    }
    const password = await vscode.window.showInputBox({
      title: 'Synchro password',
      prompt: 'SSH password, or the private key passphrase for key auth. Stored in the OS keychain and passed as SYNCHRO_PASSWORD; it overrides "password" in the config file.',
      password: true,
      ignoreFocusOut: true,
    });
    if (password === undefined) {
      return;
    }
    const key = secretKey(configPath(folder));
    if (password === '') {
      await this.context.secrets.delete(key);
      void vscode.window.showInformationMessage('Synchro password cleared.');
      return;
    }
    await this.context.secrets.store(key, password);
    void vscode.window.showInformationMessage(
      this.running ? 'Synchro password saved. Restart syncing to use it.' : 'Synchro password saved.',
    );
  }

  async clearPassword(): Promise<void> {
    const folder = await pickFolder(foldersWithConfig(), 'Select the folder to clear the password for');
    if (!folder) {
      return;
    }
    await this.context.secrets.delete(secretKey(configPath(folder)));
    void vscode.window.showInformationMessage('Synchro password cleared.');
  }

  private async launch(configFile: string, args: string[]): Promise<Launch> {
    const env = { ...process.env };
    const password = await this.context.secrets.get(secretKey(configFile));
    if (password) {
      env.SYNCHRO_PASSWORD = password;
    }
    return {
      binary: resolveBinary(this.context),
      args: [`--config=${configFile}`, ...args],
      // A relative "directory" in the config is resolved from the working directory.
      cwd: path.dirname(configFile),
      env,
    };
  }

  async showLog(): Promise<void> {
    await this.logView.show();
  }

  /** Writes a line to both the Synchro panel tab and the "Synchro" output channel. */
  private log(line: string, level?: SynchroEvent['level']): void {
    this.output.appendLine(line);
    this.logView.append(line, level);
  }

  private logEvent(e: SynchroEvent): void {
    this.log(formatEvent(e), e.level);
  }

  private handleEvent(e: SynchroEvent): void {
    this.logEvent(e);
    switch (e.event) {
      case 'connected':
        if (this.disconnectNotified) {
          this.disconnectNotified = false;
          void vscode.window.showInformationMessage(`Synchro reconnected to ${e.host}.`);
        }
        if (this.watchingTarget) {
          this.status.set({ kind: 'watching', target: this.watchingTarget });
        }
        break;
      case 'disconnected':
        this.status.set({ kind: 'disconnected' });
        if (!this.disconnectNotified) {
          this.disconnectNotified = true;
          void this.warn('Synchro lost the connection and is reconnecting.');
        }
        break;
      case 'syncAllStart':
        this.syncAllDone = 0;
        this.status.set({ kind: 'syncAll', done: 0, total: e.total });
        break;
      case 'upload': {
        const current = this.status.current;
        if (current.kind === 'syncAll') {
          this.syncAllDone++;
          this.status.set({ kind: 'syncAll', done: this.syncAllDone, total: current.total });
        }
        break;
      }
      case 'syncAllDone':
        if (e.uploaded < e.total && !this.stopRequested) {
          void this.warn(`Synchro full sync uploaded ${e.uploaded} of ${e.total} files.`);
        }
        break;
      case 'watching':
        this.watchingTarget = e.remote;
        this.status.set({ kind: 'watching', target: e.remote });
        break;
      case 'error':
        this.lastError = e.message;
        this.status.addError();
        break;
      case 'stopping':
        this.status.set({ kind: 'stopping' });
        break;
    }
  }

  private handleText(line: string): void {
    this.log(line);
    if (line.startsWith('Failed to start')) {
      this.lastError = line;
    }
  }

  private handleExit(proc: SynchroProcess, code: number | null): void {
    if (this.process !== proc) {
      return;
    }
    this.process = undefined;
    this.status.set({ kind: 'stopped' });
    this.refreshWorkspace();
    this.log(`--- synchro exited with code ${code ?? 'none'}`);
    if (!this.stopRequested && code !== 0) {
      void this.showExitError(this.lastError ?? `synchro exited with code ${code ?? 'none'}`);
    }
  }

  private async showExitError(message: string): Promise<void> {
    const notFound = message.startsWith('Failed to start') && message.includes('ENOENT');
    const buttons = notFound ? ['Open settings', 'Show log'] : ['Show log'];
    const text = notFound
      ? 'Synchro binary not found. Install synchro on PATH or set "synchro.binaryPath".'
      : `Synchro stopped: ${message}`;
    const action = await vscode.window.showErrorMessage(text, ...buttons);
    if (action === 'Open settings') {
      await vscode.commands.executeCommand('workbench.action.openSettings', 'synchro.binaryPath');
    } else if (action === 'Show log') {
      void this.showLog();
    }
  }

  private async reportFailure(title: string, events: SynchroEvent[]): Promise<void> {
    const errors = events.filter((e) => e.event === 'error').map((e) => (e.event === 'error' ? e.message : ''));
    const detail = errors[0] ?? this.lastError ?? 'see the Synchro output for details';
    const action = await vscode.window.showErrorMessage(`Synchro: ${title}: ${detail}`, 'Show log');
    if (action === 'Show log') {
      void this.showLog();
    }
  }

  private async warn(message: string): Promise<void> {
    const action = await vscode.window.showWarningMessage(message, 'Show log');
    if (action === 'Show log') {
      void this.showLog();
    }
  }

  /** Re-evaluates which folders have a config: status bar visibility and file watchers. */
  private refreshWorkspace(): void {
    this.status.setVisible(foldersWithConfig().length > 0);
    this.configWatchers.forEach((d) => d.dispose());
    this.configWatchers = (vscode.workspace.workspaceFolders ?? []).map((folder) => {
      const relative = path.relative(folder.uri.fsPath, configPath(folder)).split(path.sep).join('/');
      const watcher = vscode.workspace.createFileSystemWatcher(new vscode.RelativePattern(folder, relative));
      watcher.onDidCreate(() => this.status.setVisible(true));
      watcher.onDidDelete(() => this.status.setVisible(foldersWithConfig().length > 0));
      watcher.onDidChange((uri) => void this.offerRestart(uri.fsPath));
      return watcher;
    });
  }

  private async offerRestart(changedFile: string): Promise<void> {
    if (!this.process || this.configFile !== changedFile) {
      return;
    }
    const action = await vscode.window.showInformationMessage('Synchro config changed. Restart syncing to apply it?', 'Restart');
    if (action === 'Restart') {
      await this.stop();
      await this.start('--sync');
    }
  }

  dispose(): void {
    this.configWatchers.forEach((d) => d.dispose());
    this.disposables.forEach((d) => d.dispose());
  }
}

function configPath(folder: vscode.WorkspaceFolder): string {
  const relative = vscode.workspace.getConfiguration('synchro', folder.uri).get<string>('configPath', DEFAULT_CONFIG);
  return path.resolve(folder.uri.fsPath, relative || DEFAULT_CONFIG);
}

function foldersWithConfig(): vscode.WorkspaceFolder[] {
  return (vscode.workspace.workspaceFolders ?? []).filter((folder) => fs.existsSync(configPath(folder)));
}

async function pickFolder(
  folders: readonly vscode.WorkspaceFolder[],
  placeHolder: string,
): Promise<vscode.WorkspaceFolder | undefined> {
  if (folders.length <= 1) {
    return folders[0];
  }
  const picked = await vscode.window.showQuickPick(
    folders.map((folder) => ({ label: folder.name, description: folder.uri.fsPath, folder })),
    { placeHolder },
  );
  return picked?.folder;
}

/** Picks a folder that has a config, offering to create one when none does. */
async function pickConfiguredFolder(placeHolder: string): Promise<vscode.WorkspaceFolder | undefined> {
  const folders = foldersWithConfig();
  if (folders.length === 0) {
    await offerCreateConfig();
    return undefined;
  }
  return pickFolder(folders, placeHolder);
}

async function offerCreateConfig(): Promise<void> {
  const action = await vscode.window.showWarningMessage('No Synchro config found in this workspace.', 'Create config');
  if (action === 'Create config') {
    await vscode.commands.executeCommand('synchro.createConfig');
  }
}

function secretKey(configFile: string): string {
  return `synchro.password:${configFile}`;
}
