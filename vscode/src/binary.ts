import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';

/** Executable name of the synchro binary on this platform. */
const EXECUTABLE = process.platform === 'win32' ? 'synchro.exe' : 'synchro';

/**
 * Picks the synchro binary: the `synchro.binaryPath` setting, then the copy
 * bundled in the extension's bin/ directory, then `synchro` from PATH.
 */
export function resolveBinary(context: vscode.ExtensionContext): string {
  const configured = vscode.workspace.getConfiguration('synchro').get<string>('binaryPath', '').trim();
  if (configured) {
    return expandHome(configured);
  }
  const bundled = context.asAbsolutePath(path.join('bin', EXECUTABLE));
  if (fs.existsSync(bundled)) {
    ensureExecutable(bundled);
    return bundled;
  }
  return EXECUTABLE;
}

/**
 * Expands a leading `~` to the user's home directory; other paths are returned unchanged.
 * @param value Path from the `synchro.binaryPath` setting.
 */
function expandHome(value: string): string {
  if (value === '~' || value.startsWith('~/')) {
    return path.join(process.env.HOME ?? process.env.USERPROFILE ?? '', value.slice(1));
  }
  return value;
}

/**
 * Restores the executable bit on the bundled binary; VSIX packaging can drop it on macOS/Linux.
 * Failures are ignored — spawning the binary will then report the real error.
 * @param file Absolute path of the bundled binary.
 */
function ensureExecutable(file: string): void {
  if (process.platform === 'win32') {
    return;
  }
  try {
    fs.accessSync(file, fs.constants.X_OK);
  } catch {
    try {
      fs.chmodSync(file, 0o755);
    } catch {
      // The spawn error will explain the problem.
    }
  }
}
