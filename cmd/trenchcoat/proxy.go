package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yesdevnull/genai-experiments/trenchcoat/internal/proxy"
)

func newProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "proxy <upstream-url>",
		Short:         "Start in proxy capture mode",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runProxy,
	}

	cmd.Flags().Int("port", 8080, "Port to listen on")
	cmd.Flags().String("write-dir", ".", "Directory to write captured coat files to")
	cmd.Flags().String("filter", "", "Only capture requests whose URI matches this glob pattern")
	cmd.Flags().StringSlice("strip-headers", []string{"Authorization", "Cookie", "Set-Cookie"}, "Headers to redact from captured coat files")
	cmd.Flags().String("dedupe", "overwrite", "Deduplication strategy: overwrite, skip, or append")
	cmd.Flags().String("tls-cert", "", "Path to TLS certificate file (PEM)")
	cmd.Flags().String("tls-key", "", "Path to TLS private key file (PEM)")
	cmd.Flags().String("tls-ca", "", "Path to CA certificate chain file (PEM)")
	cmd.Flags().Bool("verbose", false, "Log each proxied request and capture event")
	cmd.Flags().String("log-format", "text", "Log output format: text or json")

	return cmd
}

func runProxy(cmd *cobra.Command, args []string) error {
	upstreamURL := args[0]
	port, _ := cmd.Flags().GetInt("port")
	writeDir, _ := cmd.Flags().GetString("write-dir")
	filter, _ := cmd.Flags().GetString("filter")
	stripHeaders, _ := cmd.Flags().GetStringSlice("strip-headers")
	dedupe, _ := cmd.Flags().GetString("dedupe")
	verbose, _ := cmd.Flags().GetBool("verbose")
	logFormat, _ := cmd.Flags().GetString("log-format")
	tlsCert, _ := cmd.Flags().GetString("tls-cert")
	tlsKey, _ := cmd.Flags().GetString("tls-key")

	logger := newLogger(logFormat)

	// Validate TLS flags.
	if (tlsCert != "" && tlsKey == "") || (tlsCert == "" && tlsKey != "") {
		return fmt.Errorf("both --tls-cert and --tls-key must be provided together")
	}

	// Validate dedupe value.
	switch dedupe {
	case "overwrite", "skip", "append":
		// Valid.
	default:
		return fmt.Errorf("invalid --dedupe value %q: must be overwrite, skip, or append", dedupe)
	}

	p, err := proxy.New(proxy.Config{
		UpstreamURL:  upstreamURL,
		Port:         port,
		WriteDir:     writeDir,
		Filter:       filter,
		StripHeaders: stripHeaders,
		Dedupe:       dedupe,
		Verbose:      verbose,
		Logger:       logger,
	})
	if err != nil {
		return err
	}

	addr := fmt.Sprintf(":%d", port)
	listenAddr, err := p.Start(addr)
	if err != nil {
		return err
	}

	logger.Info("proxy started",
		"address", listenAddr,
		"upstream", upstreamURL,
		"write_dir", writeDir,
		"filter", filter,
		"dedupe", dedupe,
	)

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig)

	if err := p.Shutdown(10 * time.Second); err != nil {
		return err
	}

	logger.Info("proxy stopped")
	return nil
}
