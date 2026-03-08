package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"strings"
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

//go:embed help.txt
var longHelp string

func main() {
	rootCmd := &cobra.Command{
		Use:     "trenchcoat",
		Short:   "Extensible mock, and proxy-to-mock, HTTP server",
		Long:    strings.TrimSpace(longHelp),
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
