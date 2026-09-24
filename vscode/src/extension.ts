import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { resolveBinary } from './binary';
import { formatEvent, SynchroEvent } from './events';
import { shortLabels } from './label';
import { LogView } from './logView';
import { Launch, runOnce } from './process';
import { Session } from './session';

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
    vscode.commands.registerCommand('synchro.toggle', (configFile?: string) => c.toggle(configFile)),
    command('synchro.testConnection', () => c.testConnection()),
    command('synchro.setPassword', () => c.setPassword()),
    command('synchro.clearPassword', () => c.clearPassword()),
    command('synchro.showLog', () => c.showLog()),
    command('synchro.clearLog', () => c.logView.clear()),
  );
  void c.autoStart();
}

export async function deactivate(): Promise<void> {
  await controller?.stopAll();
}

class Controller implements vscode.Disposable {
  private readonly output = vscode.window.createOutputChannel('Synchro');
  readonly logView = new LogView();
  private readonly disposables: vscode.Disposable[] = [this.output, this.logView];
  private configWatchers: vscode.Disposable[] = [];
  /** One session per config file of a workspace folder, running or not. */
  private readonly sessions = new Map<string, Session>();

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

  async autoStart(): Promise<void> {
    const folders = foldersWithConfig().filter((folder) =>
      vscode.workspace.getConfiguration('synchro', folder.uri).get<boolean>('autoStart', false),
    );
    await Promise.all(folders.map((folder) => this.start('--sync', configPath(folder))));
  }

  /** Starts syncing `configFile`, or a folder picked from those not syncing yet. */
  async start(mode: '--sync' | '--syncAll', configFile?: string): Promise<void> {
    const session = configFile ? this.sessions.get(configFile) : await this.pickSessionToStart();
    if (!session) {
      return;
    }
    if (session.running) {
      const action = await vscode.window.showInformationMessage(`${session.name} is already running.`, 'Restart', 'Show log');
      if (action === 'Restart') {
        await session.stop();
        await this.start(mode, session.configFile);
      } else if (action === 'Show log') {
        void this.showLog();
      }
      return;
    }
    const args: string[] = [mode];
    if (vscode.workspace.getConfiguration('synchro').get<boolean>('quiet', false)) {
      args.push('--quiet');
    }
    session.start(await this.launch(session.configFile, args));
  }

  private async pickSessionToStart(): Promise<Session | undefined> {
    const folders = foldersWithConfig();
    if (folders.length === 0) {
      await offerCreateConfig();
      return undefined;
    }
    this.syncSessions();
    const idle = folders.filter((folder) => !this.sessions.get(configPath(folder))?.running);
    if (idle.length === 0 && folders.length > 1) {
      const action = await vscode.window.showInformationMessage('Synchro is already running in every folder.', 'Show log');
      if (action === 'Show log') {
        void this.showLog();
      }
      return undefined;
    }
    // With a single folder that is already running, start() offers a restart.
    const folder = idle.length === 0 ? folders[0] : await pickFolder(idle, 'Select the folder to sync');
    return folder && this.sessions.get(configPath(folder));
  }

  /** Stops `configFile`, or the running folder (picked when several are running). */
  async stop(configFile?: string): Promise<void> {
    const running = [...this.sessions.values()].filter((s) => s.running);
    let targets: Session[];
    if (configFile) {
      targets = running.filter((s) => s.configFile === configFile);
    } else if (running.length <= 1) {
      targets = running;
    } else {
      const picked = await vscode.window.showQuickPick(
        [
          { label: 'All folders', sessions: running },
          ...running.map((s) => ({ label: s.folder.name, description: s.folder.uri.fsPath, sessions: [s] })),
        ],
        { placeHolder: 'Select the folder to stop' },
      );
      targets = picked?.sessions ?? [];
    }
    await Promise.all(targets.map((s) => s.stop()));
  }

  async stopAll(): Promise<void> {
    await Promise.all([...this.sessions.values()].map((s) => s.stop()));
  }

