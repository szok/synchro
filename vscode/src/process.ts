import { ChildProcess, spawn } from 'child_process';
import * as readline from 'readline';
import { parseEvent, SynchroEvent } from './events';

/** How to run synchro: binary, extra arguments, working directory and environment. */
export interface Launch {
  binary: string;
  args: string[];
  cwd: string;
  env: NodeJS.ProcessEnv;
}

/** Callbacks receiving the output of a synchro process. */
export interface Handlers {
  /** A parsed JSON event from stdout. */
  event(e: SynchroEvent): void;
  /** Output that is not a JSON event: stderr, or a spawn failure. */
  text(line: string): void;
}

// Stop escalation: closing stdin asks synchro to finish in-flight operations
// (it force-quits itself after 15s); these timers only cover a stuck process.
const TERM_AFTER_MS = 20_000;
const KILL_AFTER_MS = 25_000;

/** A long-running `synchro --json --stop-on-stdin-close ...` process. */
export class SynchroProcess {
  private readonly child: ChildProcess;
  /** Resolves with the exit code (null when killed by a signal or never started). */
  readonly exited: Promise<number | null>;

  /**
   * Spawns the process and starts forwarding its output.
   * @param launch Binary, arguments, working directory and environment.
   * @param handlers Receive events and plain text lines.
   */
  constructor(launch: Launch, handlers: Handlers) {
    this.child = spawn(launch.binary, ['--json', '--stop-on-stdin-close', ...launch.args], {
      cwd: launch.cwd,
      env: launch.env,
      stdio: ['pipe', 'pipe', 'pipe'],
      windowsHide: true,
    });
    pipeLines(this.child, handlers);
    this.exited = new Promise((resolve) => {
      this.child.once('error', (err) => {
        handlers.text(`Failed to start ${launch.binary}: ${err.message}`);
        resolve(null);
      });
      this.child.once('close', (code) => resolve(code));
    });
    // Writing to a closed stdin would otherwise throw EPIPE asynchronously.
    this.child.stdin?.on('error', () => undefined);
  }

  /** True while the process has started and not yet exited. */
  get running(): boolean {
    return this.child.exitCode === null && this.child.signalCode === null && this.child.pid !== undefined;
  }

  /** Gracefully stops the process and resolves once it has exited. */
  async stop(): Promise<void> {
    if (!this.running) {
      await this.exited;
      return;
    }
    this.child.stdin?.end();
    const term = setTimeout(() => this.child.kill('SIGTERM'), TERM_AFTER_MS);
    const kill = setTimeout(() => this.child.kill('SIGKILL'), KILL_AFTER_MS);
    try {
      await this.exited;
    } finally {
      clearTimeout(term);
      clearTimeout(kill);
    }
  }
}

/** Runs a short synchro command (`--test`, `--init`) and collects its events. */
export function runOnce(launch: Launch, handlers: Handlers): Promise<{ code: number | null; events: SynchroEvent[] }> {
  const events: SynchroEvent[] = [];
  const child = spawn(launch.binary, ['--json', ...launch.args], {
    cwd: launch.cwd,
    env: launch.env,
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
  pipeLines(child, {
    event(e) {
      events.push(e);
      handlers.event(e);
    },
    text: handlers.text,
  });
  return new Promise((resolve) => {
    child.once('error', (err) => {
      handlers.text(`Failed to start ${launch.binary}: ${err.message}`);
      resolve({ code: null, events });
    });
    child.once('close', (code) => resolve({ code, events }));
  });
}

/**
 * Splits the child's stdout and stderr into lines and forwards them: JSON events to
 * `handlers.event`, anything else (non-blank) to `handlers.text`.
 */
function pipeLines(child: ChildProcess, handlers: Handlers): void {
  if (child.stdout) {
    readline.createInterface({ input: child.stdout }).on('line', (line) => {
      const event = parseEvent(line);
      if (event) {
        handlers.event(event);
      } else if (line.trim() !== '') {
        handlers.text(line);
      }
    });
  }
  if (child.stderr) {
    readline.createInterface({ input: child.stderr }).on('line', (line) => {
      if (line.trim() !== '') {
        handlers.text(line);
      }
    });
  }
}
