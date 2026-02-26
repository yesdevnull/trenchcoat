package main

import (
	"fmt"
	"os"

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

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
