// Synchro watches a local directory and synchronizes changes to an SFTP server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/szok/synchro/internal/config"
	"github.com/szok/synchro/internal/logx"
	"github.com/szok/synchro/internal/paths"
	"github.com/szok/synchro/internal/sftpclient"
	"github.com/szok/synchro/internal/syncer"
	"github.com/szok/synchro/internal/version"
	"github.com/szok/synchro/internal/watcher"
)

// shutdownTimeout bounds how long in-flight operations may run after a stop request.
const shutdownTimeout = 15 * time.Second

type options struct {
	init, test, sync, syncAll, noLogo, help, quiet, version, json, stopOnStdinClose bool
	config                                                                          string
	upload                                                                          []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, out, errOut io.Writer) int {
	options, err := parseFlags(args, errOut)
	if err != nil {
		return 2
	}
	if options.version {
		fmt.Fprintln(out, version.Version)
		return 0
	}
	if options.help {
		printHelp(out)
		return 0
	}
	if !options.noLogo && !options.json {
		logx.PrintLogo(out)
	}
	log := logx.New(out, errOut, options.quiet, options.json)
	log.Header(version.Version)
	if options.init {
		if err := config.WriteExample(config.DefaultFile); err != nil {
			if errors.Is(err, os.ErrExist) {
				log.Error(fmt.Sprintf("File %s already exists. Remove it first.", config.DefaultFile))
			} else {
				log.Error(fmt.Sprintf("Create config: %v", err))
			}
			return 1
		}
		absolute, _ := filepath.Abs(config.DefaultFile)
		log.Success("Config created: " + absolute)
		log.Info("Edit the file above, then run: synchro --syncAll")
		return 0
	}
	if !options.test && !options.sync && !options.syncAll && len(options.upload) == 0 {
		printHelp(out)
		return 0
	}
	cfg, err := config.Load(options.config)
	if err != nil {
		log.Error(err.Error())
		log.Error("Run with --init to generate an example config.")
		return 1
	}
	if options.test {
		if err := sftpclient.TestConnection(cfg); err != nil {
			log.Error(fmt.Sprintf("SSH connection failed: %v", err))
			return 1
		}
		log.Success(fmt.Sprintf("SSH connection successful: %s@%s:%d", cfg.Username, cfg.Host, cfg.Port))
		return 0
	}
	if info, err := os.Stat(cfg.Directory); err != nil || !info.IsDir() {
		log.Error(fmt.Sprintf("Local directory is not accessible: %s", cfg.Directory))
		return 1
	}
	filter, err := loadFilter(cfg, log)
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	if len(options.upload) > 0 {
		return uploadOnce(cfg, filter, options.upload, log)
	}
	var stdinClosed io.Reader
	if options.stopOnStdinClose {
		stdinClosed = stdin
	}
	ctx, stop := watchShutdown(log, stdinClosed)
	defer stop()
	// The connection outlives ctx so operations still running when a stop is
	// requested can finish; it is closed once the work below has drained.
	connectionCtx, closeConnection := context.WithCancel(context.Background())
	defer closeConnection()
	connection := sftpclient.New(cfg, log)
	connection.Start(connectionCtx)
	defer connection.Close()
	select {
	case <-connection.Ready():
	case <-ctx.Done():
		log.Stopped()
		return 0
	}
	s, err := syncer.New(cfg, filter, connection.Run, log)
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	if options.syncAll {
		s.SyncAll(ctx)
	}
	if ctx.Err() == nil {
		log.Watching(cfg.Directory, cfg.RemoteDirectory, cfg.Exclude)
		if err := watcher.Watch(ctx, cfg.Directory, watcher.Options{Filter: filter, NewDirectoryLimit: cfg.MaxNewDirectoryFiles}, s, log); err != nil {
			log.Error(fmt.Sprintf("Watcher failed: %v", err))
			return 1
		}
	}
	connection.Close()
	closeConnection()
	log.Stopped()
	return 0
}

// loadFilter builds the exclusion filter: the config's patterns plus, with
// useGitignore, the .gitignore in the synced directory.
func loadFilter(cfg config.Config, log *logx.Logger) (*paths.Filter, error) {
	root, err := cfg.LocalDirectory()
	if err != nil {
		return nil, err
	}
	var gitignore *paths.Gitignore
	if cfg.UseGitignore {
		file := filepath.Join(root, ".gitignore")
		if gitignore, err = paths.LoadGitignore(file); err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		if gitignore != nil {
			log.Info("Also excluding paths ignored by " + file)
		}
	}
	return paths.NewFilter(root, cfg.Exclude, gitignore), nil
}

