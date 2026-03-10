package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yesdevnull/trenchcoat/internal/coat"
	"github.com/yesdevnull/trenchcoat/internal/server"
)

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "serve",
		Short:         "Start the mock HTTP server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runServe,
	}

	cmd.Flags().StringSlice("coats", nil, "Paths to coat files or directories to load")
	cmd.Flags().Int("port", 8080, "Port to listen on")
	cmd.Flags().String("tls-cert", "", "Path to TLS certificate file (PEM)")
	cmd.Flags().String("tls-key", "", "Path to TLS private key file (PEM)")
	cmd.Flags().Bool("watch", false, "Watch coat files for changes and hot-reload")
	cmd.Flags().Bool("verbose", false, "Log each incoming request and match result")
	cmd.Flags().String("log-format", "text", "Log output format: text or json")

	return cmd
}

func runServe(cmd *cobra.Command, args []string) error {
	// Bind CLI flags to viper config keys so config file values serve as defaults.
	// Flag names use hyphens, but config file keys use underscores/nesting.
	for _, b := range []struct{ key, flag string }{
		{"coats", "coats"},
		{"port", "port"},
		{"verbose", "verbose"},
		{"watch", "watch"},
		{"log_format", "log-format"},
		{"tls.cert", "tls-cert"},
		{"tls.key", "tls-key"},
	} {
		if err := viper.BindPFlag(b.key, cmd.Flags().Lookup(b.flag)); err != nil {
			return fmt.Errorf("binding flag %q to config key %q: %w", b.flag, b.key, err)
		}
	}

	coatPaths := viper.GetStringSlice("coats")
	port := viper.GetInt("port")
	verbose := viper.GetBool("verbose")
	watch := viper.GetBool("watch")
	logFormat := viper.GetString("log_format")
	tlsCert := viper.GetString("tls.cert")
	tlsKey := viper.GetString("tls.key")

	logger, err := newLogger(logFormat)
	if err != nil {
		return err
	}

	// Validate port range.
	if port < 0 || port > 65535 {
		return fmt.Errorf("invalid port %d: must be between 0 and 65535", port)
	}

	// Validate TLS flags.
	if (tlsCert != "" && tlsKey == "") || (tlsCert == "" && tlsKey != "") {
		return fmt.Errorf("both --tls-cert and --tls-key must be provided together")
	}

	if len(coatPaths) == 0 {
		logger.Warn("no coat paths provided — server will return 404 for all requests")
	}

	// Load coats.
	loadResult := coat.LoadPathsWithWarnings(coatPaths)
	loaded := loadResult.Coats
	for _, w := range loadResult.Warnings {
		logger.Warn("coat validation warning", "warning", w)
	}
	for _, e := range loadResult.Errors {
		logger.Warn("coat loading error", "error", e)
	}
	logger.Info("coats loaded", "count", len(loaded))

	srv := server.New(loaded, server.Config{
		Verbose: verbose,
		Logger:  logger,
	})

	addr := fmt.Sprintf(":%d", port)
	var listenAddr string
	var startErr error
	if tlsCert != "" {
		listenAddr, startErr = srv.StartTLS(addr, tlsCert, tlsKey)
		if startErr != nil {
			return startErr
		}
		logger.Info("server started (TLS)", "address", listenAddr)
	} else {
		listenAddr, startErr = srv.Start(addr)
		if startErr != nil {
			return startErr
		}
		logger.Info("server started", "address", listenAddr)
	}

	// Set up file watching.
	ctx := cmd.Context()
	if watch {
		go watchCoats(ctx, logger, srv, coatPaths)
	}

	// Wait for context cancellation (signal-based in production, explicit in tests).
	<-ctx.Done()
	logger.Info("context canceled, shutting down", "reason", ctx.Err())

	if err := srv.Shutdown(10 * time.Second); err != nil {
		return err
	}

	logger.Info("server stopped")
	return nil
}

func watchCoats(ctx context.Context, logger *slog.Logger, srv *server.Server, coatPaths []string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error("failed to create file watcher", "error", err)
		return
	}
	defer func() { _ = watcher.Close() }()

	// Collect all directories and files to watch.
	for _, p := range coatPaths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if err := watcher.Add(p); err != nil {
				logger.Warn("failed to watch directory", "path", p, "error", err)
			}
		} else {
			if err := watcher.Add(filepath.Dir(p)); err != nil {
				logger.Warn("failed to watch directory", "path", filepath.Dir(p), "error", err)
			}
		}
	}

	logger.Info("watching coat files for changes")

	// Debounce rapid file events (editors often trigger multiple events per save).
	const debounceDelay = 100 * time.Millisecond
	var debounceTimer *time.Timer

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				if coat.IsCoatFile(event.Name) {
					changedFile := event.Name
					// Reset the debounce timer on each qualifying event.
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(debounceDelay, func() {
						logger.Info("coat file changed, reloading", "file", changedFile)
						reloadResult := coat.LoadPathsWithWarnings(coatPaths)
						for _, w := range reloadResult.Warnings {
							logger.Warn("coat validation warning", "warning", w)
						}
						for _, e := range reloadResult.Errors {
							logger.Warn("reload error", "error", e)
						}
						srv.Reload(reloadResult.Coats)
					})
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Error("watcher error", "error", err)
		}
	}
}

func newLogger(format string) (*slog.Logger, error) {
	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, nil)
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, nil)
	default:
		return nil, fmt.Errorf("invalid --log-format value %q: must be text or json", format)
	}
	return slog.New(handler), nil
}
