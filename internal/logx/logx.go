// Package logx contains Synchro's terminal presentation.
package logx

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	reset   = "\x1b[0m"
	dim     = "\x1b[2m"
	bold    = "\x1b[1m"
	green   = "\x1b[32m"
	red     = "\x1b[31m"
	yellow  = "\x1b[33m"
	cyan    = "\x1b[36m"
	blue    = "\x1b[34m"
	magenta = "\x1b[35m"
	gray    = "\x1b[90m"
)

// Logger writes compact, timestamped terminal messages.
// Quiet suppresses the per-file Upload/Delete/Mkdir/Rmdir/Change lines.
// JSON switches to one JSON object per line on Out (see emit), for editor integrations.
type Logger struct {
	Out, Err io.Writer
	Quiet    bool
	JSON     bool
	mu       sync.Mutex
}

func New(out, errOut io.Writer, quiet, jsonMode bool) *Logger {
	return &Logger{Out: out, Err: errOut, Quiet: quiet, JSON: jsonMode}
}
func stamp() string { return gray + time.Now().Format("15:04:05") + reset }
func (l *Logger) line(w io.Writer, icon, color, label, message string) {
	fmt.Fprintf(w, "%s %s%s%s  %s%s%s %s\n", stamp(), color, icon, reset, gray, label, reset, message)
}

