// Allow proxying to upstream servers whose TLS certificates have negative
// serial numbers. Go 1.23+ rejects these by default, but they exist in the
// wild (e.g. legacy CAs, self-signed certs from older tooling). See
// https://go.dev/doc/godebug#x509negativeserial.
//
//go:debug x509negativeserial=1

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/yesdevnull/trenchcoat/internal/config"
)

// Version information set at build time via ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "trenchcoat",
		Short:   "Extensible mock, and proxy-to-mock, HTTP server",
		Long:    "Trenchcoat is a CLI tool that serves mock HTTP responses based on configurable request/response definitions called coats.",
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfgFile, _ := cmd.Flags().GetString("config")
			return config.Load(cfgFile)
		},
	}

	rootCmd.PersistentFlags().String("config", "", "Path to configuration file")

	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newProxyCmd())

	// Set up signal-based context so serve/proxy commands shut down on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
