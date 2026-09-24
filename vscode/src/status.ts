import * as vscode from 'vscode';

export type State =
  | { kind: 'stopped' }
  | { kind: 'connecting' }
  | { kind: 'syncAll'; done: number; total: number }
  | { kind: 'watching'; target: string }
  | { kind: 'disconnected' }
  | { kind: 'stopping' };

/** Status bar item showing the sync state; clicking it toggles syncing. */
export class Status implements vscode.Disposable {
  private readonly item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 50);
  private errors = 0;
  private state: State = { kind: 'stopped' };

  constructor() {
    this.item.name = 'Synchro';
    this.item.command = 'synchro.toggle';
    this.render();
  }

  /** Shows the item only in workspaces that use Synchro. */
  setVisible(visible: boolean): void {
    if (visible || this.state.kind !== 'stopped') {
      this.item.show();
    } else {
      this.item.hide();
    }
  }

  set(state: State): void {
    this.state = state;
    if (state.kind === 'connecting') {
      this.errors = 0;
    }
    this.render();
  }

  get current(): State {
    return this.state;
  }

  addError(): void {
    this.errors++;
    this.render();
  }

  private render(): void {
    const s = this.state;
    let text: string;
    let tooltip: string;
    this.item.backgroundColor = undefined;
    switch (s.kind) {
      case 'stopped':
        text = '$(circle-slash) Synchro';
        tooltip = 'Synchro is stopped — click to start syncing';
        break;
      case 'connecting':
        text = '$(sync~spin) Synchro';
        tooltip = 'Synchro is connecting — click to stop';
        break;
      case 'syncAll':
        text = s.total > 0 ? `$(sync~spin) Synchro ${s.done}/${s.total}` : '$(sync~spin) Synchro';
        tooltip = 'Synchro is uploading all files — click to stop';
        break;
      case 'watching':
        text = '$(check) Synchro';
        tooltip = `Synchro is syncing to ${s.target} — click to stop`;
        break;
      case 'disconnected':
        text = '$(warning) Synchro';
        tooltip = 'Synchro lost the connection and is reconnecting — click to stop';
        this.item.backgroundColor = new vscode.ThemeColor('statusBarItem.warningBackground');
        break;
      case 'stopping':
        text = '$(loading~spin) Synchro';
        tooltip = 'Synchro is finishing in-flight operations';
        break;
    }
    if (this.errors > 0 && s.kind !== 'stopped') {
      text += ` $(error) ${this.errors}`;
      tooltip += `\n${this.errors} error(s) — see the Synchro output`;
    }
    this.item.text = text;
    this.item.tooltip = tooltip;
  }

  dispose(): void {
    this.item.dispose();
  }
}
