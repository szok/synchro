import * as vscode from 'vscode';

/** Sync state shown by the status bar item. */
export type State =
  | { kind: 'stopped' }
  | { kind: 'connecting' }
  | { kind: 'syncAll'; done: number; total: number }
  | { kind: 'watching'; target: string }
  | { kind: 'disconnected' }
  | { kind: 'stopping' };

/** Status bar item of one folder showing its sync state; clicking it toggles that folder. */
export class Status implements vscode.Disposable {
  private readonly item: vscode.StatusBarItem;
  private errors = 0;
  private state: State = { kind: 'stopped' };
  /** Full folder name for the tooltip and short one for the text. */
  private label?: { name: string; short: string };

  /**
   * Creates and shows the status bar item.
   * @param configFile Config file of the folder; passed to `synchro.toggle` on click.
   */
  constructor(configFile: string) {
    this.item = vscode.window.createStatusBarItem(`synchro.status:${configFile}`, vscode.StatusBarAlignment.Left, 50);
    this.item.name = 'Synchro';
    this.item.command = { title: 'Toggle sync', command: 'synchro.toggle', arguments: [configFile] };
    this.render();
    this.item.show();
  }

  /** Folder name shown instead of "Synchro" when several folders use Synchro. */
  setLabel(label: { name: string; short: string } | undefined): void {
    this.label = label;
    this.item.name = label ? `Synchro (${label.name})` : 'Synchro';
    this.render();
  }

  /** Switches to a new state; entering `connecting` resets the error counter. */
  set(state: State): void {
    this.state = state;
    if (state.kind === 'connecting') {
      this.errors = 0;
    }
    this.render();
  }

  /** The state currently shown. */
  get current(): State {
    return this.state;
  }

  /** Counts one more error reported by synchro in the current run. */
  addError(): void {
    this.errors++;
    this.render();
  }

  /** Updates the item's icon, text, colours and tooltip from the state, label and error count. */
  private render(): void {
    const s = this.state;
    const name = this.label ? `Synchro (${this.label.name})` : 'Synchro';
    let icon: string;
    let progress = '';
    let tooltip: string;
    this.item.backgroundColor = undefined;
    this.item.color = undefined;
    switch (s.kind) {
      case 'stopped':
        icon = '$(cloud-upload)';
        this.item.color = new vscode.ThemeColor('disabledForeground');
        tooltip = `${name} is stopped — click to start syncing`;
        break;
      case 'connecting':
        icon = '$(sync~spin)';
        tooltip = `${name} is connecting — click to stop`;
        break;
      case 'syncAll':
        icon = '$(sync~spin)';
        progress = s.total > 0 ? ` ${s.done}/${s.total}` : '';
        tooltip = `${name} is uploading all files — click to stop`;
        break;
      case 'watching':
        icon = '$(cloud-upload)';
        this.item.color = new vscode.ThemeColor('charts.green');
        tooltip = `${name} is syncing to ${s.target} — click to stop`;
        break;
      case 'disconnected':
        icon = '$(debug-disconnect)';
        tooltip = `${name} lost the connection and is reconnecting — click to stop`;
        this.item.backgroundColor = new vscode.ThemeColor('statusBarItem.warningBackground');
        break;
      case 'stopping':
        icon = '$(loading~spin)';
        tooltip = `${name} is finishing in-flight operations`;
        break;
    }
    let text = `${icon} ${this.label?.short ?? 'Synchro'}${progress}`;
    if (this.errors > 0 && s.kind !== 'stopped') {
      text += ` $(error) ${this.errors}`;
      tooltip += `\n${this.errors} error(s) — see the Synchro log`;
    }
    this.item.text = text;
    this.item.tooltip = tooltip;
  }

  /** Removes the status bar item. */
  dispose(): void {
    this.item.dispose();
  }
}
