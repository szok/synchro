import { Entry } from './events';

/** One line of the log view. */
export interface LogLine {
  /** Order of arrival across all folders; merges folders in the "All" tab. */
  seq: number;
  /** Workspace folder the line belongs to; unset in a single-folder workspace. */
  folder?: string;
  /** Raw process output, shown dimmed, or an event entry, coloured like the CLI. */
  line: string | Entry;
}

/** Log lines kept per folder, each folder capped separately so a busy one cannot push out a quiet one. */
export class LogBuffer {
  private readonly folders = new Map<string | undefined, LogLine[]>();
  private seq = 0;

  /** @param max Lines kept per folder. */
  constructor(private readonly max: number) {}

  /**
   * Adds a line, dropping the folder's oldest line beyond the cap.
   * @returns The stored line.
   */
  push(line: string | Entry, folder?: string): LogLine {
    const stored: LogLine = { seq: this.seq++, line, ...(folder !== undefined && { folder }) };
    let lines = this.folders.get(folder);
    if (!lines) {
      lines = [];
      this.folders.set(folder, lines);
    }
    lines.push(stored);
    if (lines.length > this.max) {
      lines.splice(0, lines.length - this.max);
    }
    return stored;
  }

  /** Every buffered line in arrival order. */
  all(): LogLine[] {
    return [...this.folders.values()].flat().sort((a, b) => a.seq - b.seq);
  }

  /**
   * Empties one folder, or everything.
   * @param folder Folder to empty; omit to empty all folders.
   */
  clear(folder?: string): void {
    if (folder === undefined) {
      this.folders.clear();
    } else {
      this.folders.delete(folder);
    }
  }
}
