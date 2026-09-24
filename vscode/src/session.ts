import * as vscode from 'vscode';
import { formatEvent, SynchroEvent } from './events';
import { Launch, SynchroProcess } from './process';
import { Status } from './status';

/** What a session needs from the controller: the shared log. */
export interface SessionHost {
  /** Appends a line to the shared log. */
  log(line: string, level?: SynchroEvent['level']): void;
  /** Reveals the Synchro log tab. */
  showLog(): Promise<void>;
}

/** The synchro process of one workspace folder, with its state and status bar item. */
export class Session implements vscode.Disposable {
  private readonly status: Status;
  private process?: SynchroProcess;
  private stopRequested = false;
  private lastError?: string;
  private watchingTarget?: string;
  private disconnectNotified = false;
  private syncAllDone = 0;
  /** Folder name shown in logs and messages; only set when the workspace has several folders. */
  private label?: string;

  /**
   * @param folder Workspace folder this session syncs.
   * @param configFile Absolute path of the folder's config file; also the session's key.
   * @param host Shared log.
   * @param onExit Called after the process has exited.
   */
  constructor(
    readonly folder: vscode.WorkspaceFolder,
    readonly configFile: string,
    private readonly host: SessionHost,
    private readonly onExit: () => void,
  ) {
    this.status = new Status(configFile);
  }

  /** True while a synchro process is running for this folder. */
  get running(): boolean {
    return this.process !== undefined;
  }

  /** "Synchro", or "Synchro (folder)" in a multi-root workspace. */
  get name(): string {
    return this.label ? `Synchro (${this.label})` : 'Synchro';
  }

  /**
   * Sets the folder name used in logs, messages and the status bar item.
   * @param label Full and short folder name, or undefined in a single-folder workspace.
   */
  setLabel(label: { name: string; short: string } | undefined): void {
    this.label = label?.name;
    this.status.setLabel(label);
  }

  /**
   * Starts the synchro process and resets the per-run state. The caller checks `running` first.
   * @param launch How to run synchro for this folder.
   */
  start(launch: Launch): void {
    this.stopRequested = false;
    this.lastError = undefined;
    this.watchingTarget = undefined;
    this.disconnectNotified = false;
    this.status.set({ kind: 'connecting' });
    this.log(`--- ${launch.binary} ${launch.args.join(' ')}`);

    const proc = new SynchroProcess(launch, {
      event: (e) => this.handleEvent(e),
      text: (line) => this.handleText(line),
    });
    this.process = proc;
    void proc.exited.then((code) => this.handleExit(proc, code));
  }

  /** Gracefully stops the process; resolves once it has exited. Does nothing when not running. */
  async stop(): Promise<void> {
    const proc = this.process;
    if (!proc) {
      return;
    }
    this.stopRequested = true;
    this.status.set({ kind: 'stopping' });
    await proc.stop();
  }

  /** Writes a line to the shared log, prefixed with the folder name in a multi-root workspace. */
  log(line: string, level?: SynchroEvent['level']): void {
    this.host.log(this.label ? `[${this.label}] ${line}` : line, level);
  }

  /** Writes an event to the shared log as a formatted line. */
  logEvent(e: SynchroEvent): void {
    this.log(formatEvent(e), e.level);
  }

  /** Logs an event and updates the status bar and notifications from it. */
  private handleEvent(e: SynchroEvent): void {
    this.logEvent(e);
    switch (e.event) {
      case 'connected':
        if (this.disconnectNotified) {
          this.disconnectNotified = false;
          void vscode.window.showInformationMessage(`${this.name} reconnected to ${e.host}.`);
        }
        if (this.watchingTarget) {
          this.status.set({ kind: 'watching', target: this.watchingTarget });
        }
        break;
      case 'disconnected':
        this.status.set({ kind: 'disconnected' });
        if (!this.disconnectNotified) {
          this.disconnectNotified = true;
          void this.warn(`${this.name} lost the connection and is reconnecting.`);
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
          void this.warn(`${this.name} full sync uploaded ${e.uploaded} of ${e.total} files.`);
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

  /** Logs a non-event line, remembering spawn failures for the exit message. */
  private handleText(line: string): void {
    this.log(line);
    if (line.startsWith('Failed to start')) {
      this.lastError = line;
    }
  }

  /**
   * Resets the session after its process exited and reports unexpected exits.
   * Ignores exits of processes replaced by a restart.
   * @param proc The process that exited.
   * @param code Its exit code, or null when killed or never started.
   */
  private handleExit(proc: SynchroProcess, code: number | null): void {
    if (this.process !== proc) {
      return;
    }
    this.process = undefined;
    this.status.set({ kind: 'stopped' });
    this.log(`--- synchro exited with code ${code ?? 'none'}`);
    if (!this.stopRequested && code !== 0) {
      void this.showExitError(this.lastError ?? `synchro exited with code ${code ?? 'none'}`);
    }
    this.onExit();
  }

  /**
   * Shows an error for an unexpected exit, with a shortcut to the binary setting
   * when the binary could not be found.
   * @param message The last error reported by synchro, or a generic exit message.
   */
  private async showExitError(message: string): Promise<void> {
    const notFound = message.startsWith('Failed to start') && message.includes('ENOENT');
    const buttons = notFound ? ['Open settings', 'Show log'] : ['Show log'];
    const text = notFound
      ? 'Synchro binary not found. Install synchro on PATH or set "synchro.binaryPath".'
      : `${this.name} stopped: ${message}`;
    const action = await vscode.window.showErrorMessage(text, ...buttons);
    if (action === 'Open settings') {
      await vscode.commands.executeCommand('workbench.action.openSettings', 'synchro.binaryPath');
    } else if (action === 'Show log') {
      void this.host.showLog();
    }
  }

  /** Shows a warning notification with a "Show log" button. */
  private async warn(message: string): Promise<void> {
    const action = await vscode.window.showWarningMessage(message, 'Show log');
    if (action === 'Show log') {
      void this.host.showLog();
    }
  }

  /** Removes the status bar item. Does not stop the process. */
  dispose(): void {
    this.status.dispose();
  }
}
