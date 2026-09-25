import * as vscode from 'vscode';
import { Entry } from './events';
import { LogBuffer } from './logBuffer';

/** Oldest lines of a folder are dropped beyond this many, both here and in the webview. */
const MAX_LINES = 5000;

// Same logotype as `logx.PrintLogo` in the CLI.
const LOGO = `███████╗██╗   ██╗███╗   ██╗ ██████╗██╗  ██╗██████╗  ██████╗
██╔════╝╚██╗ ██╔╝████╗  ██║██╔════╝██║  ██║██╔══██╗██╔═══██╗
███████╗ ╚████╔╝ ██╔██╗ ██║██║     ███████║██████╔╝██║   ██║
╚════██║  ╚██╔╝  ██║╚██╗██║██║     ██╔══██║██╔══██╗██║   ██║
███████║   ██║   ██║ ╚████║╚██████╗██║  ██║██║  ██║╚██████╔╝
╚══════╝   ╚═╝   ╚═╝  ╚═══╝ ╚═════╝╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝`;

/**
 * The "Synchro" tab in the bottom panel, next to Output and Terminal. In a
 * multi-root workspace it shows one sub-tab per folder plus "All".
 */
export class LogView implements vscode.WebviewViewProvider, vscode.Disposable {
  /** View id contributed in package.json. */
  static readonly viewId = 'synchro.log';

  private readonly buffer = new LogBuffer(MAX_LINES);
  /** Folders with a tab; empty in a single-folder workspace, where no tabs are shown. */
  private folders: string[] = [];
  /** Folder whose tab is selected, or undefined for "All". Kept here so it survives the view being hidden. */
  private selected?: string;
  private view?: vscode.WebviewView;
  private readonly registration = vscode.window.registerWebviewViewProvider(LogView.viewId, this);

  /**
   * Called by VS Code when the tab is first shown or recreated; renders the page
   * and replays the buffered lines once it reports ready.
   */
  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view;
    view.webview.options = { enableScripts: true };
    view.webview.html = html(nonce());
    view.webview.onDidReceiveMessage((message) => {
      // The page posts "ready" whenever it (re)loads, e.g. after the panel was hidden.
      if (message?.type === 'ready') {
        this.post({ type: 'reset', lines: this.buffer.all(), folders: this.folders, selected: this.selected });
      } else if (message?.type === 'select') {
        this.selected = message.folder ?? undefined;
      }
    });
    view.onDidDispose(() => {
      if (this.view === view) {
        this.view = undefined;
      }
    });
  }

  /**
   * Buffers a line and sends it to the view if it is open.
   * @param line Raw output, shown dimmed, or an event entry, coloured like the CLI.
   * @param folder Folder tab of the line; unset in a single-folder workspace.
   */
  append(line: string | Entry, folder?: string): void {
    this.post({ type: 'append', line: this.buffer.push(line, folder) });
  }

  /**
   * Sets the folders that get a tab, in display order.
   * @param folders Folder names; fewer than two hides the tab bar.
   */
  setFolders(folders: string[]): void {
    if (folders.length === this.folders.length && folders.every((f, i) => f === this.folders[i])) {
      return;
    }
    this.folders = folders;
    if (this.selected !== undefined && !folders.includes(this.selected)) {
      this.selected = undefined;
    }
    this.post({ type: 'folders', folders, selected: this.selected });
  }

  /** Empties the selected folder's tab, or the whole log when "All" is selected. */
  clear(): void {
    this.buffer.clear(this.selected);
    this.post({ type: 'clear', folder: this.selected });
  }

  /**
   * Reveals the tab in the bottom panel.
   * @param folder Folder tab to select; omit to keep the current one.
   */
  async show(folder?: string): Promise<void> {
    if (folder !== undefined && this.folders.includes(folder)) {
      this.selected = folder;
      this.post({ type: 'select', folder });
    }
    await vscode.commands.executeCommand(`${LogView.viewId}.focus`);
  }

  /** Unregisters the view provider. */
  dispose(): void {
    this.registration.dispose();
  }

  /** Sends a message to the page if it is open. */
  private post(message: object): void {
    void this.view?.webview.postMessage(message);
  }
}

