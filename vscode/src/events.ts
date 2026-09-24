// Events printed by `synchro --json`, one JSON object per line on stdout.
// The contract is documented in the repository README ("JSON output").

export type Level = 'info' | 'success' | 'warn' | 'error';

interface Base {
  time: string;
  level: Level;
}

export type SynchroEvent = Base &
  (
    | { event: 'start'; version: string }
    | { event: 'info' | 'success' | 'warn' | 'error'; message: string }
    | { event: 'connected'; username: string; host: string }
    | { event: 'disconnected'; retryInSeconds: number }
    | { event: 'syncAllStart'; total: number; workers: number }
    | { event: 'syncAllDone'; uploaded: number; total: number }
    | { event: 'watching'; local: string; remote: string; exclude?: string[] | null }
    | { event: 'change'; change: string; path: string }
    | { event: 'upload' | 'delete' | 'mkdir' | 'rmdir'; path: string }
    | { event: 'stopping'; reason: string }
    | { event: 'stopped' }
  );

/** Parses one stdout line; returns undefined for anything that is not an event. */
export function parseEvent(line: string): SynchroEvent | undefined {
  const trimmed = line.trim();
  if (!trimmed.startsWith('{')) {
    return undefined;
  }
  try {
    const value = JSON.parse(trimmed);
    if (value && typeof value.event === 'string' && typeof value.level === 'string') {
      return value as SynchroEvent;
    }
  } catch {
    // Not JSON: fall through.
  }
  return undefined;
}

function clock(time: string): string {
  const date = new Date(time);
  if (Number.isNaN(date.getTime())) {
    return '--:--:--';
  }
  return date.toTimeString().slice(0, 8);
}

/** Renders an event as one human-readable log line for the output channel. */
export function formatEvent(e: SynchroEvent): string {
  return `${clock(e.time)} ${describe(e)}`;
}

function describe(e: SynchroEvent): string {
  switch (e.event) {
    case 'start':
      return `synchro ${e.version}`;
    case 'info':
      return `ℹ ${e.message}`;
    case 'success':
      return `✔ ${e.message}`;
    case 'warn':
      return `⚠ ${e.message}`;
    case 'error':
      return `✖ ${e.message}`;
    case 'connected':
      return `⚡ Connected to ${e.username}@${e.host}`;
    case 'disconnected':
      return `✖ Disconnected — reconnecting in ${e.retryInSeconds}s...`;
    case 'syncAllStart':
      return `⟳ Full sync — uploading ${e.total} files with ${e.workers} workers...`;
    case 'syncAllDone':
      return `⟳ Full sync complete: ${e.uploaded}/${e.total} files uploaded.`;
    case 'watching': {
      const exclude = e.exclude?.length ? `  (excluded: ${e.exclude.join(', ')})` : '';
      return `◉ Watching ${e.local} → ${e.remote}${exclude}`;
    }
    case 'change':
      return `~ [${e.change}] ${e.path}`;
    case 'upload':
      return `↑ ${e.path}`;
    case 'delete':
      return `✕ ${e.path}`;
    case 'mkdir':
      return `+ ${e.path}/`;
    case 'rmdir':
      return `− ${e.path}/`;
    case 'stopping':
      return `ℹ Stopping (${e.reason})...`;
    case 'stopped':
      return '✔ Stopped';
  }
}
