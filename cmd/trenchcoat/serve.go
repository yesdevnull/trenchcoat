package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
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
	coatPaths, _ := cmd.Flags().GetStringSlice("coats")
	port, _ := cmd.Flags().GetInt("port")
	verbose, _ := cmd.Flags().GetBool("verbose")
	watch, _ := cmd.Flags().GetBool("watch")
	logFormat, _ := cmd.Flags().GetString("log-format")
	tlsCert, _ := cmd.Flags().GetString("tls-cert")
	tlsKey, _ := cmd.Flags().GetString("tls-key")

	logger := newLogger(logFormat)

	// Validate TLS flags.
	if (tlsCert != "" && tlsKey == "") || (tlsCert == "" && tlsKey != "") {
		return fmt.Errorf("both --tls-cert and --tls-key must be provided together")
	}

	if len(coatPaths) == 0 {
		logger.Warn("no coat paths provided — server will return 404 for all requests")
	}

	// Load coats.
	loaded, loadErrs := coat.LoadPaths(coatPaths)
	for _, e := range loadErrs {
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

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				ext := strings.ToLower(filepath.Ext(event.Name))
				if ext == ".yaml" || ext == ".yml" || ext == ".json" {
					logger.Info("coat file changed, reloading", "file", event.Name)
					loaded, loadErrs := coat.LoadPaths(coatPaths)
					for _, e := range loadErrs {
						logger.Warn("reload error", "error", e)
					}
					srv.Reload(loaded)
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

func newLogger(format string) *slog.Logger {
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, nil)
	default:
		handler = slog.NewTextHandler(os.Stderr, nil)
	}
	return slog.New(handler)
}
