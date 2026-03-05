package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yesdevnull/trenchcoat/internal/proxy"
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
	cmd.Flags().Bool("no-headers", false, "Omit all headers from captured coat files (mutually exclusive with --strip-headers)")
	cmd.Flags().String("dedupe", "overwrite", "Deduplication strategy: overwrite, skip, or append")
	cmd.Flags().Bool("capture-body", true, "Capture request body in coat files for any request with a body")
	cmd.Flags().Bool("verbose", false, "Log each proxied request and capture event")
	cmd.Flags().String("log-format", "text", "Log output format: text or json")
	cmd.Flags().Bool("allow-negative-serial", false, "Allow TLS certificates with negative serial numbers (legacy CAs, older tooling)")

	return cmd
}

func runProxy(cmd *cobra.Command, args []string) error {
	upstreamURL := args[0]
	port, _ := cmd.Flags().GetInt("port")
	writeDir, _ := cmd.Flags().GetString("write-dir")
	filter, _ := cmd.Flags().GetString("filter")
	stripHeaders, _ := cmd.Flags().GetStringSlice("strip-headers")
	noHeaders, _ := cmd.Flags().GetBool("no-headers")
	dedupe, _ := cmd.Flags().GetString("dedupe")

	// --no-headers and --strip-headers are mutually exclusive.
	if noHeaders && cmd.Flags().Changed("strip-headers") {
		return fmt.Errorf("--no-headers and --strip-headers are mutually exclusive")
	}
	// When --no-headers is set, clear strip-headers so they don't leak into Config.
	if noHeaders {
		stripHeaders = nil
	}
	captureBody, _ := cmd.Flags().GetBool("capture-body")
	verbose, _ := cmd.Flags().GetBool("verbose")
	logFormat, _ := cmd.Flags().GetString("log-format")

	allowNegativeSerial, _ := cmd.Flags().GetBool("allow-negative-serial")

	logger := newLogger(logFormat)

	// Allow TLS certificates with negative serial numbers when opted in.
	// Go 1.23+ rejects these by default. See https://go.dev/doc/godebug#x509negativeserial.
	if allowNegativeSerial {
		if err := os.Setenv("GODEBUG", setGODEBUG(os.Getenv("GODEBUG"), "x509negativeserial=1")); err != nil {
			logger.Warn("failed to set GODEBUG for x509negativeserial", "error", err)
		}
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
		WriteDir:     writeDir,
		Filter:       filter,
		StripHeaders: stripHeaders,
		NoHeaders:    noHeaders,
		Dedupe:       dedupe,
		CaptureBody:  &captureBody,
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

	// Wait for context cancellation (signal-based in production, explicit in tests).
	<-cmd.Context().Done()
	logger.Info("context canceled, shutting down", "reason", cmd.Context().Err())

	if err := p.Shutdown(10 * time.Second); err != nil {
		return err
	}

	logger.Info("proxy stopped")
	return nil
}

// setGODEBUG sets or replaces a key=value pair in a GODEBUG string,
// preserving any other existing values.
func setGODEBUG(existing, kv string) string {
	key := strings.SplitN(kv, "=", 2)[0]
	if existing == "" {
		return kv
	}
	// Replace existing key if present.
	parts := strings.Split(existing, ",")
	for i, part := range parts {
		if strings.HasPrefix(part, key+"=") {
			parts[i] = kv
			return strings.Join(parts, ",")
		}
	}
	return existing + "," + kv
}
