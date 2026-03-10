package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	cmd.Flags().Bool("pretty-json", false, "Pretty-print JSON response bodies in captured coat files")
	cmd.Flags().Int("body-file-threshold", 0, "Write response bodies larger than N bytes to separate files (0 = always inline)")
	cmd.Flags().String("name-template", "", "Custom template for captured coat file names (e.g. {{.Method}}-{{.Path}}-{{.Status}})")
	cmd.Flags().Bool("verbose", false, "Log each proxied request and capture event")
	cmd.Flags().String("log-format", "text", "Log output format: text or json")

	return cmd
}

func runProxy(cmd *cobra.Command, args []string) error {
	// Bind CLI flags to viper config keys so config file values serve as defaults.
	// Flag names use hyphens, but config file keys use underscores/nesting.
	for _, b := range []struct{ key, flag string }{
		{"port", "port"},
		{"proxy.write_dir", "write-dir"},
		{"proxy.filter", "filter"},
		{"proxy.strip_headers", "strip-headers"},
		{"proxy.no_headers", "no-headers"},
		{"proxy.dedupe", "dedupe"},
		{"proxy.capture_body", "capture-body"},
		{"proxy.pretty_json", "pretty-json"},
		{"proxy.body_file_threshold", "body-file-threshold"},
		{"proxy.name_template", "name-template"},
		{"verbose", "verbose"},
		{"log_format", "log-format"},
	} {
		if err := viper.BindPFlag(b.key, cmd.Flags().Lookup(b.flag)); err != nil {
			return fmt.Errorf("binding flag %q to config key %q: %w", b.flag, b.key, err)
		}
	}

	upstreamURL := args[0]
	port := viper.GetInt("port")
	writeDir := viper.GetString("proxy.write_dir")
	filter := viper.GetString("proxy.filter")
	stripHeaders := viper.GetStringSlice("proxy.strip_headers")
	noHeaders := viper.GetBool("proxy.no_headers")
	dedupe := viper.GetString("proxy.dedupe")

	// --no-headers and --strip-headers are mutually exclusive.
	// Check both CLI flag changes and viper config-file settings.
	if noHeaders && (cmd.Flags().Changed("strip-headers") || viper.IsSet("proxy.strip_headers")) {
		return fmt.Errorf("--no-headers and --strip-headers are mutually exclusive")
	}
	// When --no-headers is set, clear strip-headers so they don't leak into Config.
	if noHeaders {
		stripHeaders = nil
	}
	captureBody := viper.GetBool("proxy.capture_body")
	prettyJSON := viper.GetBool("proxy.pretty_json")
	bodyFileThreshold := viper.GetInt("proxy.body_file_threshold")
	nameTemplate := viper.GetString("proxy.name_template")
	verbose := viper.GetBool("verbose")
	logFormat := viper.GetString("log_format")

	logger, err := newLogger(logFormat)
	if err != nil {
		return err
	}

	// Validate port range.
	if port < 0 || port > 65535 {
		return fmt.Errorf("invalid port %d: must be between 0 and 65535", port)
	}

	// Validate dedupe value.
	switch dedupe {
	case "overwrite", "skip", "append":
		// Valid.
	default:
		return fmt.Errorf("invalid --dedupe value %q: must be overwrite, skip, or append", dedupe)
	}

	p, err := proxy.New(proxy.Config{
		UpstreamURL:       upstreamURL,
		WriteDir:          writeDir,
		Filter:            filter,
		StripHeaders:      stripHeaders,
		NoHeaders:         noHeaders,
		Dedupe:            dedupe,
		CaptureBody:       &captureBody,
		PrettyJSON:        prettyJSON,
		BodyFileThreshold: bodyFileThreshold,
		NameTemplate:      nameTemplate,
		Verbose:           verbose,
		Logger:            logger,
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