// uploadOnce uploads the given files and directories over a single connection
// without retrying, then exits. It fails when any of them was not uploaded.
func uploadOnce(cfg config.Config, filter *paths.Filter, targets []string, log *logx.Logger) int {
	root, err := cfg.LocalDirectory()
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	files, err := syncer.ResolveTargets(root, filter, targets)
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	if len(files) == 0 {
		log.Warn("Nothing to upload")
		return 0
	}
	client, closeClient, err := sftpclient.Dial(cfg)
	if err != nil {
		log.Error(fmt.Sprintf("SSH connection failed: %v", err))
		return 1
	}
	defer closeClient()
	s, err := syncer.New(cfg, filter, func(operation sftpclient.Operation) {
		if err := operation(client); err != nil {
			log.Error(fmt.Sprintf("SFTP operation error: %v", err))
		}
	}, log)
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	ctx, stop := watchShutdown(log, nil)
	defer stop()
	uploaded := s.UploadFiles(ctx, files)
	destination := fmt.Sprintf("%s@%s", cfg.Username, cfg.Host)
	if uploaded < len(files) {
		log.Error(fmt.Sprintf("Uploaded %d of %d files to %s", uploaded, len(files), destination))
		return 1
	}
	if len(files) == 1 {
		rel, _ := filepath.Rel(root, files[0])
		log.Success(fmt.Sprintf("Uploaded %s to %s", filepath.ToSlash(rel), destination))
	} else {
		log.Success(fmt.Sprintf("Uploaded %d files to %s", len(files), destination))
	}
	return 0
}

// watchShutdown returns a context cancelled by the first SIGINT/SIGTERM, or by
// EOF on stdinClosed when it is non-nil (Windows has no SIGTERM, so editor
// integrations stop Synchro by closing its stdin). After that, a second signal
// or shutdownTimeout without returning exits the process immediately.
func watchShutdown(log *logx.Logger, stdinClosed io.Reader) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	eof := make(chan struct{})
	if stdinClosed != nil {
		go func() {
			_, _ = io.Copy(io.Discard, stdinClosed)
			close(eof)
		}()
	}
	go func() {
		select {
		case received := <-signals:
			log.Stopping(received.String())
		case <-eof:
			log.Stopping("stdin closed")
		case <-ctx.Done():
			return
		}
		cancel()
		select {
		case <-signals:
			log.Error("Forced quit")
			os.Exit(130)
		case <-time.After(shutdownTimeout):
			log.Error(fmt.Sprintf("Shutdown did not finish within %s, forcing quit", shutdownTimeout))
			os.Exit(1)
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		cancel()
	}
}

func parseFlags(args []string, errOut io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("synchro", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.BoolVar(&o.init, "init", false, "Generate a sample .synchro.json config in the current directory")
	fs.BoolVar(&o.test, "test", false, "Test the SSH/SFTP connection and exit")
	fs.BoolVar(&o.sync, "sync", false, "Connect and watch for changes")
	fs.BoolVar(&o.syncAll, "syncAll", false, "Full sync then watch for changes")
	fs.Func("upload", "Upload a file or directory once and exit (repeatable)", func(path string) error {
		o.upload = append(o.upload, path)
		return nil
	})
	fs.StringVar(&o.config, "config", config.DefaultFile, "Use a custom config file")
	fs.BoolVar(&o.noLogo, "no-logo", false, "Do not display logo")
	fs.BoolVar(&o.noLogo, "hide-logo", false, "Do not display logo (alias for --no-logo)")
	fs.BoolVar(&o.help, "help", false, "Display help")
	fs.BoolVar(&o.help, "h", false, "Display help")
	fs.BoolVar(&o.quiet, "quiet", false, "Suppress per-file upload/delete/mkdir/rmdir logs")
	fs.BoolVar(&o.quiet, "q", false, "Suppress per-file upload/delete/mkdir/rmdir logs (alias for --quiet)")
	fs.BoolVar(&o.version, "version", false, "Print the Synchro version and exit")
	fs.BoolVar(&o.version, "v", false, "Print the Synchro version and exit (alias for --version)")
	fs.BoolVar(&o.json, "json", false, "Print one JSON event per line on stdout instead of colored logs")
	fs.BoolVar(&o.stopOnStdinClose, "stop-on-stdin-close", false, "Stop gracefully when stdin is closed (for editor integrations)")

	if err := fs.Parse(args); err != nil {
		return o, err
	}

	return o, nil
}

func printHelp(out io.Writer) {
	fmt.Fprint(out, `  Usage:  synchro <option>

  --init           Generate a sample .synchro.json config in the current directory
  --test           Test the SSH/SFTP connection and exit
  --sync           Connect and watch for changes, uploading them as they happen
  --syncAll        Do a full upload of all files first, then watch for changes
  --upload <path>  Upload a file or directory once and exit (repeatable)
  --config <path>  Use a custom config file instead of .synchro.json
  --no-logo        Do not display the SYNCHRO ASCII art logo
  --quiet, -q      Suppress per-file upload/delete/mkdir/rmdir logs
  --json           Print one JSON event per line on stdout instead of colored logs
  --stop-on-stdin-close
                   Stop gracefully when stdin is closed (for editor integrations)
  --version, -v    Print the Synchro version and exit
  --help, -h       Display this help message

  by Piotr Jarolewski
  Licensed under GPL-3.0; comes with ABSOLUTELY NO WARRANTY. See LICENSE and NOTICE.

`)
}
