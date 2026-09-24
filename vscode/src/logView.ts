import * as vscode from 'vscode';
import { Level } from './events';

/** Oldest lines are dropped beyond this many, both here and in the webview. */
const MAX_LINES = 5000;

// Same logotype as `logx.PrintLogo` in the CLI.
const LOGO = `███████╗██╗   ██╗███╗   ██╗ ██████╗██╗  ██╗██████╗  ██████╗
██╔════╝╚██╗ ██╔╝████╗  ██║██╔════╝██║  ██║██╔══██╗██╔═══██╗
███████╗ ╚████╔╝ ██╔██╗ ██║██║     ███████║██████╔╝██║   ██║
╚════██║  ╚██╔╝  ██║╚██╗██║██║     ██╔══██║██╔══██╗██║   ██║
███████║   ██║   ██║ ╚████║╚██████╗██║  ██║██║  ██║╚██████╔╝
╚══════╝   ╚═╝   ╚═╝  ╚═══╝ ╚═════╝╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝`;

/** One log line; lines without a level are raw process output. */
interface Line {
  text: string;
  level?: Level;
}

/** The "Synchro" tab in the bottom panel, next to Output and Terminal. */
export class LogView implements vscode.WebviewViewProvider, vscode.Disposable {
  /** View id contributed in package.json. */
  static readonly viewId = 'synchro.log';

  private readonly lines: Line[] = [];
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
    // The page posts "ready" whenever it (re)loads, e.g. after the panel was hidden.
    view.webview.onDidReceiveMessage((message) => {
      if (message?.type === 'ready') {
        void view.webview.postMessage({ type: 'reset', lines: this.lines });
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
   * @param text Line to show.
   * @param level Colours the line; omit for raw output.
   */
  append(text: string, level?: Level): void {
    const line = { text, level };
    this.lines.push(line);
    if (this.lines.length > MAX_LINES) {
      this.lines.splice(0, this.lines.length - MAX_LINES);
    }
    void this.view?.webview.postMessage({ type: 'append', line });
  }

  /** Empties the buffer and the view. */
  clear(): void {
    this.lines.length = 0;
    void this.view?.webview.postMessage({ type: 'reset', lines: [] });
  }

  /** Reveals the tab in the bottom panel. */
  async show(): Promise<void> {
    await vscode.commands.executeCommand(`${LogView.viewId}.focus`);
  }

  /** Unregisters the view provider. */
  dispose(): void {
    this.registration.dispose();
  }
}

/** Random token allowing only our own inline style and script under the page's CSP. */
function nonce(): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  return Array.from({ length: 32 }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
}

/**
 * Builds the webview page: logo, log container and the script that renders
 * `reset`/`append` messages and keeps the view scrolled to the bottom.
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
  #log { white-space: pre-wrap; word-break: break-all; }
  .success { color: var(--vscode-terminal-ansiGreen); }
  .warn { color: var(--vscode-terminal-ansiYellow); }
  .error { color: var(--vscode-terminal-ansiRed); }
  .raw { color: var(--vscode-descriptionForeground); }
</style>
</head>
<body>
<pre id="logo" aria-label="Synchro">${LOGO}</pre>
<div id="log"></div>
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi();
  const log = document.getElementById('log');
  const atBottom = () => window.innerHeight + window.scrollY >= document.body.scrollHeight - 4;
  const render = (line) => {
    const div = document.createElement('div');
    div.className = line.level ?? 'raw';
    div.textContent = line.text;
    return div;
  };
  window.addEventListener('message', ({ data }) => {
    const stick = atBottom();
    if (data.type === 'reset') {
      log.replaceChildren(...data.lines.map(render));
    } else if (data.type === 'append') {
      log.appendChild(render(data.line));
      while (log.childElementCount > ${MAX_LINES}) log.firstChild.remove();
    }
    if (stick || data.type === 'reset') window.scrollTo(0, document.body.scrollHeight);
  });
  vscode.postMessage({ type: 'ready' });
</script>
</body>
</html>`;
}
