// Events printed by `synchro --json`, one JSON object per line on stdout.
// The contract is documented in the repository README ("JSON output").
/** Severity of an event. */
export type Level = 'info' | 'success' | 'warn' | 'error';

/** Fields shared by every event. */
interface Base {
  time: string;
  level: Level;
}

/** Any event emitted by `synchro --json`, discriminated by `event`. */
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

/**
 * Formats an RFC 3339 timestamp as local `HH:MM:SS`.
 * @param time Timestamp from the event's `time` field.
 * @returns `--:--:--` when the timestamp cannot be parsed.
 */
function clock(time: string): string {
  const date = new Date(time);
  if (Number.isNaN(date.getTime())) {
    return '--:--:--';
  }
  return date.toTimeString().slice(0, 8);
}

/** Colour of an entry's icon; the same palette as the CLI's `logx` package. */
export type Color = 'green' | 'red' | 'yellow' | 'cyan' | 'blue' | 'magenta';

/** An event split into the parts the CLI colours separately. */
export interface Entry {
  /** Local `HH:MM:SS`. */
  time: string;
  icon: string;
  color: Color;
  /** Bracketed category such as `[upload]`, shown dimmed. */
  tag?: string;
  text: string;
  /** Emphasises the text, as the CLI does for `connected`. */
  bold?: boolean;
  /** Severity of the event; warnings and errors flag a hidden log tab. */
  level: Level;
}

/**
 * Splits an event into styled parts matching the CLI's terminal line.
 * @param e Event to describe.
 */
export function eventEntry(e: SynchroEvent): Entry {
  const entry = (icon: string, color: Color, text: string, tag?: string): Entry => ({
    time: clock(e.time),
    level: e.level,
    icon,
    color,
    text,
    ...(tag && { tag: `[${tag}]` }),
  });
  switch (e.event) {
    case 'start':
      return entry('ℹ', 'cyan', `synchro ${e.version}`);
    case 'info':
      return entry('ℹ', 'cyan', e.message);
    case 'success':
      return entry('✔', 'green', e.message);
    case 'warn':
      return entry('⚠', 'yellow', e.message);
    case 'error':
      return entry('✖', 'red', e.message);
    case 'connected':
      return { ...entry('⚡', 'green', `Connected to ${e.username}@${e.host}`, 'connect'), bold: true };
    case 'disconnected':
      return entry('✖', 'red', `Disconnected — reconnecting in ${e.retryInSeconds}s...`);
    case 'syncAllStart':
      return entry('⟳', 'magenta', `Full sync — uploading ${e.total} files with ${e.workers} workers...`, 'syncAll');
    case 'syncAllDone':
      return entry('⟳', 'magenta', `Full sync complete: ${e.uploaded}/${e.total} files uploaded.`, 'syncAll');
    case 'watching': {
      const exclude = e.exclude?.length ? `  (excluded: ${e.exclude.join(', ')})` : '';
      return entry('◉', 'cyan', `${e.local} → ${e.remote}${exclude}`, 'watch');
    }
    case 'change':
      return entry('~', 'yellow', e.path, e.change);
    case 'upload':
      return entry('↑', 'green', e.path, 'upload');
    case 'delete':
      return entry('✕', 'red', e.path, 'delete');
    case 'mkdir':
      return entry('+', 'blue', `${e.path}/`, 'mkdir');
    case 'rmdir':
      return entry('−', 'magenta', `${e.path}/`, 'rmdir');
    case 'stopping':
      return entry('ℹ', 'cyan', `Stopping (${e.reason})...`);
    case 'stopped':
      return entry('✔', 'green', 'Stopped');
  }
}

/** Renders an entry as one plain-text line, for the output channel. */
export function formatEntry(entry: Entry): string {
  const tag = entry.tag ? `${entry.tag} ` : '';
  return `${entry.time} ${entry.icon}  ${tag}${entry.text}`;
}

/** Renders an event as one human-readable log line. */
export function formatEvent(e: SynchroEvent): string {
  return formatEntry(eventEntry(e));
}