  /** Status bar click: toggles its folder. Without a folder, stops if anything runs. */
  async toggle(configFile?: string): Promise<void> {
    if (typeof configFile !== 'string') {
      const anyRunning = [...this.sessions.values()].some((s) => s.running);
      return anyRunning ? this.stop() : this.start('--sync');
    }
    return this.sessions.get(configFile)?.running ? this.stop(configFile) : this.start('--sync', configFile);
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
    this.syncSessions();
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
    const configFile = configPath(folder);
    this.syncSessions();
    const session = this.sessions.get(configFile);
    const log = (line: string, level?: SynchroEvent['level']) => (session ? session.log(line, level) : this.log(line, level));
    const launch = await this.launch(configFile, ['--test']);
    const result = await vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title: `${session?.name ?? 'Synchro'}: testing connection…` },
      () =>
        runOnce(launch, {
          event: (e) => log(formatEvent(e), e.level),
          text: (line) => log(line),
        }),
    );
    const success = result.events.find((e) => e.event === 'success');
    if (result.code === 0 && success && success.event === 'success') {
      void vscode.window.showInformationMessage(`${session?.name ?? 'Synchro'}: ${success.message}`);
      return;
    }
    await this.reportFailure('Connection test failed', result.events, session?.name);
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
    const configFile = configPath(folder);
    const key = secretKey(configFile);
    if (password === '') {
      await this.context.secrets.delete(key);
      void vscode.window.showInformationMessage('Synchro password cleared.');
      return;
    }
    await this.context.secrets.store(key, password);
    void vscode.window.showInformationMessage(
      this.sessions.get(configFile)?.running ? 'Synchro password saved. Restart syncing to use it.' : 'Synchro password saved.',
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
  log(line: string, level?: SynchroEvent['level']): void {
    this.output.appendLine(line);
    this.logView.append(line, level);
  }

  private logEvent(e: SynchroEvent): void {
    this.log(formatEvent(e), e.level);
  }

  private async reportFailure(title: string, events: SynchroEvent[], name = 'Synchro'): Promise<void> {
    const errors = events.filter((e) => e.event === 'error').map((e) => (e.event === 'error' ? e.message : ''));
    const detail = errors[0] ?? 'see the Synchro output for details';
    const action = await vscode.window.showErrorMessage(`${name}: ${title}: ${detail}`, 'Show log');
    if (action === 'Show log') {
      void this.showLog();
    }
  }

  /** Re-evaluates which folders have a config: sessions and config file watchers. */
  private refreshWorkspace(): void {
    this.syncSessions();
    this.configWatchers.forEach((d) => d.dispose());
    this.configWatchers = (vscode.workspace.workspaceFolders ?? []).map((folder) => {
      const relative = path.relative(folder.uri.fsPath, configPath(folder)).split(path.sep).join('/');
      const watcher = vscode.workspace.createFileSystemWatcher(new vscode.RelativePattern(folder, relative));
      watcher.onDidCreate(() => this.syncSessions());
      watcher.onDidDelete(() => this.syncSessions());
      watcher.onDidChange((uri) => void this.offerRestart(uri.fsPath));
      return watcher;
    });
  }

  /**
   * Keeps one session per folder with a config. A running session whose config
   * disappeared stays until its process exits.
   */
  private syncSessions(): void {
    const configured = new Map(foldersWithConfig().map((folder) => [configPath(folder), folder]));
    for (const [configFile, session] of this.sessions) {
      if (!configured.has(configFile) && !session.running) {
        session.dispose();
        this.sessions.delete(configFile);
      }
    }
    for (const [configFile, folder] of configured) {
      if (!this.sessions.has(configFile)) {
        this.sessions.set(configFile, new Session(folder, configFile, this, () => this.syncSessions()));
      }
    }
    const sessions = [...this.sessions.values()];
    const multiRoot = (vscode.workspace.workspaceFolders ?? []).length > 1;
    const short = shortLabels(sessions.map((s) => s.folder.name));
    sessions.forEach((s, i) => s.setLabel(multiRoot ? { name: s.folder.name, short: short[i] } : undefined));
  }

  private async offerRestart(changedFile: string): Promise<void> {
    const session = this.sessions.get(changedFile);
    if (!session?.running) {
      return;
    }
    const action = await vscode.window.showInformationMessage(`${session.name} config changed. Restart syncing to apply it?`, 'Restart');
    if (action === 'Restart') {
      await session.stop();
      await this.start('--sync', changedFile);
    }
  }

  dispose(): void {
    this.configWatchers.forEach((d) => d.dispose());
    this.sessions.forEach((s) => s.dispose());
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