/** Random token allowing only our own inline style and script under the page's CSP. */
function nonce(): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  return Array.from({ length: 32 }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
}

/**
 * Builds the webview page: logo, folder tabs, log container and the script
 * that renders messages from `LogView` and keeps the view scrolled to the bottom.
 * @param nonce CSP nonce for the inline style and script.
 */
function html(nonce: string): string {
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'nonce-${nonce}'; script-src 'nonce-${nonce}';">
<style nonce="${nonce}">
  body {
    margin: 0;
    padding: 4px 12px;
    font-family: var(--vscode-editor-font-family);
    font-size: var(--vscode-editor-font-size);
    color: var(--vscode-foreground);
  }
  #logo {
    display: inline-block;
    margin: 8px 0 12px;
    font-family: Menlo, Consolas, 'DejaVu Sans Mono', monospace;
    font-size: 10px;
    line-height: 1;
    /* Emerald-to-cyan gradient, as in the CLI. */
    background: linear-gradient(to right, rgb(0, 201, 167), rgb(0, 183, 255));
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }
  #tabs {
    position: sticky;
    top: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 2px 12px;
    margin: 0 -12px 6px;
    padding: 2px 12px 0;
    background: var(--vscode-panel-background, var(--vscode-editor-background));
    border-bottom: 1px solid var(--vscode-panel-border, transparent);
    font-family: var(--vscode-font-family);
    font-size: var(--vscode-font-size);
  }
  #tabs[hidden] { display: none; }
  #tabs button {
    padding: 4px 0 3px;
    border: 0;
    border-bottom: 1px solid transparent;
    background: none;
    color: var(--vscode-panelTitle-inactiveForeground, var(--vscode-descriptionForeground));
    font: inherit;
    cursor: pointer;
  }
  #tabs button:hover { color: var(--vscode-panelTitle-activeForeground, var(--vscode-foreground)); }
  #tabs button[aria-selected="true"] {
    color: var(--vscode-panelTitle-activeForeground, var(--vscode-foreground));
    border-bottom-color: var(--vscode-panelTitle-activeBorder, var(--vscode-focusBorder));
  }
  #tabs button:focus-visible { outline: 1px solid var(--vscode-focusBorder); outline-offset: 1px; }
  .alert { margin-left: 4px; font-size: 0.8em; }
  #log { white-space: pre-wrap; word-break: break-all; }
  /* Same palette as the CLI's logx package, following the terminal theme. */
  .green { color: var(--vscode-terminal-ansiGreen); }
  .red { color: var(--vscode-terminal-ansiRed); }
  .yellow { color: var(--vscode-terminal-ansiYellow); }
  .cyan { color: var(--vscode-terminal-ansiCyan); }
  .blue { color: var(--vscode-terminal-ansiBlue); }
  .magenta { color: var(--vscode-terminal-ansiMagenta); }
  .dim { color: var(--vscode-terminal-ansiBrightBlack, var(--vscode-descriptionForeground)); }
  .bold { font-weight: bold; }