// emit writes one JSON event: {"time":…,"level":…,"event":…} plus fields.
// Every event goes to Out so a consumer reads a single stream.
func (l *Logger) emit(level, event string, fields map[string]any) {
	record := map[string]any{"time": time.Now().Format(time.RFC3339Nano), "level": level, "event": event}
	for key, value := range fields {
		record[key] = value
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Out.Write(append(encoded, '\n'))
}
func (l *Logger) message(level, icon, color string, w io.Writer, s string) {
	if l.JSON {
		l.emit(level, level, map[string]any{"message": s})
		return
	}
	l.line(w, icon, color, "", s)
}

// file reports a per-file operation; display is the terminal form of path.
func (l *Logger) file(event, icon, color, path, display string) {
	if l.Quiet {
		return
	}
	if l.JSON {
		l.emit("info", event, map[string]any{"path": path})
		return
	}
	l.line(l.Out, icon, color, "["+event+"]", display)
}

func (l *Logger) Info(s string)    { l.message("info", "ℹ", cyan, l.Out, s) }
func (l *Logger) Success(s string) { l.message("success", "✔", green, l.Out, s) }
func (l *Logger) Warn(s string)    { l.message("warn", "⚠", yellow, l.Err, s) }
func (l *Logger) Error(s string)   { l.message("error", "✖", red, l.Err, s) }
func (l *Logger) Upload(s string)  { l.file("upload", "↑", green, s, s) }
func (l *Logger) Delete(s string)  { l.file("delete", "✕", red, s, s) }
func (l *Logger) Mkdir(s string)   { l.file("mkdir", "+", blue, s, s+"/") }
func (l *Logger) Rmdir(s string)   { l.file("rmdir", "−", magenta, s, s+"/") }
func (l *Logger) Change(event, s string) {
	if l.Quiet {
		return
	}
	if l.JSON {
		l.emit("info", "change", map[string]any{"change": event, "path": s})
		return
	}
	l.line(l.Out, "~", yellow, "["+event+"]", s)
}

// Header announces the running version.
func (l *Logger) Header(version string) {
	if l.JSON {
		l.emit("info", "start", map[string]any{"version": version})
		return
	}
	fmt.Fprintf(l.Out, "  synchro %s — sync local files to a remote server over SFTP\n\n", version)
}

// Connected reports an established SFTP session.
func (l *Logger) Connected(user, host string) {
	if l.JSON {
		l.emit("success", "connected", map[string]any{"username": user, "host": host})
		return
	}
	l.line(l.Out, "⚡", green, "[connect]", bold+"Connected to "+user+"@"+host+reset)
}

// Disconnected reports a dropped SFTP session that will be retried.
func (l *Logger) Disconnected(retry time.Duration) {
	if l.JSON {
		l.emit("warn", "disconnected", map[string]any{"retryInSeconds": retry.Seconds()})
		return
	}
	l.line(l.Err, "✖", red, "", fmt.Sprintf("Disconnected — reconnecting in %s...", retry))
}

// Watching reports the directories being synchronized; watching starts right after.
func (l *Logger) Watching(local, remote string, exclude []string) {
	if l.JSON {
		l.emit("info", "watching", map[string]any{"local": local, "remote": remote, "exclude": exclude})
		return
	}
	l.line(l.Out, "◉", cyan, "[watch]", "Local:    "+local)
	l.line(l.Out, "◉", cyan, "[watch]", "Remote:   "+remote)
	if len(exclude) > 0 {
		l.Info("Excluded: " + fmt.Sprint(exclude))
	}
	l.Info("Press Ctrl+C to stop")
}

// Skipped reports a directory created while watching that was not synced
// because it holds more than limit files. It is shown even in quiet mode.
func (l *Logger) Skipped(path string, limit int) {
	if l.JSON {
		l.emit("warn", "skipped", map[string]any{"path": path, "limit": limit})
		return
	}
	l.line(l.Err, "⚠", yellow, "[skipped]", fmt.Sprintf("%s/ has more than %d files and was not synced. Add it to \"exclude\", or upload it once with --upload %s", path, limit, path))
}

func (l *Logger) SyncAllStart(total, workers int) {
	if l.JSON {
		l.emit("info", "syncAllStart", map[string]any{"total": total, "workers": workers})
		return
	}
	l.line(l.Out, "⟳", magenta, "[syncAll]", fmt.Sprintf("Full sync — uploading %d files with %d workers...", total, workers))
}
func (l *Logger) SyncAllDone(uploaded, total int) {
	if l.JSON {
		l.emit("info", "syncAllDone", map[string]any{"uploaded": uploaded, "total": total})
		return
	}
	l.line(l.Out, "⟳", magenta, "[syncAll]", fmt.Sprintf("Full sync complete: %d/%d files uploaded.", uploaded, total))
}

// Stopping reports that shutdown began; in-flight operations are finishing.
func (l *Logger) Stopping(reason string) {
	if l.JSON {
		l.emit("info", "stopping", map[string]any{"reason": reason})
		return
	}
	l.Info("Stopping (" + reason + ") — finishing in-flight operations, press Ctrl+C again to force quit")
}

// Stopped reports a clean exit.
func (l *Logger) Stopped() {
	if l.JSON {
		l.emit("info", "stopped", nil)
		return
	}
	l.Success("Stopped")
}

// PrintLogo prints the SYNCHRO logotype with an emerald-to-cyan gradient.
func PrintLogo(out io.Writer) {
	const logo = `███████╗██╗   ██╗███╗   ██╗ ██████╗██╗  ██╗██████╗  ██████╗
██╔════╝╚██╗ ██╔╝████╗  ██║██╔════╝██║  ██║██╔══██╗██╔═══██╗
███████╗ ╚████╔╝ ██╔██╗ ██║██║     ███████║██████╔╝██║   ██║
╚════██║  ╚██╔╝  ██║╚██╗██║██║     ██╔══██║██╔══██╗██║   ██║
███████║   ██║   ██║ ╚████║╚██████╗██║  ██║██║  ██║╚██████╔╝
╚══════╝   ╚═╝   ╚═╝  ╚═══╝ ╚═════╝╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝`

	fmt.Fprintln(out)
	for _, line := range strings.Split(logo, "\n") {
		fmt.Fprint(out, "  ")
		chars := []rune(line)
		for i, char := range chars {
			ratio := float64(i) / float64(len(chars))
			green := int(201 - ratio*18)
			blue := int(167 + ratio*88)
			fmt.Fprintf(out, "\x1b[38;2;0;%d;%dm%c%s", green, blue, char, reset)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out)
}