</style>
</head>
<body>
<pre id="logo" aria-label="Synchro">${LOGO}</pre>
<div id="tabs" role="tablist" aria-label="Workspace folders" hidden></div>
<div id="log" role="log"></div>
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi();
  const MAX = ${MAX_LINES};
  // Folder labels cycle through these; red is left out so it only ever means trouble.
  const FOLDER_COLORS = ['cyan', 'magenta', 'blue', 'yellow', 'green'];
  const tabs = document.getElementById('tabs');
  const log = document.getElementById('log');

  /** Lines per folder ('' for lines without one), capped like LogBuffer. */
  let lines = new Map();
  let folders = [];
  /** Selected folder, or undefined for "All". */
  let selected;
  /** Folder → 'error' | 'warn' for hidden tabs that received one. */
  const alerts = new Map();

  const key = (line) => line.folder ?? '';
  const visible = (line) => selected === undefined || line.folder === selected;
  const folderColor = (folder) => FOLDER_COLORS[Math.max(folders.indexOf(folder), 0) % FOLDER_COLORS.length];
  const atBottom = () => window.innerHeight + window.scrollY >= document.body.scrollHeight - 4;
  const toBottom = () => window.scrollTo(0, document.body.scrollHeight);
  const span = (text, className) => {
    const el = document.createElement('span');
    el.textContent = text;
    if (className) el.className = className;
    return el;
  };

  const store = (line) => {
    const list = lines.get(key(line)) ?? [];
    list.push(line);
    if (list.length > MAX) list.splice(0, list.length - MAX);
    lines.set(key(line), list);
  };

  // Mirrors the CLI line: dim time, coloured icon, dim tag, then the text.
  // The "All" tab prefixes each line with its folder in the folder's colour.
  const render = ({ folder, line }) => {
    const div = document.createElement('div');
    if (selected === undefined && folder !== undefined) {
      div.append(span('[' + folder + '] ', folderColor(folder)));
    }
    if (typeof line === 'string') {
      div.append(span(line, 'dim'));
      return div;
    }
    div.append(span(line.time, 'dim'), ' ', span(line.icon, line.color), '  ');
    if (line.tag) div.append(span(line.tag, 'dim'), ' ');
    div.append(span(line.text, line.bold ? 'bold' : ''));
    return div;
  };

  const renderLog = () => {
    const shown = selected === undefined ? [...lines.values()].flat().sort((a, b) => a.seq - b.seq) : lines.get(selected) ?? [];
    log.replaceChildren(...shown.slice(-MAX).map(render));
    toBottom();
  };

  const tab = (label, folder) => {
    const button = document.createElement('button');
    button.setAttribute('role', 'tab');
    button.setAttribute('aria-selected', String(folder === selected));
    button.append(folder === undefined ? label : span(label, folderColor(folder)));
    const alert = folder !== undefined && alerts.get(folder);
    if (alert) {
      const dot = span('●', 'alert ' + (alert === 'error' ? 'red' : 'yellow'));
      dot.title = alert === 'error' ? 'New errors' : 'New warnings';
      button.append(dot);
    }
    button.addEventListener('click', () => select(folder, true));
    return button;
  };

  const renderTabs = () => {
    tabs.hidden = folders.length < 2;
    tabs.replaceChildren(tab('All'), ...folders.map((f) => tab(f, f)));
  };

  /** Shows a folder's tab (undefined for "All"); report tells LogView about a click. */
  const select = (folder, report) => {
    selected = folder !== undefined && folders.includes(folder) ? folder : undefined;
    if (selected === undefined) alerts.clear();
    else alerts.delete(selected);
    if (report) vscode.postMessage({ type: 'select', folder: selected ?? null });
    renderTabs();
    renderLog();
  };

  const append = (line) => {
    store(line);
    if (visible(line)) {
      const stick = atBottom();
      log.appendChild(render(line));
      while (log.childElementCount > MAX) log.firstChild.remove();
      if (stick) toBottom();
      return;
    }
    const level = typeof line.line === 'string' ? undefined : line.line.level;
    if ((level === 'error' || level === 'warn') && alerts.get(line.folder) !== 'error') {
      alerts.set(line.folder, level);
      renderTabs();
    }
  };

  window.addEventListener('message', ({ data }) => {
    switch (data.type) {
      case 'reset':
        lines = new Map();
        data.lines.forEach(store);
        folders = data.folders;
        select(data.selected, false);
        break;
      case 'append':
        append(data.line);
        break;
      case 'folders':
        folders = data.folders;
        for (const folder of alerts.keys()) if (!folders.includes(folder)) alerts.delete(folder);
        select(data.selected, false);
        break;
      case 'select':
        select(data.folder, false);
        break;
      case 'clear':
        if (data.folder === undefined) lines.clear();
        else lines.delete(data.folder);
        renderLog();
        break;
    }
  });
  vscode.postMessage({ type: 'ready' });
</script>
</body>
</html>`;
}
